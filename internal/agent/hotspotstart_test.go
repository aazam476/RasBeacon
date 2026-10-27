package agent

import (
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/regdb"
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

// The channels the Hotspot page offers come from the regulatory database:
// Japan has no channel 149, and a country without its own rules gets the
// world's (no 5 GHz hotspot).
func TestHotspot_ChannelsFollowTheCountry(t *testing.T) {
	e := newClaimedEnv(t, false)
	for country, want := range map[string]map[config.Band][]int{
		"US": {config.Band2GHz: {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, config.Band5GHz: {36, 40, 44, 48, 149, 153, 157, 161, 165}},
		"JP": {config.Band2GHz: {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, config.Band5GHz: {36, 40, 44, 48}},
	} {
		got, err := e.agent.HotspotChannels(api.HotspotChannelsRequest{Country: country})
		if err != nil || !reflect.DeepEqual(got.Channels, want) {
			t.Errorf("%s: %v, %v; want %v", country, got.Channels, err, want)
		}
	}
	var refused *config.ValidationError
	if _, err := e.agent.HotspotChannels(api.HotspotChannelsRequest{Country: "UK"}); !errors.As(err, &refused) || refused.Field != "hotspot.country" {
		t.Errorf("UK = %v, want refused (it's GB)", err)
	}

	// Saving refuses what the country doesn't allow.
	settings := e.agent.HotspotSettings()
	settings.Band, settings.Channel, settings.Country = config.Band5GHz, 149, "JP"
	if err := e.agent.UpdateHotspotSettings(settings); !errors.As(err, &refused) || refused.Field != "hotspot.channel" {
		t.Errorf("channel 149 in Japan = %v, want refused on the channel", err)
	}
	settings.Channel = 44
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Errorf("channel 44 in Japan = %v", err)
	}
}

// A valid country without its own rules gets the world's, which have no 5
// GHz hotspot: 5 GHz is refused there, and offered nowhere on the page.
func TestHotspot_5GHzRefusedWhereTheCountryHasNone(t *testing.T) {
	e := newClaimedEnv(t, false)
	got, _ := e.agent.HotspotChannels(api.HotspotChannelsRequest{Country: "AQ"})
	if len(got.Channels[config.Band5GHz]) != 0 || len(got.Channels[config.Band2GHz]) == 0 {
		t.Errorf("Antarctica: %v", got.Channels)
	}
	settings := e.agent.HotspotSettings()
	settings.Band, settings.Country = config.Band5GHz, "AQ"
	var refused *config.ValidationError
	if err := e.agent.UpdateHotspotSettings(settings); !errors.As(err, &refused) || refused.Field != "hotspot.band" {
		t.Errorf("5 GHz in Antarctica = %v, want refused on the band", err)
	}
}

// "Default" picks the band's usual channel, or the first the country
// allows when that one isn't (here, only the world's 2.4 GHz rules).
func TestHotspot_DefaultChannelIsOneTheCountryAllows(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	if got := e.agent.hotspotChannel(); got != 0 {
		t.Errorf("US, 2.4 GHz: channel %d, want the default (6)", got)
	}
	// A database where the band's default isn't allowed: only channel 1.
	e.agent.regdb = regdb.DB{"US": {{StartKHz: 2402000, EndKHz: 2422000, MaxWidthKHz: 20000}}}
	if got := e.agent.hotspotChannel(); got != 1 {
		t.Errorf("only channel 1 allowed: channel %d, want 1", got)
	}
}

// Without the database (an image without wireless-regdb), every offered
// channel is allowed: the radio's own check and the check after the
// restart still apply.
func TestHotspot_WithoutTheDatabaseEveryChannelIsOffered(t *testing.T) {
	e := newTestEnv(t)
	if err := os.Remove(e.paths.RegulatoryDB()); err != nil {
		t.Fatal(err)
	}
	e.start()
	got, _ := e.agent.HotspotChannels(api.HotspotChannelsRequest{Country: "JP"})
	if !slices.Contains(got.Channels[config.Band5GHz], 149) {
		t.Errorf("without the database: %v", got.Channels)
	}
	mustWrite(t, e.paths.RegulatoryDB(), "damaged")
	e.agent.loadRegulatoryDB()
	if e.agent.regdb != nil {
		t.Error("a damaged database must not be used")
	}
}

// iwInfoWithCMAC is a dual-band radio that can run a WPA3 hotspot (it has
// the cipher protected management frames need; see radio.SupportsWPA3AP).
const iwInfoWithCMAC = iwInfoDualBand + "\tSupported Ciphers:\n\t\t* CCMP-128 (00-0f-ac:4)\n\t\t* CMAC (00-0f-ac:6)\n"

func TestHotspot_WPA3OnlyIsRefusedOnARadioThatCantRunIt(t *testing.T) {
	e := newClaimedEnv(t, false) // its radio lists no CMAC cipher
	settings := e.agent.HotspotSettings()
	if !settings.NoWPA3 {
		t.Error("the Hotspot page should hear that WPA3 only isn't available")
	}
	settings.Security = config.SecurityWPA3
	var refused *config.ValidationError
	if err := e.agent.UpdateHotspotSettings(settings); !errors.As(err, &refused) || refused.Field != "hotspot.security" {
		t.Errorf("WPA3 only = %v, want refused on the security", err)
	}
}

// A WPA3-only hotspot moved to an adapter that can't run it (or restored
// from a backup) still starts, as WPA2/WPA3, and the saved choice stays.
func TestHotspot_SavedWPA3OnlyRunsAsWPA2WPA3OnARadioWithoutIt(t *testing.T) {
	e := newTestEnv(t)
	e.runner.outputs["iw phy phy1 info"] = iwInfoWithCMAC
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	settings := e.agent.HotspotSettings()
	if settings.NoWPA3 {
		t.Fatal("a radio with CMAC can run WPA3 only")
	}
	settings.Security = config.SecurityWPA3
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Fatal(err)
	}
	if hostapd := e.readFile(e.paths.HostapdConfig()); !strings.Contains(hostapd, "wpa_key_mgmt=SAE\n") {
		t.Fatalf("hostapd should run WPA3 only:\n%s", hostapd)
	}

	// The adapter is swapped for one without the cipher.
	e.runner.outputs["iw phy phy1 info"] = iwInfoDualBand
	restarted := New(e.paths, e.runner)
	restarted.afterFunc, restarted.usbRadioWait, restarted.background = e.timers.afterFunc, 0, func(func()) {}
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	if hostapd := e.readFile(e.paths.HostapdConfig()); strings.Contains(hostapd, "wpa_key_mgmt=SAE\n") {
		t.Errorf("hostapd must not run WPA3 only on this radio:\n%s", hostapd)
	}
	if got := restarted.HotspotSettings().Security; got != config.SecurityWPA3 {
		t.Errorf("saved security = %s, want WPA3 only kept for a capable adapter", got)
	}
	status := restarted.Status()
	if status.Hotspot.Security != config.SecurityWPA2WPA3 || !strings.Contains(strings.Join(status.Warnings, " "), "can't run WPA3 only") {
		t.Errorf("status = %s, warnings %q", status.Hotspot.Security, status.Warnings)
	}
}

// iwInfoWiFi6 is a dual-band radio that can run a Wi-Fi 6 hotspot.
const iwInfoWiFi6 = iwInfoDualBand + "\t\tHE Iftypes: managed, AP\n"

func TestHotspot_WiFiStandard(t *testing.T) {
	e := newClaimedEnv(t, true)
	// The adapter can't run Wi-Fi 6: the best is Wi-Fi 4 on 2.4 GHz.
	if s := e.agent.HotspotSettings(); s.Standard != config.StandardWiFi4 || !s.NoWiFi6 {
		t.Errorf("settings = %+v", s)
	}
	settings := e.agent.HotspotSettings()
	settings.Standard = config.StandardWiFi6
	var ve *config.ValidationError
	if err := e.agent.UpdateHotspotSettings(settings); !errors.As(err, &ve) || ve.Field != "hotspot.standard" {
		t.Errorf("Wi-Fi 6 without the adapter: %v", err)
	}
	settings.Standard = config.StandardWiFi5
	if err := e.agent.UpdateHotspotSettings(settings); !errors.As(err, &ve) || ve.Field != "hotspot.standard" {
		t.Errorf("Wi-Fi 5 on 2.4 GHz: %v", err)
	}

	// A Wi-Fi 6 adapter: the best by itself, without choosing.
	e.runner.outputs["iw phy phy1 info"] = iwInfoWiFi6
	restarted := New(e.paths, e.runner)
	restarted.usbRadioWait = 0
	restarted.afterFunc = e.timers.afterFunc
	restarted.background = func(func()) {}
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.readFile(e.paths.HostapdConfig()), "ieee80211ax=1") || restarted.Status().Hotspot.Standard != config.StandardWiFi6 {
		t.Error("a Wi-Fi 6 adapter should run Wi-Fi 6")
	}
	// Choosing Wi-Fi 4, for very old devices, sticks.
	settings = restarted.HotspotSettings()
	settings.Standard = config.StandardWiFi4
	if err := restarted.UpdateHotspotSettings(settings); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.HostapdConfig()), "ieee80211ax") || restarted.HotspotSettings().Standard != config.StandardWiFi4 {
		t.Error("Wi-Fi 4 was chosen")
	}
}
