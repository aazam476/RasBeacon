package agent

// This file holds the actions that the web UI can request through rbweb.
// Each method checks its input, saves settings, then applies them.
//
// Errors that should be shown to the owner are returned as
// *config.ValidationError (bad input, HTTP 400) or ErrWrongMode /
// ErrWrongPassword. Anything else is an unexpected failure (HTTP 500).

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"

	"rasbeacon/internal/api"
	"rasbeacon/internal/auth"
	"rasbeacon/internal/config"
	"rasbeacon/internal/setupfile"
	"rasbeacon/internal/system"
)

// ErrWrongMode means the action is not possible in the current mode, for
// example claiming a device that is already claimed.
var ErrWrongMode = errors.New("this action is not available in the device's current mode")

// ErrWrongPassword means the admin password given with a request was wrong.
var ErrWrongPassword = errors.New("wrong admin password")

// SetupState returns the public information the UI needs before login.
func (a *Agent) SetupState() api.SetupState {
	a.mu.Lock()
	defer a.mu.Unlock()

	state := api.SetupState{
		Mode:                a.mode,
		Claimed:             a.cfg.Claimed,
		SetupNetworkSecured: a.setupSecured,
		DeviceShortID:       a.identity.ShortID,
		LANAddress:          a.lan.Address(),
	}
	if a.mode == api.ModeSetup {
		state.SuggestedHotspotSSID = a.cfg.Hotspot.SSID
		state.SuggestedHotspotPassword = a.identity.SuggestedHotspotPassword
		state.SetupClosesInSeconds = int(a.setupDeadline.Sub(a.now()).Seconds())
	}
	return state
}

// Claim finishes first-time setup: it stores the admin password and the
// hotspot settings, then (after switchDelay) replaces the setup network with
// the owner's hotspot.
func (a *Agent) Claim(req api.ClaimRequest) (api.ClaimResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.mode != api.ModeSetup {
		return api.ClaimResponse{}, ErrWrongMode
	}

	// Check everything before saving anything.
	if err := auth.CheckAdminPasswordStrength(req.AdminPassword); err != nil {
		return api.ClaimResponse{}, &config.ValidationError{Field: "adminPassword", Message: err.Error()}
	}
	if err := config.ValidateWiFiPassword(req.HotspotPassword); err != nil {
		return api.ClaimResponse{}, &config.ValidationError{Field: "hotspotPassword", Message: err.Error()}
	}
	newCfg := a.cfg.Clone()
	newCfg.Hotspot.SSID = req.HotspotSSID
	newCfg.Hotspot.Band = req.HotspotBand
	if err := newCfg.Hotspot.Validate(); err != nil {
		return api.ClaimResponse{}, err
	}

	hash, err := auth.HashPassword(req.AdminPassword)
	if err != nil {
		return api.ClaimResponse{}, err
	}

	// Save secrets first, then the config. Setting Claimed=true in the config
	// is the "commit point": if power fails before it is written, the device
	// simply boots into setup mode again.
	newSecrets := config.Secrets{HotspotPassword: req.HotspotPassword, AdminPasswordHash: hash}
	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return api.ClaimResponse{}, err
	}
	newCfg.Claimed = true
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return api.ClaimResponse{}, err
	}
	a.cfg = newCfg
	a.secrets = newSecrets
	slog.Info("device claimed", "hotspotSsid", newCfg.Hotspot.SSID)

	// Switch networks a little later so this response reaches the phone.
	a.afterFunc(switchDelay, a.finishClaim)

	return api.ClaimResponse{
		HotspotSSID:     newCfg.Hotspot.SSID,
		SwitchInSeconds: int(switchDelay.Seconds()),
		AdminURL:        "http://" + a.lan.Address() + "/",
	}, nil
}

// finishClaim replaces the setup network with the owner's hotspot.
func (a *Agent) finishClaim() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.setupWindowTimer != nil {
		a.setupWindowTimer.Stop()
	}
	// The setup password is no longer needed; don't leave it on the card.
	if err := setupfile.Remove(a.paths.SetupTxtFile()); err != nil {
		slog.Warn("could not delete setup.txt", "error", err)
	}
	a.setupPassword = ""
	a.setupSecured = false

	if err := a.startNormalMode(); err != nil {
		slog.Error("could not start the hotspot after setup", "error", err)
		a.warnings = append(a.warnings, "The hotspot failed to start after setup: "+err.Error())
	}
}

// passwordCheckMu makes password checks run one at a time. Each check uses
// 32 MiB of memory on purpose (see internal/auth), so many at once could run
// a 512 MB Raspberry Pi Zero 2 W out of memory.
var passwordCheckMu sync.Mutex

// VerifyAdminPassword reports whether password is the admin password.
func (a *Agent) VerifyAdminPassword(password string) bool {
	a.mu.Lock()
	hash := a.secrets.AdminPasswordHash
	a.mu.Unlock()

	if hash == "" {
		return false // not claimed yet: there is no admin password
	}
	passwordCheckMu.Lock()
	defer passwordCheckMu.Unlock()
	ok, err := auth.VerifyPassword(password, hash)
	if err != nil {
		slog.Error("stored admin password hash is damaged", "error", err)
		return false
	}
	return ok
}

// HotspotSettings returns the current hotspot settings (without password).
func (a *Agent) HotspotSettings() api.HotspotSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := a.cfg.Hotspot
	return api.HotspotSettings{SSID: h.SSID, Band: h.Band, Channel: h.Channel, Security: h.Security, Country: h.Country}
}

// UpdateHotspotSettings saves new hotspot settings and restarts the hotspot.
// Connected devices will briefly disconnect and rejoin (or need the new
// password, if it changed).
func (a *Agent) UpdateHotspotSettings(req api.HotspotSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	newCfg.Hotspot = config.HotspotConfig{
		SSID: req.SSID, Band: req.Band, Channel: req.Channel, Security: req.Security, Country: req.Country,
	}
	if err := newCfg.Hotspot.Validate(); err != nil {
		return err
	}
	newSecrets := a.secrets.Clone()
	if req.Password != "" {
		if err := config.ValidateWiFiPassword(req.Password); err != nil {
			return &config.ValidationError{Field: "password", Message: err.Error()}
		}
		newSecrets.HotspotPassword = req.Password
	}

	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	countryChanged := newCfg.Hotspot.Country != a.cfg.Hotspot.Country
	a.cfg = newCfg
	a.secrets = newSecrets

	if countryChanged {
		if err := system.SetRegulatoryCountry(a.runner, newCfg.Hotspot.Country); err != nil {
			slog.Warn("could not set Wi-Fi country", "error", err)
		}
	}
	return a.startNormalMode()
}

// RevealHotspotPassword returns the hotspot password if adminPassword is
// correct. The password is only shown on request, never in normal responses.
func (a *Agent) RevealHotspotPassword(adminPassword string) (string, error) {
	if !a.VerifyAdminPassword(adminPassword) {
		return "", ErrWrongPassword
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.secrets.HotspotPassword, nil
}

// SSHSettings returns whether SSH is on and which keys may log in.
func (a *Agent) SSHSettings() api.SSHSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys, _ := os.ReadFile(a.paths.SSHAuthorizedKeysFile())
	return api.SSHSettings{Enabled: a.cfg.SSH.Enabled, AuthorizedKeys: string(keys)}
}

// UpdateSSHSettings turns SSH on or off and saves the allowed keys. SSH only
// allows public-key login (no passwords) and only from the hotspot side.
func (a *Agent) UpdateSSHSettings(req api.SSHSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	keys, err := cleanAuthorizedKeys(req.AuthorizedKeys)
	if err != nil {
		return &config.ValidationError{Field: "authorizedKeys", Message: err.Error()}
	}
	if req.Enabled && keys == "" {
		return &config.ValidationError{Field: "authorizedKeys", Message: "Add at least one public key before turning SSH on."}
	}

	keyFile := a.paths.SSHAuthorizedKeysFile()
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(keyFile, []byte(keys), 0o600); err != nil {
		return err
	}
	newCfg := a.cfg.Clone()
	newCfg.SSH.Enabled = req.Enabled
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	return a.applySSH()
}

// applySSH opens or closes the SSH port and starts or stops the SSH server.
func (a *Agent) applySSH() error {
	if err := a.applyFirewall(); err != nil {
		return err
	}
	if a.cfg.SSH.Enabled {
		return system.ServiceRestart(a.runner, sshService)
	}
	return system.ServiceStop(a.runner, sshService)
}

// cleanAuthorizedKeys checks that every non-empty line is a valid OpenSSH
// public key and returns the keys, one per line.
func cleanAuthorizedKeys(text string) (string, error) {
	var keys []string
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line)); err != nil {
			return "", fmt.Errorf("Line %d is not a valid SSH public key. It should start with ssh-ed25519, ssh-rsa or ecdsa-sha2-.", i+1)
		}
		keys = append(keys, line)
	}
	if len(keys) == 0 {
		return "", nil
	}
	return strings.Join(keys, "\n") + "\n", nil
}

// FactoryReset erases all settings and reboots, if adminPassword is correct.
func (a *Agent) FactoryReset(adminPassword string) error {
	if !a.VerifyAdminPassword(adminPassword) {
		return ErrWrongPassword
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	slog.Warn("factory reset requested from the web UI")
	if err := a.wipeData(); err != nil {
		return err
	}
	a.afterFunc(switchDelay, func() { _, _ = a.runner.Run("systemctl", "reboot") })
	return nil
}

// Reboot restarts the device shortly after answering the request.
func (a *Agent) Reboot() {
	slog.Info("reboot requested from the web UI")
	a.afterFunc(switchDelay, func() { _, _ = a.runner.Run("systemctl", "reboot") })
}
