package agent

// This file makes sure a hotspot change can't take the hotspot down for
// good. The settings page and the hotspot are the same network: if hostapd
// (the program that runs the hotspot) can't start with the new settings,
// the owner loses both, and has no page left to undo the change from.
//
// So a change is checked three times:
//  1. before saving: 5 GHz needs a radio with a 5 GHz band, and the channel
//     must be one the country allows, by the regulatory database and by
//     the radio itself (checkHotspotForRadio);
//  2. after restarting hostapd: it must still be running a few seconds
//     later (hotspotStayedUp). If it isn't, the previous settings are put
//     back;
//  3. at every start: a saved 5 GHz hotspot runs on 2.4 GHz when the
//     radio has no 5 GHz band, for example after the USB adapter was
//     swapped for another (hotspotBand).

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/regdb"
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

// hotspotHas6GHz reports whether the hotspot's radio can run a 6 GHz
// hotspot (radio.Radio.Has6GHz).
func (a *Agent) hotspotHas6GHz() bool {
	r := a.hotspotRadio()
	return r != nil && r.Has6GHz
}

// hotspotHasWPA3 reports whether the hotspot's radio can run a WPA3-only
// hotspot (radio.SupportsWPA3AP).
func (a *Agent) hotspotHasWPA3() bool {
	r := a.hotspotRadio()
	return r != nil && r.HasWPA3
}

// hotspotHasWiFi6 reports whether the hotspot's radio can run Wi-Fi 6
// (radio.SupportsWiFi6AP).
func (a *Agent) hotspotHasWiFi6() bool {
	r := a.hotspotRadio()
	return r != nil && r.HasWiFi6
}

// hotspotStandard is the Wi-Fi standard hostapd runs: the saved one, or
// the best the radio and band allow ("" saved, or Wi-Fi 6 saved for a radio
// that can't). Like the security setting, the saved choice stays.
func (a *Agent) hotspotStandard() config.Standard {
	saved := a.cfg.Hotspot.Standard
	if a.hotspotBand() == config.Band6GHz {
		return config.StandardWiFi6 // 6 GHz is Wi-Fi 6E
	}
	if saved == config.StandardWiFi4 || (saved == config.StandardWiFi5 && a.hotspotBand() == config.Band5GHz) {
		return saved
	}
	if a.hotspotHasWiFi6() {
		return config.StandardWiFi6
	}
	if a.hotspotBand() == config.Band5GHz {
		return config.StandardWiFi5
	}
	return config.StandardWiFi4
}

// hotspotSecurity is the security hostapd uses for the saved settings: the
// saved one, except WPA2/WPA3 when "WPA3 only" is saved but the radio can't
// run it (another adapter, or a restored backup). Like the band, the saved
// choice stays, so a capable adapter brings WPA3 only back.
func (a *Agent) hotspotSecurity() config.Security {
	if a.cfg.Hotspot.Security == config.SecurityWPA3 && !a.hotspotHasWPA3() {
		return config.SecurityWPA2WPA3
	}
	return a.cfg.Hotspot.Security
}

// hotspotBand is the band hostapd uses for the saved settings: the saved
// band, except when the radio can't use it: 6 GHz moves to 5 GHz (or 2.4
// GHz), 5 GHz to 2.4 GHz.
func (a *Agent) hotspotBand() config.Band {
	band := a.cfg.Hotspot.Band
	if band == config.Band6GHz && !a.hotspotHas6GHz() {
		band = config.Band5GHz
	}
	if band == config.Band5GHz && !a.hotspotHas5GHz() {
		band = config.Band2GHz
	}
	return band
}

// hotspotChannel is the channel that goes with hotspotBand: the saved one,
// or else ("Default", or after hotspotBand moved the hotspot to 2.4 GHz)
// the band's default channel if the country allows it, otherwise the
// first channel it allows.
func (a *Agent) hotspotChannel() int {
	band, saved := a.hotspotBand(), a.cfg.Hotspot.Channel
	if band == a.cfg.Hotspot.Band && saved != 0 {
		return saved
	}
	allowed := a.countryChannels(a.cfg.Hotspot.Country)[band]
	if len(allowed) == 0 || slices.Contains(allowed, config.DefaultChannel(band)) {
		return 0 // the band's default (hostapd.conf writes it out)
	}
	return allowed[0]
}

// loadRegulatoryDB reads the regulatory database, if the image has it.
// Without it, every offered channel is allowed here, and the radio's own
// check and the check after a restart still apply.
func (a *Agent) loadRegulatoryDB() {
	data, err := os.ReadFile(a.paths.RegulatoryDB())
	if err != nil {
		slog.Warn("no Wi-Fi regulatory database: channels aren't checked per country", "error", err)
		return
	}
	if a.regdb, err = regdb.Parse(data); err != nil {
		slog.Warn("the Wi-Fi regulatory database can't be read", "error", err)
	}
}

// countryChannels lists, for each band, the channels RasBeacon offers that
// a hotspot may use in country (see regdb.HotspotChannelAllowed). 5 GHz is
// empty when the hotspot's radio can't use it.
func (a *Agent) countryChannels(country string) map[config.Band][]int {
	channels := map[config.Band][]int{}
	for _, band := range []config.Band{config.Band2GHz, config.Band5GHz, config.Band6GHz} {
		channels[band] = []int{}
		if (band == config.Band5GHz && !a.hotspotHas5GHz()) || (band == config.Band6GHz && !a.hotspotHas6GHz()) {
			continue
		}
		for _, channel := range config.OfferedChannels(band) {
			if a.regdb == nil || a.regdb.HotspotChannelAllowed(country, config.ChannelMHz(band, channel)) {
				channels[band] = append(channels[band], channel)
			}
		}
	}
	return channels
}

// HotspotChannels answers which channels the Hotspot page may offer for
// country (POST /settings/hotspot/channels).
func (a *Agent) HotspotChannels(req api.HotspotChannelsRequest) (api.HotspotChannels, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := config.HotspotConfig{SSID: "x", Band: config.Band2GHz, Security: config.SecurityWPA2WPA3, Country: req.Country}
	if err := h.Validate(); err != nil {
		return api.HotspotChannels{}, err
	}
	return api.HotspotChannels{Channels: a.countryChannels(req.Country)}, nil
}

// checkHotspotForRadio refuses settings the hotspot's radio can't run: 5 GHz
// on a 2.4 GHz-only radio, and a channel the radio says the country
// forbids. It must run after the country is set (iw reports the channels
// under the current country's rules).
func (a *Agent) checkHotspotForRadio(h config.HotspotConfig) error {
	if h.Band == config.Band5GHz && !a.hotspotHas5GHz() {
		return &config.ValidationError{Field: "hotspot.band", Message: "This Wi-Fi adapter can't use 5 GHz."}
	}
	if h.Band == config.Band6GHz && !a.hotspotHas6GHz() {
		return &config.ValidationError{Field: "hotspot.band", Message: "This Wi-Fi adapter can't run a hotspot on 6 GHz."}
	}
	if h.Security == config.SecurityWPA3 && !a.hotspotHasWPA3() {
		return &config.ValidationError{Field: "hotspot.security", Message: "This Wi-Fi adapter can't run a WPA3-only hotspot."}
	}
	if h.Standard == config.StandardWiFi6 && !a.hotspotHasWiFi6() {
		return &config.ValidationError{Field: "hotspot.standard", Message: "This Wi-Fi adapter can't run Wi-Fi 6."}
	}
	allowed := a.countryChannels(h.Country)[h.Band]
	if len(allowed) == 0 {
		return &config.ValidationError{Field: "hotspot.band", Message: fmt.Sprintf("This country doesn't allow a hotspot on %s.", bandLabel(h.Band))}
	}
	if h.Channel != 0 && !slices.Contains(allowed, h.Channel) {
		return &config.ValidationError{Field: "hotspot.channel", Message: "This channel isn't allowed in this country."}
	}
	// The radio itself may know rules the database doesn't (its driver's
	// own limits): it marks those channels disabled. Its marks follow the
	// country in force now, so they only count when that one stays.
	r := a.hotspotRadio()
	if r == nil || h.Channel == 0 || h.Country != a.cfg.Hotspot.Country {
		return nil // "Default" picks an allowed channel (hotspotChannel)
	}
	info, err := system.PhyInfo(a.runner, r.Phy)
	if err != nil {
		return nil // can't tell; the check after the restart still runs
	}
	radioAllows, listed := radio.ParseBandCaps(info)[bandNumber(h.Band)].Channels[h.Channel]
	if listed && !radioAllows {
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
// with the saved band or security.
func (a *Agent) hotspotWarnings() []string {
	if a.mode != api.ModeNormal {
		return nil
	}
	var warnings []string
	if band := a.hotspotBand(); band != a.cfg.Hotspot.Band {
		warnings = append(warnings, fmt.Sprintf("The hotspot runs on %s: this Wi-Fi adapter can't use %s.",
			bandLabel(band), bandLabel(a.cfg.Hotspot.Band)))
	}
	if a.hotspotSecurity() != a.cfg.Hotspot.Security {
		warnings = append(warnings, "The hotspot runs as WPA2/WPA3: this Wi-Fi adapter can't run WPA3 only.")
	}
	if a.hotspotFallback != "" {
		warnings = append(warnings, a.hotspotFallback)
	}
	return warnings
}

// bandLabel is a band as people read it: "2.4 GHz", "5 GHz", "6 GHz".
func bandLabel(band config.Band) string {
	return strings.Replace(string(band), "GHz", " GHz", 1)
}
