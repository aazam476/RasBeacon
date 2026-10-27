package agent

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// newMT7921Env is a set-up RasBeacon whose hotspot adapter is an MT7921AU
// (Wi-Fi 6, with its real capabilities), on 5 GHz.
func newMT7921Env(t *testing.T) *testEnv {
	t.Helper()
	info, err := os.ReadFile("../radio/testdata/mt7921au.txt")
	if err != nil {
		t.Fatal(err)
	}
	e := newTestEnv(t)
	e.runner.outputs["iw phy phy1 info"] = string(info)
	e.start()
	claim := validClaim
	claim.HotspotBand = "5GHz"
	if _, err := e.agent.Claim(claim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	e.runner.outputs["systemctl is-active rasbeacon-hostapd.service"] = "active"
	return e
}

func TestHostapd_UsesTheRadiosCapabilities(t *testing.T) {
	e := newMT7921Env(t)
	conf := e.readFile(e.paths.HostapdConfig())
	for _, want := range []string{"ieee80211ax=1", "vht_capab=[MAX-MPDU-11454][RXLDPC][SHORT-GI-80]", "he_su_beamformer=1"} {
		if !strings.Contains(conf, want) {
			t.Errorf("hostapd.conf lacks %q", want)
		}
	}
	if w := strings.Join(e.agent.Status().Warnings, " "); strings.Contains(w, "hostapd refused") {
		t.Errorf("no fallback expected: %s", w)
	}
}

func TestHostapd_FallsBackWhenRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		refusals  int
		want      string
		notWanted string
		warning   string
	}{
		"without the extra capabilities": {1, "ieee80211ax=1", "vht_capab", "without some of its adapter's Wi-Fi features"},
		"as Wi-Fi 4":                     {2, "Wi-Fi 4 only", "ieee80211ax", "runs as Wi-Fi 4"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newMT7921Env(t)
			// The next save: hostapd refuses tc.refusals times.
			refusals := tc.refusals
			e.runner.effect = func(args []string) {
				if strings.Join(args, " ") != "systemctl restart rasbeacon-hostapd.service" {
					return
				}
				state := "active"
				if refusals > 0 {
					state = "activating"
					refusals--
				}
				e.runner.outputs["systemctl is-active rasbeacon-hostapd.service"] = state
			}
			settings := e.agent.HotspotSettings()
			settings.Password = ""
			settings.Channel = 44 // a change that restarts the hotspot
			if err := e.agent.UpdateHotspotSettings(settings); err != nil {
				t.Fatal(err)
			}
			conf := e.readFile(e.paths.HostapdConfig())
			if !strings.Contains(conf, tc.want) || strings.Contains(conf, tc.notWanted) {
				t.Errorf("hostapd.conf should have %q and not %q:\n%s", tc.want, tc.notWanted, conf)
			}
			if w := strings.Join(e.agent.Status().Warnings, " "); !strings.Contains(w, tc.warning) {
				t.Errorf("warnings: %s", w)
			}
		})
	}
}

// An adapter that can run a 6 GHz hotspot offers it; WPA3 only is needed.
func TestHotspot_6GHz(t *testing.T) {
	e := newMT7921Env(t)
	// It runs under the US rules, which make every 6 GHz channel "no IR":
	// moving to Germany must still be possible.
	e.runner.outputs["iw phy phy1 info"] = strings.ReplaceAll(e.runner.outputs["iw phy phy1 info"], "(23.0 dBm)", "(no IR)")
	settings := e.agent.HotspotSettings()
	if !settings.Has6GHz || settings.Country != "US" {
		t.Fatalf("the MT7921AU can run a 6 GHz hotspot: %+v", settings)
	}
	// The US doesn't let Linux start a 6 GHz network ("no IR" in the
	// regulatory database); Germany allows its lower part.
	us, err := e.agent.HotspotChannels(api.HotspotChannelsRequest{Country: "US"})
	if err != nil || len(us.Channels[config.Band6GHz]) != 0 {
		t.Errorf("6 GHz channels in the US: %v %v", us.Channels[config.Band6GHz], err)
	}
	de, err := e.agent.HotspotChannels(api.HotspotChannelsRequest{Country: "DE"})
	if err != nil || !slices.Contains(de.Channels[config.Band6GHz], 37) || slices.Contains(de.Channels[config.Band6GHz], 101) {
		t.Fatalf("6 GHz channels in Germany: %v %v", de.Channels[config.Band6GHz], err)
	}
	settings.Password = ""
	settings.Country = "DE"
	settings.Band, settings.Channel, settings.Security = config.Band6GHz, 37, config.SecurityWPA3
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Fatal(err)
	}
	conf := e.readFile(e.paths.HostapdConfig())
	for _, want := range []string{"op_class=133", "channel=37", "sae_pwe=1", "he_su_beamformee=1"} {
		if !strings.Contains(conf, want) {
			t.Errorf("hostapd.conf lacks %q", want)
		}
	}
	if got := e.agent.HotspotSettings(); got.Band != config.Band6GHz || got.Standard != config.StandardWiFi6 {
		t.Errorf("settings: %+v", got)
	}
	// A channel past Germany's part of 6 GHz (up to 6425 MHz) is refused.
	settings.Channel = 101
	var verr *config.ValidationError
	if err := e.agent.UpdateHotspotSettings(settings); !errors.As(err, &verr) || verr.Field != "hotspot.channel" {
		t.Errorf("channel 101 in Germany: %v", err)
	}
}

// 6 GHz saved, but the adapter can't: 5 GHz, with a warning.
func TestHotspot_6GHzOnAnAdapterWithout(t *testing.T) {
	e := newMT7921Env(t)
	settings := e.agent.HotspotSettings()
	settings.Password = ""
	settings.Country = "DE"
	settings.Band, settings.Channel, settings.Security = config.Band6GHz, 37, config.SecurityWPA3
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	e.agent.radios[1].Has6GHz = false // another adapter now
	e.agent.mu.Unlock()
	if band := e.agent.hotspotBand(); band != config.Band5GHz {
		t.Errorf("band %s", band)
	}
	if w := strings.Join(e.agent.Status().Warnings, " "); !strings.Contains(w, "runs on 5 GHz: this Wi-Fi adapter can't use 6 GHz") {
		t.Errorf("warnings: %s", w)
	}
	if err := e.agent.UpdateHotspotSettings(settings); err == nil || !strings.Contains(err.Error(), "6 GHz") {
		t.Errorf("saving 6 GHz now: %v", err)
	}
}
