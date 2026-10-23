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
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"rasbeacon/internal/plugin"
	"rasbeacon/internal/render"
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
