package agent

import (
	"fmt"
	"log/slog"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
	"rasbeacon/internal/system"
)

// AccessSettings returns which and how many devices may join the hotspot.
func (a *Agent) AccessSettings() api.AccessSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.cfg.Access
	allowed := c.Allowed
	if allowed == nil {
		allowed = []string{}
	}
	return api.AccessSettings{AllowListOn: c.AllowListOn, Allowed: allowed, MaxDevices: c.MaxDevices}
}

// UpdateAccessSettings saves the allow list and the device limit, then
// restarts the hotspot so they take effect. Devices that are no longer
// allowed are disconnected; the others rejoin within seconds.
func (a *Agent) UpdateAccessSettings(req api.AccessSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	access := config.AccessConfig{AllowListOn: req.AllowListOn, MaxDevices: req.MaxDevices}
	seen := map[string]bool{}
	for _, text := range req.Allowed {
		mac, err := config.NormalizeMAC(text)
		if err != nil {
			return &config.ValidationError{Field: "allowed", Message: err.Error()}
		}
		if !seen[mac] {
			seen[mac] = true
			access.Allowed = append(access.Allowed, mac)
		}
	}
	if err := access.Validate(); err != nil {
		return err
	}
	newCfg := a.cfg.Clone()
	newCfg.Access = access
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("hotspot access saved", "allowListOn", access.AllowListOn, "allowed", len(access.Allowed), "maxDevices", access.MaxDevices)

	hotspot, err := a.ownerHotspot()
	if err != nil {
		return err
	}
	hostapdConf, err := render.Hostapd(hotspot)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(a.paths.HostapdConfig(), []byte(hostapdConf), 0o600); err != nil {
		return err
	}
	if err := system.ServiceRestart(a.runner, hostapdService); err != nil {
		return fmt.Errorf("restarting the hotspot: %w", err)
	}
	return nil
}
