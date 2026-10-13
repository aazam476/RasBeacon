package netns

// Devices on the Ethernet client port show up on the Clients page: rbd asks
// the real bridge which devices it has seen behind the port
// (`bridge -json fdb show`), and matches them with dnsmasq's leases.
//
//	[phone]  eth0 ═══ ap0  ─┐
//	                        ├ br-lan 10.87.0.1  [router]
//	[laptop] eth0 ═══ lan0 ─┘  (lan0 stands in for the Pi's Ethernet port)

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"rasbeacon/internal/render"
	"rasbeacon/internal/stations"
)

const laptopNS = "rb-laptop"

func TestWiredDevice_FoundBehindTheBridgePort(t *testing.T) {
	requireRoot(t)
	if _, err := exec.LookPath("bridge"); err != nil {
		t.Skip("the bridge tool (iproute2) is missing")
	}
	buildNetwork(t)
	exec.Command("ip", "netns", "del", laptopNS).Run()
	run(t, "ip", "netns", "add", laptopNS)
	t.Cleanup(func() { exec.Command("ip", "netns", "del", laptopNS).Run() })
	inNS(t, laptopNS, "ip", "link", "set", "lo", "up")
	run(t, "ip", "link", "add", "rbtmp-laptop", "type", "veth", "peer", "name", "rbtmp-lan0")
	run(t, "ip", "link", "set", "rbtmp-laptop", "netns", laptopNS, "name", "eth0")
	run(t, "ip", "link", "set", "rbtmp-lan0", "netns", routerNS, "name", "lan0")
	inNS(t, routerNS, "ip", "link", "set", "lan0", "master", render.BridgeName)
	inNS(t, routerNS, "ip", "link", "set", "lan0", "up")
	inNS(t, laptopNS, "ip", "link", "set", "eth0", "up")

	leaseFile := startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2"})
	laptopMAC := strings.Fields(inNS(t, laptopNS, "cat", "/sys/class/net/eth0/address"))[0]
	phoneMAC := strings.Fields(inNS(t, phoneNS, "cat", "/sys/class/net/eth0/address"))[0]

	// The laptop gets an address over the cable; the phone is on the hotspot.
	out := inNS(t, laptopNS, "busybox", "udhcpc", "-i", "eth0", "-n", "-q", "-t", "5", "-s", "/bin/true")
	if !strings.Contains(out, "lease of 10.87.0.") {
		t.Fatalf("the laptop did not get an address over the cable:\n%s", out)
	}
	givePhoneAddress(t)
	inNS(t, phoneNS, "ping", "-c", "1", "-W", "2", "10.87.0.1")

	// What rbd runs (internal/agent/status.go, wiredClients), on the port.
	fdb := inNS(t, routerNS, "bridge", "-json", "fdb", "show", "br", render.BridgeName, "brport", "lan0")
	macs, err := stations.ParseBridgeFDB(fdb)
	if err != nil {
		t.Fatalf("%v\n%s", err, fdb)
	}
	if !slices.Equal(macs, []string{laptopMAC}) {
		t.Errorf("devices behind the port = %v, want only the laptop %s\n%s", macs, laptopMAC, fdb)
	}
	// The hotspot's port lists the phone, not the laptop.
	hotspot, err := stations.ParseBridgeFDB(inNS(t, routerNS, "bridge", "-json", "fdb", "show", "br", render.BridgeName, "brport", "ap0"))
	if err != nil || !slices.Equal(hotspot, []string{phoneMAC}) {
		t.Errorf("devices behind the hotspot's port = %v, %v; want only the phone %s", hotspot, err, phoneMAC)
	}

	// With the lease, the laptop has its address on the Clients page.
	leases, err := os.ReadFile(leaseFile)
	if err != nil {
		t.Fatal(err)
	}
	wired := stations.Merge([]stations.Station{{MAC: laptopMAC}}, stations.ParseLeases(string(leases)))
	if len(wired) != 1 || !strings.HasPrefix(wired[0].IP, "10.87.0.") {
		t.Errorf("the laptop's address from the lease: %+v\nleases:\n%s", wired, leases)
	}
}
