package netns

// Devices on the Ethernet client port show up on the Clients page: rbd asks
// the real bridge which devices it has seen behind the port
// (`bridge -json fdb show`), matches them with dnsmasq's leases, and reads
// their data use from the firewall's counters.
//
//	[phone]  eth0 ═══ ap0  ─┐
//	                        ├ br-lan 10.87.0.1  [router]
//	[laptop] eth0 ═══ lan0 ─┘  (lan0 stands in for the Pi's Ethernet port)

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

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

	// The firewall counts the laptop's bytes both ways (its data use):
	// what rbd reads in internal/agent/readings.go, wiredCounters.
	// (udhcpc above only asked for a lease; this gives the laptop an address.)
	inNS(t, laptopNS, "ip", "address", "add", "10.87.0.98/24", "dev", "eth0")
	inNS(t, laptopNS, "ping", "-c", "3", "-i", "0.2", "-s", "1000", "-W", "2", "10.87.0.1")
	for _, set := range []string{"up", "down"} {
		out := inNS(t, routerNS, "nft", "-j", "list", "set", "bridge", "rasbeacon", set)
		counts, err := stations.ParseSetCounters(out)
		if err != nil || counts[laptopMAC] < 3000 {
			t.Errorf("%s: the laptop's bytes = %d, want at least 3 pings of 1000 bytes (%v)\n%s", set, counts[laptopMAC], err, out)
		}
		// The phone is on the hotspot: the radio counts it, not the
		// firewall.
		if _, counted := counts[phoneMAC]; counted {
			t.Errorf("%s: the phone on the hotspot is in the firewall's count", set)
		}
	}

	// A device on the hotspot sends from 1100 made-up addresses, more than
	// the sets hold (1024). None of them goes in, so a device that comes
	// on the cable afterwards is still counted.
	sendFromManyAddresses(t, phoneNS, "eth0", 1100)
	newMAC := "02:5e:00:00:00:42"
	inNS(t, laptopNS, "ip", "link", "set", "eth0", "address", newMAC)
	inNS(t, laptopNS, "ping", "-c", "3", "-i", "0.2", "-s", "1000", "-W", "2", "10.87.0.1")
	out = inNS(t, routerNS, "nft", "-j", "list", "set", "bridge", "rasbeacon", "up")
	counts, err := stations.ParseSetCounters(out)
	if err != nil || counts[newMAC] < 3000 || len(counts) > 3 {
		t.Errorf("after the made-up addresses: %d addresses counted, the new laptop's bytes = %d (%v); want the cable's devices only", len(counts), counts[newMAC], err)
	}
}

// sendFromManyAddresses sends count broadcast frames out of iface in the
// namespace ns, each from a different made-up MAC address, as a device
// trying to fill the firewall's sets would.
func sendFromManyAddresses(t *testing.T, ns, iface string, count int) {
	t.Helper()
	done := make(chan error)
	go func() {
		// This thread joins the namespace and is never handed back (no
		// UnlockOSThread): Go ends it with the goroutine, so nothing else
		// runs in the phone's namespace by mistake.
		runtime.LockOSThread()
		done <- func() error {
			handle, err := os.Open("/var/run/netns/" + ns)
			if err != nil {
				return err
			}
			defer handle.Close()
			if err := unix.Setns(int(handle.Fd()), unix.CLONE_NEWNET); err != nil {
				return err
			}
			link, err := net.InterfaceByName(iface)
			if err != nil {
				return err
			}
			const etherType = 0x88b5 // for local experiments (IEEE 802)
			// The socket takes it in network byte order.
			var wire [2]byte
			binary.BigEndian.PutUint16(wire[:], etherType)
			proto := int(binary.NativeEndian.Uint16(wire[:]))
			fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, proto)
			if err != nil {
				return err
			}
			defer unix.Close(fd)
			to := &unix.SockaddrLinklayer{Ifindex: link.Index, Protocol: uint16(proto)}
			for i := range count {
				frame := make([]byte, 60)
				copy(frame, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
				copy(frame[6:], []byte{0x02, 0xbd, 0x00, byte(i >> 16), byte(i >> 8), byte(i)})
				binary.BigEndian.PutUint16(frame[12:], etherType)
				if err := unix.Sendto(fd, frame, 0, to); err != nil {
					return fmt.Errorf("frame %d: %w", i, err)
				}
			}
			return nil
		}()
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
