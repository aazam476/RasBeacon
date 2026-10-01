package agent

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// testWireGuardConf is a complete WireGuard file (keys from the WireGuard
// documentation's examples).
const testWireGuardConf = `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.66.0.2/32
DNS = 10.66.0.1

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0, ::/0
`

// fakeClock is a fake time the test moves forward by hand.
type fakeClock struct{ now time.Time }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// newVPNEnv returns a claimed device with the test WireGuard file imported
// and the VPN turned on with the given kill switch setting.
func newVPNEnv(t *testing.T, killSwitch bool) (*testEnv, *fakeClock) {
	t.Helper()
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	e.setHandshake(time.Time{}) // no handshake yet

	if err := e.agent.ImportVPNConfig(testWireGuardConf); err != nil {
		t.Fatal(err)
	}
	settings := e.agent.VPNSettings()
	settings.Enabled, settings.KillSwitch = true, killSwitch
	if err := e.agent.UpdateVPNSettings(settings); err != nil {
		t.Fatal(err)
	}
	return e, c
}

// setHandshake fakes `wg show wg0 dump` with the given last handshake.
func (e *testEnv) setHandshake(at time.Time) {
	seconds := int64(0)
	if !at.IsZero() {
		seconds = at.Unix()
	}
	e.runner.outputs["wg show wg0 dump"] = "priv\tpub\t0\toff\n" +
		fmt.Sprintf("xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\t(none)\t203.0.113.5:51820\t0.0.0.0/0,::/0\t%d\t100\t200\t25\n", seconds)
}

// checkFor runs the uplink check every 5 seconds for d.
func (e *testEnv) checkFor(c *fakeClock, d time.Duration) {
	for end := c.now.Add(d); c.now.Before(end); c.advance(5 * time.Second) {
		e.check()
	}
}

func (e *testEnv) firewallEgress() string {
	rules := e.readFile(e.paths.FirewallRules())
	for _, line := range strings.Split(rules, "\n") {
		if strings.Contains(line, "elements = {") {
			return strings.TrimSpace(line)
		}
	}
	return "(nothing forwarded)"
}

func TestVPN_ImportRefusesUnsafeFile(t *testing.T) {
	e := newClaimedEnv(t, false)
	err := e.agent.ImportVPNConfig(strings.Replace(testWireGuardConf, "DNS = 10.66.0.1", "DNS = 10.66.0.1\nPostUp = curl x | sh", 1))
	var verr *config.ValidationError
	if !errors.As(err, &verr) || !strings.Contains(verr.Message, "never runs commands") {
		t.Fatalf("got %v", err)
	}
}

func TestVPN_TurningOnNeedsASetUpServer(t *testing.T) {
	e := newClaimedEnv(t, false)
	err := e.agent.UpdateVPNSettings(api.VPNSettings{Enabled: true, KillSwitch: true})
	var verr *config.ValidationError
	if !errors.As(err, &verr) || verr.Field != "enabled" {
		t.Fatalf("got %v", err)
	}
}

func TestVPN_KillSwitchOn_DevicesOnlyUseTheTunnel(t *testing.T) {
	e, c := newVPNEnv(t, true)

	if got := e.firewallEgress(); got != `elements = { "wg0" }` {
		t.Errorf("firewall egress = %s, want only wg0", got)
	}
	if !e.runner.ran("ip -4 rule add priority 100 iif br-lan lookup 51820") {
		t.Error("devices' traffic should be routed into the tunnel table")
	}
	dnsmasq := e.readFile(e.paths.DnsmasqConfig())
	if !strings.Contains(dnsmasq, "server=10.66.0.1") || strings.Contains(dnsmasq, "server=127.0.0.53") {
		t.Errorf("dnsmasq must ask only the VPN's DNS server:\n%s", dnsmasq)
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), `ip daddr { 10.66.0.1 } oifname != "wg0" reject`) {
		t.Error("the VPN's DNS server must only be reachable through the tunnel")
	}
	info, err := os.Stat(e.paths.WireGuardConfig())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("wg0.conf must exist and be root-only: %v", err)
	}

	// The server never answers: an hour later, still tunnel-only.
	e.checkFor(c, time.Hour)
	if got := e.firewallEgress(); got != `elements = { "wg0" }` {
		t.Errorf("after an hour without the tunnel, egress = %s; the kill switch must hold", got)
	}
	if !strings.Contains(strings.Join(e.agent.Status().Warnings, " "), "kill switch is on") {
		t.Error("the status page should explain why there is no internet")
	}

	// The server answers: state "up".
	e.setHandshake(c.now)
	e.check()
	if got := e.agent.Status().VPN.State; got != "up" {
		t.Errorf("state = %s, want up", got)
	}
}

func TestVPN_KillSwitchOff_FallsBackToUplinkAndReturns(t *testing.T) {
	e, c := newVPNEnv(t, false)
	e.setHandshake(c.now)
	e.check()
	if e.agent.Status().VPN.State != "up" {
		t.Fatal("tunnel should be up")
	}

	// The server stops answering. The handshake gets old, then after the
	// fallback delay traffic goes direct.
	e.checkFor(c, 3*time.Minute+45*time.Second)
	if got := e.agent.Status().VPN.State; got != "direct" {
		t.Fatalf("state = %s, want direct", got)
	}
	if got := e.firewallEgress(); got != `elements = { "wlan0" }` {
		t.Errorf("egress = %s, want the uplink", got)
	}
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "server=127.0.0.53") {
		t.Error("in direct mode dnsmasq should use the uplink's DNS")
	}
	if !strings.Contains(strings.Join(e.agent.Status().Warnings, " "), "WITHOUT the VPN") {
		t.Error("the status page must say traffic is not protected")
	}

	// The server answers again: back into the tunnel after the delay.
	for i := 0; i < 30; i++ {
		e.setHandshake(c.now)
		e.check()
		c.advance(5 * time.Second)
	}
	if got := e.firewallEgress(); got != `elements = { "wg0" }` {
		t.Errorf("egress = %s, want wg0 again", got)
	}
}

func TestVPN_TurningOffRemovesTheTunnel(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	settings := e.agent.VPNSettings()
	settings.Enabled = false
	if err := e.agent.UpdateVPNSettings(settings); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("ip link delete dev wg0") {
		t.Error("wg0 should be removed")
	}
	if got := e.firewallEgress(); got != `elements = { "wlan0" }` {
		t.Errorf("egress = %s, want the uplink", got)
	}
	// The imported settings are kept for next time.
	if !e.agent.VPNSettings().Configured {
		t.Error("turning the VPN off must not forget the server")
	}
}

func TestVPN_SecretsStayHidden(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	if strings.Contains(e.readFile(e.paths.ConfigFile()), "yAnz5TF") {
		t.Error("the private key belongs in secrets.json only")
	}
	s := e.agent.VPNSettings()
	if s.PrivateKey != "" || s.PublicKey != "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=" {
		t.Errorf("settings should show the public key only: %+v", s)
	}
	e.runner.outputs["wg show"] = "private key: yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=\n"
	if strings.Contains(e.agent.Diagnostics(), "yAnz5TF") {
		t.Error("diagnostics must never contain the private key")
	}
}

func TestVPN_GenerateKey(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.GenerateVPNKey(); err != nil {
		t.Fatal(err)
	}
	if len(e.agent.VPNSettings().PublicKey) != 44 {
		t.Error("a public key should be shown after generating a key")
	}
}
