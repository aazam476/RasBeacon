package agent

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
	"rasbeacon/internal/routing"
)

// iwInfoTwoAPs describes a radio that can broadcast up to 4 networks.
const iwInfoTwoAPs = iwInfoWithAP + "\tvalid interface combinations:\n" +
	"\t\t * #{ managed } <= 1, #{ AP } <= 4,\n\t\t   total <= 4, #channels <= 1\n"

// newGuestCapableEnv is a claimed device whose USB adapter can run the
// guest network.
func newGuestCapableEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	e.runner.outputs["iw phy phy1 info"] = iwInfoTwoAPs
	// The hotspot radio after it was renamed to ap0.
	mustWrite(t, filepath.Join(e.paths.SysClassNet(), "ap0", "address"), "50:3e:aa:12:34:56\n")
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	return e
}

var guestOn = api.GuestSettings{Enabled: true, SSID: "Ali's Guests", Password: "visitors-welcome"}

func TestGuest_TurningOnAddsSecondNetwork(t *testing.T) {
	e := newGuestCapableEnv(t)
	if s := e.agent.GuestSettings(); !s.Supported || s.Enabled || s.SSID != "Ali's Travel Wi-Fi-Guest" {
		t.Fatalf("unexpected defaults: %+v", s)
	}
	if err := e.agent.UpdateGuestSettings(guestOn); err != nil {
		t.Fatal(err)
	}

	hostapd := e.readFile(e.paths.HostapdConfig())
	for _, want := range []string{"bss=guest0", "bssid=52:3e:aa:12:34:56", "bridge=br-guest", "ssid2=" + hexOf("Ali's Guests"), "ap_isolate=1"} {
		if !strings.Contains(hostapd, want) {
			t.Errorf("hostapd.conf lacks %q", want)
		}
	}
	if !e.runner.ran("ip address replace 10.87.1.1/24 dev br-guest") {
		t.Error("the guest bridge should get RasBeacon's guest address")
	}
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "dhcp-range=set:guest,10.87.1.20,10.87.1.250") {
		t.Error("dnsmasq should hand out guest addresses")
	}
	rules := e.readFile(e.paths.FirewallRules())
	if !strings.Contains(rules, `iifname "br-guest" reject`) || !strings.Contains(rules, `"br-lan", "br-guest"`) {
		t.Errorf("the firewall should let guests out but not in:\n%s", rules)
	}

	// The password is never shown, and is hidden in diagnostics.
	if s := e.agent.GuestSettings(); !s.HasPassword || s.Password != "" {
		t.Errorf("unexpected settings: %+v", s)
	}
	if strings.Contains(e.agent.Diagnostics(), "visitors-welcome") {
		t.Error("diagnostics must not contain the guest password")
	}

	// Turning it off removes it again.
	off := guestOn
	off.Enabled, off.Password = false, ""
	if err := e.agent.UpdateGuestSettings(off); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.HostapdConfig()), "bss=guest0") {
		t.Error("the guest network should be gone")
	}
	if !e.runner.ran("ip link delete dev br-guest") {
		t.Error("the guest bridge should be removed")
	}
}

func TestGuest_RefusedWhenAdapterCanOnlyRunOneNetwork(t *testing.T) {
	e := newClaimedEnv(t, false) // iwInfoWithAP lists no combinations
	if e.agent.GuestSettings().Supported {
		t.Fatal("this adapter can't run two networks")
	}
	err := e.agent.UpdateGuestSettings(guestOn)
	var ve *config.ValidationError
	if !errors.As(err, &ve) || ve.Field != "enabled" {
		t.Fatalf("got %v, want a validation error for enabled", err)
	}
}

func TestGuest_Validation(t *testing.T) {
	e := newGuestCapableEnv(t)
	cases := map[string]api.GuestSettings{
		"ssid":     {Enabled: true, SSID: validClaim.HotspotSSID, Password: "visitors-welcome"},
		"password": {Enabled: true, SSID: "Guests", Password: validClaim.HotspotPassword},
	}
	for field, req := range cases {
		var ve *config.ValidationError
		if err := e.agent.UpdateGuestSettings(req); !errors.As(err, &ve) || ve.Field != field {
			t.Errorf("%s: got %v", field, err)
		}
	}
	noPassword := api.GuestSettings{Enabled: true, SSID: "Guests"}
	if err := e.agent.UpdateGuestSettings(noPassword); err == nil {
		t.Error("a guest network needs a password")
	}
}

func TestGuest_FollowsHotspotNetworkMove(t *testing.T) {
	e := newGuestCapableEnv(t)
	if err := e.agent.UpdateGuestSettings(guestOn); err != nil {
		t.Fatal(err)
	}
	e.fakeUplink("wlan0", "10.87.1.50", "24", false) // the hotel uses our guest network
	e.check()
	if !e.runner.ran("ip address replace 10.88.1.1/24 dev br-guest") {
		t.Error("the guest network should move along with the hotspot network")
	}
}

func TestGuest_TrafficRulesCoverBothNetworks(t *testing.T) {
	// The tunnel rules (routing) must cover exactly the networks the
	// firewall forwards (render), or guests could skip the VPN.
	if want := []string{render.BridgeName, render.GuestBridgeName}; !slices.Equal(routing.LANInterfaces, want) {
		t.Errorf("routing.LANInterfaces = %v, want %v", routing.LANInterfaces, want)
	}
}

func TestMetered_OnlyRestartsDHCP(t *testing.T) {
	e := newClaimedEnv(t, false)
	settings := e.agent.HotspotSettings()
	settings.Metered = true
	before := len(e.runner.commands)
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "dhcp-option=43,ANDROID_METERED") {
		t.Error("dnsmasq should send the metered hint")
	}
	for _, c := range e.runner.commands[before:] {
		if strings.Contains(c, "restart rasbeacon-hostapd") {
			t.Error("changing only the metered setting must not restart the hotspot")
		}
	}
	if !e.agent.Status().Hotspot.Metered || !e.agent.HotspotSettings().Metered {
		t.Error("the setting should be shown")
	}
}
