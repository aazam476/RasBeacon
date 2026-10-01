package netns

// Milestone 4 tests: the WireGuard VPN and its kill switch, with a real
// WireGuard server in the "upstream" namespace.
//
//	[phone] ── [router: br-lan, wg0 ──── tunnel ────┐]
//	                   eth0 ═══════ eth0 [upstream: wgs0 10.66.0.1 (VPN server)]
//
// The upstream namespace stands in for both the hotel network and the
// internet behind the VPN server:
//
//   - 198.51.100.80:8080 is a "website" that only accepts connections that
//     arrive through the tunnel. Reaching it proves traffic used the VPN.
//   - 10.99.0.2:8081 accepts anyone: reaching it through the uplink would
//     be a leak.
//   - Two DNS servers answer "example.test" differently: the VPN's
//     (10.66.0.1) with 198.51.100.80, the hotel's (10.99.0.2) with
//     203.0.113.66. The hotel's server logs every question, so a DNS leak
//     shows up in its log.
//
// The router runs exactly what rbd would: render.Firewall, render.Dnsmasq,
// render.WireGuard and the commands from internal/routing.

import (
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/render"
	"rasbeacon/internal/routing"
	"rasbeacon/internal/wireguard"
)

const (
	vpnServerAddress = "10.66.0.1"
	routerTunnelAddr = "10.66.0.2/32"
	tunnelSite       = "198.51.100.80"
	serverInterface  = "wgs0"
)

// requireWireGuard skips (or fails, in CI) when WireGuard isn't available.
// It returns how to create a WireGuard interface: the kernel module, or the
// userspace program wireguard-go.
func requireWireGuard(t *testing.T) (useKernel bool) {
	t.Helper()
	requireRoot(t)
	if _, err := exec.LookPath("wg"); err != nil {
		if os.Getenv("RASBEACON_REQUIRE_NETNS") == "1" {
			t.Fatal("the wg tool is missing (install wireguard-tools)")
		}
		t.Skip("the wg tool is missing")
	}
	if exec.Command("ip", "link", "add", "rbwgprobe", "type", "wireguard").Run() == nil {
		exec.Command("ip", "link", "delete", "rbwgprobe").Run()
		return true
	}
	if _, err := exec.LookPath("wireguard-go"); err == nil {
		return false
	}
	if os.Getenv("RASBEACON_REQUIRE_NETNS") == "1" {
		t.Fatal("no WireGuard: the kernel has no wireguard module and wireguard-go is not installed")
	}
	t.Skip("no WireGuard (kernel module or wireguard-go)")
	return false
}

// createWireGuard makes WireGuard interface name inside namespace ns.
func createWireGuard(t *testing.T, ns, name string, useKernel bool) {
	t.Helper()
	if useKernel {
		inNS(t, ns, "ip", "link", "add", "dev", name, "type", "wireguard")
		return
	}
	cmd := exec.Command("ip", "netns", "exec", ns, "wireguard-go", "-f", name)
	// wireguard-go only runs on Linux when told the kernel module is not an
	// option (here: it is missing).
	cmd.Env = append(os.Environ(), "WG_I_PREFER_BUGGY_USERSPACE_TO_POLISHED_KMOD=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting wireguard-go: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for i := 0; i < 50; i++ {
		if _, ok := tryInNS(ns, time.Second, "ip", "link", "show", name); ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("wireguard-go did not create %s", name)
}

// vpnNetwork is the test network with a VPN server and the router's
// tunnel.
type vpnNetwork struct {
	dir           string
	hotelDNSLog   string
	routerPrivate wireguard.Key
	serverPublic  wireguard.Key
}

// buildVPNNetwork sets up the server, the "internet" behind it, and the
// router with the VPN in use and the kill switch on.
func buildVPNNetwork(t *testing.T) vpnNetwork {
	t.Helper()
	useKernel := requireWireGuard(t)
	buildNetwork(t)
	n := vpnNetwork{dir: t.TempDir()}
	if err := os.Chmod(n.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var err error
	if n.routerPrivate, err = wireguard.GeneratePrivateKey(); err != nil {
		t.Fatal(err)
	}
	serverPrivate, _ := wireguard.GeneratePrivateKey()
	n.serverPublic = serverPrivate.PublicKey()

	// --- Upstream: VPN server, "website", DNS servers ---
	keyFile := filepath.Join(n.dir, "server.key")
	writeFile(t, keyFile, serverPrivate.String())
	createWireGuard(t, upstreamNS, serverInterface, useKernel)
	inNS(t, upstreamNS, "wg", "set", serverInterface, "private-key", keyFile, "listen-port", "51820",
		"peer", n.routerPrivate.PublicKey().String(), "allowed-ips", "10.66.0.2/32")
	inNS(t, upstreamNS, "ip", "address", "add", vpnServerAddress+"/24", "dev", serverInterface)
	inNS(t, upstreamNS, "ip", "link", "set", serverInterface, "up")
	inNS(t, upstreamNS, "ip", "address", "add", tunnelSite+"/32", "dev", "lo")

	// The website only accepts tunnel traffic; port 8081 accepts anyone.
	upstreamRules := filepath.Join(n.dir, "upstream.nft")
	writeFile(t, upstreamRules, `table inet upstream {
	chain input {
		type filter hook input priority filter; policy accept;
		tcp dport 8080 iifname != "`+serverInterface+`" drop
	}
}
`)
	inNS(t, upstreamNS, "nft", "-f", upstreamRules)
	startBackground(t, upstreamNS, "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "tunnel-site-standin")
	startBackground(t, upstreamNS, "busybox", "nc", "-ll", "-p", "8081", "-e", "/bin/echo", "direct-site-standin")

	startBackground(t, upstreamNS, "dnsmasq", "--keep-in-foreground", "--no-resolv", "--no-hosts",
		"--listen-address="+vpnServerAddress, "--bind-dynamic", "--address=/example.test/"+tunnelSite,
		"--pid-file=", "--user=nobody", "--log-facility=-")
	n.hotelDNSLog = filepath.Join(n.dir, "hotel-dns.log")
	writeFile(t, n.hotelDNSLog, "")
	os.Chmod(n.hotelDNSLog, 0o666)
	startBackground(t, upstreamNS, "dnsmasq", "--keep-in-foreground", "--no-resolv", "--no-hosts",
		"--listen-address=10.99.0.2", "--bind-interfaces", "--address=/example.test/203.0.113.66",
		"--pid-file=", "--user=nobody", "--log-queries", "--log-facility="+n.hotelDNSLog)

	// --- Router: as rbd sets it up with the VPN on ---
	conf, err := render.WireGuard(render.WireGuardInput{
		PrivateKey:    n.routerPrivate.String(),
		PeerPublicKey: n.serverPublic.String(),
		Endpoint:      "10.99.0.2:51820",
		AllowedIPs:    []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
		Keepalive:     wireguard.DefaultKeepalive,
	})
	if err != nil {
		t.Fatal(err)
	}
	confFile := filepath.Join(n.dir, "wg0.conf")
	writeFile(t, confFile, conf)
	createWireGuard(t, routerNS, routing.TunnelInterface, useKernel)
	inNS(t, routerNS, "ip", "route", "add", "default", "via", "10.99.0.2")
	runAll(t, routerNS, routing.TunnelUp(routing.TunnelSettings{
		ConfigFile: confFile,
		Addresses:  []netip.Prefix{netip.MustParsePrefix(routerTunnelAddr)},
		MTU:        wireguard.DefaultMTU,
		DNS:        []netip.Addr{netip.MustParseAddr(vpnServerAddress)},
		KernelIPv6: routing.KernelHasIPv6("/proc/sys/net/ipv6"),
	}))
	runAll(t, routerNS, routing.ClientsThroughTunnel(routing.KernelHasIPv6("/proc/sys/net/ipv6")))

	n.applyRouterRules(t, []string{routing.TunnelInterface}, []string{vpnServerAddress})
	return n
}

// routerRules chooses a firewall and DNS setup for the router.
type routerRules struct {
	egress      []string
	upstreamDNS []string
	portalOnly  bool
	routedIPv6  netip.Prefix
}

// renderRouterRules writes rbd's firewall for r to a file and returns its
// path.
func renderRouterRules(t *testing.T, r routerRules) string {
	t.Helper()
	rules, err := render.Firewall(render.FirewallInput{
		Egress:          r.egress,
		TunnelInterface: routing.TunnelInterface,
		TunnelDNS:       []netip.Addr{netip.MustParseAddr(vpnServerAddress)},
		PortalOnly:      r.portalOnly,
		RoutedIPv6:      r.routedIPv6,
	})
	if err != nil {
		t.Fatal(err)
	}
	rulesFile := filepath.Join(t.TempDir(), "firewall.nft")
	writeFile(t, rulesFile, rules)
	return rulesFile
}

// applyRouterRules loads rbd's firewall and starts dnsmasq, as for normal
// mode with the given egress interfaces and upstream DNS servers.
func (n vpnNetwork) applyRouterRules(t *testing.T, egress []string, upstreamDNS []string) {
	t.Helper()
	n.applyRouterRulesWith(t, routerRules{egress: egress, upstreamDNS: upstreamDNS})
}

// applyRouterRulesWith is applyRouterRules with all options.
func (n vpnNetwork) applyRouterRulesWith(t *testing.T, r routerRules) {
	t.Helper()
	inNS(t, routerNS, "nft", "-f", renderRouterRules(t, r))

	dnsmasqConf, err := render.Dnsmasq(render.DnsmasqInput{
		LAN: render.DefaultLAN, LeaseFile: filepath.Join(n.dir, "leases"), Upstream: r.upstreamDNS,
	})
	if err != nil {
		t.Fatal(err)
	}
	confFile := filepath.Join(n.dir, "dnsmasq.conf")
	writeFile(t, confFile, dnsmasqConf)
	startBackground(t, routerNS, "dnsmasq", "--keep-in-foreground", "--conf-file="+confFile,
		"--pid-file=", "--user=nobody", "--log-facility=-")
	time.Sleep(500 * time.Millisecond)
}

// runAll runs commands from internal/routing in a namespace.
func runAll(t *testing.T, ns string, cmds [][]string) {
	t.Helper()
	for _, cmd := range cmds {
		inNS(t, ns, cmd...)
	}
}

// waitForHandshake waits until the router's tunnel has a handshake.
func waitForHandshake(t *testing.T) {
	t.Helper()
	// Any traffic into the tunnel starts a handshake; keepalives do too.
	for i := 0; i < 40; i++ {
		out, _ := tryInNS(routerNS, 2*time.Second, "wg", "show", routing.TunnelInterface, "dump")
		if status, ok := wireguard.ParseDump(out); ok && !status.LatestHandshake.IsZero() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	out, _ := tryInNS(routerNS, 2*time.Second, "wg", "show")
	t.Fatalf("the tunnel never completed a handshake:\n%s", out)
}

// phoneLookup asks the router's DNS for example.test from the phone.
func phoneLookup(timeout time.Duration) (string, bool) {
	return tryInNS(phoneNS, timeout, "busybox", "nslookup", "-type=a", "example.test", "10.87.0.1")
}

func (n vpnNetwork) hotelDNSWasAsked(t *testing.T) bool {
	t.Helper()
	data, _ := os.ReadFile(n.hotelDNSLog)
	return strings.Contains(string(data), "example.test")
}

func TestVPN_DevicesUseTheTunnel(t *testing.T) {
	n := buildVPNNetwork(t)
	givePhoneAddress(t)
	waitForHandshake(t)

	if !tcpConnects(phoneNS, tunnelSite, "8080") {
		t.Fatal("the phone should reach the site behind the VPN (only reachable through the tunnel)")
	}
	out, ok := phoneLookup(5 * time.Second)
	if !ok || !strings.Contains(out, "Address: "+tunnelSite) {
		t.Errorf("DNS should be answered by the VPN's DNS server:\n%s", out)
	}
	if n.hotelDNSWasAsked(t) {
		t.Error("the hotel's DNS server must never see devices' lookups while the VPN is in use")
	}
}

func TestVPN_KillSwitchHoldsWhenTheServerStops(t *testing.T) {
	n := buildVPNNetwork(t)
	givePhoneAddress(t)
	waitForHandshake(t)

	// Control: the direct site is reachable from the router itself, so a
	// "blocked" result below really is the kill switch.
	if !tcpConnects(routerNS, "10.99.0.2", "8081") {
		t.Fatal("test setup broken: the direct site is not reachable at all")
	}

	// The VPN server disappears.
	inNS(t, upstreamNS, "ip", "link", "delete", serverInterface)

	if tcpConnects(phoneNS, tunnelSite, "8080") {
		t.Error("the tunnel site must not be reachable without the tunnel")
	}
	if tcpConnects(phoneNS, "10.99.0.2", "8081") {
		t.Error("LEAK: the phone reached the uplink network with the kill switch on")
	}
	if _, ok := phoneLookup(6 * time.Second); ok {
		t.Error("DNS must not work without the tunnel")
	}
	if n.hotelDNSWasAsked(t) {
		t.Error("LEAK: the hotel's DNS server saw a lookup")
	}
}

func TestVPN_FailsClosedIfTheTunnelInterfaceDisappears(t *testing.T) {
	n := buildVPNNetwork(t)
	givePhoneAddress(t)

	// As if wg0 were deleted by accident (or rbd crashed half-way): its
	// routes go with it, including the VPN DNS route.
	inNS(t, routerNS, "ip", "link", "delete", routing.TunnelInterface)

	if tcpConnects(phoneNS, "10.99.0.2", "8081") {
		t.Error("LEAK: without wg0, devices' traffic reached the uplink network")
	}
	if _, ok := phoneLookup(6 * time.Second); ok {
		t.Error("DNS must not work without the tunnel")
	}
	// The router's own DNS server must not send devices' questions to the
	// VPN's DNS address through the uplink either: the firewall's output
	// rule refuses it.
	if out, ok := tryInNS(routerNS, 6*time.Second, "busybox", "nslookup", "example.test", vpnServerAddress); ok {
		t.Errorf("the VPN's DNS address must only be reachable through the tunnel:\n%s", out)
	}
	if n.hotelDNSWasAsked(t) {
		t.Error("LEAK: the hotel's DNS server saw a lookup")
	}
}

func TestVPN_DirectModeUsesTheUplink(t *testing.T) {
	// Kill switch OFF and the tunnel broken for a while: rbd removes the
	// routing rule, allows the uplinks and switches DNS (agent/vpn.go).
	n := buildVPNNetwork(t)
	givePhoneAddress(t)
	inNS(t, upstreamNS, "ip", "link", "delete", serverInterface)

	runAllIgnoringErrors(routerNS, routing.ClientsDirect())
	// dnsmasq would use 127.0.0.53 on the device; here the hotel's server.
	exec.Command("ip", "netns", "exec", routerNS, "pkill", "-f", filepath.Join(n.dir, "dnsmasq.conf")).Run()
	n.applyRouterRules(t, []string{"eth0"}, []string{"10.99.0.2"})

	if !tcpConnects(phoneNS, "10.99.0.2", "8081") {
		t.Error("in direct mode the phone should reach the internet through the uplink")
	}
	out, ok := phoneLookup(5 * time.Second)
	if !ok || !strings.Contains(out, "Address: 203.0.113.66") {
		t.Errorf("in direct mode DNS should come from the uplink's server:\n%s", out)
	}
}

// runAllIgnoringErrors runs commands whose failure is expected and fine
// (like deleting a rule that may not exist).
func runAllIgnoringErrors(ns string, cmds [][]string) {
	for _, cmd := range cmds {
		exec.Command("ip", append([]string{"netns", "exec", ns}, cmd...)...).Run()
	}
}
