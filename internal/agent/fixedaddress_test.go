package agent

import (
	"errors"
	"strings"
	"testing"

	"rasbeacon/internal/config"
)

// A fixed address is saved, handed out by dnsmasq, shown on the device,
// and removed again with an empty address.
func TestFixedAddress_SavedAndHandedOut(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.SetFixedAddress("3A:1F:22:AA:BB:CC", "10.87.0.50"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "dhcp-host=3a:1f:22:aa:bb:cc,10.87.0.50") {
		t.Errorf("dnsmasq.conf has no dhcp-host line:\n%s", e.readFile(e.paths.DnsmasqConfig()))
	}
	if got := e.agent.cfg.Clients.FixedAddresses["3a:1f:22:aa:bb:cc"]; got != "10.87.0.50" {
		t.Errorf("saved %q", got)
	}
	if err := e.agent.SetFixedAddress("3a:1f:22:aa:bb:cc", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "dhcp-host") {
		t.Error("the fixed address should be gone from dnsmasq.conf")
	}
}

// Each address that can't work is refused with what to do instead.
func TestFixedAddress_Rules(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.SetClientName("8e:00:11:22:33:44", "Pixel-8"); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.SetFixedAddress("8e:00:11:22:33:44", "10.87.0.60"); err != nil {
		t.Fatal(err)
	}
	for address, want := range map[string]string{
		"not an address": "Enter an address like 10.87.0.2.",
		"fe80::1":        "Enter an address like 10.87.0.2.",
		"192.168.1.20":   "Use an address from 10.87.0.1 to 10.87.0.254.",
		"10.87.0.255":    "Use an address from 10.87.0.1 to 10.87.0.254.",
		"10.87.0.1":      "That is RasBeacon's own address.",
		"10.87.0.60":     "Pixel-8 already uses this address.",
	} {
		err := e.agent.SetFixedAddress("3a:1f:22:aa:bb:cc", address)
		var verr *config.ValidationError
		if !errors.As(err, &verr) || verr.Field != "address" || verr.Message != want {
			t.Errorf("%s: got %v, want %q", address, err, want)
		}
	}
	if err := e.agent.SetFixedAddress("not a mac", "10.87.0.70"); err == nil {
		t.Error("a broken MAC address must be refused")
	}
	// The same device may keep (save again) its own address.
	if err := e.agent.SetFixedAddress("8e:00:11:22:33:44", "10.87.0.60"); err != nil {
		t.Errorf("saving a device's own address again: %v", err)
	}
}

// When RasBeacon moves the hotspot's network (the uplink uses the same
// one), addresses that no longer fit are left out of dnsmasq's file, not
// given out on the wrong network.
func TestFixedAddress_LeftOutWhenTheNetworkMoves(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	e.agent.cfg.Clients.FixedAddresses = map[string]string{"3a:1f:22:aa:bb:cc": "10.87.0.50", "8e:00:11:22:33:44": "10.99.0.5"}
	got := e.agent.fixedAddresses()
	if len(got) != 1 || got[0].IP != "10.87.0.50" {
		t.Errorf("fixed addresses = %+v, want only the one on the current network", got)
	}
}

func TestStatus_ShowsADevicesFixedAddress(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.runner.outputs["iw dev ap0 station dump"] = "Station 3a:1f:22:aa:bb:cc (on ap0)\n\tsignal:\t-48 dBm\n"
	if err := e.agent.SetFixedAddress("3a:1f:22:aa:bb:cc", "10.87.0.50"); err != nil {
		t.Fatal(err)
	}
	clients := e.agent.Status().Clients
	if len(clients) != 1 || clients[0].FixedAddress != "10.87.0.50" {
		t.Errorf("clients = %+v, want the device with its fixed address", clients)
	}
}
