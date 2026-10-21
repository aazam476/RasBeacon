package agent

// This file makes sure a hotspot change can't take the hotspot down for
// good. The settings page and the hotspot are the same network: if hostapd
// (the program that runs the hotspot) can't start with the new settings,
// the owner loses both, and has no page left to undo the change from.
//
// So a change is checked three times:
//  1. before saving: 5 GHz needs a radio with a 5 GHz band, and the channel
//     must be one the country allows (checkHotspotForRadio);
//  2. after restarting hostapd: it must still be running a few seconds
//     later (hotspotStayedUp). If it isn't, the previous settings are put
//     back;
//  3. at every start: a saved 5 GHz hotspot runs on 2.4 GHz when the
//     radio has no 5 GHz band, for example after the USB adapter was
//     swapped for another (hotspotBand).

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/system"
)

// hostapdStartWait is how long hostapd gets to start before it is checked.
// With settings the radio refuses, it stops within a second or two.
const hostapdStartWait = 5 * time.Second

// hotspotHas5GHz reports whether the hotspot's radio can use 5 GHz.
func (a *Agent) hotspotHas5GHz() bool {
	r := a.hotspotRadio()
	return r != nil && r.Has5GHz
}

// hotspotBand is the band hostapd uses for the saved settings: the saved
// band, except 2.4 GHz when the radio has no 5 GHz band.
func (a *Agent) hotspotBand() config.Band {
	if a.cfg.Hotspot.Band == config.Band5GHz && !a.hotspotHas5GHz() {
		return config.Band2GHz
	}
	return a.cfg.Hotspot.Band
}

// hotspotChannel is the channel that goes with hotspotBand: the saved one,
// or the band's default when hotspotBand moved the hotspot to 2.4 GHz.
func (a *Agent) hotspotChannel() int {
	if a.hotspotBand() != a.cfg.Hotspot.Band {
		return 0
	}
	return a.cfg.Hotspot.Channel
}

// checkHotspotForRadio refuses settings the hotspot's radio can't run: 5 GHz
// on a 2.4 GHz-only radio, and a channel the radio says the country
// forbids. It must run after the country is set (iw reports the channels
// under the current country's rules).
func (a *Agent) checkHotspotForRadio(h config.HotspotConfig) error {
	if h.Band == config.Band5GHz && !a.hotspotHas5GHz() {
		return &config.ValidationError{Field: "hotspot.band", Message: "This Wi-Fi adapter can't use 5 GHz."}
	}
	r := a.hotspotRadio()
	if r == nil || h.Channel == 0 {
		return nil // the band's default channel is allowed everywhere
	}
	info, err := system.PhyInfo(a.runner, r.Phy)
	if err != nil {
		return nil // can't tell; the check after the restart still runs
	}
	allowed, listed := radio.ChannelAllowed(info, h.Channel)
	if listed && !allowed {
		return &config.ValidationError{Field: "hotspot.channel", Message: "This channel isn't allowed in this country."}
	}
	return nil
}

// hotspotStayedUp waits hostapdStartWait after a restart, then reports
// whether hostapd is still running. It counts as stopped when systemd says
// it isn't active, or that it had to restart it (it crashed and systemd
// started it again: NRestarts starts at 0 with every restart rbd asks for).
// The caller holds a.mu.
func (a *Agent) hotspotStayedUp() bool {
	a.sleep(hostapdStartWait)
	state, _ := a.runner.Run("systemctl", "is-active", hostapdService)
	switch strings.TrimSpace(state) {
	case "inactive", "failed", "activating", "deactivating":
		return false
	}
	restarts, _ := a.runner.Run("systemctl", "show", "--property=NRestarts", "--value", hostapdService)
	if n, err := strconv.Atoi(strings.TrimSpace(restarts)); err == nil && n > 0 {
		return false
	}
	return true
}

// restoreHotspot puts the previous hotspot settings back after hostapd
// couldn't run the new ones, and restarts the hotspot with them. The
// caller holds a.mu.
func (a *Agent) restoreHotspot(oldCfg config.Config, oldSecrets config.Secrets) error {
	slog.Warn("the hotspot didn't start with the new settings; putting the previous ones back")
	if err := config.WriteJSON(a.paths.SecretsFile(), oldSecrets, 0o600); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), oldCfg, 0o644); err != nil {
		return err
	}
	if oldCfg.Hotspot.Country != a.cfg.Hotspot.Country {
		if err := system.SetRegulatoryCountry(a.runner, oldCfg.Hotspot.Country); err != nil {
			slog.Warn("could not set Wi-Fi country", "error", err)
		}
	}
	a.cfg = oldCfg
	a.secrets = oldSecrets
	return a.startNormalMode()
}

// errHotspotDidntStart is the answer when hostapd couldn't run new settings.
var errHotspotDidntStart = &config.ValidationError{
	Field:   "hotspot",
	Message: "The Wi-Fi adapter couldn't run the hotspot with these settings, so the previous settings were put back.",
}

// hotspotWarnings explains on the Status page when the hotspot doesn't run
// on the saved band.
func (a *Agent) hotspotWarnings() []string {
	if a.mode == api.ModeNormal && a.hotspotBand() != a.cfg.Hotspot.Band {
		return []string{"The hotspot runs on 2.4 GHz: this Wi-Fi adapter can't use 5 GHz."}
	}
	return nil
}
