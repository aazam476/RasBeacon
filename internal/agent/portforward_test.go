package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// portForwardEnv is a set-up RasBeacon online through Wi-Fi with address
// address, and a phone with a fixed address.
func portForwardEnv(t *testing.T, address string) *testEnv {
	t.Helper()
	e := newClaimedEnv(t, false)
	if err := e.agent.SetFixedAddress("3a:1f:22:aa:bb:cc", "10.87.0.50"); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	e.agent.uplink.status.Connected = true
	e.agent.uplink.status.Address = address + "/24"
	e.agent.mu.Unlock()
	return e
}

func rule(name string, port int) api.PortForwardRule {
	return api.PortForwardRule{Name: name, Protocol: "tcp", OutsidePort: port, MAC: "3a:1f:22:aa:bb:cc", Enabled: true}
}

func TestPortForwarding_SavedAndInTheFirewall(t *testing.T) {
	e := portForwardEnv(t, "203.0.113.20")
	web := rule("Web server", 8080)
	web.InsidePort = 80
	game := rule("Game", 27015)
	game.Protocol, game.OutsidePortEnd = "both", 27030
	if err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: []api.PortForwardRule{web, game}}); err != nil {
		t.Fatal(err)
	}
	rules := e.readFile(e.paths.FirewallRules())
	for _, want := range []string{
		"tcp dport 8080 ct mark set 0x5246 dnat ip to 10.87.0.50:80",
		"tcp dport 27015-27030 ct mark set 0x5246 dnat ip to 10.87.0.50",
		"udp dport 27015-27030 ct mark set 0x5246 dnat ip to 10.87.0.50",
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("the firewall has no %q", want)
		}
	}

	s := e.agent.PortForwardingSettings()
	if s.OutsideAddress != "203.0.113.20" || s.Reachability != "public" || len(s.Rules) != 2 {
		t.Fatalf("settings = %+v", s)
	}
	if r := s.Rules[0]; r.ID == "" || r.Address != "10.87.0.50" || r.Target != "3a:1f:22:aa:bb:cc" || r.InsidePort != 80 {
		t.Errorf("rule = %+v", r)
	}

	// A disabled rule stays saved but leaves the firewall.
	saved := s.Rules
	saved[0].Enabled = false
	if err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: saved}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "dport 8080") {
		t.Error("a disabled rule must not be in the firewall")
	}
	if got := e.agent.PortForwardingSettings().Rules[0].ID; got != saved[0].ID {
		t.Errorf("the rule's ID changed from %s to %s", saved[0].ID, got)
	}
}

// Moving the device's fixed address moves its rules with it.
func TestPortForwarding_FollowsTheFixedAddress(t *testing.T) {
	e := portForwardEnv(t, "203.0.113.20")
	if err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: []api.PortForwardRule{rule("Web", 8080)}}); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.SetFixedAddress("3a:1f:22:aa:bb:cc", "10.87.0.60"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), "dnat ip to 10.87.0.60") {
		t.Error("the rule should follow the device to its new address")
	}
	// Without a fixed address the rule does nothing (and says so).
	if err := e.agent.SetFixedAddress("3a:1f:22:aa:bb:cc", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "dport 8080") {
		t.Error("a rule without an address must not be in the firewall")
	}
	if got := e.agent.PortForwardingSettings().Rules[0].Address; got != "" {
		t.Errorf("address = %q, want none", got)
	}
}

func TestPortForwarding_Rules(t *testing.T) {
	e := portForwardEnv(t, "203.0.113.20")
	change := func(f func(r *api.PortForwardRule)) []api.PortForwardRule {
		r := rule("Web", 8080)
		f(&r)
		return []api.PortForwardRule{r}
	}
	for field, rules := range map[string][]api.PortForwardRule{
		"name":        change(func(r *api.PortForwardRule) { r.Name = " " }),
		"protocol":    change(func(r *api.PortForwardRule) { r.Protocol = "sctp" }),
		"outsidePort": change(func(r *api.PortForwardRule) { r.OutsidePort = 70000 }),
		"insidePort":  change(func(r *api.PortForwardRule) { r.InsidePort = -1 }),
		"target":      change(func(r *api.PortForwardRule) { r.MAC = "8e:00:11:22:33:44" }), // no fixed address
	} {
		err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: rules})
		var verr *config.ValidationError
		if !errors.As(err, &verr) || verr.Field != field {
			t.Errorf("%s: got %v", field, err)
		}
	}
	for want, rules := range map[string][]api.PortForwardRule{
		"must end on a port after": change(func(r *api.PortForwardRule) { r.OutsidePortEnd = 8000 }),
		"A range keeps its ports":  change(func(r *api.PortForwardRule) { r.OutsidePortEnd = 8090; r.InsidePort = 80 }),
		"UDP ports 67 and 68":      change(func(r *api.PortForwardRule) { r.Protocol, r.OutsidePort = "udp", 68 }),
		"Choose a device or":       change(func(r *api.PortForwardRule) { r.MAC, r.Plugin = "", "no-such-plugin" }),
		`"Web" already forwards`:   {rule("Web", 8080), func() api.PortForwardRule { r := rule("Other", 8000); r.OutsidePortEnd = 8100; return r }()},
	} {
		err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: rules})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
	// A TCP rule and a UDP rule may share a port.
	udp := rule("Voice", 8080)
	udp.Protocol = "udp"
	if err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: []api.PortForwardRule{rule("Web", 8080), udp}}); err != nil {
		t.Errorf("TCP and UDP on the same port: %v", err)
	}
	// So may two rules when one of them is off.
	off := rule("Old", 8080)
	off.Enabled = false
	if err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: []api.PortForwardRule{rule("Web", 8080), off}}); err != nil {
		t.Errorf("an off rule on the same port: %v", err)
	}
	too := make([]api.PortForwardRule, maxPortForwards+1)
	if err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: too}); err == nil {
		t.Error("more than the maximum must be refused")
	}
}

func TestPortForwarding_RiskyPortsAreWarnedAbout(t *testing.T) {
	e := portForwardEnv(t, "203.0.113.20")
	ssh := rule("SSH", 22)
	ranged := rule("Range", 3380)
	ranged.OutsidePortEnd = 3390
	if err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: []api.PortForwardRule{ssh, ranged, rule("Web", 8080)}}); err != nil {
		t.Fatal(err)
	}
	rules := e.agent.PortForwardingSettings().Rules
	if !strings.Contains(rules[0].Warning, "Port 22 (SSH)") || !strings.Contains(rules[1].Warning, "Port 3389 (Remote Desktop)") || rules[2].Warning != "" {
		t.Errorf("warnings: %q, %q, %q", rules[0].Warning, rules[1].Warning, rules[2].Warning)
	}
}

func TestPortForwarding_Reachability(t *testing.T) {
	for address, want := range map[string]string{
		"203.0.113.20": "public", "192.168.1.23": "private", "10.0.0.5": "private", "100.72.3.4": "shared", "": "offline",
	} {
		if got := reachability(address); got != want {
			t.Errorf("reachability(%q) = %q, want %q", address, got, want)
		}
	}
	e := newClaimedEnv(t, false)
	if got := e.agent.PortForwardingSettings(); got.Reachability != "offline" || got.OutsideAddress != "" {
		t.Errorf("without an uplink: %+v", got)
	}
}

// A rule for a plugin goes to the plugin's address.
func TestPortForwarding_ToAPlugin(t *testing.T) {
	e := newPluginEnv(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), key), nil); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	e.agent.uplink.status.Connected = true
	address := e.agent.cfg.Plugins.Installed["adguard-home"].Address.String()
	e.agent.mu.Unlock()
	r := api.PortForwardRule{Name: "AdGuard", Protocol: "tcp", OutsidePort: 3000, Plugin: "adguard-home", Enabled: true}
	if err := e.agent.UpdatePortForwarding(api.PortForwardingSettings{Rules: []api.PortForwardRule{r}}); err != nil {
		t.Fatal(err)
	}
	got := e.agent.PortForwardingSettings().Rules[0]
	if got.Address != address || got.Target != "AdGuard Home" {
		t.Errorf("rule = %+v, want the plugin at %s", got, address)
	}
}
