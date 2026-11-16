package netns

// Milestone 7 tests: the guest network. Guests reach the internet, but not
// RasBeacon's settings, SSH, or the owner's devices, and the owner's
// devices can't reach them either.
//
//	[guest] eth0 ═══ guest0 ─ br-guest 10.87.1.1 ┐
//	rb-guest                                     ├ [router] ═══ [upstream]
//	[phone] eth0 ═══ ap0 ──── br-lan   10.87.0.1 ┘

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/render"
	"rasbeacon/internal/routing"
)

const guestNS = "rb-guest"

// buildGuest adds the guest machine and the router's guest bridge to the
// network from buildNetwork.
func buildGuest(t *testing.T) {
	t.Helper()
	exec.Command("ip", "netns", "del", guestNS).Run()
	run(t, "ip", "netns", "add", guestNS)
	t.Cleanup(func() { exec.Command("ip", "netns", "del", guestNS).Run() })
	inNS(t, guestNS, "ip", "link", "set", "lo", "up")

	run(t, "ip", "link", "add", "rbtmp-guest", "type", "veth", "peer", "name", "rbtmp-g0")
	run(t, "ip", "link", "set", "rbtmp-guest", "netns", guestNS, "name", "eth0")
	run(t, "ip", "link", "set", "rbtmp-g0", "netns", routerNS, "name", render.GuestInterface)

	guest := render.DefaultLAN.Guest().Prefix.String()
	inNS(t, routerNS, "ip", "link", "add", "name", render.GuestBridgeName, "type", "bridge")
	inNS(t, routerNS, "ip", "address", "add", guest, "dev", render.GuestBridgeName)
	inNS(t, routerNS, "ip", "link", "set", render.GuestInterface, "master", render.GuestBridgeName)
	for _, link := range []string{render.GuestBridgeName, render.GuestInterface} {
		inNS(t, routerNS, "ip", "link", "set", link, "up")
	}
	inNS(t, guestNS, "ip", "link", "set", "eth0", "up")
}

// startGuestNetwork builds the network with the guest network on, and
// gives the phone and the guest addresses.
func startGuestNetwork(t *testing.T) {
	t.Helper()
	buildNetwork(t)
	buildGuest(t)
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2", Guest: true})
	inNS(t, routerNS, "ip", "route", "add", "default", "via", "10.99.0.2")
	givePhoneAddress(t)
}

// giveGuestAddress configures the guest as DHCP would.
func giveGuestAddress(t *testing.T) {
	t.Helper()
	inNS(t, guestNS, "ip", "address", "add", "10.87.1.99/24", "dev", "eth0")
	inNS(t, guestNS, "ip", "route", "add", "default", "via", "10.87.1.1")
}

func TestGuest_GetsAddressAndDNS(t *testing.T) {
	requireRoot(t)
	startGuestNetwork(t)
	startBackground(t, upstreamNS, "dnsmasq", "--keep-in-foreground", "--no-resolv", "--no-hosts",
		"--listen-address=10.99.0.2", "--bind-interfaces", "--address=/example.test/10.99.0.80",
		"--pid-file=", "--user=nobody", "--log-facility=-")
	time.Sleep(300 * time.Millisecond)

	out := inNS(t, guestNS, "busybox", "udhcpc", "-i", "eth0", "-n", "-q", "-t", "5", "-s", "/bin/true")
	if !strings.Contains(out, "lease of 10.87.1.") {
		t.Fatalf("the guest did not get a 10.87.1.x address:\n%s", out)
	}
	giveGuestAddress(t)
	out = inNS(t, guestNS, "busybox", "nslookup", "-type=a", "example.test", "10.87.1.1")
	if !strings.Contains(out, "Address: 10.99.0.80") {
		t.Errorf("guests should be able to look up names:\n%s", out)
	}
}

func TestGuest_ReachesInternetButNothingElse(t *testing.T) {
	requireRoot(t)
	startGuestNetwork(t)
	giveGuestAddress(t)
	startUpstreamServer(t)
	// Servers on the owner's phone and on the guest's device.
	startBackground(t, phoneNS, "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "phone-standin")
	startBackground(t, guestNS, "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "guest-standin")
	time.Sleep(300 * time.Millisecond)

	// Control checks: everything answers when asked from inside the router.
	for _, address := range []string{"10.87.0.99", "10.87.1.99"} {
		if !tcpConnects(routerNS, address, "8080") {
			t.Fatalf("test setup broken: %s does not answer", address)
		}
	}

	if !tcpConnects(guestNS, "10.99.0.2", "8080") {
		t.Error("guests should reach the internet")
	}
	for _, target := range []struct{ address, port, what string }{
		{"10.87.1.1", "80", "the web UI (guest side)"},
		{"10.87.0.1", "80", "the web UI (hotspot side)"},
		{"10.87.1.1", "22", "SSH"},
		{"10.87.0.99", "8080", "the owner's phone"},
	} {
		if tcpConnects(guestNS, target.address, target.port) {
			t.Errorf("guests must not reach %s", target.what)
		}
	}
	if tcpConnects(phoneNS, "10.87.1.99", "8080") {
		t.Error("the owner's devices must not reach guests either")
	}
	if !tcpConnects(phoneNS, "10.87.0.1", "80") {
		t.Error("the owner's devices should still reach the web UI")
	}
}

// packetsCounted reads a counter made by countPackets.
func packetsCounted(t *testing.T, ns string) int {
	t.Helper()
	out := inNS(t, ns, "nft", "list", "table", "ip", "rbtest")
	_, rest, found := strings.Cut(out, "counter packets ")
	if !found {
		t.Fatalf("no counter in:\n%s", out)
	}
	count, _ := strconv.Atoi(strings.Fields(rest)[0])
	return count
}

// countPackets counts, in namespace ns, the packets matching rule on their
// way in ("input") or out ("output").
func countPackets(t *testing.T, ns, hook, rule string) {
	t.Helper()
	rules := "table ip rbtest {\n\tchain count {\n\t\ttype filter hook " + hook + " priority 0;\n\t\t" + rule + " counter\n\t}\n}\n"
	file := filepath.Join(t.TempDir(), "count.nft")
	writeFile(t, file, rules)
	inNS(t, ns, "nft", "-f", file)
}

// A guest that sends packets with the address of one of the owner's
// devices as their sender must not get them through: the answers from the
// internet would be delivered to that device, which is a way for a guest
// (with a helper outside) to send it packets.
func TestGuest_CantSendWithADevicesAddress(t *testing.T) {
	requireRoot(t)
	startGuestNetwork(t)
	giveGuestAddress(t)
	startUpstreamServer(t)

	// What reaches the owner's phone from the upstream server.
	countPackets(t, phoneNS, "input", "ip saddr 10.99.0.2 tcp sport 8080")
	// The control: the phone's own connection is answered, and counted.
	if !tcpConnects(phoneNS, "10.99.0.2", "8080") || packetsCounted(t, phoneNS) == 0 {
		t.Fatal("test setup broken: the phone's own connection to the server isn't answered or counted")
	}
	before := packetsCounted(t, phoneNS)

	// The guest takes the phone's address as its sender for that server.
	inNS(t, guestNS, "ip", "address", "add", "10.87.0.99/32", "dev", "eth0")
	inNS(t, guestNS, "ip", "route", "add", "10.99.0.2/32", "via", "10.87.1.1", "src", "10.87.0.99")
	countPackets(t, guestNS, "output", "ip saddr 10.87.0.99 ip daddr 10.99.0.2")
	tryInNS(guestNS, 4*time.Second, "busybox", "nc", "-w", "2", "10.99.0.2", "8080")
	if packetsCounted(t, guestNS) == 0 {
		t.Fatal("test setup broken: the guest sent nothing with the phone's address")
	}
	if got := packetsCounted(t, phoneNS) - before; got != 0 {
		t.Errorf("the phone got %d packet(s) from the server in answer to what the guest sent with its address", got)
	}

	// With its own address the guest still reaches the internet.
	inNS(t, guestNS, "ip", "route", "del", "10.99.0.2/32")
	if !tcpConnects(guestNS, "10.99.0.2", "8080") {
		t.Error("guests should still reach the internet with their own address")
	}
}

// Guests use the VPN like the owner's devices: the site behind the VPN
// answers them, the hotel's network doesn't, and when the VPN server
// stops answering (kill switch on), they reach nothing at all.
func TestGuest_UsesTheVPNAndTheKillSwitch(t *testing.T) {
	n := buildVPNNetwork(t)
	buildGuest(t)
	// The guest network's routing rule and firewall, as rbd sets them with
	// the guest network on (ClientsThroughTunnel covers both bridges).
	runAllIgnoringErrors(routerNS, routing.ClientsDirect())
	runAll(t, routerNS, routing.ClientsThroughTunnel(routing.KernelHasIPv6("/proc/sys/net/ipv6")))
	n.applyRouterRulesWith(t, routerRules{egress: []string{routing.TunnelInterface}, upstreamDNS: []string{vpnServerAddress}, guest: true})
	giveGuestAddress(t)
	waitForHandshake(t)

	if !tcpConnects(guestNS, tunnelSite, "8080") {
		t.Fatal("guests should reach the site behind the VPN (only reachable through the tunnel)")
	}
	// The hotel's network itself is only reached through the tunnel too
	// (10.99.0.2 is also the VPN server, which answers through it).
	leaked := countPacketsOutsideTheTunnel(t, n, "8081")
	tcpConnects(guestNS, "10.99.0.2", "8081")
	if out := leaked(); out != "" {
		t.Errorf("LEAK: a guest's connection reached the hotel's network outside the VPN:\n%s", out)
	}

	// The VPN server disappears: guests reach nothing.
	inNS(t, upstreamNS, "ip", "link", "delete", serverInterface)
	if tcpConnects(guestNS, tunnelSite, "8080") || tcpConnects(guestNS, "10.99.0.2", "8081") {
		t.Error("LEAK: with the kill switch on and the VPN down, guests must reach nothing")
	}
}
