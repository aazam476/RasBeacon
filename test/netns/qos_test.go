package netns

// v0.3.0 test: priority devices. While another device fills the internet
// connection in both directions, a priority device's pings stay quick, and
// RasBeacon's queues put each device's packets in the right class.

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/render"
)

// busyNS is a second device on the hotspot, the one filling the connection.
const busyNS = "rb-busy"

func TestQoS_PriorityDeviceStaysQuickOnABusyConnection(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	exec.Command("ip", "netns", "del", busyNS).Run()
	run(t, "ip", "netns", "add", busyNS)
	t.Cleanup(func() { exec.Command("ip", "netns", "del", busyNS).Run() })
	run(t, "ip", "link", "add", "rbtmp-busy", "type", "veth", "peer", "name", "rbtmp-ap1")
	run(t, "ip", "link", "set", "rbtmp-busy", "netns", busyNS, "name", "eth0")
	run(t, "ip", "link", "set", "rbtmp-ap1", "netns", routerNS, "name", "ap1")
	inNS(t, routerNS, "ip", "link", "set", "ap1", "master", render.BridgeName)
	inNS(t, routerNS, "ip", "link", "set", "ap1", "up")
	for _, cmd := range [][]string{
		{"ip", "link", "set", "lo", "up"},
		{"ip", "link", "set", "eth0", "up"},
		{"ip", "address", "add", "10.87.0.98/24", "dev", "eth0"},
		{"ip", "route", "add", "default", "via", "10.87.0.1"},
	} {
		inNS(t, busyNS, cmd...)
	}

	// The phone is the priority device.
	mac := strings.Fields(inNS(t, phoneNS, "cat", "/sys/class/net/eth0/address"))[0]
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2",
		QoS: &render.QoSRule{PriorityMACs: []string{mac}, Upload: true, Download: true}})
	inNS(t, routerNS, "ip", "route", "add", "default", "via", "10.99.0.2")
	givePhoneAddress(t)

	// A 4 Mb/s connection both ways (RasBeacon sends at 3.6).
	for _, c := range render.QoS(render.QoSInput{Upload: []string{"eth0"}, UploadKbps: 4_000,
		LANs: []string{render.BridgeName}, DownloadKbps: 4_000}) {
		out, err := exec.Command("ip", append([]string{"netns", "exec", routerNS}, c.Args...)...).CombinedOutput()
		if err != nil && !c.Optional {
			t.Fatalf("%v: %v\n%s", c.Args, err, out)
		}
	}

	// The busy device sends 500 big pings a second (about 6 Mb/s each way,
	// more than the connection) for 12 seconds.
	busy := exec.Command("ip", "netns", "exec", busyNS, "ping", "-q", "-i", "0.002", "-s", "1400", "-w", "12", "10.99.0.2")
	if err := busy.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { busy.Process.Kill(); busy.Wait() })
	time.Sleep(3 * time.Second) // let the queues fill

	out, ok := tryInNS(phoneNS, 15*time.Second, "ping", "-q", "-c", "20", "-i", "0.2", "-s", "1000", "10.99.0.2")
	if !ok {
		t.Fatalf("the priority device's pings got no answer:\n%s", out)
	}
	if avg := averageRTT(t, out); avg > 60 {
		t.Errorf("the priority device's pings took %.0f ms on average; they should skip the queue:\n%s", avg, out)
	}

	// Each device's packets went into its class, in both directions.
	for _, dev := range []string{"eth0", render.IFBName} {
		stats := inNS(t, routerNS, "tc", "-s", "class", "show", "dev", dev)
		for _, class := range []string{render.QoSPriorityClass, render.QoSNormalClass} {
			if sentBytes(stats, class) == 0 {
				t.Errorf("%s: nothing went through class %s:\n%s", dev, class, stats)
			}
		}
	}
}

// averageRTT reads ping's "rtt min/avg/max/mdev = 0.1/0.2/0.3/0.1 ms".
func averageRTT(t *testing.T, out string) float64 {
	t.Helper()
	m := regexp.MustCompile(`= [\d.]+/([\d.]+)/`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no round-trip times in:\n%s", out)
	}
	avg, _ := strconv.ParseFloat(m[1], 64)
	return avg
}

// sentBytes reads how much went through an HTB class from `tc -s class
// show`: "class htb 1:10 parent 1:1 ... \n Sent 12345 bytes ...".
func sentBytes(stats, class string) int {
	m := regexp.MustCompile(`class htb ` + regexp.QuoteMeta(class) + ` [^\n]*\n\s*Sent (\d+) bytes`).FindStringSubmatch(stats)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
