package netns

// Milestone 5 tests: login mode for captive portals, and that connections
// never outlive a change of path (for example from login mode back into
// the VPN).

import (
	"bufio"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/routing"
)

func TestLoginMode_OnlyWebAndDNSReachTheUplink(t *testing.T) {
	n := buildVPNNetwork(t)
	givePhoneAddress(t)
	// The hotel's login page (port 80) and something else on its network.
	startBackground(t, upstreamNS, "busybox", "nc", "-ll", "-p", "80", "-e", "/bin/echo", "login-page-standin")
	time.Sleep(300 * time.Millisecond)

	// Kill switch on, tunnel blocked by the hotel: nothing gets through.
	inNS(t, upstreamNS, "ip", "link", "delete", serverInterface)
	if tcpConnects(phoneNS, "10.99.0.2", "80") {
		t.Fatal("before login mode, the login page must not be reachable (kill switch)")
	}

	// Login mode, as rbd sets it up: devices' traffic leaves the tunnel
	// table, the firewall allows web and DNS to the uplink only, and DNS
	// goes to the hotel's server.
	runAllIgnoringErrors(routerNS, routing.ClientsDirect())
	exec.Command("ip", "netns", "exec", routerNS, "pkill", "-f", n.dir+"/dnsmasq.conf").Run()
	n.applyRouterRulesWith(t, routerRules{egress: []string{"eth0"}, upstreamDNS: []string{"10.99.0.2"}, portalOnly: true})

	if !tcpConnects(phoneNS, "10.99.0.2", "80") {
		t.Error("in login mode the phone should reach the login page (port 80)")
	}
	if tcpConnects(phoneNS, "10.99.0.2", "8081") {
		t.Error("in login mode only web and DNS may pass; port 8081 must stay blocked")
	}
	out, ok := phoneLookup(5 * time.Second)
	if !ok || !strings.Contains(out, "Address: 203.0.113.66") {
		t.Errorf("in login mode DNS should come from the hotel's server:\n%s", out)
	}
}

// echoConnection is an open TCP connection from the phone to an echo
// server upstream, kept open across firewall changes.
type echoConnection struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan string
}

func openEchoConnection(t *testing.T) *echoConnection {
	t.Helper()
	cmd := exec.Command("ip", "netns", "exec", phoneNS, "busybox", "nc", "10.99.0.2", "8082")
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &echoConnection{cmd: cmd, in: in, lines: make(chan string, 10)}
	go func() {
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			c.lines <- scanner.Text()
		}
	}()
	t.Cleanup(func() { in.Close(); cmd.Process.Kill(); cmd.Wait() })
	return c
}

// echoes sends text and reports whether it comes back within 3 seconds.
func (c *echoConnection) echoes(text string) bool {
	io.WriteString(c.in, text+"\n")
	select {
	case line := <-c.lines:
		return line == text
	case <-time.After(3 * time.Second):
		return false
	}
}

func TestPathChange_StopsConnectionsOnTheOldPath(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "10.99.0.2"})
	inNS(t, routerNS, "ip", "route", "add", "default", "via", "10.99.0.2")
	givePhoneAddress(t)
	startBackground(t, upstreamNS, "busybox", "nc", "-ll", "-p", "8082", "-e", "/bin/cat")
	time.Sleep(300 * time.Millisecond)

	// A connection made while traffic may use the uplink (no VPN, direct
	// mode, or login mode)...
	conn := openEchoConnection(t)
	if !conn.echoes("before") {
		t.Fatal("test setup broken: the echo connection does not work")
	}

	// ...must stop as soon as devices' traffic may only use the tunnel,
	// even though it was already established.
	rules := renderRouterRules(t, routerRules{egress: []string{routing.TunnelInterface}})
	inNS(t, routerNS, "nft", "-f", rules)
	if conn.echoes("after") {
		t.Error("LEAK: a connection opened before the switch kept using the uplink")
	}
}
