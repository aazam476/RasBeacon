package render

import (
	"fmt"
	"strconv"
)

// Priority devices ("quality of service").
//
// # The idea
//
// A queue builds up wherever a connection is slowest, usually in the hotel's
// or the ISP's router. RasBeacon can't choose what goes first there. So it
// sends slightly slower than the connection (90% of the speeds the owner
// entered): the queue then forms in RasBeacon, where priority devices' packets
// can skip it.
//
// # How
//
// The firewall (the qos chain in firewall.nft.tmpl) sorts each forwarded
// packet into a class by setting its "priority" field:
//
//	1:10  priority devices (both directions)
//	1:20  every other device's internet traffic
//	(none) RasBeacon's own traffic, and traffic between devices: not shaped
//
// An HTB qdisc ("hierarchical token bucket", a queue that sends at a set
// rate) uses that field directly. Its class 1:1 holds the whole speed; 1:10
// is guaranteed three quarters of it and is served first, 1:20 the rest, and
// each may borrow what the other doesn't use. Packets without a class bypass
// the shaping ("default 30" names a class that doesn't exist).
//
//   - Upload: the HTB sits on the interfaces devices' traffic leaves by: the
//     uplink, or the VPN tunnel while the VPN carries devices' traffic.
//   - Download: a queue only works where packets leave, and downloads leave
//     RasBeacon by the hotspot's (and guest network's) bridge. Each bridge
//     sends everything it is about to send into one virtual "ifb" device
//     (IFBName), whose HTB holds the download speed for all of them together,
//     and which passes the packets back to their bridge afterwards.

// IFBName is the virtual device downloads are queued in.
const IFBName = "rbifb0"

// The firewall's classes (see above). The firewall sets them with
// "meta priority set 1:10", which the HTB with handle 1: understands.
const (
	QoSPriorityClass = "1:10"
	QoSNormalClass   = "1:20"
)

// QoSLabel is the conntrack label (0 to 127) the firewall puts on priority
// devices' connections, so the answers coming back are known too. A label
// leaves the connection mark to port forwarding (ForwardMark).
const QoSLabel = 64

// QoSInput says where and how fast to shape. A direction without a speed
// (0) or without interfaces isn't shaped.
type QoSInput struct {
	// Upload is where devices' traffic leaves: the uplink(s), or the VPN
	// tunnel.
	Upload     []string
	UploadKbps int
	// LANs are the networks devices are on: the hotspot's bridge and the
	// guest network's.
	LANs         []string
	DownloadKbps int
}

// Active reports whether anything is shaped.
func (q QoSInput) Active() bool {
	return q.uploads() || q.downloads()
}

func (q QoSInput) uploads() bool   { return q.UploadKbps > 0 && len(q.Upload) > 0 }
func (q QoSInput) downloads() bool { return q.DownloadKbps > 0 && len(q.LANs) > 0 }

// Command is one command to run. Optional ones may fail without harm (a
// queue that wasn't there to remove, or fq_codel missing from a kernel).
type Command struct {
	Args     []string
	Optional bool
}

// ShapedKbps is the rate RasBeacon sends at for a connection of kbps: 90%,
// so the queue forms in RasBeacon.
func ShapedKbps(kbps int) int {
	return kbps * 9 / 10
}

// QoS returns the commands that set up the shaping for in. Run QoSRemove
// for what was set up before first.
func QoS(in QoSInput) []Command {
	var commands []Command
	if in.uploads() {
		for _, dev := range in.Upload {
			commands = append(commands, htb(dev, ShapedKbps(in.UploadKbps))...)
		}
	}
	if in.downloads() {
		commands = append(commands,
			Command{Args: []string{"ip", "link", "del", IFBName}, Optional: true}, // left from a crash
			Command{Args: []string{"ip", "link", "add", IFBName, "type", "ifb"}},
			Command{Args: []string{"ip", "link", "set", IFBName, "up"}},
		)
		commands = append(commands, htb(IFBName, ShapedKbps(in.DownloadKbps))...)
		for _, lan := range in.LANs {
			commands = append(commands,
				// clsact gives the bridge a place for filters on what it sends.
				// A new one each time, so the filter below is never there twice.
				Command{Args: []string{"tc", "qdisc", "del", "dev", lan, "clsact"}, Optional: true},
				Command{Args: []string{"tc", "qdisc", "add", "dev", lan, "clsact"}},
				// "match u32 0 0" matches every packet: all of it goes through
				// the ifb, which lets unclassified packets straight through.
				Command{Args: []string{"tc", "filter", "add", "dev", lan, "egress", "protocol", "all", "pref", "1",
					"u32", "match", "u32", "0", "0", "action", "mirred", "egress", "redirect", "dev", IFBName}},
			)
		}
	}
	return commands
}

// htb is the queue described at the top of this file, on dev at kbps.
func htb(dev string, kbps int) []Command {
	rate := func(k int) string { return strconv.Itoa(max(k, 1)) + "kbit" }
	all := rate(kbps)
	return []Command{
		{Args: []string{"tc", "qdisc", "replace", "dev", dev, "root", "handle", "1:", "htb", "default", "30"}},
		{Args: []string{"tc", "class", "replace", "dev", dev, "parent", "1:", "classid", "1:1", "htb", "rate", all, "ceil", all}},
		{Args: []string{"tc", "class", "replace", "dev", dev, "parent", "1:1", "classid", QoSPriorityClass, "htb",
			"rate", rate(kbps * 3 / 4), "ceil", all, "prio", "0"}},
		{Args: []string{"tc", "class", "replace", "dev", dev, "parent", "1:1", "classid", QoSNormalClass, "htb",
			"rate", rate(kbps / 4), "ceil", all, "prio", "1"}},
		// fq_codel within each class keeps one device's big download from
		// delaying another's video call in the same class. Without it (a
		// kernel without the module) each class is a plain queue.
		{Args: []string{"tc", "qdisc", "replace", "dev", dev, "parent", QoSPriorityClass, "fq_codel"}, Optional: true},
		{Args: []string{"tc", "qdisc", "replace", "dev", dev, "parent", QoSNormalClass, "fq_codel"}, Optional: true},
	}
}

// QoSRemove returns the commands that undo QoS(in). They are all optional:
// an interface may be gone already (the VPN tunnel after the VPN stopped).
func QoSRemove(in QoSInput) []Command {
	var commands []Command
	optional := func(args ...string) { commands = append(commands, Command{Args: args, Optional: true}) }
	if in.uploads() {
		for _, dev := range in.Upload {
			optional("tc", "qdisc", "del", "dev", dev, "root")
		}
	}
	if in.downloads() {
		for _, lan := range in.LANs {
			optional("tc", "qdisc", "del", "dev", lan, "clsact")
		}
		optional("ip", "link", "del", IFBName)
	}
	return commands
}

// String shows a command as it would be typed (for logs and tests).
func (c Command) String() string {
	s := fmt.Sprint(c.Args)
	if c.Optional {
		s += " (optional)"
	}
	return s
}
