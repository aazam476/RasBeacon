package netns

// Milestone 2 tests: after setup, devices on the hotspot reach the internet
// through the uplink with NAT, DNS is passed upstream, IPv6 is announced
// correctly, and the Ethernet safety check finds DHCP servers.
//
// The network is the same as in setupmode_test.go; the router's "eth0"
// stands in for the uplink and "rb-up" for the internet.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/dhcpprobe"
)

// startNormalRouter runs the router as after setup, with the upstream
// machine (10.99.0.2) as its DNS server.
func startNormalRouter(t *testing.T, ipv6 bool) {
	t.Helper()
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2", IPv6: ipv6})
	// The router reaches the internet through the upstream machine.
	inNS(t, routerNS, "ip", "route", "add", "default", "via", "10.99.0.2")
}

// givePhoneAddress configures the phone as DHCP would.
func givePhoneAddress(t *testing.T) {
	t.Helper()
	inNS(t, phoneNS, "ip", "address", "add", "10.87.0.99/24", "dev", "eth0")
	inNS(t, phoneNS, "ip", "route", "add", "default", "via", "10.87.0.1")
}

// startUpstreamServer runs a stand-in "website" on the upstream machine.
func startUpstreamServer(t *testing.T) {
	t.Helper()
	startBackground(t, upstreamNS, "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "website-standin")
	time.Sleep(300 * time.Millisecond)
}

func TestNormalMode_DeviceReachesInternetThroughNAT(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startNormalRouter(t, false)
	givePhoneAddress(t)
	startUpstreamServer(t)

	// The upstream network knows nothing about 10.87.0.0/24 (like a real
	// hotel network). Replies can only find their way back if RasBeacon
	// replaced the phone's address with its own (NAT).
	inNS(t, upstreamNS, "ip", "route", "del", "10.87.0.0/24")

	if !tcpConnects(phoneNS, "10.99.0.2", "8080") {
		t.Fatal("the phone should reach the upstream server through RasBeacon")
	}
	if _, ok := tryInNS(phoneNS, 3*time.Second, "ping", "-c", "1", "-W", "2", "10.99.0.2"); !ok {
		t.Error("ping from the phone should work through NAT")
	}
}

func TestNormalMode_UpstreamCannotReachDevices(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startNormalRouter(t, false)
	givePhoneAddress(t)
	// A server on the phone (like a laptop sharing files).
	startBackground(t, phoneNS, "busybox", "nc", "-ll", "-p", "8080", "-e", "/bin/echo", "phone-standin")
	time.Sleep(300 * time.Millisecond)

	// Control check: the phone's server answers on the hotspot side.
	if !tcpConnects(routerNS, "10.87.0.99", "8080") {
		t.Fatal("test setup broken: the phone's server does not answer")
	}
	// The upstream machine has a route to the hotspot (rb-up keeps the one
	// buildNetwork added), so only the firewall can stop it.
	if tcpConnects(upstreamNS, "10.87.0.99", "8080") {
		t.Error("nobody on the upstream network may open connections to devices on the hotspot")
	}
}

func TestNormalMode_DNSIsPassedUpstreamAndOtherServersAreRedirected(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	// The "internet's" DNS server on the upstream machine.
	startBackground(t, upstreamNS, "dnsmasq", "--keep-in-foreground", "--no-resolv", "--no-hosts",
		"--listen-address=10.99.0.2", "--bind-interfaces", "--address=/example.test/10.99.0.80",
		"--pid-file=", "--user=nobody", "--log-facility=-")
	startNormalRouter(t, false)
	givePhoneAddress(t)

	out := inNS(t, phoneNS, "busybox", "nslookup", "-type=a", "example.test", "10.87.0.1")
	if !strings.Contains(out, "Address: 10.99.0.80") {
		t.Errorf("RasBeacon should pass the question upstream:\n%s", out)
	}
	if strings.Contains(out, "Address: 10.87.0.1") {
		t.Error("normal mode must not answer every name with RasBeacon's address")
	}

	// A phone that ignores RasBeacon's DNS server and asks 192.0.2.53 (which
	// doesn't exist) still gets an answer: RasBeacon redirects the question
	// to itself.
	out, ok := tryInNS(phoneNS, 6*time.Second, "busybox", "nslookup", "-type=a", "example.test", "192.0.2.53")
	if !ok || !strings.Contains(out, "Address: 10.99.0.80") {
		t.Errorf("DNS to other servers should be redirected to RasBeacon:\n%s", out)
	}
}

// phoneIPv6 returns the phone's IPv6 addresses and its IPv6 routes.
func phoneIPv6() (addresses, routes string) {
	addresses, _ = tryInNS(phoneNS, 2*time.Second, "ip", "-6", "address", "show", "dev", "eth0")
	routes, _ = tryInNS(phoneNS, 2*time.Second, "ip", "-6", "route", "show")
	return addresses, routes
}

func TestNormalMode_IPv6PrivateNetworkWithoutDefaultRoute(t *testing.T) {
	requireRoot(t)
	if _, err := os.Stat("/proc/sys/net/ipv6"); err != nil {
		t.Skip("this kernel has no IPv6 support") // CI runners do have it
	}
	buildNetwork(t)
	// Make sure the phone accepts router advertisements.
	inNS(t, phoneNS, "sysctl", "-q", "-w", "net.ipv6.conf.eth0.accept_ra=2")
	startNormalRouter(t, true)
	// Ask for a router advertisement now, as a phone does when it joins.
	inNS(t, phoneNS, "ip", "link", "set", "eth0", "down")
	inNS(t, phoneNS, "ip", "link", "set", "eth0", "up")

	// Wait up to 15 s for the phone to pick an address in RasBeacon's
	// private IPv6 network (fd12:3456:789a:1::/64).
	var addresses, routes string
	for i := 0; i < 30; i++ {
		addresses, routes = phoneIPv6()
		if strings.Contains(addresses, "fd12:3456:789a:1:") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !strings.Contains(addresses, "fd12:3456:789a:1:") {
		t.Fatalf("the phone should get a private IPv6 address:\n%s", addresses)
	}
	// The uplink has no IPv6, so RasBeacon announces "router lifetime 0":
	// the phone must not get an IPv6 default route. (A route line starting
	// with "default" would be one.)
	for _, line := range strings.Split(routes, "\n") {
		if strings.HasPrefix(line, "default") {
			t.Errorf("without IPv6 internet the phone must not get an IPv6 default route:\n%s", routes)
		}
	}
}

// TestMain lets the test program double as a small DHCP probe tool: when
// RB_PROBE_INTERFACE is set, it runs dhcpprobe.Probe on that interface,
// prints the result and exits. probeInNamespace uses this to run the probe
// inside a namespace (a running Go program can't easily switch itself).
func TestMain(m *testing.M) {
	if iface := os.Getenv("RB_PROBE_INTERFACE"); iface != "" {
		result, err := dhcpprobe.Probe(iface, 3*time.Second)
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		fmt.Printf("found=%v server=%s\n", result.ServerFound, result.ServerAddress)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// probeInNamespace runs the DHCP probe on iface inside namespace ns and
// returns what it printed, e.g. "found=true server=10.99.0.2".
func probeInNamespace(t *testing.T, ns, iface string) string {
	t.Helper()
	cmd := exec.Command("ip", "netns", "exec", ns, os.Args[0])
	cmd.Env = append(os.Environ(), "RB_PROBE_INTERFACE="+iface)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestDHCPProbe_FindsServerOnlyWhenOneAnswers(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)

	// No DHCP server on the "uplink" cable yet.
	if got := probeInNamespace(t, routerNS, "eth0"); got != "found=false server=" {
		t.Errorf("no server should be found, got %q", got)
	}

	// A hotel's DHCP server appears on the upstream side.
	startBackground(t, upstreamNS, "dnsmasq", "--keep-in-foreground", "--no-resolv", "--no-hosts",
		"--port=0", "--interface=eth0", "--bind-interfaces", "--dhcp-range=10.99.0.100,10.99.0.150,1h",
		"--dhcp-leasefile=/dev/null", "--pid-file=", "--log-facility=-")
	time.Sleep(500 * time.Millisecond)

	if got := probeInNamespace(t, routerNS, "eth0"); got != "found=true server=10.99.0.2" {
		t.Errorf("the upstream DHCP server should be found, got %q", got)
	}
}
