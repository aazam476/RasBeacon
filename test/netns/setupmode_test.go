// Package netns tests RasBeacon's real network configuration inside Linux
// network namespaces: small, isolated copies of the network stack that
// behave like separate computers, all on one machine.
//
// # What is simulated
//
//	[phone]  eth0 ═══════ ap0 ─┐ br-lan 10.87.0.1   [router]   eth0 ═══════ eth0  [upstream]
//	rb-phone                   └─ (the hotspot side)  rb-router  10.99.0.1   10.99.0.2  rb-up
//
// The hotspot's Wi-Fi link is replaced by a virtual cable (a "veth pair"),
// because Wi-Fi hardware is not available here. Everything else is real: the
// firewall rules and dnsmasq configuration that rbd generates, the real
// nftables and dnsmasq programs, and real DHCP and DNS traffic.
//
// # Running
//
// These tests need root (to create namespaces) and the tools ip, nft,
// dnsmasq and busybox. Without root they are skipped, unless
// RASBEACON_REQUIRE_NETNS=1 is set (as CI does), in which case they fail.
//
//	sudo -E go test ./test/netns/ -v
package netns

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/render"
)

// Namespace names. The "rb-" prefix makes leftovers easy to spot.
const (
	phoneNS    = "rb-phone"
	routerNS   = "rb-router"
	upstreamNS = "rb-up"
)

// requireRoot skips (or fails, in CI) when the test can't run.
func requireRoot(t *testing.T) {
	t.Helper()
	missing := []string{}
	for _, tool := range []string{"ip", "nft", "dnsmasq", "busybox"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	reason := ""
	if os.Geteuid() != 0 {
		reason = "needs root (run with sudo -E)"
	} else if len(missing) > 0 {
		reason = "missing tools: " + strings.Join(missing, ", ")
	}
	if reason == "" {
		return
	}
	if os.Getenv("RASBEACON_REQUIRE_NETNS") == "1" {
		t.Fatalf("network namespace tests cannot run: %s", reason)
	}
	t.Skipf("skipping network namespace tests: %s", reason)
}

// run executes a command and fails the test if it fails.
func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// inNS runs a command inside a namespace and fails the test if it fails.
func inNS(t *testing.T, ns string, args ...string) string {
	t.Helper()
	return run(t, "ip", append([]string{"netns", "exec", ns}, args...)...)
}

// tryInNS runs a command inside a namespace with a timeout and reports
// whether it succeeded, without failing the test.
func tryInNS(ns string, timeout time.Duration, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ip", append([]string{"netns", "exec", ns}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// buildNetwork creates the three namespaces and the cables between them.
// Cleanup deletes everything when the test ends.
func buildNetwork(t *testing.T) {
	t.Helper()
	for _, ns := range []string{phoneNS, routerNS, upstreamNS} {
		exec.Command("ip", "netns", "del", ns).Run() // leftovers from a crashed run
		run(t, "ip", "netns", "add", ns)
		inNS(t, ns, "ip", "link", "set", "lo", "up")
	}
	t.Cleanup(func() {
		for _, ns := range []string{phoneNS, routerNS, upstreamNS} {
			exec.Command("ip", "netns", "del", ns).Run()
		}
	})

	// Phone <-> router: stands in for the Wi-Fi hotspot link.
	run(t, "ip", "link", "add", "rbtmp-phone", "type", "veth", "peer", "name", "rbtmp-ap0")
	run(t, "ip", "link", "set", "rbtmp-phone", "netns", phoneNS, "name", "eth0")
	run(t, "ip", "link", "set", "rbtmp-ap0", "netns", routerNS, "name", "ap0")

	// Router <-> upstream: stands in for the uplink (hotel Wi-Fi, ...).
	run(t, "ip", "link", "add", "rbtmp-rup", "type", "veth", "peer", "name", "rbtmp-up")
	run(t, "ip", "link", "set", "rbtmp-rup", "netns", routerNS, "name", "eth0")
	run(t, "ip", "link", "set", "rbtmp-up", "netns", upstreamNS, "name", "eth0")

	// Router: bridge br-lan with 10.87.0.1, like rbd's EnsureBridge does.
	inNS(t, routerNS, "ip", "link", "add", "name", render.BridgeName, "type", "bridge")
	inNS(t, routerNS, "ip", "address", "add", render.DefaultLAN.Prefix.String(), "dev", render.BridgeName)
	inNS(t, routerNS, "ip", "link", "set", "ap0", "master", render.BridgeName)
	for _, link := range []string{render.BridgeName, "ap0", "eth0"} {
		inNS(t, routerNS, "ip", "link", "set", link, "up")
	}
	inNS(t, routerNS, "ip", "address", "add", "10.99.0.1/24", "dev", "eth0")
	// Forwarding is on at the kernel level, so only the firewall stops it.
	inNS(t, routerNS, "sysctl", "-q", "-w", "net.ipv4.ip_forward=1")

	// Upstream side.
	inNS(t, upstreamNS, "ip", "address", "add", "10.99.0.2/24", "dev", "eth0")
	inNS(t, upstreamNS, "ip", "link", "set", "eth0", "up")
	inNS(t, upstreamNS, "ip", "route", "add", "10.87.0.0/24", "via", "10.99.0.1")

	// Phone: link up (it gets its address from DHCP in the test).
	inNS(t, phoneNS, "ip", "link", "set", "eth0", "up")
}

// startRouterServices applies rbd's firewall and starts dnsmasq with rbd's
// setup-mode config. It also starts a dummy web server on port 80 (standing
// in for rbweb) and one on port 22 (standing in for SSH) so we can check
// which ports the firewall lets through.
func startRouterServices(t *testing.T, sshEnabled bool) (leaseFile string) {
	t.Helper()
	dir := t.TempDir()
	// Same permissions as rbd uses for /run/rbd.
	if err := os.Chmod(dir, 0o711); err != nil {
		t.Fatal(err)
	}

	rules, err := render.Firewall(render.FirewallInput{SSHEnabled: sshEnabled})
	if err != nil {
		t.Fatal(err)
	}
	rulesFile := filepath.Join(dir, "firewall.nft")
	writeFile(t, rulesFile, rules)
	inNS(t, routerNS, "nft", "-f", rulesFile)

	leaseFile = filepath.Join(dir, "dnsmasq.leases")
	conf, err := render.Dnsmasq(render.DnsmasqInput{LAN: render.DefaultLAN, LeaseFile: leaseFile, Captive: true})
	if err != nil {
		t.Fatal(err)
	}
	confFile := filepath.Join(dir, "dnsmasq.conf")
	writeFile(t, confFile, conf)

	// dnsmasq drops to an unprivileged user after starting, as it does on
	// the device. The lease file must still be writable.
	startBackground(t, routerNS, "dnsmasq", "--keep-in-foreground", "--conf-file="+confFile,
		"--pid-file=", "--user=nobody", "--log-facility=-")
	// "-ll" keeps busybox nc listening after each connection; "-e" makes it
	// answer every connection with a short greeting we can check for.
	startBackground(t, routerNS, "busybox", "nc", "-ll", "-p", "80", "-e", "/bin/echo", "rbweb-standin")
	startBackground(t, routerNS, "busybox", "nc", "-ll", "-p", "22", "-e", "/bin/echo", "ssh-standin")
	time.Sleep(500 * time.Millisecond) // let the servers start
	return leaseFile
}

// startBackground starts a long-running program in a namespace and stops it
// when the test ends.
func startBackground(t *testing.T, ns string, args ...string) {
	t.Helper()
	cmd := exec.Command("ip", append([]string{"netns", "exec", ns}, args...)...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %v: %v", args, err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tcpConnects reports whether a TCP connection from ns to address:port
// reaches one of the stand-in servers within 2 seconds. Checking for the
// server's greeting (not just the exit code) makes sure a "blocked" result
// really means the firewall stopped it, not that nothing was listening.
func tcpConnects(ns, address, port string) bool {
	out, _ := tryInNS(ns, 4*time.Second, "busybox", "nc", "-w", "2", address, port)
	return strings.Contains(out, "standin")
}

// TestStandInServersWork is the control check for tcpConnects: from inside
// the router, both stand-in servers must answer. If this fails, the other
// firewall tests would pass for the wrong reason.
func TestStandInServersWork(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startRouterServices(t, false)
	for _, port := range []string{"80", "22"} {
		if !tcpConnects(routerNS, "127.0.0.1", port) {
			t.Fatalf("stand-in server on port %s is not answering", port)
		}
	}
}

func TestSetupMode_PhoneGetsAddressAndCaptiveDNS(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	leaseFile := startRouterServices(t, false)

	// DHCP: the phone asks for an address. "-s /bin/true" stops udhcpc from
	// configuring the interface itself; we only check what it was offered.
	out := inNS(t, phoneNS, "busybox", "udhcpc", "-i", "eth0", "-n", "-q", "-t", "5", "-s", "/bin/true")
	if !strings.Contains(out, "lease of 10.87.0.") {
		t.Fatalf("phone did not get a 10.87.0.x address:\n%s", out)
	}

	// The lease must be recorded even though dnsmasq runs as "nobody".
	leases, err := os.ReadFile(leaseFile)
	if err != nil || !strings.Contains(string(leases), "10.87.0.") {
		t.Fatalf("lease file not written (dnsmasq privilege drop problem?): %v %q", err, leases)
	}

	// Give the phone an address so it can make DNS and TCP requests.
	inNS(t, phoneNS, "ip", "address", "add", "10.87.0.99/24", "dev", "eth0")

	// Captive DNS: any name resolves to RasBeacon during setup.
	for _, name := range []string{"captive.apple.com", "connectivitycheck.gstatic.com", "example.com"} {
		out := inNS(t, phoneNS, "busybox", "nslookup", "-type=a", name, "10.87.0.1")
		if !strings.Contains(out, "Address: 10.87.0.1") {
			t.Errorf("%s should resolve to 10.87.0.1 in setup mode:\n%s", name, out)
		}
		// IPv6 lookups get a clean "no address" answer, not an error.
		out, ok := tryInNS(phoneNS, 5*time.Second, "busybox", "nslookup", "-type=aaaa", name, "10.87.0.1")
		if !ok || strings.Contains(out, "REFUSED") {
			t.Errorf("IPv6 lookup of %s should succeed with no address:\n%s", name, out)
		}
	}
}

func TestFirewall_HotspotSideReachesWebUIButNotSSHWhenOff(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startRouterServices(t, false)
	inNS(t, phoneNS, "ip", "address", "add", "10.87.0.99/24", "dev", "eth0")

	if !tcpConnects(phoneNS, "10.87.0.1", "80") {
		t.Error("phone on the hotspot must reach the web UI (port 80)")
	}
	if tcpConnects(phoneNS, "10.87.0.1", "22") {
		t.Error("SSH (port 22) must be closed while SSH is off")
	}
}

func TestFirewall_SSHOpensOnlyOnHotspotSideWhenOn(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startRouterServices(t, true)
	inNS(t, phoneNS, "ip", "address", "add", "10.87.0.99/24", "dev", "eth0")

	if !tcpConnects(phoneNS, "10.87.0.1", "22") {
		t.Error("with SSH on, the hotspot side should reach port 22")
	}
	if tcpConnects(upstreamNS, "10.99.0.1", "22") {
		t.Error("SSH must never be reachable from the upstream network")
	}
}

func TestFirewall_UpstreamCannotReachWebUIOrDNS(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startRouterServices(t, false)

	if tcpConnects(upstreamNS, "10.99.0.1", "80") {
		t.Error("the web UI must not be reachable from the upstream network")
	}
	// Even addressing the hotspot IP directly via the router must fail.
	if tcpConnects(upstreamNS, "10.87.0.1", "80") {
		t.Error("the web UI must not be reachable from upstream, even via the hotspot address")
	}
	if _, ok := tryInNS(upstreamNS, 5*time.Second, "busybox", "nslookup", "example.com", "10.99.0.1"); ok {
		t.Error("DNS must not answer queries from the upstream network")
	}
}

func TestFirewall_NothingIsForwardedInMilestone1(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startRouterServices(t, false)
	inNS(t, phoneNS, "ip", "address", "add", "10.87.0.99/24", "dev", "eth0")
	inNS(t, phoneNS, "ip", "route", "add", "default", "via", "10.87.0.1")

	// Control check: the upstream machine itself answers pings from the router.
	if _, ok := tryInNS(routerNS, 3*time.Second, "ping", "-c", "1", "-W", "1", "10.99.0.2"); !ok {
		t.Fatal("test network is broken: router cannot reach upstream")
	}
	// The phone must not get through the router (no internet until M2).
	if out, ok := tryInNS(phoneNS, 3*time.Second, "ping", "-c", "1", "-W", "1", "10.99.0.2"); ok {
		t.Errorf("phone traffic must not be forwarded upstream:\n%s", out)
	}
}
