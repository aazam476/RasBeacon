package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/config"
)

func TestClients_PauseAllowDisconnectAndFlush(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	e.runner.outputs["iw dev ap0 station dump"] = "Station 3a:1f:22:aa:bb:cc (on ap0)\n\tsignal:\t-48 dBm\n"
	mustWrite(t, e.paths.DnsmasqLeases(), "1790800000 3a:1f:22:aa:bb:cc 10.87.0.23 Alis-iPhone *\n")

	// Pausing: the firewall stops forwarding from the device, also after a
	// restart (it is in the settings).
	if err := e.agent.SetClientInternetAccess("3A:1F:22:AA:BB:CC", false); err != nil {
		t.Fatal(err)
	}
	if rules := e.readFile(e.paths.FirewallRules()); !strings.Contains(rules, "ether saddr { 3a:1f:22:aa:bb:cc } reject") {
		t.Errorf("paused device should be blocked:\n%s", rules)
	}
	var saved config.Config
	err := config.ReadJSON(e.paths.ConfigFile(), &saved)
	if err != nil || saved.Clients.Paused["3a:1f:22:aa:bb:cc"].IsZero() {
		t.Errorf("pause should be saved: %+v %v", saved.Clients, err)
	}
	c.advance(90 * time.Second)
	s := e.agent.StatusFor("10.87.0.23")
	if len(s.Clients) != 1 || s.Clients[0].InternetAccess || s.Clients[0].PausedSeconds != 90 || !s.Clients[0].IsThisDevice {
		t.Errorf("clients = %+v", s.Clients)
	}
	if e.agent.StatusFor("10.87.0.99").Clients[0].IsThisDevice || e.agent.Status().Clients[0].IsThisDevice {
		t.Error("only the viewer's own device is this device")
	}

	// Allowing it again.
	if err := e.agent.SetClientInternetAccess("3a:1f:22:aa:bb:cc", true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "ether saddr") {
		t.Error("device should be allowed again")
	}
	if cl := e.agent.Status().Clients[0]; !cl.InternetAccess || cl.PausedSeconds != 0 {
		t.Errorf("client = %+v", cl)
	}

	// Disconnecting and forgetting the address.
	if err := e.agent.DisconnectClient("3a:1f:22:aa:bb:cc"); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("hostapd_cli -p /run/hostapd -i ap0 deauthenticate 3a:1f:22:aa:bb:cc") {
		t.Errorf("commands: %v", e.runner.commands)
	}
	if err := e.agent.FlushClientLease("3a:1f:22:aa:bb:cc"); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("dhcp_release br-lan 10.87.0.23 3a:1f:22:aa:bb:cc") {
		t.Errorf("commands: %v", e.runner.commands)
	}

	// Mistakes.
	var invalid *config.ValidationError
	if err := e.agent.SetClientInternetAccess("not-a-mac", false); !errors.As(err, &invalid) {
		t.Errorf("got %v", err)
	}
	if err := e.agent.DisconnectClient("8e:00:11:22:33:44"); !errors.As(err, &invalid) {
		t.Errorf("a device that isn't connected: got %v", err)
	}
	if err := e.agent.FlushClientLease("8e:00:11:22:33:44"); !errors.As(err, &invalid) {
		t.Errorf("a device without a lease: got %v", err)
	}
}

func TestClients_DevicesOnTheEthernetClientPort(t *testing.T) {
	e := newClaimedEnv(t, true)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	mustWrite(t, e.paths.DnsmasqLeases(), "1790800000 a4:83:e7:12:34:56 10.87.0.31 Alis-MacBook *\n")
	e.runner.outputs["bridge -json fdb show br br-lan brport eth0"] =
		`[{"mac":"dc:a6:32:01:02:03","ifname":"eth0","master":"br-lan","state":"permanent"},` +
			`{"mac":"a4:83:e7:12:34:56","ifname":"eth0","master":"br-lan","state":""}]`

	// Not listed while the port isn't on the hotspot network.
	if n := len(e.agent.Status().Clients); n != 0 {
		t.Fatalf("%d clients before the port joined", n)
	}
	e.agent.mu.Lock()
	e.agent.uplink.ethernetInBridge = true
	e.agent.mu.Unlock()

	e.agent.Status()
	c.advance(2 * time.Minute)
	clients := e.agent.StatusFor("10.87.0.31").Clients
	if len(clients) != 1 {
		t.Fatalf("clients = %+v", clients)
	}
	laptop := clients[0]
	if !laptop.Wired || laptop.IP != "10.87.0.31" || laptop.Hostname != "Alis-MacBook" || laptop.Band != "" ||
		laptop.ConnectedSeconds != 120 || !laptop.InternetAccess || !laptop.IsThisDevice {
		t.Errorf("laptop = %+v", laptop)
	}

	// It can be paused, but not sent off like a Wi-Fi device.
	if err := e.agent.SetClientInternetAccess(laptop.MAC, false); err != nil {
		t.Fatal(err)
	}
	if e.agent.Status().Clients[0].InternetAccess {
		t.Error("the laptop should be paused")
	}
	var ve *config.ValidationError
	if err := e.agent.DisconnectClient(laptop.MAC); !errors.As(err, &ve) || !strings.Contains(ve.Message, "Ethernet cable") {
		t.Errorf("got %v", err)
	}
}

// A paused device's IPv6 addresses come from the neighbor table (there is
// no lease for them), so answers to its earlier connections stop there too.
func TestClients_PausedDeviceGetsNoIPv6AnswersEither(t *testing.T) {
	e := newClaimedEnv(t, false)
	mustWrite(t, e.paths.DnsmasqLeases(), "1790800000 3a:1f:22:aa:bb:cc 10.87.0.23 Alis-iPhone *\n")
	e.runner.outputs["ip -json -6 neigh show"] = `[
{"dst":"fd12:3456:789a:1::23","dev":"br-lan","lladdr":"3a:1f:22:aa:bb:cc","state":["REACHABLE"]},
{"dst":"fe80::381f:22ff:feaa:bbcc","dev":"br-lan","lladdr":"3a:1f:22:aa:bb:cc","state":["STALE"]},
{"dst":"fd12:3456:789a:1::99","dev":"br-lan","lladdr":"8e:00:11:22:33:44","state":["REACHABLE"]}]`

	if err := e.agent.SetClientInternetAccess("3a:1f:22:aa:bb:cc", false); err != nil {
		t.Fatal(err)
	}
	rules := e.readFile(e.paths.FirewallRules())
	if !strings.Contains(rules, "ip daddr { 10.87.0.23 } drop") || !strings.Contains(rules, "ip6 daddr { fd12:3456:789a:1::23 } drop") {
		t.Errorf("answers to the paused device's addresses should be dropped:\n%s", rules)
	}
	if strings.Contains(rules, "fe80::") || strings.Contains(rules, "::99") {
		t.Error("only the paused device's own addresses, without link-local ones (they never cross RasBeacon)")
	}
}

// One rule for MAC addresses (config.NormalizeMAC), the stricter one: a
// group (multicast) address is no device's, so it is refused everywhere.
func TestClients_GroupAddressesAreRefused(t *testing.T) {
	e := newClaimedEnv(t, false)
	for _, mac := range []string{"01:00:5e:00:00:01", "ff:ff:ff:ff:ff:ff"} {
		var refused *config.ValidationError
		if err := e.agent.SetClientName(mac, "Printer"); !errors.As(err, &refused) || refused.Field != "mac" {
			t.Errorf("naming %s = %v, want refused", mac, err)
		}
		if err := e.agent.SetClientInternetAccess(mac, false); !errors.As(err, &refused) {
			t.Errorf("pausing %s = %v, want refused", mac, err)
		}
	}
}
