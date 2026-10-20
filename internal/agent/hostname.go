package agent

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/stations"
)

// applyHostname gives the system RasBeacon's name. The system partition is
// read-only, so the name is kept in the settings and set at every start.
func (a *Agent) applyHostname() {
	if _, err := a.runner.Run("hostname", a.cfg.Name()); err != nil {
		slog.Warn("could not set the host name", "name", a.cfg.Name(), "error", err)
	}
}

// SetHostname renames RasBeacon and restarts it, so every service and
// device picks up the new name. A name a device on the hotspot already uses
// is refused.
func (a *Agent) SetHostname(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if err := config.ValidateHostname(name); err != nil {
		return err
	}
	if name == a.cfg.Name() {
		return nil
	}
	leases, _ := os.ReadFile(a.paths.DnsmasqLeases())
	for _, lease := range stations.ParseLeases(string(leases)) {
		if strings.EqualFold(lease.Hostname, name) {
			return &config.ValidationError{Field: "hostname",
				Message: fmt.Sprintf("A device on the hotspot (%s) already uses this name.", lease.IP)}
		}
	}
	newCfg := a.cfg.Clone()
	newCfg.Hostname = name
	if name == config.DefaultHostname {
		newCfg.Hostname = ""
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("host name changed; restarting", "name", name)
	// A certificate RasBeacon made itself should name the new address too.
	if !a.cfg.HTTPS.Uploaded && fileExists(a.tlsCertFile()) {
		if err := a.generateCertificate(); err != nil {
			slog.Warn("could not renew the HTTPS certificate for the new name", "error", err)
		}
	}
	a.afterFunc(switchDelay, func() { _, _ = a.runner.Run("systemctl", "reboot") })
	return nil
}
