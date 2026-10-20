package agent

// This file handles the HTTPS setting for the web UI.
//
// rbd keeps the certificate and key in /persistent/rasbeacon/tls (root
// only) and, while HTTPS is on, puts a copy in /run/rbd/tls readable by
// rbweb's group. rbweb serves HTTPS when it finds them at startup, so rbd
// restarts rbweb after every change.

import (
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
	"rasbeacon/internal/system"
	"rasbeacon/internal/tlscert"
)

const rbwebService = "rbweb.service"

// rbwebRestartDelay lets the answer to the settings request reach the
// browser before rbweb restarts.
const rbwebRestartDelay = 2 * time.Second

// tlsCertFile and tlsKeyFile are where the HTTPS certificate and its key
// are kept.
func (a *Agent) tlsCertFile() string { return filepath.Join(a.paths.TLSDir(), "cert.pem") }
func (a *Agent) tlsKeyFile() string  { return filepath.Join(a.paths.TLSDir(), "key.pem") }

// applyHTTPS puts (or removes) the copy of the certificate for rbweb. It
// returns whether anything changed.
func (a *Agent) applyHTTPS() (bool, error) {
	runDir := a.paths.RunTLSDir()
	if !a.cfg.HTTPS.Enabled {
		if !fileExists(runDir) {
			return false, nil
		}
		return true, os.RemoveAll(runDir)
	}
	cert, err := os.ReadFile(a.tlsCertFile())
	if err != nil {
		return false, err
	}
	key, err := os.ReadFile(a.tlsKeyFile())
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		return false, err
	}
	changedCert, err := writeFileIfChanged(filepath.Join(runDir, "cert.pem"), string(cert), 0o640)
	if err != nil {
		return false, err
	}
	changedKey, err := writeFileIfChanged(filepath.Join(runDir, "key.pem"), string(key), 0o640)
	if err != nil {
		return false, err
	}
	// rbweb's group may read the directory and files; nobody else.
	if a.webGroup >= 0 {
		for _, path := range []string{runDir, filepath.Join(runDir, "cert.pem"), filepath.Join(runDir, "key.pem")} {
			if err := os.Chown(path, 0, a.webGroup); err != nil {
				return false, err
			}
		}
	}
	return changedCert || changedKey, nil
}

// SetWebGroup tells rbd rbweb's group ID (from cmd/rbd), so rbweb can read
// the HTTPS key. -1 (the default) leaves the group unchanged.
func (a *Agent) SetWebGroup(gid int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.webGroup = gid
}

// HTTPSSettings describes the HTTPS setting and certificate.
func (a *Agent) HTTPSSettings() api.HTTPSSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := api.HTTPSSettings{Enabled: a.cfg.HTTPS.Enabled, Uploaded: a.cfg.HTTPS.Uploaded}
	if data, err := os.ReadFile(a.tlsCertFile()); err == nil {
		if info, err := tlscert.Describe(string(data)); err == nil {
			s.Subject, s.Fingerprint = info.Subject, info.Fingerprint
			s.ValidUntil = info.NotAfter.Format("2006-01-02")
		}
	}
	return s
}

// UpdateHTTPSSettings turns HTTPS on or off, and makes or stores a
// certificate. rbweb restarts a moment later.
func (a *Agent) UpdateHTTPSSettings(req api.HTTPSSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()

	switch {
	case req.Certificate != "" || req.PrivateKey != "":
		if err := tlscert.Check(req.Certificate, req.PrivateKey, a.now()); err != nil {
			return &config.ValidationError{Field: "certificate", Message: err.Error()}
		}
		if err := a.saveTLSFiles(req.Certificate, req.PrivateKey); err != nil {
			return err
		}
		newCfg.HTTPS.Uploaded = true
	case req.GenerateNew || (req.Enabled && !fileExists(a.tlsCertFile())):
		if err := a.generateCertificate(); err != nil {
			return err
		}
		newCfg.HTTPS.Uploaded = false
	}
	newCfg.HTTPS.Enabled = req.Enabled

	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("HTTPS settings saved", "enabled", newCfg.HTTPS.Enabled, "uploaded", newCfg.HTTPS.Uploaded)
	if _, err := a.applyHTTPS(); err != nil {
		return err
	}
	a.afterFunc(rbwebRestartDelay, func() {
		if err := system.ServiceRestart(a.runner, rbwebService); err != nil {
			slog.Error("could not restart the web server", "error", err)
		}
	})
	return nil
}

// generateCertificate makes a new self-signed certificate.
func (a *Agent) generateCertificate() error {
	var addresses []net.IP
	for _, prefix := range render.LANCandidates {
		addresses = append(addresses, net.IP(prefix.Addr().AsSlice()))
	}
	if a.lan.IPv6Prefix.IsValid() {
		addresses = append(addresses, net.IP(a.lan.IPv6Prefix.Addr().AsSlice()))
	}
	cert, key, err := tlscert.Generate("RasBeacon "+a.identity.ShortID, []string{a.cfg.Name() + ".lan"}, addresses, a.now())
	if err != nil {
		return err
	}
	return a.saveTLSFiles(cert, key)
}

// saveTLSFiles stores a certificate and key (the key root-only).
func (a *Agent) saveTLSFiles(cert, key string) error {
	if err := os.MkdirAll(a.paths.TLSDir(), 0o700); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(a.tlsKeyFile(), []byte(key), 0o600); err != nil {
		return err
	}
	return config.WriteFileAtomic(a.tlsCertFile(), []byte(cert), 0o644)
}
