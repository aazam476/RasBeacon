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

	if err := e.agent.ImportVPNProfile(testWireGuardConf, "home.conf"); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.SetVPNSwitches(api.VPNSwitches{Enabled: true, KillSwitch: killSwitch}); err != nil {
		t.Fatal(err)
	}
	return e, c
}

// profileDetails returns a profile's form as the VPN page fills it in.
func (e *testEnv) profileDetails(name string) api.VPNProfileDetails {
	e.t.Helper()
	for _, p := range e.agent.VPNSettings().Profiles {
		if p.Name == name {
			return api.VPNProfileDetails{VPNProfile: p}
		}
	}
	e.t.Fatalf("no profile %q", name)
	return api.VPNProfileDetails{}
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
	err := e.agent.ImportVPNProfile(strings.Replace(testWireGuardConf, "DNS = 10.66.0.1", "DNS = 10.66.0.1\nPostUp = curl x | sh", 1), "")
	var verr *config.ValidationError
	if !errors.As(err, &verr) || !strings.Contains(verr.Message, "never runs commands") {
		t.Fatalf("got %v", err)
	}
}

func TestVPN_TurningOnNeedsASetUpServer(t *testing.T) {
	e := newClaimedEnv(t, false)
	err := e.agent.SetVPNSwitches(api.VPNSwitches{Enabled: true, KillSwitch: true})
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
	if err := e.agent.SetVPNSwitches(api.VPNSwitches{Enabled: false, KillSwitch: true}); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("ip link delete dev wg0") {
		t.Error("wg0 should be removed")
	}
	if got := e.firewallEgress(); got != `elements = { "wlan0" }` {
		t.Errorf("egress = %s, want the uplink", got)
	}
	// The imported settings are kept for next time.
	if s := e.agent.VPNSettings(); s.ActiveProfile != "home.conf" || len(s.Profiles) != 1 {
		t.Error("turning the VPN off must not forget the server")
	}
}

func TestVPN_SecretsStayHidden(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	if strings.Contains(e.readFile(e.paths.ConfigFile()), "yAnz5TF") {
		t.Error("the private key belongs in secrets.json only")
	}
	s := e.agent.VPNSettings().Profiles[0]
	if s.PublicKey != "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=" {
		t.Errorf("settings should show the public key only: %+v", s)
	}
	e.runner.outputs["wg show"] = "private key: yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=\n"
	if strings.Contains(e.agent.Diagnostics(), "yAnz5TF") {
		t.Error("diagnostics must never contain the private key")
	}
}

func TestVPN_GenerateKey(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	if err := e.agent.GenerateVPNKey("home.conf"); err != nil {
		t.Fatal(err)
	}
	if key := e.agent.VPNSettings().Profiles[0].PublicKey; len(key) != 44 || key == "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=" {
		t.Errorf("a new public key should be shown after generating a key, got %q", key)
	}
	var ve *config.ValidationError
	if err := e.agent.GenerateVPNKey("nope.conf"); !errors.As(err, &ve) {
		t.Errorf("unknown profile: got %v", err)
	}
}

func TestVPN_RoutedIPv6Network(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	// The tests' fake device has IPv6 switched on.
	mustWrite(t, e.paths.ProcSysNetIPv6()+"/conf/all/forwarding", "1")

	settings := e.profileDetails("home.conf")
	settings.RoutedIPv6Prefix = "2001:db8:1234:5678::/64"
	var ve *config.ValidationError
	if err := e.agent.SaveVPNProfile(settings); !errors.As(err, &ve) || ve.Field != "routedIpv6Prefix" {
		t.Fatalf("a VPN without IPv6 can't carry a routed IPv6 network, got %v", err)
	}

	ipv6Conf := strings.Replace(testWireGuardConf, "Address = 10.66.0.2/32", "Address = 10.66.0.2/32, fd66::2/128", 1)
	if err := e.agent.ImportVPNProfile(ipv6Conf, "home.conf"); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.SetActiveVPNProfile("home-2.conf"); err != nil {
		t.Fatal(err)
	}
	settings = e.profileDetails("home-2.conf")
	settings.RoutedIPv6Prefix = "2001:db8:1234:5678::/64"
	if err := e.agent.SaveVPNProfile(settings); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("ip -6 address replace 2001:db8:1234:5678::1/64 dev br-lan") {
		t.Error("RasBeacon should take the first address of the routed network on the hotspot")
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), `oifname "wg0" ip6 saddr 2001:db8:1234:5678::/64 accept`) {
		t.Error("the routed network must not be NATed in the tunnel")
	}
	if got := e.agent.Status().VPN.RoutedIPv6Prefix; got != "2001:db8:1234:5678::/64" {
		t.Errorf("status shows %q", got)
	}

	// Turning the VPN off takes the network off the hotspot again.
	if err := e.agent.SetVPNSwitches(api.VPNSwitches{Enabled: false, KillSwitch: true}); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("ip -6 address del 2001:db8:1234:5678::1/64 dev br-lan") {
		t.Error("the routed network should be removed when the VPN is off")
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "2001:db8") {
		t.Error("no routed-network rule without the VPN")
	}

	// Bad values are explained.
	for _, text := range []string{"2001:db8::/48", "fd00:1:2:3::/64", "not a network"} {
		settings.RoutedIPv6Prefix = text
		if err := e.agent.SaveVPNProfile(settings); !errors.As(err, &ve) || ve.Field != "routedIpv6Prefix" {
			t.Errorf("%s: got %v", text, err)
		}
	}
}

func TestVPN_Profiles(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	other := strings.Replace(testWireGuardConf, "vpn.example.com", "office.example.com", 1)
	if err := e.agent.ImportVPNProfile(other, "office.conf"); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.ImportVPNProfile(other, "office.conf"); err != nil {
		t.Fatal(err)
	}
	s := e.agent.VPNSettings()
	if s.ActiveProfile != "home.conf" || len(s.Profiles) != 3 || s.Profiles[2].Name != "office-2.conf" {
		t.Fatalf("importing keeps the profile in use and makes names unique: %+v", s)
	}

	// Switching starts a fresh tunnel with the other server.
	e.runner.commands = nil
	if err := e.agent.SetActiveVPNProfile("office.conf"); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("ip link delete dev wg0") || !strings.Contains(e.readFile(e.paths.WireGuardConfig()), "office.example.com") {
		t.Errorf("the tunnel should be rebuilt for office.conf; commands: %v", e.runner.commands)
	}
	if got := e.firewallEgress(); got != `elements = { "wg0" }` {
		t.Errorf("devices' traffic must stay in the tunnel while switching, egress = %s", got)
	}

	// Editing a profile that isn't in use doesn't touch the tunnel.
	details := e.profileDetails("home.conf")
	details.Endpoint = "home.example.net:51820"
	e.runner.commands = nil
	if err := e.agent.SaveVPNProfile(details); err != nil {
		t.Fatal(err)
	}
	if e.runner.ran("ip link delete") || strings.Contains(e.readFile(e.paths.WireGuardConfig()), "home.example.net") {
		t.Error("editing another profile must not change the tunnel")
	}
	var ve *config.ValidationError
	details.Name = "missing.conf"
	if err := e.agent.SaveVPNProfile(details); !errors.As(err, &ve) {
		t.Errorf("unknown profile: got %v", err)
	}

	// Removing the profile in use falls back to the first one left; its
	// keys go too.
	if err := e.agent.RemoveVPNProfile("office.conf"); err != nil {
		t.Fatal(err)
	}
	s = e.agent.VPNSettings()
	if s.ActiveProfile != "home.conf" || len(s.Profiles) != 2 || !s.Enabled {
		t.Errorf("after removing: %+v", s)
	}
	if _, kept := e.agent.secrets.WireGuardKeys["office.conf"]; kept {
		t.Error("a removed profile's keys must be deleted")
	}
	// Removing the last one turns the VPN off.
	for _, name := range []string{"home.conf", "office-2.conf"} {
		if err := e.agent.RemoveVPNProfile(name); err != nil {
			t.Fatal(err)
		}
	}
	s = e.agent.VPNSettings()
	if s.Enabled || s.ActiveProfile != "" || len(s.Profiles) != 0 {
		t.Errorf("without profiles: %+v", s)
	}
	if got := e.firewallEgress(); got != `elements = { "wlan0" }` {
		t.Errorf("egress = %s, want the uplink", got)
	}
}

func TestVPN_Version2SettingsBecomeAProfile(t *testing.T) {
	e := newClaimedEnv(t, false)
	// A config.json and secrets.json as version 2 wrote them.
	var raw map[string]any
	if err := config.ReadJSON(e.paths.ConfigFile(), &raw); err != nil {
		t.Fatal(err)
	}
	raw["version"] = 2
	raw["vpn"] = map[string]any{
		"enabled": true, "killSwitch": true,
		"addresses": []string{"10.66.0.2/32"}, "dns": []string{"10.66.0.1"}, "mtu": 0,
		"peer":             map[string]any{"publicKey": "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=", "endpoint": "vpn.example.com:51820", "allowedIps": []string{"0.0.0.0/0"}},
		"routedIpv6Prefix": "",
	}
	if err := config.WriteJSON(e.paths.ConfigFile(), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	var secrets map[string]any
	if err := config.ReadJSON(e.paths.SecretsFile(), &secrets); err != nil {
		t.Fatal(err)
	}
	secrets["wireguardPrivateKey"] = "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	if err := config.WriteJSON(e.paths.SecretsFile(), secrets, 0o600); err != nil {
		t.Fatal(err)
	}

	restarted := New(e.paths, e.runner)
	if err := restarted.loadOrCreateSettings(); err != nil {
		t.Fatal(err)
	}
	s := restarted.VPNSettings()
	if !s.Enabled || s.ActiveProfile != "wireguard.conf" || len(s.Profiles) != 1 ||
		s.Profiles[0].Endpoint != "vpn.example.com:51820" || s.Profiles[0].PublicKey != "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=" {
		t.Errorf("after the upgrade: %+v", s)
	}
	if text := e.readFile(e.paths.SecretsFile()); strings.Contains(text, "wireguardPrivateKey") || !strings.Contains(text, `"wireguard.conf"`) {
		t.Errorf("the key should have moved to the profile:\n%s", text)
	}
	if !strings.Contains(e.readFile(e.paths.ConfigFile()), `"version": 3`) {
		t.Error("the upgraded settings should be saved")
	}
}
