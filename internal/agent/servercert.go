package agent

// This file remembers a WPA-Enterprise network's login server at the first
// connection ("trust on first connect"), for networks saved without a CA
// certificate or a server name.
//
// Such a network starts with wpa_supplicant's ca_cert="probe://": it
// connects only far enough to receive the login server's certificate, then
// stops, before any password is sent. rbd reads the certificate's SHA-256
// from wpa_supplicant's events, saves it, and writes the network again with
// ca_cert="hash://server/sha256/…", which accepts that certificate and no
// other. A fake network with the same name, met later, can't collect the
// password. If the real network replaces its certificate, the connection
// fails and the status page says to remove and add the network again.

import (
	"fmt"
	"log/slog"
	"time"

	"rasbeacon/internal/config"
	"rasbeacon/internal/wpaevents"
)

// serverCertState is what the watcher knows.
type serverCertState struct {
	// joining is the network wpa_supplicant is joining now.
	joining string
	// rejected are networks whose server sent a different certificate
	// than the one trusted, by SSID.
	rejected map[string]bool
}

// RunServerCertWatch listens to wpa_supplicant's events while a saved
// Enterprise network relies on trust on first connect. cmd/rbd starts it.
func (a *Agent) RunServerCertWatch() {
	for {
		time.Sleep(5 * time.Second)
		iface := a.serverCertWatchInterface()
		if iface == "" {
			continue
		}
		keep := func() bool { return a.serverCertWatchInterface() == iface }
		if err := wpaevents.Listen(a.paths.WPAControlDir(), iface, a.paths.RunDir(), keep, a.handleWPAEvent); err != nil {
			slog.Debug("wpa_supplicant events stopped", "error", err)
		}
	}
}

// serverCertWatchInterface is the Wi-Fi interface whose wpa_supplicant
// events to listen to, or "" when no saved network trusts its login server
// on first connect (or no radio joins networks).
func (a *Agent) serverCertWatchInterface() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.trustsOnFirstConnect() {
		return ""
	}
	return a.wifiUplinkInterface()
}

// trustsOnFirstConnect reports whether a saved network has neither a CA
// certificate nor a server name (its certificate is or will be trusted at
// the first connection). The caller holds a.mu.
func (a *Agent) trustsOnFirstConnect() bool {
	for _, n := range a.cfg.Uplink.Networks {
		if a.firstConnectTrust(n) {
			return true
		}
	}
	return false
}

// firstConnectTrust reports whether n's login server is trusted on first
// connect. The caller holds a.mu.
func (a *Agent) firstConnectTrust(n config.SavedNetwork) bool {
	e := n.Enterprise
	return n.Security == config.UplinkEnterprise && e != nil && !e.AllowNoCertificateCheck &&
		e.ServerName == "" && !fileExists(a.paths.CACertFile(n.ID))
}

// handleWPAEvent reacts to one wpa_supplicant event.
func (a *Agent) handleWPAEvent(event string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ssid, ok := wpaevents.TryingSSID(event); ok {
		a.serverCert.joining = ssid
		return
	}
	if wpaevents.CertificateRejected(event) {
		for _, n := range a.cfg.Uplink.Networks {
			if n.SSID == a.serverCert.joining && a.firstConnectTrust(n) && n.Enterprise.ServerCertHash != "" {
				if a.serverCert.rejected == nil {
					a.serverCert.rejected = map[string]bool{}
				}
				if !a.serverCert.rejected[n.SSID] {
					slog.Warn("login server sent a different certificate than the trusted one", "ssid", n.SSID)
				}
				a.serverCert.rejected[n.SSID] = true
			}
		}
		return
	}
	hash, ok := wpaevents.ServerCertHash(event)
	if !ok {
		return
	}
	for i, n := range a.cfg.Uplink.Networks {
		if n.SSID != a.serverCert.joining || !a.firstConnectTrust(n) || n.Enterprise.ServerCertHash != "" {
			continue
		}
		newCfg := a.cfg.Clone()
		e := *newCfg.Uplink.Networks[i].Enterprise
		e.ServerCertHash = hash
		newCfg.Uplink.Networks[i].Enterprise = &e
		if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
			slog.Error("could not remember the login server's certificate", "error", err)
			return
		}
		a.cfg = newCfg
		slog.Info("login server's certificate trusted from now on", "ssid", n.SSID, "sha256", hash)
		// Join for real, accepting only this certificate.
		if err := a.applyUplinkConfig(); err != nil {
			slog.Error("could not join with the trusted certificate", "error", err)
		}
		return
	}
}

// serverCertWarnings lists networks whose login server changed certificate.
// The caller holds a.mu.
func (a *Agent) serverCertWarnings() []string {
	var warnings []string
	for _, n := range a.cfg.Uplink.Networks {
		if a.serverCert.rejected[n.SSID] && a.firstConnectTrust(n) {
			warnings = append(warnings, fmt.Sprintf("The login server of %q sent a different certificate than the one RasBeacon trusted at the first connection, so RasBeacon didn't log in. "+
				"If the network replaced its certificate, remove it on the Internet page and add it again. If not, someone nearby may be faking it.", n.SSID))
		}
	}
	return warnings
}
