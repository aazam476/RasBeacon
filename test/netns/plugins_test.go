package netns

// Plugins on their shared bridge: each one reaches only what its
// permissions allow, never another plugin directly.
//
//	[plugin A] eth0 ═══ rbport2 ─┐
//	rb-plug-a  10.89.0.2         ├ rbplug0 10.89.0.1  [router]
//	[plugin B] eth0 ═══ rbport3 ─┘
//	rb-plug-b  10.89.0.3
//
// Packets between A and B are passed along by the bridge itself, so only
// the firewall's bridge table can stop them.

import (
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/plugin"
	"rasbeacon/internal/render"
	"rasbeacon/internal/routing"
)

// buildPlugins adds two plugins to the router of buildNetwork, plugged into
// the plugins' bridge with the port names Podman gives them.
func buildPlugins(t *testing.T) map[string]string {
	t.Helper()
	plugins := map[string]string{"rb-plug-a": "10.89.0.2", "rb-plug-b": "10.89.0.3"}
	inNS(t, routerNS, "ip", "link", "add", "name", render.PluginBridge, "type", "bridge")
	inNS(t, routerNS, "ip", "address", "add", render.PluginGateway+"/24", "dev", render.PluginBridge)
	inNS(t, routerNS, "ip", "link", "set", render.PluginBridge, "up")
	// Where the kernel's br_netfilter module is loaded (on this test
	// machine, not usually on a Pi), it also sends bridged packets through
	// the inet table, which would hide what this test checks: so it is
	// switched off here, as on a Pi without it.
	for _, setting := range []string{"net.bridge.bridge-nf-call-iptables", "net.bridge.bridge-nf-call-ip6tables"} {
		exec.Command("ip", "netns", "exec", routerNS, "sysctl", "-q", "-w", setting+"=0").Run()
	}
	for ns, address := range plugins {
		exec.Command("ip", "netns", "del", ns).Run()
		run(t, "ip", "netns", "add", ns)
		t.Cleanup(func() { exec.Command("ip", "netns", "del", ns).Run() })
		inNS(t, ns, "ip", "link", "set", "lo", "up")

		port := plugin.HostPortName(address)
		run(t, "ip", "link", "add", "rbtmp-"+port, "type", "veth", "peer", "name", "rbtmp-c"+port)
		run(t, "ip", "link", "set", "rbtmp-c"+port, "netns", ns, "name", "eth0")
		run(t, "ip", "link", "set", "rbtmp-"+port, "netns", routerNS, "name", port)
		inNS(t, routerNS, "ip", "link", "set", port, "master", render.PluginBridge)
		inNS(t, routerNS, "ip", "link", "set", port, "up")
		inNS(t, ns, "ip", "address", "add", address+"/24", "dev", "eth0")
		inNS(t, ns, "ip", "link", "set", "eth0", "up")
		inNS(t, ns, "ip", "route", "add", "default", "via", render.PluginGateway)
	}
	return plugins
}

func TestPlugins_CantReachEachOther(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	buildPlugins(t)
	// Each plugin runs a server, and RasBeacon a DNS stand-in on its
	// address on the plugins' network.
	startBackground(t, "rb-plug-a", "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "plugin-a-standin")
	startBackground(t, "rb-plug-b", "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "plugin-b-standin")
	startBackground(t, routerNS, "busybox", "nc", "-ll", "-p", "53", "-e", "/bin/echo", "dns-standin")

	// The control: without RasBeacon's firewall, the bridge passes
	// packets between the plugins.
	time.Sleep(300 * time.Millisecond) // let the servers start
	if !tcpConnects("rb-plug-a", "10.89.0.3", "8080") || !tcpConnects("rb-plug-a", render.PluginGateway, "53") {
		t.Fatal("without the firewall, plugin A should reach plugin B and RasBeacon (the test network is broken)")
	}

	// Plugin A may use the internet (and so RasBeacon's DNS server);
	// plugin B has no permissions.
	rules, err := render.Firewall(render.FirewallInput{
		Egress:     []string{"eth0"},
		LANAddress: render.DefaultLAN.Prefix.Addr(),
		Plugins: []render.PluginRule{
			{Address: netip.MustParseAddr("10.89.0.2"), Internet: true},
			{Address: netip.MustParseAddr("10.89.0.3")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rulesFile := filepath.Join(t.TempDir(), "firewall.nft")
	writeFile(t, rulesFile, rules)
	inNS(t, routerNS, "nft", "-f", rulesFile)

	if tcpConnects("rb-plug-a", "10.89.0.3", "8080") {
		t.Error("plugin A reached plugin B")
	}
	if tcpConnects("rb-plug-b", "10.89.0.2", "8080") {
		t.Error("plugin B reached plugin A")
	}
	// What the permissions allow still works: the bridge table only stops
	// packets between two plugins' ports.
	if !tcpConnects("rb-plug-a", render.PluginGateway, "53") {
		t.Error("plugin A, which may use the internet, should reach RasBeacon's DNS server")
	}
	if tcpConnects("rb-plug-b", render.PluginGateway, "53") {
		t.Error("plugin B has no permissions, so RasBeacon's DNS server shouldn't answer it")
	}
}

// TestPlugins_ReachTheInternetOnlyThroughTheVPN checks SECURITY.md's
// promise that a plugin never reaches the internet outside the VPN. Rule
// 102 sends plugins' traffic for networks the Pi is on (such as the
// hotel's 10.99.0.0/24) to the main table, so only the firewall keeps it
// out of the uplink.
func TestPlugins_ReachTheInternetOnlyThroughTheVPN(t *testing.T) {
	buildVPNNetwork(t)
	buildPlugins(t)
	// The plugin may use the internet; with the VPN in use, that means the
	// tunnel only.
	rules, err := render.Firewall(render.FirewallInput{
		Egress:          []string{routing.TunnelInterface},
		TunnelInterface: routing.TunnelInterface,
		TunnelDNS:       []netip.Addr{netip.MustParseAddr(vpnServerAddress)},
		LANAddress:      render.DefaultLAN.Prefix.Addr(),
		Plugins:         []render.PluginRule{{Address: netip.MustParseAddr("10.89.0.2"), Internet: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rulesFile := filepath.Join(t.TempDir(), "firewall.nft")
	writeFile(t, rulesFile, rules)
	inNS(t, routerNS, "nft", "-f", rulesFile)
	waitForHandshake(t)

	if !tcpConnects("rb-plug-a", tunnelSite, "8080") {
		t.Fatal("the plugin should reach the site behind the VPN (only reachable through the tunnel)")
	}
	if tcpConnects("rb-plug-a", "10.99.0.2", "8081") {
		t.Error("LEAK: the plugin reached the uplink network outside the VPN")
	}

	// RasBeacon's own downloads for plugins (marked, rule 111) take the
	// tunnel too. The control: the same connection unmarked goes out of
	// the uplink, where the site doesn't answer.
	markedDial := func(mark string) bool {
		out, _ := tryInNS(routerNS, 4*time.Second, "env", "RB_DIAL="+tunnelSite+":8080", "RB_DIAL_MARK="+mark, os.Args[0])
		return strings.Contains(out, "standin")
	}
	if !markedDial(routing.PluginMark) {
		t.Error("RasBeacon's marked downloads for plugins should go through the tunnel")
	}
	if markedDial("") {
		t.Error("test setup broken: an unmarked connection from RasBeacon should not use the tunnel")
	}

	// The VPN server disappears: the plugin reaches nothing.
	inNS(t, upstreamNS, "ip", "link", "delete", serverInterface)
	if tcpConnects("rb-plug-a", tunnelSite, "8080") || tcpConnects("rb-plug-a", "10.99.0.2", "8081") {
		t.Error("LEAK: without the tunnel, the plugin must reach nothing")
	}
}

// loadPluginRules loads rbd's firewall for the network of buildPlugins.
func loadPluginRules(t *testing.T, in render.FirewallInput) {
	t.Helper()
	in.LANAddress = render.DefaultLAN.Prefix.Addr()
	rules, err := render.Firewall(in)
	if err != nil {
		t.Fatal(err)
	}
	rulesFile := filepath.Join(t.TempDir(), "firewall.nft")
	writeFile(t, rulesFile, rules)
	inNS(t, routerNS, "nft", "-f", rulesFile)
}

// A plugin with the lan-access permission opens connections to the owner's
// devices, and must get their answers back. The answers are neither a
// connection to one of the plugin's ports nor headed for the internet, so
// they need a rule of their own.
func TestPlugins_WithLANAccessReachDevices(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	buildPlugins(t)
	givePhoneAddress(t)
	startBackground(t, phoneNS, "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "phone-standin")
	startBackground(t, "rb-plug-b", "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "plugin-b-standin")
	time.Sleep(300 * time.Millisecond)

	// Plugin B may reach the owner's devices; plugin A has no permissions.
	loadPluginRules(t, render.FirewallInput{
		Egress: []string{"eth0"},
		Plugins: []render.PluginRule{
			{Address: netip.MustParseAddr("10.89.0.2")},
			{Address: netip.MustParseAddr("10.89.0.3"), LANAccess: true},
		},
	})
	if !tcpConnects("rb-plug-b", "10.87.0.99", "8080") {
		t.Error("a plugin with lan-access should reach the owner's device and get its answer")
	}
	if tcpConnects("rb-plug-a", "10.87.0.99", "8080") {
		t.Error("a plugin without lan-access reached the owner's device")
	}
	// The permission is one way: a device reaches a plugin only on the
	// ports the owner gave it (none here).
	if tcpConnects(phoneNS, "10.89.0.3", "8080") {
		t.Error("the owner's device started a connection to the plugin's own address")
	}
}

// With the VPN in use, devices' traffic is looked up in the tunnel's table.
// A connection to a plugin's port (at RasBeacon's address, rewritten to the
// plugin's) must not follow it into the tunnel: it would go to the VPN
// server instead of the plugin. The tunnel is a stand-in here (a cable
// called wg0): only the routes matter.
func TestPlugins_DevicesReachTheirPortsWhileTheVPNIsOn(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	buildPlugins(t)
	givePhoneAddress(t)
	startBackground(t, "rb-plug-a", "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "plugin-a-standin")
	time.Sleep(300 * time.Millisecond)
	withPort := []render.PluginRule{{Address: netip.MustParseAddr("10.89.0.2"),
		Ports: []render.PluginPort{{Protocol: "tcp", Host: 8443, Container: 8080}}}}

	// The control: without the VPN the port answers.
	loadPluginRules(t, render.FirewallInput{Egress: []string{"eth0"}, Plugins: withPort})
	if !tcpConnects(phoneNS, "10.87.0.1", "8443") {
		t.Fatal("the plugin's port should answer the owner's device (the test network is broken)")
	}

	// The VPN in use: devices' traffic goes to the tunnel's table, and may
	// only leave through the tunnel.
	tunnel := routing.TunnelInterface
	inNS(t, routerNS, "ip", "link", "add", "dev", tunnel, "type", "veth", "peer", "name", "rbtmp-wg0")
	inNS(t, routerNS, "ip", "address", "add", "10.66.0.2/32", "dev", tunnel)
	inNS(t, routerNS, "ip", "link", "set", tunnel, "up")
	inNS(t, routerNS, "ip", "link", "set", "rbtmp-wg0", "up")
	inNS(t, routerNS, "ip", "-4", "route", "replace", "default", "dev", tunnel, "table", routing.TunnelTable)
	runAll(t, routerNS, routing.ClientsThroughTunnel(false))
	loadPluginRules(t, render.FirewallInput{Egress: []string{tunnel}, TunnelInterface: tunnel, Plugins: withPort})

	if !tcpConnects(phoneNS, "10.87.0.1", "8443") {
		route, _ := tryInNS(routerNS, 2*time.Second, "ip", "route", "get", "10.89.0.2", "from", "10.87.0.99", "iif", render.BridgeName)
		t.Errorf("with the VPN in use, the plugin's port should still answer the owner's device; its packets go: %s", strings.TrimSpace(route))
	}
	// Still only its ports: the plugin's own address stays out of reach.
	if tcpConnects(phoneNS, "10.89.0.2", "8080") {
		t.Error("the owner's device reached the plugin's own address")
	}

	// Back to the uplink (the VPN turned off): the rules go, the port stays.
	runAllIgnoringErrors(routerNS, routing.ClientsDirect())
	loadPluginRules(t, render.FirewallInput{Egress: []string{"eth0"}, Plugins: withPort})
	if out := inNS(t, routerNS, "ip", "-4", "rule"); strings.Contains(out, "10.89.0.0/24") {
		t.Errorf("the rule for plugins' addresses should be removed with the others:\n%s", out)
	}
	if !tcpConnects(phoneNS, "10.87.0.1", "8443") {
		t.Error("without the VPN the plugin's port should answer again")
	}
}
