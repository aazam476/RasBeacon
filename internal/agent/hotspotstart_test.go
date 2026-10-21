package agent

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// Hotspot changes the radio can't run are refused, or undone, so the owner
// never loses the hotspot and the settings page with it (hotspotstart.go).

// newTwoPointFourEnv is a fake device whose USB adapter has no 5 GHz band.
func newTwoPointFourEnv(t *testing.T) *testEnv {
	e := newTestEnv(t)
	e.runner.outputs["iw phy phy1 info"] = iwInfoWithAP
	return e
}

func TestHotspot_5GHzRefusedOnARadioWithout5GHz(t *testing.T) {
	e := newTwoPointFourEnv(t)
	e.start()
	if !e.agent.SetupState().No5GHz {
		t.Error("setup should hear that 5 GHz isn't available")
	}
	claim := validClaim
	claim.HotspotBand = config.Band5GHz
	var refused *config.ValidationError
	if _, err := e.agent.Claim(claim); !errors.As(err, &refused) || refused.Field != "hotspot.band" {
		t.Fatalf("claim with 5 GHz = %v, want refused on the band", err)
	}

	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	settings := e.agent.HotspotSettings()
	if !settings.No5GHz {
		t.Error("the Hotspot page should hear that 5 GHz isn't available")
	}
	settings.Band = config.Band5GHz
	if err := e.agent.UpdateHotspotSettings(settings); !errors.As(err, &refused) || refused.Field != "hotspot.band" {
		t.Errorf("5 GHz = %v, want refused on the band", err)
	}
}

func TestHotspot_ChannelTheCountryForbidsIsRefused(t *testing.T) {
	e := newClaimedEnv(t, false)
	// Under the new country's rules, channel 149 is off.
	e.runner.outputs["iw phy phy1 info"] = iwInfoDualBand + "\t\t\t* 5745 MHz [149] (disabled)\n"
	settings := e.agent.HotspotSettings()
	settings.Band, settings.Channel, settings.Country = config.Band5GHz, 149, "JP"
	e.runner.commands = nil

	var refused *config.ValidationError
	if err := e.agent.UpdateHotspotSettings(settings); !errors.As(err, &refused) || refused.Field != "hotspot.channel" {
		t.Fatalf("a forbidden channel = %v, want refused on the channel", err)
	}
	if got := e.agent.HotspotSettings(); got.Country != "US" || got.Channel != 0 {
		t.Errorf("a refused change was kept: %+v", got)
	}
	// The new country was tried, then the old one set back.
	if got := strings.Join(e.runner.commands, "\n"); !strings.Contains(got, "iw reg set JP") || !strings.HasSuffix(got, "iw reg set US") {
		t.Errorf("commands:\n%s", got)
	}

	settings.Channel = 36 // allowed (not listed as disabled)
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Errorf("an allowed channel = %v", err)
	}
}

func TestHotspot_SettingsHostapdCantRunAreUndone(t *testing.T) {
	for name, outputs := range map[string]map[string]string{
		"hostapd stopped":   {"systemctl is-active " + hostapdService: "failed\n"},
		"systemd restarted": {"systemctl show --property=NRestarts --value " + hostapdService: "2\n"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newClaimedEnv(t, false)
			for command, output := range outputs {
				e.runner.outputs[command] = output
			}
			settings := e.agent.HotspotSettings()
			settings.SSID = "Too New"

			err := e.agent.UpdateHotspotSettings(settings)
			var refused *config.ValidationError
			if !errors.As(err, &refused) || !strings.Contains(refused.Message, "previous settings were put back") {
				t.Fatalf("UpdateHotspotSettings = %v, want the previous settings put back", err)
			}
			if got := e.agent.HotspotSettings().SSID; got != validClaim.HotspotSSID {
				t.Errorf("SSID = %q, want the old one", got)
			}
			if saved := e.readFile(e.paths.ConfigFile()); strings.Contains(saved, "Too New") {
				t.Error("the refused name was left in config.json")
			}
			// The name is written in hexadecimal in hostapd.conf.
			if hostapd := e.readFile(e.paths.HostapdConfig()); !strings.Contains(hostapd, "ssid2="+hex.EncodeToString([]byte(validClaim.HotspotSSID))) {
				t.Errorf("hostapd runs:\n%s", hostapd)
			}
		})
	}
}

func TestHotspot_SetupFallsBackTo2GHzWhenHostapdCantStart(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	claim := validClaim
	claim.HotspotBand = config.Band5GHz
	if _, err := e.agent.Claim(claim); err != nil {
		t.Fatal(err)
	}
	e.runner.outputs["systemctl is-active "+hostapdService] = "failed\n"
	e.timers.runAll()

	if got := e.agent.HotspotSettings().Band; got != config.Band2GHz {
		t.Errorf("band after a failed start = %s, want 2.4 GHz", got)
	}
	if !strings.Contains(e.readFile(e.paths.HostapdConfig()), "hw_mode=g") {
		t.Error("hostapd should run on 2.4 GHz")
	}
	if status := e.agent.Status(); !strings.Contains(strings.Join(status.Warnings, " "), "runs on 2.4 GHz") {
		t.Errorf("warnings = %q", status.Warnings)
	}
}

// After the adapter is swapped for one without 5 GHz, a saved 5 GHz
// hotspot still starts, on 2.4 GHz, and the Status page says why.
func TestHotspot_Saved5GHzRunsOn2GHzOnARadioWithout5GHz(t *testing.T) {
	e := newClaimedEnv(t, false)
	settings := e.agent.HotspotSettings()
	settings.Band, settings.Channel = config.Band5GHz, 44
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Fatal(err)
	}

	restarted := New(e.paths, e.runner)
	restarted.usbRadioWait = 0
	restarted.afterFunc = e.timers.afterFunc
	restarted.background = func(func()) {}
	e.runner.outputs["iw phy phy1 info"] = iwInfoWithAP
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	hostapd := e.readFile(e.paths.HostapdConfig())
	if !strings.Contains(hostapd, "hw_mode=g") || strings.Contains(hostapd, "channel=44") {
		t.Errorf("hostapd should run on 2.4 GHz's default channel:\n%s", hostapd)
	}
	if got := restarted.HotspotSettings(); got.Band != config.Band5GHz || !got.No5GHz {
		t.Errorf("the saved setting should stay 5 GHz: %+v", got)
	}
	warnings := strings.Join(restarted.Status().Warnings, " ")
	if restarted.mode != api.ModeNormal || !strings.Contains(warnings, "can't use 5 GHz") {
		t.Errorf("mode %s, warnings %q", restarted.mode, warnings)
	}
}
