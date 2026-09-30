// Package agent is the heart of rbd, RasBeacon's privileged background
// program. It owns all network state: it decides which mode the device is
// in, writes the configuration files, and starts or restarts the programs
// that do the actual networking (hostapd for Wi-Fi, dnsmasq for DHCP/DNS,
// nftables for the firewall).
//
// # Files in this package
//
//	agent.go       startup sequence and switching between modes (this file)
//	operations.go  actions requested through the API (claim, settings, ...)
//	status.go      the status page and diagnostics export
//	server.go      the local HTTP API that rbweb talks to
//
// # One change at a time
//
// Every public method takes the agent's lock (a.mu) before touching state.
// That means two requests can never change settings at the same moment, and
// you can read each method top to bottom knowing nothing else changes the
// state while it runs.
//
// # Modes
//
// On first boot the device is in "setup" mode: it broadcasts a setup network
// and waits for the owner to choose an admin password (see api.Mode for all
// modes). After that it runs in "normal" mode with the owner's hotspot.
package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/identity"
	"rasbeacon/internal/led"
	"rasbeacon/internal/paths"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/render"
	"rasbeacon/internal/setupfile"
	"rasbeacon/internal/system"
)

// systemd services rbd controls. Their unit files are in image/overlay/.
const (
	hostapdService = "rasbeacon-hostapd.service"
	dnsmasqService = "rasbeacon-dnsmasq.service"
	sshService     = "ssh.service"
)

// SetupWindow is how long the setup network stays open after boot if nobody
// claims the device. A power cycle reopens it.
const SetupWindow = 30 * time.Minute

// switchDelay is how long after a successful claim the setup network is
// replaced by the real hotspot. It gives the phone time to receive the
// "success" page before its connection drops.
const switchDelay = 5 * time.Second

// Agent holds all of rbd's state. Create it with New and call Start once.
type Agent struct {
	mu sync.Mutex

	paths  paths.Paths
	runner system.Runner
	// afterFunc schedules work for later. It is time.AfterFunc in production
	// and a controllable fake in tests.
	afterFunc func(d time.Duration, f func()) *time.Timer
	now       func() time.Time

	identity config.Identity
	cfg      config.Config
	secrets  config.Secrets

	radios   []radio.Radio
	warnings []string
	mode     api.Mode
	lan      render.LAN

	// Setup mode details.
	setupSecured     bool
	setupPassword    string // from setup.txt; only kept to redact it from diagnostics
	setupDeadline    time.Time
	setupWindowTimer *time.Timer
	stopLEDBlink     chan struct{}

	// usbRadioWait is how long startup waits for a USB Wi-Fi adapter to
	// appear (see waitForRadios). Tests set it to zero.
	usbRadioWait time.Duration
}

// New creates an agent that uses the given file locations and command
// runner. Production code passes paths.Default() and system.ExecRunner{}.
func New(p paths.Paths, runner system.Runner) *Agent {
	return &Agent{
		paths:     p,
		runner:    runner,
		afterFunc: time.AfterFunc,
		now:       time.Now,
		lan:       render.DefaultLAN,
		// USB adapters usually appear within a few seconds of power-on.
		usbRadioWait: 20 * time.Second,
	}
}

// Start runs the boot sequence. It returns an error only for problems that
// make it impossible to continue (for example an unreadable data partition).
// Problems with Wi-Fi hardware are reported as warnings and a mode instead,
// so the status page can explain them.
func (a *Agent) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// The data directory holds secrets, so only root may open it (0700).
	// The runtime directory holds rbweb's API socket, so other users may pass
	// through it (0711) but not list it; the secret files inside are 0600.
	dirs := []struct {
		path string
		mode os.FileMode
	}{
		{a.paths.DataDir(), 0o700},
		{a.paths.RunDir(), 0o711},
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir.path, dir.mode); err != nil {
			return fmt.Errorf("creating %s: %w", dir.path, err)
		}
		if err := os.Chmod(dir.path, dir.mode); err != nil {
			return fmt.Errorf("setting permissions on %s: %w", dir.path, err)
		}
	}

	// 1. Factory reset requested by placing a file on the SD card?
	if err := a.handleFactoryResetFile(); err != nil {
		return err
	}

	// 2. Load settings, creating them on first boot.
	if err := a.loadOrCreateSettings(); err != nil {
		return err
	}

	// 3. Firewall first, so the web UI is only reachable from the hotspot.
	if err := a.applyFirewall(); err != nil {
		return err
	}

	// 4. Find the Wi-Fi radios and pick the hotspot radio.
	if err := a.setUpRadios(); err != nil {
		slog.Error("no usable hotspot radio", "error", err)
		a.mode = api.ModeNoRadio
		return nil
	}

	// 5. Start the setup network or the owner's hotspot.
	if a.cfg.Claimed {
		return a.startNormalMode()
	}
	return a.startSetupMode()
}

// handleFactoryResetFile wipes all settings if the owner created a file
// named "factory-reset" on the SD card's FAT partition.
func (a *Agent) handleFactoryResetFile() error {
	resetFile := a.paths.FactoryResetFile()
	if _, err := os.Stat(resetFile); err != nil {
		return nil // no reset requested
	}
	slog.Warn("factory-reset file found: erasing all settings")
	if err := a.wipeData(); err != nil {
		return err
	}
	if err := os.Remove(resetFile); err != nil {
		// If the file stayed, every boot would wipe the settings again.
		return fmt.Errorf("factory reset done, but could not delete %s: %w", resetFile, err)
	}
	return nil
}

// wipeData deletes everything in RasBeacon's data directory.
func (a *Agent) wipeData() error {
	if err := os.RemoveAll(a.paths.DataDir()); err != nil {
		return fmt.Errorf("erasing settings: %w", err)
	}
	return os.MkdirAll(a.paths.DataDir(), 0o700)
}

// loadOrCreateSettings reads identity, config and secrets. Missing files
// are created with defaults (this is what makes first boot work).
func (a *Agent) loadOrCreateSettings() error {
	// Identity: generated once, never changed.
	err := config.ReadJSON(a.paths.IdentityFile(), &a.identity)
	if errors.Is(err, config.ErrNotFound) {
		slog.Info("first boot: generating device identity")
		a.identity, err = identity.New()
		if err != nil {
			return err
		}
		err = config.WriteJSON(a.paths.IdentityFile(), a.identity, 0o600)
	}
	if err != nil {
		return err
	}

	// Config: defaults until the owner changes something.
	err = config.ReadJSON(a.paths.ConfigFile(), &a.cfg)
	if errors.Is(err, config.ErrNotFound) {
		a.cfg = config.DefaultConfig(a.identity)
		err = nil
	}
	if err != nil {
		return err
	}

	// Secrets: empty until the device is claimed.
	err = config.ReadJSON(a.paths.SecretsFile(), &a.secrets)
	if errors.Is(err, config.ErrNotFound) {
		a.secrets = config.Secrets{}
		err = nil
	}
	return err
}

// setUpRadios detects the Wi-Fi radios, assigns roles, and renames the
// hotspot radio to "ap0". It also applies the regulatory country.
func (a *Agent) setUpRadios() error {
	// Raspberry Pi images can start with Wi-Fi "soft-blocked" (switched off
	// in software) until a country is set. Switch it on; if the rfkill tool
	// is missing, there is nothing to unblock.
	_, _ = a.runner.Run("rfkill", "unblock", "wifi")

	detected, err := a.waitForRadios()
	if err != nil {
		return err
	}
	var warnings []string
	a.radios, warnings = radio.AssignRoles(detected)
	a.warnings = append(a.warnings, warnings...)

	hotspot := a.hotspotRadio()
	if hotspot == nil {
		return errors.New("no radio can run the hotspot")
	}
	if hotspot.Interface != radio.HotspotInterfaceName {
		slog.Info("renaming hotspot radio", "from", hotspot.Interface, "to", radio.HotspotInterfaceName,
			"driver", hotspot.Driver, "usbId", hotspot.USBID)
		if err := system.RenameLink(a.runner, hotspot.Interface, radio.HotspotInterfaceName); err != nil {
			return err
		}
		hotspot.Interface = radio.HotspotInterfaceName
	}

	if err := system.SetRegulatoryCountry(a.runner, a.cfg.Hotspot.Country); err != nil {
		// Not fatal: the radio falls back to the most restrictive rules.
		a.warnings = append(a.warnings, "Could not set the Wi-Fi country: "+err.Error())
	}
	return system.EnsureBridge(a.runner, render.BridgeName, a.lan.Prefix.String())
}

// waitForRadios detects the Wi-Fi radios. When the device boots, USB
// adapters take a few seconds to appear, and rbd may start before them. So
// if no USB radio that can run a hotspot is found yet, it looks again every
// second until usbRadioWait has passed. After that it returns whatever it
// found, and AssignRoles falls back to the built-in radio with a warning.
func (a *Agent) waitForRadios() ([]radio.Radio, error) {
	phyInfo := func(phy string) (string, error) { return system.PhyInfo(a.runner, phy) }
	deadline := a.now().Add(a.usbRadioWait)
	for {
		detected, err := radio.Detect(a.paths.SysClassNet(), phyInfo)
		if err != nil {
			return nil, err
		}
		for _, r := range detected {
			if r.IsUSB && r.SupportsAP {
				return detected, nil
			}
		}
		if !a.now().Before(deadline) {
			return detected, nil
		}
		slog.Info("waiting for a USB Wi-Fi adapter to appear")
		time.Sleep(time.Second)
	}
}

// hotspotRadio returns the radio with the hotspot role, or nil.
func (a *Agent) hotspotRadio() *radio.Radio {
	for i := range a.radios {
		if a.radios[i].Role == radio.RoleHotspot {
			return &a.radios[i]
		}
	}
	return nil
}

// startSetupMode brings up the first-boot setup network.
func (a *Agent) startSetupMode() error {
	result, err := setupfile.Read(a.paths.SetupTxtFile())
	if err != nil {
		// setup.txt exists but is unusable. Do NOT fall back to an open
		// network (see internal/setupfile for why): keep everything off and
		// blink the LED so the owner knows to check the file.
		slog.Error("setup.txt is unusable; setup network stays off", "error", err)
		a.mode = api.ModeSetupError
		a.warnings = append(a.warnings, err.Error())
		_ = system.ServiceStop(a.runner, hostapdService)
		a.startLEDBlink()
		return nil
	}
	a.setupSecured = result.Present
	a.setupPassword = result.Password

	hotspot := render.HotspotInput{
		Interface: radio.HotspotInterfaceName,
		SSID:      "RasBeacon-Setup-" + a.identity.ShortID,
		Band:      config.Band2GHz,
		Country:   a.cfg.Hotspot.Country,
		Security:  config.SecurityWPA2WPA3,
		Open:      !result.Present,
		Password:  result.Password,
	}
	if err := a.applyHotspotAndDHCP(hotspot, true); err != nil {
		return err
	}

	a.mode = api.ModeSetup
	a.setupDeadline = a.now().Add(SetupWindow)
	a.setupWindowTimer = a.afterFunc(SetupWindow, a.closeSetupWindow)
	slog.Info("setup network started", "ssid", hotspot.SSID, "passwordProtected", result.Present)
	return nil
}

// closeSetupWindow runs when nobody claimed the device within SetupWindow.
func (a *Agent) closeSetupWindow() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeSetup {
		return // already claimed
	}
	slog.Warn("setup window expired; turning off the setup network until the next power cycle")
	_ = system.ServiceStop(a.runner, hostapdService)
	_ = system.ServiceStop(a.runner, dnsmasqService)
	a.mode = api.ModeSetupClosed
}

// startNormalMode runs the owner's hotspot.
func (a *Agent) startNormalMode() error {
	h := a.cfg.Hotspot
	hotspot := render.HotspotInput{
		Interface: radio.HotspotInterfaceName,
		SSID:      h.SSID,
		Band:      h.Band,
		Channel:   h.Channel,
		Country:   h.Country,
		Security:  h.Security,
		Password:  a.secrets.HotspotPassword,
	}
	if err := a.applyHotspotAndDHCP(hotspot, false); err != nil {
		return err
	}
	if err := a.applySSH(); err != nil {
		a.warnings = append(a.warnings, "Could not apply SSH settings: "+err.Error())
	}
	a.mode = api.ModeNormal
	slog.Info("hotspot started", "ssid", h.SSID, "band", h.Band)
	return nil
}

// applyHotspotAndDHCP writes hostapd.conf and dnsmasq.conf and restarts both
// programs. captive=true makes every DNS name point at RasBeacon (setup mode).
func (a *Agent) applyHotspotAndDHCP(hotspot render.HotspotInput, captive bool) error {
	hostapdConf, err := render.Hostapd(hotspot)
	if err != nil {
		return err
	}
	dnsmasqConf, err := render.Dnsmasq(render.DnsmasqInput{
		LAN:       a.lan,
		LeaseFile: a.paths.DnsmasqLeases(),
		Captive:   captive,
	})
	if err != nil {
		return err
	}

	// hostapd.conf contains the Wi-Fi password, so only root may read it.
	if err := config.WriteFileAtomic(a.paths.HostapdConfig(), []byte(hostapdConf), 0o600); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(a.paths.DnsmasqConfig(), []byte(dnsmasqConf), 0o644); err != nil {
		return err
	}
	if err := system.ServiceRestart(a.runner, hostapdService); err != nil {
		return fmt.Errorf("starting the hotspot: %w", err)
	}
	if err := system.ServiceRestart(a.runner, dnsmasqService); err != nil {
		return fmt.Errorf("starting DHCP/DNS: %w", err)
	}
	return nil
}

// applyFirewall writes and loads the firewall rules for the current
// settings.
func (a *Agent) applyFirewall() error {
	rules, err := render.Firewall(render.FirewallInput{SSHEnabled: a.cfg.SSH.Enabled})
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(a.paths.FirewallRules(), []byte(rules), 0o644); err != nil {
		return err
	}
	return system.ApplyFirewall(a.runner, a.paths.FirewallRules())
}

// startLEDBlink blinks the setup.txt error pattern until stopLED is called.
func (a *Agent) startLEDBlink() {
	if a.stopLEDBlink != nil {
		return // already blinking
	}
	a.stopLEDBlink = make(chan struct{})
	go led.Blink(led.FindActivityLED(a.paths.SysClassLeds()), led.ErrorPattern, a.stopLEDBlink)
}

// stopLED stops the error blink, if one is running.
func (a *Agent) stopLED() {
	if a.stopLEDBlink != nil {
		close(a.stopLEDBlink)
		a.stopLEDBlink = nil
	}
}
