package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/portal"
)

// fakeProbe returns a fixed check result and counts calls.
type fakeProbe struct {
	result portal.Result
	calls  int
}

func (f *fakeProbe) probe(context.Context) portal.Result {
	f.calls++
	return f.result
}

// newPortalEnv is a device with the VPN on (kill switch on), its tunnel
// down, connected to a hotel network that shows a login page.
func newPortalEnv(t *testing.T) (*testEnv, *fakeClock, *fakeProbe) {
	t.Helper()
	e, c := newVPNEnv(t, true)
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	p := &fakeProbe{result: portal.Result{State: portal.Portal, LoginURL: "http://login.hotel.example/"}}
	e.agent.CheckPortal(p.probe)
	return e, c, p
}

func TestPortal_NoticeSendsProbeNamesToRasBeacon(t *testing.T) {
	e, _, _ := newPortalEnv(t)

	status := e.agent.PortalStatus()
	if !status.Detected || !status.CanStart || status.LoginURL != "http://login.hotel.example/" {
		t.Fatalf("status = %+v", status)
	}
	if !e.agent.SetupState().PortalNotice {
		t.Error("rbweb should be told to show the notice")
	}
	dnsmasq := e.readFile(e.paths.DnsmasqConfig())
	if !strings.Contains(dnsmasq, "address=/captive.apple.com/10.87.0.1") {
		t.Error("phones' connectivity checks should reach RasBeacon's notice page")
	}
	if got := e.firewallEgress(); got != `elements = { "wg0" }` {
		t.Errorf("the notice alone must not open anything: egress = %s", got)
	}
}

func TestPortal_LoginModeOpensWebOnlyThenClosesWhenLoggedIn(t *testing.T) {
	e, c, p := newPortalEnv(t)

	if err := e.agent.StartLoginMode(); err != nil {
		t.Fatal(err)
	}
	rules := e.readFile(e.paths.FirewallRules())
	if !strings.Contains(rules, `elements = { "wlan0" }`) || !strings.Contains(rules, "tcp dport { 80, 443 } accept") {
		t.Fatalf("login mode should allow web traffic to the uplink:\n%s", rules)
	}
	if strings.Contains(rules, `iifname "br-lan" oifname @egress accept`) {
		t.Error("login mode must not allow everything")
	}
	if !e.runner.ran("ip -4 rule delete priority 100") {
		t.Error("devices' traffic should leave the tunnel table during login mode")
	}
	dnsmasq := e.readFile(e.paths.DnsmasqConfig())
	if strings.Contains(dnsmasq, "address=/captive.apple.com/") || !strings.Contains(dnsmasq, "server=127.0.0.53") {
		t.Errorf("during login mode, DNS should be the hotel's and not answered locally:\n%s", dnsmasq)
	}
	if left := e.agent.PortalStatus().SecondsLeft; left != 600 {
		t.Errorf("seconds left = %d, want 600", left)
	}

	// The owner logs in: the next check passes and login mode ends.
	p.result = portal.Result{State: portal.Open}
	c.advance(5 * time.Second)
	e.agent.CheckPortal(p.probe)
	if e.agent.PortalStatus().LoginMode {
		t.Fatal("login mode should end once the check passes")
	}
	if got := e.firewallEgress(); got != `elements = { "wg0" }` {
		t.Errorf("after login mode, egress = %s, want only wg0", got)
	}
}

func TestPortal_LoginModeEndsAfterTheTimeLimit(t *testing.T) {
	e, c, p := newPortalEnv(t)
	if err := e.agent.StartLoginMode(); err != nil {
		t.Fatal(err)
	}
	// Extending twice reaches the 30-minute limit; a third time can't
	// go beyond it.
	for i := 0; i < 3; i++ {
		if err := e.agent.ExtendLoginMode(); err != nil {
			t.Fatal(err)
		}
	}
	if left := e.agent.PortalStatus().SecondsLeft; left != 1800 {
		t.Errorf("seconds left = %d, want 1800 (the maximum)", left)
	}

	c.advance(MaxLoginWindow + time.Second)
	e.agent.CheckPortal(p.probe) // still a login page
	if e.agent.PortalStatus().LoginMode {
		t.Fatal("login mode must end when time is up")
	}
	if got := e.firewallEgress(); got != `elements = { "wg0" }` {
		t.Errorf("egress = %s, want only wg0", got)
	}
}

func TestPortal_CannotStartWithoutAReason(t *testing.T) {
	// No VPN: devices reach the login page by themselves.
	e := newClaimedEnv(t, false)
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	p := &fakeProbe{result: portal.Result{State: portal.Portal}}
	e.agent.CheckPortal(p.probe)
	if err := e.agent.StartLoginMode(); !errors.Is(err, ErrWrongMode) {
		t.Errorf("got %v, want ErrWrongMode", err)
	}
	if !e.agent.PortalStatus().Detected {
		t.Error("the login page should still be reported")
	}
}

func TestPortal_ChecksAgainWhenTheUplinkChanges(t *testing.T) {
	e, c, p := newPortalEnv(t)
	calls := p.calls
	e.agent.CheckPortal(p.probe)
	if p.calls != calls {
		t.Fatal("no new check should run right away")
	}
	// Move to another network.
	e.runner.outputs["ip -json route show default"] = `[]`
	e.check()
	c.advance(5 * time.Second)
	e.fakeUplink("wlan0", "192.168.9.20", "24", false)
	e.check()
	e.agent.CheckPortal(p.probe)
	if p.calls != calls+1 {
		t.Error("a new uplink connection should be checked at once")
	}
}
