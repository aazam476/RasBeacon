package netns

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/render"
)

// greeting connects to address:port from namespace ns and returns what the
// server there says first ("" when nothing answers within two seconds).
func greeting(ns, address, port string) string {
	out, ok := tryInNS(ns, 5*time.Second, "busybox", "nc", "-w", "2", address, port)
	if !ok || strings.Contains(out, "nc: ") { // "nc: timed out" and the like
		return ""
	}
	return strings.TrimSpace(out)
}

// Port forwarding: a connection from the internet to RasBeacon's uplink
// address on a forwarded port reaches the device; other ports don't. A
// device on the hotspot can use the outside address too ("hairpin"), and
// forwarding port 80 doesn't take RasBeacon's own settings page away from
// the hotspot.
func TestPortForwarding_ReachesTheDeviceAndNothingElse(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	// The forwarded device is at 10.87.0.98; the phone (10.87.0.99) stands
	// in for another device on the hotspot too, for the hairpin. (A device
	// can't hairpin to itself: the router won't route a packet whose
	// sender and receiver are the same address.)
	server := netip.MustParseAddr("10.87.0.98")
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2", PortForwards: []render.PortForward{
		{Protocol: "tcp", From: 8080, To: 8080, Address: server},               // the same port
		{Protocol: "tcp", From: 80, To: 80, Address: server, InsidePort: 8081}, // another port inside
		{Protocol: "tcp", From: 9000, To: 9002, Address: server},               // a range
	}})
	inNS(t, phoneNS, "ip", "address", "add", "10.87.0.99/24", "dev", "eth0")
	inNS(t, phoneNS, "ip", "address", "add", "10.87.0.98/24", "dev", "eth0")
	inNS(t, phoneNS, "ip", "route", "add", "default", "via", "10.87.0.1")
	// The phone's servers: each says which port it is.
	for _, port := range []string{"8080", "8081", "9001", "7000"} {
		startBackground(t, phoneNS, "busybox", "nc", "-ll", "-p", port, "-e", "/bin/echo", "phone-"+port)
	}
	time.Sleep(300 * time.Millisecond)

	for port, want := range map[string]string{"8080": "phone-8080", "80": "phone-8081", "9001": "phone-9001", "7000": ""} {
		if got := greeting(upstreamNS, "10.99.0.1", port); got != want {
			t.Errorf("from the internet, port %s: got %q, want %q", port, got, want)
		}
	}
	// Hairpin: a device on the hotspot reaches the forwarded port at the
	// outside address.
	if got := greeting(phoneNS, "10.99.0.1", "8080"); got != "phone-8080" {
		t.Errorf("from the hotspot, the outside address's port 8080: got %q", got)
	}
	// RasBeacon's own address on the hotspot still answers with its settings page.
	if got := greeting(phoneNS, "10.87.0.1", "80"); got != "rbweb-standin" {
		t.Errorf("the settings page at 10.87.0.1:80: got %q", got)
	}
}

// With the VPN on and its kill switch, devices' own traffic only goes
// through the tunnel, but a connection forwarded in from the uplink is
// answered back out of the uplink (a marked reply and a routing rule, see
// the firewall's forward_answers). The kill switch still holds for the
// device's own connections.
func TestPortForwarding_AnswersLeaveByTheUplinkWithTheVPNOn(t *testing.T) {
	buildVPNNetwork(t)
	givePhoneAddress(t)
	waitForHandshake(t)
	// The same firewall as buildVPNNetwork's, plus a forwarded port.
	inNS(t, routerNS, "nft", "-f", renderRouterRules(t, routerRules{
		egress: []string{"wg0"}, upstreamDNS: []string{vpnServerAddress},
		portForwards: []render.PortForward{{Protocol: "tcp", From: 7070, To: 7070, Address: netip.MustParseAddr("10.87.0.99")}},
	}))
	startBackground(t, phoneNS, "busybox", "nc", "-ll", "-p", "7070", "-e", "/bin/echo", "phone-7070")
	time.Sleep(300 * time.Millisecond)

	if got := greeting(upstreamNS, "10.99.0.1", "7070"); got != "phone-7070" {
		t.Errorf("from the internet with the VPN on: got %q, want the phone's answer", got)
	}

	// The VPN server stops: the kill switch keeps the phone's own traffic
	// off the uplink, while the forwarded port still works.
	inNS(t, upstreamNS, "ip", "link", "delete", serverInterface)
	if tcpConnects(phoneNS, "10.99.0.2", "8081") {
		t.Error("LEAK: the phone's own connection reached the uplink network with the kill switch on")
	}
	if got := greeting(upstreamNS, "10.99.0.1", "7070"); got != "phone-7070" {
		t.Errorf("from the internet with the tunnel down: got %q, want the phone's answer", got)
	}
}
