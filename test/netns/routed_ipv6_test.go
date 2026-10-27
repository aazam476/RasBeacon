package netns

// Milestone 7: a public IPv6 network routed to RasBeacon by the WireGuard
// server. Devices' IPv6 traffic from it must go through the tunnel
// without NAT: the "website" behind the VPN only accepts connections from
// that network, so a NATed connection (from RasBeacon's tunnel address)
// would be refused.

import (
	"net/netip"
	"path/filepath"
	"testing"

	"rasbeacon/internal/render"
	"rasbeacon/internal/routing"
	"rasbeacon/internal/wireguard"
)

const (
	routedNetwork = "2001:db8:1234:5678::/64"
	routedRouter  = "2001:db8:1234:5678::1/64"
	routedPhone   = "2001:db8:1234:5678::99"
	ipv6Site      = "2001:db8:ffff::80"
)

func TestVPN_RoutedIPv6NetworkIsNotNATed(t *testing.T) {
	if !routing.KernelHasIPv6("/proc/sys/net/ipv6") {
		t.Skip("IPv6 is switched off in this kernel")
	}
	n := buildVPNNetwork(t)

	// Server: the router may also use IPv6 in the tunnel, and the routed
	// network belongs to it.
	inNS(t, upstreamNS, "wg", "set", serverInterface, "peer", n.routerPrivate.PublicKey().String(),
		"allowed-ips", "10.66.0.2/32,fd66::2/128,"+routedNetwork)
	inNS(t, upstreamNS, "ip", "address", "add", "fd66::1/64", "dev", serverInterface)
	inNS(t, upstreamNS, "ip", "route", "add", routedNetwork, "dev", serverInterface)
	inNS(t, upstreamNS, "ip", "address", "add", ipv6Site+"/128", "dev", "lo")
	siteRules := filepath.Join(n.dir, "ipv6-site.nft")
	writeFile(t, siteRules, `table inet ipv6site {
	chain input {
		type filter hook input priority filter; policy accept;
		tcp dport 8082 ip6 saddr != `+routedNetwork+` drop
	}
}
`)
	inNS(t, upstreamNS, "nft", "-f", siteRules)
	startGreeter(t, upstreamNS, "["+ipv6Site+"]:8082")

	// Router: IPv6 in the tunnel, the routed network on the hotspot, and
	// rbd's firewall for it.
	inNS(t, routerNS, "sysctl", "-q", "-w", "net.ipv6.conf.all.forwarding=1")
	runAll(t, routerNS, routing.TunnelUp(routing.TunnelSettings{
		ConfigFile: filepath.Join(n.dir, "wg0.conf"),
		Addresses:  []netip.Prefix{netip.MustParsePrefix(routerTunnelAddr), netip.MustParsePrefix("fd66::2/128")},
		MTU:        wireguard.DefaultMTU,
		DNS:        []netip.Addr{netip.MustParseAddr(vpnServerAddress)},
		IPv6:       true,
		KernelIPv6: true,
	}))
	inNS(t, routerNS, "ip", "-6", "address", "add", routedRouter, "dev", "br-lan", "nodad")
	inNS(t, routerNS, "nft", "-f", renderRouterRules(t, routerRules{
		egress: []string{routing.TunnelInterface}, routedIPv6: netip.MustParsePrefix(routedNetwork),
	}))

	// Phone: an address in the routed network, as it would pick from
	// RasBeacon's announcements.
	inNS(t, phoneNS, "ip", "-6", "address", "add", routedPhone+"/64", "dev", "eth0", "nodad")
	inNS(t, phoneNS, "ip", "-6", "route", "add", "default", "via", "2001:db8:1234:5678::1")
	waitForHandshake(t)

	if !tcpConnectsTo(phoneNS, "["+ipv6Site+"]:8082") {
		t.Fatal("the phone should reach the IPv6 site through the tunnel with its own public address")
	}

	// Inbound: nothing from the internet reaches the phone's public
	// address, until a pinhole opens one port (port forwarding's "Also
	// over IPv6"); other ports stay closed.
	startGreeter(t, phoneNS, "["+routedPhone+"]:8090")
	startGreeter(t, phoneNS, "["+routedPhone+"]:8091")
	if tcpConnectsTo(upstreamNS, "["+routedPhone+"]:8090") {
		t.Error("the phone's public address must be closed to the internet by default")
	}
	inNS(t, routerNS, "nft", "-f", renderRouterRules(t, routerRules{
		egress: []string{routing.TunnelInterface}, routedIPv6: netip.MustParsePrefix(routedNetwork),
		pinholes: []render.Pinhole{{Protocol: "tcp", From: 8090, To: 8090, Addresses: []netip.Addr{netip.MustParseAddr(routedPhone)}}},
	}))
	if !tcpConnectsTo(upstreamNS, "["+routedPhone+"]:8090") {
		t.Error("the pinhole should let the internet reach the phone on port 8090")
	}
	if tcpConnectsTo(upstreamNS, "["+routedPhone+"]:8091") {
		t.Error("a pinhole must open only its own port")
	}
	if !tcpConnectsTo(phoneNS, "["+ipv6Site+"]:8082") {
		t.Error("the phone's own connections should still work with a pinhole")
	}

	// Control: with the ordinary firewall (NAT for everything), the site
	// refuses the connection, so the success above really means no NAT.
	inNS(t, routerNS, "nft", "-f", renderRouterRules(t, routerRules{egress: []string{routing.TunnelInterface}}))
	if tcpConnectsTo(phoneNS, "["+ipv6Site+"]:8082") {
		t.Error("test setup broken: the site should refuse NATed connections")
	}
}
