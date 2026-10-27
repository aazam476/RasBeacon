package netns

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/render"
)

// The welcome page (the owner's own captive portal): until the phone has
// accepted it, its web requests to anywhere reach the welcome page server
// (port 2050 on RasBeacon), everything else it sends is refused, and
// RasBeacon's own settings page still answers. Once its MAC address is in
// welcome_ok it reaches the internet, until its time runs out.
func TestWelcome_HoldsDevicesUntilTheyAccept(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2", Welcome: &render.WelcomeRule{
		Interfaces:   []string{render.BridgeName},
		OwnAddresses: []netip.Addr{render.DefaultLAN.Prefix.Addr()},
	}})
	inNS(t, phoneNS, "ip", "address", "add", "10.87.0.99/24", "dev", "eth0")
	inNS(t, phoneNS, "ip", "route", "add", "default", "via", "10.87.0.1")
	// The welcome page server on RasBeacon, and two "websites".
	startBackground(t, routerNS, "busybox", "nc", "-ll", "-p", "2050", "-e", "/bin/echo", "welcome-page")
	startBackground(t, upstreamNS, "busybox", "nc", "-ll", "-p", "80", "-e", "/bin/echo", "website-80")
	startBackground(t, upstreamNS, "busybox", "nc", "-ll", "-p", "7000", "-e", "/bin/echo", "website-7000")
	time.Sleep(300 * time.Millisecond)

	if got := greeting(phoneNS, "10.99.0.2", "80"); got != "welcome-page" {
		t.Errorf("before accepting, a website: got %q, want the welcome page", got)
	}
	if got := greeting(phoneNS, "10.99.0.2", "7000"); got != "" {
		t.Errorf("before accepting, other traffic: got %q, want nothing", got)
	}
	if got := greeting(phoneNS, "10.87.0.1", "80"); got != "rbweb-standin" {
		t.Errorf("the settings page: got %q", got)
	}

	// Accepted for 3 seconds (rbd writes the set the same way).
	out, _ := tryInNS(phoneNS, 2*time.Second, "cat", "/sys/class/net/eth0/address")
	mac := strings.TrimSpace(out)
	inNS(t, routerNS, "nft", "add", "element", "inet", "rasbeacon", "welcome_ok", "{ "+mac+" timeout 3s }")
	if got := greeting(phoneNS, "10.99.0.2", "80"); got != "website-80" {
		t.Errorf("after accepting, a website: got %q", got)
	}
	if got := greeting(phoneNS, "10.99.0.2", "7000"); got != "website-7000" {
		t.Errorf("after accepting, other traffic: got %q", got)
	}

	// The time runs out: the page again.
	time.Sleep(3500 * time.Millisecond)
	if got := greeting(phoneNS, "10.99.0.2", "80"); got != "welcome-page" {
		t.Errorf("after the time ran out: got %q, want the welcome page", got)
	}
}
