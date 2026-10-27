package netns

// Milestone 7: a public IPv6 network routed to RasBeacon by the WireGuard
// server. Devices' IPv6 traffic from it must go through the tunnel
// without NAT: the "website" behind the VPN only accepts connections from
// that network, so a NATed connection (from RasBeacon's tunnel address)
// would be refused.

import (
	"net/netip"
	"os"
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

// buildRoutedIPv6 is buildVPNNetwork with the routed IPv6 network: the
// VPN server routes it to RasBeacon, the phone has an address in it, and
// the IPv6 site behind the VPN (port 8082) only accepts that network.
func buildRoutedIPv6(t *testing.T) vpnNetwork {
	t.Helper()
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
	return n
}

func TestVPN_RoutedIPv6NetworkIsNotNATed(t *testing.T) {
	buildRoutedIPv6(t)
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

// Login mode with the routed IPv6 network. Connections from devices'
// public addresses aren't NATed, so the kernel doesn't end them when the
// routes change (it does end NATed ones): only the firewall stops their
// next packets from leaving through the hotel's network.
func TestLoginMode_RoutedIPv6ConnectionsDontLeaveOutsideTheVPN(t *testing.T) {
	n := buildRoutedIPv6(t)
	// The hotel's network has IPv6 too, with a way to the site.
	inNS(t, upstreamNS, "ip", "-6", "address", "add", "2001:db8:99::2/64", "dev", "eth0", "nodad")
	inNS(t, routerNS, "ip", "-6", "address", "add", "2001:db8:99::1/64", "dev", "eth0", "nodad")
	inNS(t, routerNS, "ip", "-6", "route", "add", "default", "via", "2001:db8:99::2", "dev", "eth0")
	startBackground(t, upstreamNS, "env", "RB_ECHO=["+ipv6Site+"]:8083", os.Args[0])
	leaked := countPacketsOutsideTheTunnel(t, n, "8083")

	conn := openEchoConnection(t, ipv6Site, "8083")
	if !conn.echoes("through the VPN") {
		t.Fatal("test setup broken: the echo connection through the tunnel does not work")
	}
	if out := leaked(); out != "" {
		t.Fatalf("test setup broken: the connection should use the tunnel:\n%s", out)
	}

	runAllIgnoringErrors(routerNS, routing.ClientsDirect())
	inNS(t, routerNS, "nft", "-f", renderRouterRules(t, routerRules{
		egress: []string{"eth0"}, portalOnly: true, routedIPv6: netip.MustParsePrefix(routedNetwork),
	}))
	conn.echoes("in login mode")
	if out := leaked(); out != "" {
		t.Errorf("LEAK: a connection from a public IPv6 address opened through the VPN carried on through the uplink:\n%s", out)
	}
}
