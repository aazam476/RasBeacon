package netns

import (
	"strings"
	"testing"

	"rasbeacon/internal/render"
)

// A device with a fixed address ("Always use this address") gets exactly
// that address from dnsmasq, with rbd's own dnsmasq.conf.
func TestFixedAddress_DnsmasqHandsItOut(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	mac := strings.TrimSpace(inNS(t, phoneNS, "cat", "/sys/class/net/eth0/address"))
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2",
		FixedAddresses: []render.FixedAddress{{MAC: mac, IP: "10.87.0.77"}}})

	out := inNS(t, phoneNS, "busybox", "udhcpc", "-i", "eth0", "-n", "-q", "-t", "5", "-s", "/bin/true")
	if !strings.Contains(out, "lease of 10.87.0.77 ") {
		t.Errorf("the phone should get its fixed address 10.87.0.77:\n%s", out)
	}
}
