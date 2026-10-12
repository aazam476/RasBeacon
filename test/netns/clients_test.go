package netns

// Milestone 8 test: pausing a device's internet access on the Clients page.

import (
	"strings"
	"testing"
)

func TestPausedDevice_ReachesRasBeaconButNotTheInternet(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	// The phone's MAC address, as the router sees it on the hotspot.
	mac := strings.Fields(inNS(t, phoneNS, "cat", "/sys/class/net/eth0/address"))[0]
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2", PausedMACs: []string{mac}})
	inNS(t, routerNS, "ip", "route", "add", "default", "via", "10.99.0.2")
	givePhoneAddress(t)
	startUpstreamServer(t)

	if !tcpConnects(routerNS, "10.99.0.2", "8080") {
		t.Fatal("test setup broken: the upstream server does not answer")
	}
	if tcpConnects(phoneNS, "10.99.0.2", "8080") {
		t.Error("a paused device must not reach the internet")
	}
	if !tcpConnects(phoneNS, "10.87.0.1", "80") {
		t.Error("a paused device should still reach the web UI (to see why, or to allow itself again)")
	}
}
