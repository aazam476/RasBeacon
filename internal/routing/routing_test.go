package routing

import (
	"net/netip"
	"strings"
	"testing"
)

func joined(cmds [][]string) string {
	var lines []string
	for _, c := range cmds {
		lines = append(lines, strings.Join(c, " "))
	}
	return strings.Join(lines, "\n")
}

func TestTunnelUp(t *testing.T) {
	got := joined(TunnelUp(TunnelSettings{
		ConfigFile: "/run/rbd/wg0.conf",
		Addresses:  []netip.Prefix{netip.MustParsePrefix("10.66.0.2/32")},
		MTU:        1380,
		DNS:        []netip.Addr{netip.MustParseAddr("10.66.0.1")},
	}))
	for _, want := range []string{
		"wg setconf wg0 /run/rbd/wg0.conf",
		"ip address add 10.66.0.2/32 dev wg0",
		"ip link set dev wg0 mtu 1380 up",
		"ip -4 route replace default dev wg0 table 51820",
		"ip -4 route replace unreachable default metric 4096 table 51820",
		"ip route replace 10.66.0.1/32 dev wg0",
		"ip -6 route flush dev wg0 table 51820", // no IPv6 in this tunnel
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-6 route replace default dev wg0") {
		t.Error("a tunnel without IPv6 must not get an IPv6 default route")
	}
}

func TestClientRules(t *testing.T) {
	if got := joined(ClientsThroughTunnel("br-lan")); !strings.Contains(got, "ip -4 rule add priority 100 iif br-lan lookup 51820") {
		t.Errorf("got:\n%s", got)
	}
	if got := joined(TunnelDown()); !strings.Contains(got, "ip link delete dev wg0") || !strings.Contains(got, "rule delete priority 100") {
		t.Errorf("got:\n%s", got)
	}
}
