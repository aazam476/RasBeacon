// Package agent is the heart of rbd, RasBeacon's privileged background
// program. It owns all network state: it decides which mode the device is
// in, writes the configuration files, and starts or restarts the programs
// that do the actual networking (hostapd for Wi-Fi, dnsmasq for DHCP/DNS,
// nftables for the firewall).
//
// # Files in this package
//
//	agent.go           startup sequence and switching between modes (this file)
//	operations.go      actions requested through the API (claim, settings, ...)
//	server.go          the local HTTP API that rbweb talks to
//	status.go          the status page and the diagnostics report
//	readings.go        hardware readings, traffic, storage, Ethernet details
//
//	The hotspot and its devices:
//	hotspotstart.go    checking new hotspot settings, and undoing ones that fail
//	hotspotcheck.go    the minute check that repairs the hotspot
//	hotspotstall.go    noticing a hotspot that stopped sending to its devices
//	guest.go           the guest network
//	access.go          who may join: the allow list and the device limit
//	clients.go         pausing, disconnecting and forgetting devices
//	usage.go           device names and the data each one uses
//	qr.go              the QR codes for joining the hotspot
//
//	The internet side:
//	uplink.go          the connection to the internet (Ethernet, Wi-Fi)
//	uplinkcontrols.go  the Internet page's buttons (disconnect, renew, ...)
//	enterprise.go      certificates for eduroam and other Enterprise networks
//	servercert.go      trusting an Enterprise login server on first connect
//	dns.go             the owner's DNS servers, plain and DNS-over-HTTPS
//	vpn.go             the WireGuard VPN and its kill switch
//	portal.go          hotel login pages (captive portals) with the VPN on
//	portalbrowser.go   logging in to them from RasBeacon's own browser
//	speedtest.go       the speed test
//
//	Plugins:
//	plugins.go         the plugins' state and settings (see its list of files)
//	pluginsinstall.go  previewing and installing plugins
//	pluginsrun.go      starting, watching and stopping their containers
//	pluginsnet.go      the catalog, and downloads through the VPN
//	pluginsfetch.go    downloading a plugin's app again after a restore
//	pluginskept.go     data kept when a plugin is removed
//	socketmark_*.go    marking plugin downloads for the VPN (Linux only)
//
//	The System page:
//	update.go          installing and confirming updates
//	backup.go          backups and restores
//	https.go           the HTTPS setting
//	hostname.go        RasBeacon's name on the network
//	swap.go            the swap file
//	clock.go           keeping the time right without a clock battery
//	remember.go        "Stay logged in"
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
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/dhcpprobe"
	"rasbeacon/internal/identity"
	"rasbeacon/internal/led"
	"rasbeacon/internal/paths"
	"rasbeacon/internal/portal"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/regdb"
	"rasbeacon/internal/render"
	"rasbeacon/internal/routing"
	"rasbeacon/internal/setupfile"
	"rasbeacon/internal/speedtest"
	"rasbeacon/internal/system"
)

// systemd services rbd controls. Their unit files are in
// image/layer/rasbeacon.d/customize.overlay/usr/lib/systemd/system/.
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
	// restoring is held while a backup is uploaded or restored, so two
	// can't overlap (they share one upload file). It is separate from mu
	// because a restore loads plugin images for minutes without mu.
	restoring sync.Mutex
	mu        sync.Mutex

	paths  paths.Paths
	runner system.Runner
	// afterFunc schedules work for later. It is time.AfterFunc in production
	// and a controllable fake in tests.
	afterFunc func(d time.Duration, f func()) *time.Timer
	now       func() time.Time
	sleep     func(d time.Duration)
	// dhcpProbe is dhcpprobe.Probe, or a fake in tests and devsim.
	dhcpProbe func(iface string, timeout time.Duration) (dhcpprobe.Result, error)

	identity config.Identity
	cfg      config.Config
	secrets  config.Secrets

	radios   []radio.Radio
	warnings []string
	mode     api.Mode
	lan      render.LAN
	uplink   uplinkState
	vpn      vpnState
	// routedIPv6 is the public IPv6 network currently on the hotspot bridge
	// (see vpn.go, applyRoutedIPv6).
	routedIPv6 netip.Prefix
	portal     portalState
	readings   readingsState
	speedtest  speedtestState
	update     updateState
	usage      usageState
	plugins    pluginsState
	// hotspotCheck is the hotspot's regular self-check (hotspotcheck.go).
	hotspotCheck hotspotCheckState
	// serverCert remembers Enterprise login servers (servercert.go).
	serverCert serverCertState
	// powerSockets are the plugins' power sockets (see pluginsrun.go).
	powerSockets map[string]*http.Server
	// backupTickets are backups asked for but not downloaded yet, by token
	// (see backup.go).
	backupTickets map[string]backupTicket
	// pluginBridge is true once the plugins' network exists.
	pluginBridge bool
	// background runs slow work (plugins) without blocking: a goroutine,
	// or a list a test runs when it wants.
	background func(f func())
	// decompress is zstdDecompress, or a stand-in in tests.
	decompress func(io.Reader) (io.Reader, func() error, error)

	// Setup mode details.
	setupSecured     bool
	setupPassword    string // from setup.txt; only kept to redact it from diagnostics
	setupDeadline    time.Time
	setupWindowTimer *time.Timer
	stopLEDBlink     chan struct{}

	// webGroup is rbweb's group ID (-1 = unknown), for the HTTPS key copy.
	webGroup int
	// browser says how to start the login browser (see portalbrowser.go).
	browser browserSettings
	// dohEnv is what the DoH forwarder was last started with ("" while it
	// is stopped; "?" until rbd has told it either).
	dohEnv string
	// diskFree reports the free space where a path is (tests fake it).
	diskFree func(path string) (int64, error)

	// usbRadioWait is how long startup waits for a USB Wi-Fi adapter to
	// appear (see waitForRadios). Tests set it to zero.
	usbRadioWait time.Duration
	// regdb is the Wi-Fi regulatory database (nil without one; see
	// hotspotstart.go).
	regdb regdb.DB
	// lastCertCheck is when renewCertificateIfDue last looked (https.go).
	lastCertCheck time.Time
	// lookupIP looks up a host name's addresses (the VPN server's); an
	// address is answered as it is. Tests fake it.
	lookupIP func(ctx context.Context, host string) ([]netip.Addr, error)
	// endpointAddresses remembers where each VPN server's name pointed the
	// last time it was looked up, keyed by "host:port" (for example
	// "vpn.example.com:51820" → "203.0.113.7:51820"). WireGuard is only
	// ever given these addresses, never the name, so that `wg setconf`
	// can't hang on a lookup while a.mu is held. Guarded by a.mu.
	endpointAddresses map[string]string
	// dnsmasqApplied is the dnsmasq.conf that dnsmasq was last started
	// with. applyDHCP skips the restart when nothing changed (see there).
	// Guarded by a.mu.
	dnsmasqApplied string
}

// New creates an agent that uses the given file locations and command
// runner. Production code passes paths.Default() and system.ExecRunner{}.
func New(p paths.Paths, runner system.Runner) *Agent {
	a := &Agent{
		paths:      p,
		runner:     runner,
		afterFunc:  time.AfterFunc,
		now:        time.Now,
		sleep:      time.Sleep,
		dhcpProbe:  dhcpprobe.Probe,
		decompress: zstdDecompress,
		lan:        render.DefaultLAN,
		webGroup:   -1,
		browser:    defaultBrowserSettings(),
		diskFree:   diskFreeBytes,
		dohEnv:     "?",
		speedtest:  speedtestState{server: speedtest.DefaultServer, run: runSpeedtest},
		plugins:    pluginsState{runner: runner},
		// USB adapters usually appear within a few seconds of power-on.
		usbRadioWait: 20 * time.Second,
		lookupIP: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
	}
	a.plugins.client = a.pluginHTTPClient
	a.plugins.watchEvery = pluginWatchEvery
	a.background = func(f func()) { go f() }
	if _, real := runner.(system.ExecRunner); real {
		// Podman downloads and loads images: much longer than other commands.
		a.plugins.runner = system.ExecRunner{Timeout: 45 * time.Minute}
	}
	return a
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

	// A backup uploaded for a restore that never finished (RasBeacon
	// restarted in between) is only taking room.
	if err := os.Remove(a.restoreUploadFile()); err == nil {
		slog.Info("removed a backup left from an unfinished restore")
	}

	// 0. Don't let the clock go backwards across a reboot, and see whether
	//    this start is the trial of an update.
	a.applyTimeFloor()
	a.checkTrialStart()

	// 1. Factory reset requested by placing a file on the SD card?
	if err := a.handleFactoryResetFile(); err != nil {
		return err
	}

	// 2. Load settings, creating them on first boot.
	if err := a.loadOrCreateSettings(); err != nil {
		return err
	}

	// 2a. RasBeacon's name, and the installed plugins (started once the network is up).
	a.applyHostname()
	a.loadPlugins()

	// 2b. The swap file (its size is a setting; a problem isn't fatal).
	if err := a.applySwap(); err != nil {
		slog.Warn("swap could not be set up", "error", err)
	}

	// 2c. The HTTPS certificate for rbweb, if HTTPS is on, renewed first
	//     if it runs out soon. rbweb looks for its copy in /run/rbd/tls
	//     only when it starts, and systemd may start it before rbd gets
	//     here (/run is empty after every boot). So when the copy changes,
	//     rbweb is restarted to pick it up.
	if a.certificateDue() {
		if err := a.generateCertificate(); err != nil {
			slog.Error("could not make a new HTTPS certificate", "error", err)
		}
	}
	if changed, err := a.applyHTTPS(); err != nil {
		slog.Error("HTTPS could not be set up; the web UI stays on HTTP", "error", err)
		a.warnings = append(a.warnings, "HTTPS could not be set up: "+err.Error())
	} else if changed {
		if err := system.ServiceRestart(a.runner, rbwebService); err != nil {
			slog.Error("could not restart the web server for HTTPS", "error", err)
		}
	}

	// 3. Firewall first, so the web UI is only reachable from the hotspot.
	//    Only then may the kernel forward packets between interfaces.
	if err := a.applyFirewall(); err != nil {
		return err
	}
	if err := a.enableForwarding(); err != nil {
		return err
	}

	// 4. Find the Wi-Fi radios and pick the hotspot radio, and read which
	//    channels each country allows.
	a.loadRegulatoryDB()
	if err := a.setUpRadios(); err != nil {
		slog.Error("no usable hotspot radio", "error", err)
		a.mode = api.ModeNoRadio
		return nil
	}

	// 5. Start the setup network or the owner's hotspot, and the uplink.
	if a.cfg.Claimed {
		if err := a.startNormalMode(); err != nil {
			return err
		}
		// The plugins start once, here: a later startNormalMode (after a
		// hotspot or guest change) leaves them running.
		a.background(a.startPlugins)
		a.scheduleConfirmation() // only does something on an update's trial
		return nil
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

// wipeData deletes everything in RasBeacon's data directory, the plugins'
// images and containers, and the swap file.
func (a *Agent) wipeData() error {
	a.stopAllPlugins()
	a.removeSwapFile()
	if err := os.RemoveAll(a.paths.DataDir()); err != nil {
		return fmt.Errorf("erasing settings: %w", err)
	}
	if err := os.RemoveAll(a.paths.ContainersDir()); err != nil {
		slog.Warn("could not erase the plugins' images", "error", err)
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
	fileVersion := a.cfg.Version
	a.cfg.FillDefaults()

	// RasBeacon's private IPv6 address on the hotspot comes from the
	// device's random IPv6 prefix.
	if ipv6, err := render.ULALANAddress(a.identity.ULAPrefix); err == nil {
		a.lan.IPv6Prefix = ipv6
	} else {
		slog.Warn("no IPv6 for the hotspot", "error", err)
	}

	// Secrets: empty until the device is claimed.
	err = config.ReadJSON(a.paths.SecretsFile(), &a.secrets)
	if errors.Is(err, config.ErrNotFound) {
		a.secrets = config.Secrets{}
		err = nil
	}
	if err != nil {
		return err
	}

	// Version 2 kept a single VPN server; it becomes a profile. Both files
	// are saved now, so the keys can't be separated from their profile.
	if fileVersion > 0 && fileVersion < 3 {
		raw, err := os.ReadFile(a.paths.ConfigFile())
		if err != nil {
			return err
		}
		if err := config.MoveVPNIntoProfile(raw, &a.cfg, &a.secrets); err != nil {
			return fmt.Errorf("upgrading the VPN settings: %w", err)
		}
		if len(a.cfg.VPN.Profiles) > 0 {
			if err := config.WriteJSON(a.paths.SecretsFile(), a.secrets, 0o600); err != nil {
				return err
			}
			if err := config.WriteJSON(a.paths.ConfigFile(), a.cfg, 0o644); err != nil {
				return err
			}
			slog.Info("VPN settings moved into a profile", "profile", a.cfg.VPN.Active)
		}
	}
	return nil
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
	return system.EnsureBridge(a.runner, render.BridgeName, a.lanAddresses()...)
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
		a.sleep(time.Second) // a.sleep, so tests don't really wait
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
	// The uplink gives the device itself internet access (for the clock),
	// but nothing is forwarded for devices until setup is finished.
	if err := a.applyUplinkConfig(); err != nil {
		a.warnings = append(a.warnings, "Could not set up the internet connection: "+err.Error())
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
	a.dnsmasqApplied = "" // stopped: the next applyDHCP must start it
	a.mode = api.ModeSetupClosed
}

// startNormalMode runs the owner's hotspot.
func (a *Agent) startNormalMode() error {
	h := a.cfg.Hotspot
	hotspot, err := a.ownerHotspot()
	if err != nil {
		return err
	}
	// The guest network's bridge must exist before hostapd starts, and
	// the plugins' before dnsmasq does (it answers plugins' DNS).
	a.applyGuestBridge(hotspot.Guest != nil)
	a.pluginBridge = a.ensurePluginNetwork()
	// The VPN first, so DNS for devices goes to the VPN from the start.
	if err := a.applyVPNConfig(); err != nil {
		slog.Error("could not set up the VPN", "error", err)
		a.warnings = append(a.warnings, "Could not set up the VPN: "+err.Error())
	}
	if err := a.applyHotspotAndDHCP(hotspot, false); err != nil {
		return err
	}
	// From here on, devices' traffic may be forwarded to the uplinks (the
	// firewall rules depend on the mode).
	a.mode = api.ModeNormal
	if err := a.applySSH(); err != nil { // also reloads the firewall
		a.warnings = append(a.warnings, "Could not apply SSH settings: "+err.Error())
	}
	if err := a.applyUplinkConfig(); err != nil {
		a.warnings = append(a.warnings, "Could not set up the internet connection: "+err.Error())
	}
	a.checkUplink() // fill in the status page right away
	slog.Info("hotspot started", "ssid", h.SSID, "band", h.Band)
	return nil
}

// ownerHotspot describes the owner's hotspot for hostapd, and writes the
// allow list it reads when that is on.
func (a *Agent) ownerHotspot() (render.HotspotInput, error) {
	h := a.cfg.Hotspot
	hotspot := render.HotspotInput{
		Interface:  radio.HotspotInterfaceName,
		SSID:       h.SSID,
		Band:       a.hotspotBand(),
		Channel:    a.hotspotChannel(),
		Country:    h.Country,
		Security:   a.hotspotSecurity(),
		Password:   a.secrets.HotspotPassword,
		MaxDevices: a.cfg.Access.MaxDevices,
		Guest:      a.guestHostapd(),
	}
	if a.cfg.Access.AllowListOn {
		list := strings.Join(a.cfg.Access.Allowed, "\n") + "\n"
		if err := config.WriteFileAtomic(a.paths.HostapdAcceptFile(), []byte(list), 0o600); err != nil {
			return hotspot, err
		}
		hotspot.AcceptMACFile = a.paths.HostapdAcceptFile()
	}
	return hotspot, nil
}

// applyHotspotAndDHCP writes hostapd.conf and dnsmasq.conf and restarts both
// programs. captive=true makes every DNS name point at RasBeacon (setup mode).
func (a *Agent) applyHotspotAndDHCP(hotspot render.HotspotInput, captive bool) error {
	hostapdConf, err := render.Hostapd(hotspot)
	if err != nil {
		return err
	}
	// hostapd.conf contains the Wi-Fi password, so only root may read it.
	if err := config.WriteFileAtomic(a.paths.HostapdConfig(), []byte(hostapdConf), 0o600); err != nil {
		return err
	}
	if err := system.ServiceRestart(a.runner, hostapdService); err != nil {
		return fmt.Errorf("starting the hotspot: %w", err)
	}
	return a.applyDHCP(captive)
}

// applyDHCP writes dnsmasq.conf for the current hotspot network and uplink,
// and restarts dnsmasq. captive=true is for setup mode (see render.Dnsmasq).
func (a *Agent) applyDHCP(captive bool) error {
	input := render.DnsmasqInput{
		LAN:              a.lan,
		LeaseFile:        a.paths.DnsmasqLeases(),
		Hostname:         a.cfg.Name(),
		Captive:          captive,
		Upstream:         a.upstreamDNS(),
		Plugins:          a.pluginBridge,
		IPv6DefaultRoute: a.devicesHaveIPv6Internet(),
		Guest:            a.guestLAN(),
		Metered:          a.cfg.Hotspot.Metered,
		FixedAddresses:   a.fixedAddresses(),
	}
	if a.noticeActive() {
		input.NoticeHostnames = portal.ProbeHostnames
	}
	if captive {
		input.LAN.IPv6Prefix = netip.Prefix{} // no IPv6 for the setup network
	}
	dnsmasqConf, err := render.Dnsmasq(input)
	if err != nil {
		return err
	}
	// The DoH forwarder first, so it answers by the time dnsmasq asks it.
	if err := a.applyDoH(captive); err != nil {
		slog.Warn("the DNS-over-HTTPS forwarder could not be started", "error", err)
	}
	// Many saves (a VPN switch, a device's name) end up here without
	// changing anything for dnsmasq. Each restart drops DNS and DHCP for a
	// moment (the owner's report had 8 in 2 minutes), so it only happens
	// when the file changed, or when dnsmasq isn't running.
	if dnsmasqConf == a.dnsmasqApplied && a.serviceIsActive(dnsmasqService) {
		return nil
	}
	if err := config.WriteFileAtomic(a.paths.DnsmasqConfig(), []byte(dnsmasqConf), 0o644); err != nil {
		return err
	}
	a.dnsmasqApplied = "" // until the restart below has worked
	if err := system.ServiceRestart(a.runner, dnsmasqService); err != nil {
		return fmt.Errorf("starting DHCP/DNS: %w", err)
	}
	a.dnsmasqApplied = dnsmasqConf
	return nil
}

// bridgeNetfilterOff makes sure frames passing through the hotspot's
// bridge aren't sent through the IP firewall as well. That happens when
// the kernel's br_netfilter module is loaded (container tools load it) and
// breaks port forwarding between two devices on the hotspot ("hairpin"):
// the bridge would have to send the forwarded frame back out of the port
// it came in on, which it refuses. RasBeacon's rules don't need it: the
// plugins' isolation has its own bridge table (see the firewall). Without
// the module there is nothing to do.
func (a *Agent) bridgeNetfilterOff() {
	for _, name := range []string{"bridge-nf-call-iptables", "bridge-nf-call-ip6tables", "bridge-nf-call-arptables"} {
		path := filepath.Join(a.paths.ProcSysNetBridge(), name)
		if current, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(current)) != "0" {
			if err := os.WriteFile(path, []byte("0\n"), 0o644); err != nil {
				slog.Warn("could not turn off bridge filtering", "setting", name, "error", err)
			}
		}
	}
}

// applyFirewall writes and loads the firewall rules for the current
// settings.
func (a *Agent) applyFirewall() error {
	a.bridgeNetfilterOff()
	input := render.FirewallInput{
		SSHEnabled:      a.cfg.SSH.Enabled,
		Egress:          a.egressInterfaces(),
		TunnelDNS:       a.tunnelDNSGuard(),
		TunnelInterface: routing.TunnelInterface,
		PortalOnly:      a.mode == api.ModeNormal && a.portal.loginMode,
		Guest:           a.guestActive(),
		RoutedIPv6:      a.wantedRoutedIPv6(),
		PausedMACs:      a.pausedMACs(),
		PausedIPv4:      a.pausedIPv4(),
		PausedIPv6:      a.pausedIPv6(),
	}
	if a.mode == api.ModeNormal {
		input.Plugins = a.pluginRules()
		input.LANAddress = a.lan.Prefix.Addr()
		input.Uplinks = a.uplinkInterfaces()
		input.PortForwards = a.portForwards()
		if g := a.guestLAN(); g != nil {
			input.GuestAddress = g.Prefix.Addr()
		}
	}
	if a.browser.uid > 0 {
		input.Browser = &render.BrowserRule{UID: a.browser.uid, Uplinks: a.uplinkInterfaces()}
	}
	rules, err := render.Firewall(input)
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
