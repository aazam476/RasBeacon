// Package routing lists the commands that set up the WireGuard tunnel and
// send devices' traffic into it.
//
// The functions only return commands (as argument lists); the agent runs
// them. The network namespace tests run exactly the same commands, so what
// is tested is what runs on the device.
//
// # How devices' traffic reaches the tunnel
//
// Linux picks a route by looking up "routing tables" in the order of its
// "rules". RasBeacon adds:
//
//	rule 100:  traffic arriving from the hotspot (iif br-lan) -> table 51820
//	rule 101:  traffic arriving from the guest network (iif br-guest) -> same
//	rule 102:  plugins' traffic (iif rbplug0) to your devices -> main table
//	rule 103:  the rest of plugins' traffic -> table 51820
//	rule 111:  RasBeacon's downloads for plugins (fwmark PluginMark) -> same
//	table 51820:  default route through wg0
//	              "unreachable" default route as a fallback
//
// RasBeacon's own traffic (the encrypted tunnel packets, DNS lookups for the
// VPN server's name, the clock) doesn't come from the hotspot, so it keeps
// using the normal ("main") table and leaves through the uplink.
//
// The fallback is the routing half of the kill switch: if wg0 disappears,
// its route goes with it and devices' traffic hits "unreachable" instead of
// falling through to the uplink. The firewall is the other half.
package routing

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
)

const (
	// TunnelInterface is the WireGuard interface.
	TunnelInterface = "wg0"
	// TunnelTable is the routing table for devices' traffic.
	TunnelTable = "51820"
	// firstRulePriority places RasBeacon's rules before the main table
	// (32766): 100 for the hotspot, 101 for the guest network.
	firstRulePriority = 100
	// fallbackMetric makes the "unreachable" route lose to the wg0 route.
	fallbackMetric = "4096"
)

// TunnelSettings describes the tunnel to set up.
type TunnelSettings struct {
	// ConfigFile holds the keys and the server in `wg setconf` format
	// (see render.WireGuard).
	ConfigFile string
	Addresses  []netip.Prefix
	MTU        int
	// DNS servers are reached through the tunnel (see TunnelUp).
	DNS []netip.Addr
	// IPv6 is true when the tunnel carries devices' IPv6 traffic.
	IPv6 bool
	// KernelIPv6 is false when IPv6 is switched off in the kernel. The
	// IPv6 commands are then left out (they would fail, and with no IPv6
	// at all nothing can leak over it). See KernelHasIPv6.
	KernelIPv6 bool
}

// KernelHasIPv6 reports whether the kernel has IPv6 switched on, given the
// path of /proc/sys/net/ipv6.
func KernelHasIPv6(procSysNetIPv6 string) bool {
	_, err := os.Stat(procSysNetIPv6)
	return err == nil
}

// TunnelUp returns the commands that configure wg0 and its routes. Run them
// after `ip link add wg0 type wireguard` (see CreateTunnel). They can be run
// again at any time to apply changed settings.
func TunnelUp(s TunnelSettings) [][]string {
	cmds := [][]string{
		// Keys and server.
		{"wg", "setconf", TunnelInterface, s.ConfigFile},
		// Addresses inside the tunnel.
		{"ip", "address", "flush", "dev", TunnelInterface},
	}
	for _, a := range s.Addresses {
		cmds = append(cmds, []string{"ip", "address", "add", a.String(), "dev", TunnelInterface})
	}
	cmds = append(cmds,
		[]string{"ip", "link", "set", "dev", TunnelInterface, "mtu", strconv.Itoa(s.MTU), "up"},

		// Table 51820: everything into the tunnel, or nowhere.
		[]string{"ip", "-4", "route", "replace", "default", "dev", TunnelInterface, "table", TunnelTable},
		[]string{"ip", "-4", "route", "replace", "unreachable", "default", "metric", fallbackMetric, "table", TunnelTable},
	)
	if s.KernelIPv6 {
		cmds = append(cmds, []string{"ip", "-6", "route", "replace", "unreachable", "default", "metric", fallbackMetric, "table", TunnelTable})
		if s.IPv6 {
			cmds = append(cmds, []string{"ip", "-6", "route", "replace", "default", "dev", TunnelInterface, "table", TunnelTable})
		} else {
			// The tunnel has no IPv6: devices' IPv6 traffic must not go anywhere.
			cmds = append(cmds, []string{"ip", "-6", "route", "flush", "dev", TunnelInterface, "table", TunnelTable})
		}
	}

	// The VPN's DNS servers: RasBeacon's DNS server (dnsmasq) asks them on
	// behalf of devices. dnsmasq is RasBeacon's own program, so its queries
	// use the main table; this route makes them go through the tunnel. If
	// the tunnel is down they die inside wg0 instead of leaking out.
	for _, dns := range s.DNS {
		if dns.Is6() && !s.KernelIPv6 {
			continue
		}
		cmds = append(cmds, []string{"ip", "route", "replace", hostPrefix(dns), "dev", TunnelInterface})
	}
	return cmds
}

// CreateTunnel returns the command that creates wg0. It fails harmlessly if
// wg0 already exists.
func CreateTunnel() []string {
	return []string{"ip", "link", "add", "dev", TunnelInterface, "type", "wireguard"}
}

// TunnelDown returns the commands that remove the tunnel completely (when
// the VPN is turned off). Errors from them can be ignored: they mean the
// thing was already gone.
func TunnelDown() [][]string {
	return append(ClientsDirect(),
		[]string{"ip", "link", "delete", "dev", TunnelInterface}, // also removes its routes
		[]string{"ip", "-4", "route", "flush", "table", TunnelTable},
		[]string{"ip", "-6", "route", "flush", "table", TunnelTable},
	)
}

// LANInterfaces are the networks whose devices' traffic goes into the
// tunnel: the hotspot and the guest network. The guest network's rule is
// added even while it is off (a rule for a missing interface matches
// nothing), so turning it on can never skip the tunnel.
var LANInterfaces = []string{"br-lan", "br-guest"}

// PluginInterface is the plugins' network (see internal/plugin). Plugins
// only have IPv4 addresses.
const PluginInterface = "rbplug0"

// pluginRulePriority is the first of the plugins' two rules: their replies
// to your devices use the main table's routes to the hotspot (but not its
// default route), everything else goes into the tunnel.
const pluginRulePriority = firstRulePriority + 2

// PluginMark marks RasBeacon's own connections made for plugins (image
// downloads, the catalog), so they take the tunnel too.
const PluginMark = "0x5242"

const pluginMarkPriority = "111"

// ClientsThroughTunnel returns the commands that send devices' traffic
// (arriving on LANInterfaces), plugins' traffic and RasBeacon's downloads
// for plugins to table 51820. Run ClientsDirect first so the rules are
// never added twice.
func ClientsThroughTunnel(kernelIPv6 bool) [][]string {
	var cmds [][]string
	for i, lan := range LANInterfaces {
		priority := strconv.Itoa(firstRulePriority + i)
		cmds = append(cmds, []string{"ip", "-4", "rule", "add", "priority", priority, "iif", lan, "lookup", TunnelTable})
		if kernelIPv6 {
			cmds = append(cmds, []string{"ip", "-6", "rule", "add", "priority", priority, "iif", lan, "lookup", TunnelTable})
		}
	}
	return append(cmds,
		[]string{"ip", "-4", "rule", "add", "priority", strconv.Itoa(pluginRulePriority), "iif", PluginInterface,
			"lookup", "main", "suppress_prefixlength", "0"},
		[]string{"ip", "-4", "rule", "add", "priority", strconv.Itoa(pluginRulePriority + 1), "iif", PluginInterface,
			"lookup", TunnelTable},
		[]string{"ip", "-4", "rule", "add", "priority", pluginMarkPriority, "fwmark", PluginMark, "lookup", TunnelTable},
	)
}

// ClientsDirect returns the commands that remove RasBeacon's rules, so
// devices' traffic uses the main table (the uplink). Errors mean a rule
// was not there.
func ClientsDirect() [][]string {
	var cmds [][]string
	for i := range LANInterfaces {
		priority := strconv.Itoa(firstRulePriority + i)
		cmds = append(cmds,
			[]string{"ip", "-4", "rule", "delete", "priority", priority},
			[]string{"ip", "-6", "rule", "delete", "priority", priority},
		)
	}
	return append(cmds,
		[]string{"ip", "-4", "rule", "delete", "priority", strconv.Itoa(pluginRulePriority)},
		[]string{"ip", "-4", "rule", "delete", "priority", strconv.Itoa(pluginRulePriority + 1)},
		[]string{"ip", "-4", "rule", "delete", "priority", pluginMarkPriority},
	)
}

// programRulePriority is the rule for RasBeacon's own programs whose
// traffic belongs in the tunnel (see ProgramThroughTunnel).
const programRulePriority = "110"

// ProgramThroughTunnel returns the commands that send one of RasBeacon's
// own programs' traffic, by its user ID, to table 51820, like devices'
// traffic: the DoH forwarder asks devices' questions, so they must not
// leave outside the VPN. Run ProgramDirect first.
func ProgramThroughTunnel(uid int, kernelIPv6 bool) [][]string {
	uids := fmt.Sprintf("%d-%d", uid, uid)
	cmds := [][]string{{"ip", "-4", "rule", "add", "priority", programRulePriority, "uidrange", uids, "lookup", TunnelTable}}
	if kernelIPv6 {
		cmds = append(cmds, []string{"ip", "-6", "rule", "add", "priority", programRulePriority, "uidrange", uids, "lookup", TunnelTable})
	}
	return cmds
}

// ProgramDirect removes ProgramThroughTunnel's rules. Errors mean a rule
// was not there.
func ProgramDirect() [][]string {
	return [][]string{
		{"ip", "-4", "rule", "delete", "priority", programRulePriority},
		{"ip", "-6", "rule", "delete", "priority", programRulePriority},
	}
}

// SetEndpoint returns the command that (re)sets the server's address. The
// wg tool looks up the host name when it runs, so running this again after
// the uplink changes picks up a new address.
func SetEndpoint(peerPublicKey, endpoint string) []string {
	return []string{"wg", "set", TunnelInterface, "peer", peerPublicKey, "endpoint", endpoint}
}

func hostPrefix(a netip.Addr) string {
	return fmt.Sprintf("%s/%d", a, a.BitLen())
}
