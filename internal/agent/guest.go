package agent

// This file handles the guest network: a second Wi-Fi network name on the
// hotspot radio, for visitors.
//
// Guests get their own network (render.LAN.Guest, e.g. 10.87.1.0/24) on
// their own bridge (br-guest). They use the internet exactly like your
// devices: through the VPN when it is on, kill switch included (see
// routing.LANInterfaces). The firewall keeps them away from everything
// else: your devices, each other (hostapd's ap_isolate), and RasBeacon's
// settings and SSH (render.FirewallInput.Guest).
//
// The radio must be able to broadcast two networks at once
// (radio.Radio.MaxAPs). Many USB adapters can; if the one plugged in
// can't, the guest network stays off and the status page says why.

import (
	"log/slog"
	"strings"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/render"
	"rasbeacon/internal/stations"
	"rasbeacon/internal/system"
)

// guestSupported reports whether the hotspot radio can run the guest
// network next to the main one.
func (a *Agent) guestSupported() bool {
	hotspot := a.hotspotRadio()
	return hotspot != nil && hotspot.MaxAPs >= 2
}

// guestWanted reports whether the owner's hotspot should include the
// guest network: it is turned on, has a password, and the radio can run it.
func (a *Agent) guestWanted() bool {
	return a.cfg.Claimed && a.cfg.Guest.Enabled && a.guestSupported() && a.secrets.GuestPassword != ""
}

// guestActive reports whether the guest network is running now (it never
// runs during first-time setup).
func (a *Agent) guestActive() bool {
	return a.mode == api.ModeNormal && a.guestWanted()
}

// guestHostapd returns the guest network's part of hostapd.conf, or nil
// when it is off.
func (a *Agent) guestHostapd() *render.GuestNetwork {
	if !a.guestWanted() {
		return nil
	}
	bssid, err := render.GuestBSSID(a.readSysNet(radio.HotspotInterfaceName, "address"))
	if err != nil {
		slog.Error("guest network: can't read the hotspot radio's address", "error", err)
		return nil
	}
	return &render.GuestNetwork{SSID: a.cfg.Guest.SSID, Password: a.secrets.GuestPassword, BSSID: bssid}
}

// applyGuestBridge creates the guest bridge with RasBeacon's guest-side
// address, or removes it when the guest network is off.
func (a *Agent) applyGuestBridge(on bool) {
	if !on {
		_, _ = a.runner.Run("ip", "link", "delete", "dev", render.GuestBridgeName) // error: already gone
		return
	}
	_ = system.FlushAddresses(a.runner, "-4", render.GuestBridgeName) // after a network move
	if err := system.EnsureBridge(a.runner, render.GuestBridgeName, a.lan.Guest().Prefix.String()); err != nil {
		slog.Error("could not set up the guest network's bridge", "error", err)
	}
}

// guestLAN returns the guest network for dnsmasq, or nil when it is off.
// (dnsmasq leaves it out by itself for the setup network.)
func (a *Agent) guestLAN() *render.LAN {
	if !a.guestWanted() {
		return nil
	}
	guest := a.lan.Guest()
	return &guest
}

// guestStatus describes the guest network for the status page.
// hostapdRunning says whether hostapd, which runs it with the hotspot, is.
func (a *Agent) guestStatus(hostapdRunning bool) api.GuestStatus {
	s := api.GuestStatus{
		Enabled: a.cfg.Guest.Enabled,
		Running: a.guestActive() && hostapdRunning,
		SSID:    a.cfg.Guest.SSID,
		Clients: []stations.Station{},
	}
	if s.Running {
		s.Clients = a.stationsOn(render.GuestInterface)
	}
	return s
}

// guestWarnings explains why a guest network that is turned on isn't
// running.
func (a *Agent) guestWarnings() []string {
	if a.mode != api.ModeNormal || !a.cfg.Guest.Enabled || a.guestActive() {
		return nil
	}
	if !a.guestSupported() {
		return []string{"The guest network is turned on, but this Wi-Fi adapter can only broadcast one network, " +
			"so it is off. An adapter with a MediaTek chip (for example MT7612U) can run both."}
	}
	return []string{"The guest network is turned on but has no password yet: set one on the Guest network page."}
}

// --- Settings (called through the API) ---

// GuestSettings returns the guest network page settings (without the
// password).
func (a *Agent) GuestSettings() api.GuestSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return api.GuestSettings{
		Enabled:     a.cfg.Guest.Enabled,
		SSID:        a.cfg.Guest.SSID,
		HasPassword: a.secrets.GuestPassword != "",
		Supported:   a.guestSupported(),
	}
}

// UpdateGuestSettings saves the guest network settings and restarts the
// hotspot (your devices reconnect after a few seconds).
func (a *Agent) UpdateGuestSettings(req api.GuestSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	newCfg.Guest = config.GuestConfig{Enabled: req.Enabled, SSID: strings.TrimSpace(req.SSID)}
	newSecrets := a.secrets.Clone()
	if req.Password != "" {
		newSecrets.GuestPassword = req.Password
	}
	if err := newCfg.Guest.Validate(newSecrets.GuestPassword, newCfg.Hotspot.SSID); err != nil {
		return err
	}
	if req.Enabled && !a.guestSupported() {
		return &config.ValidationError{Field: "enabled",
			Message: "This Wi-Fi adapter can only broadcast one network, so it can't run a guest network."}
	}
	if newSecrets.GuestPassword == newSecrets.HotspotPassword && newSecrets.GuestPassword != "" {
		return &config.ValidationError{Field: "password", Message: "Use a different password than your own network's."}
	}

	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	oldCfg, oldSecrets := a.cfg, a.secrets
	a.cfg, a.secrets = newCfg, newSecrets
	slog.Info("guest network settings saved", "enabled", newCfg.Guest.Enabled, "ssid", newCfg.Guest.SSID)
	if err := a.startNormalMode(); err != nil {
		return err
	}
	// Some adapters say they can broadcast two networks but refuse the
	// second one, and hostapd then stops. Like a hotspot save, the
	// previous settings go back, so the hotspot (and this page) stays.
	// Without this, the minute check would keep restarting hostapd with
	// the same bad settings, and only a factory reset would get out.
	if !a.hotspotStayedUp() {
		if err := a.restoreHotspot(oldCfg, oldSecrets); err != nil {
			return err
		}
		return errGuestDidntStart
	}
	return nil
}

// errGuestDidntStart is the answer when hostapd couldn't run the guest
// network.
var errGuestDidntStart = &config.ValidationError{
	Field:   "enabled",
	Message: "The Wi-Fi adapter couldn't run the guest network, so the previous settings were put back.",
}
