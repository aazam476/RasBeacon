package agent

// The welcome page: the owner's own captive portal on the guest network
// (Guests page → Welcome page). The hotspot is for the owner's own devices,
// so it never shows the page. Until a guest's device accepts it, its web
// requests show the page (served by rbweb on render.WelcomePort) and
// nothing else of it is forwarded; see welcome_ok in firewall.nft.tmpl.
// Devices that accepted are kept, with when to ask again, in
// WelcomeAcceptedFile.

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
	"rasbeacon/internal/stations"
)

// Welcome states of a device (stations.Station.Welcome).
const (
	welcomeWaiting  = "waiting"
	welcomeAccepted = "accepted"
)

// welcomeTries is how many wrong access codes a device may type before it
// must wait welcomeWait.
const (
	welcomeTries = 5
	welcomeWait  = time.Minute
)

// errWrongCode means the access code typed on the welcome page is wrong.
var errWrongCode = &config.ValidationError{Field: "code", Message: "That code isn't right."}

// errTooManyCodes means a device typed too many wrong codes in a row.
var errTooManyCodes = &config.ValidationError{Field: "code", Message: "Too many wrong codes. Try again in a minute."}

// welcomeState is what rbd keeps about the welcome page between requests.
type welcomeState struct {
	// accepted is each device (by MAC address) that accepted the page,
	// with when it is asked again (zero: not until the owner asks). Read
	// from WelcomeAcceptedFile on first use.
	accepted map[string]time.Time
	loaded   bool
	// failures counts wrong codes per device, and until when it must wait.
	failures map[string]codeFailures
}

// codeFailures is how many wrong access codes a device typed in a row,
// and until when it must wait after too many.
type codeFailures struct {
	count int
	until time.Time
}

// welcomeConfig is the welcome page's settings, or the defaults before the
// owner saved any (settings from before the page existed have no title,
// which a saved page always has). The caller holds a.mu.
func (a *Agent) welcomeConfig() config.WelcomeConfig {
	if a.cfg.Welcome.Title == "" {
		return config.DefaultWelcome()
	}
	return a.cfg.Welcome
}

// WelcomeSettings is the Welcome page settings.
func (a *Agent) WelcomeSettings() api.WelcomeSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	w := a.welcomeConfig()
	return api.WelcomeSettings{
		Enabled: w.Enabled, Title: w.Title, Message: w.Message, Terms: w.Terms,
		AccessCode: w.AccessCode, AskAgainHours: w.AskAgainHours, Color: w.Color, Logo: w.Logo, CustomPage: w.CustomPage,
		Accepted: len(a.acceptedDevices()),
	}
}

// UpdateWelcomeSettings saves the Welcome page settings and loads the
// firewall again.
func (a *Agent) UpdateWelcomeSettings(req api.WelcomeSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	w := config.WelcomeConfig{
		Enabled: req.Enabled, Title: strings.TrimSpace(req.Title),
		Message: strings.TrimSpace(req.Message), Terms: strings.TrimSpace(req.Terms),
		AccessCode: strings.TrimSpace(req.AccessCode), AskAgainHours: req.AskAgainHours,
		Color: req.Color, Logo: req.Logo, CustomPage: req.CustomPage,
	}
	if err := w.Validate(); err != nil {
		return err
	}
	newCfg := a.cfg.Clone()
	newCfg.Welcome = w
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("welcome page saved", "enabled", w.Enabled)
	return a.applyFirewall()
}

// loadWelcome reads the accepted devices once. The caller holds a.mu.
func (a *Agent) loadWelcome() {
	if a.welcome.loaded {
		return
	}
	a.welcome.loaded = true
	a.welcome.accepted = map[string]time.Time{}
	if err := config.ReadJSON(a.paths.WelcomeAcceptedFile(), &a.welcome.accepted); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("could not read the devices that accepted the welcome page", "error", err)
	}
}

// acceptedDevices are the devices that accepted the welcome page and
// haven't run out yet, with when they run out (zero: never). The caller
// holds a.mu.
func (a *Agent) acceptedDevices() map[string]time.Time {
	a.loadWelcome()
	now := a.now()
	for mac, until := range a.welcome.accepted {
		if !until.IsZero() && !until.After(now) {
			delete(a.welcome.accepted, mac)
		}
	}
	return a.welcome.accepted
}

// saveWelcome writes the accepted devices. The caller holds a.mu.
func (a *Agent) saveWelcome() error {
	return config.WriteJSON(a.paths.WelcomeAcceptedFile(), a.welcome.accepted, 0o600)
}

// welcomeOn reports whether the welcome page is shown now: it is turned
// on and the guest network is up. The caller holds a.mu.
func (a *Agent) welcomeOn() bool {
	return a.welcomeConfig().Enabled && a.mode == api.ModeNormal && a.guestActive()
}

// welcomeRule is the welcome page for the firewall, or nil while it is
// off. The caller holds a.mu.
func (a *Agent) welcomeRule() *render.WelcomeRule {
	g := a.guestLAN()
	if !a.welcomeOn() || g == nil {
		return nil
	}
	rule := &render.WelcomeRule{
		Interfaces: []string{render.GuestBridgeName}, OwnAddresses: []netip.Addr{g.Prefix.Addr()},
	}
	now := a.now()
	for _, mac := range sortedKeys(a.acceptedDevices()) {
		device := render.WelcomeDevice{MAC: mac}
		if until := a.welcome.accepted[mac]; !until.IsZero() {
			device.Seconds = max(1, int(until.Sub(now).Seconds()))
		}
		rule.Accepted = append(rule.Accepted, device)
	}
	return rule
}

// welcomeDevice finds the device at ip on RasBeacon's networks: its MAC
// address and whether it is on the guest network. The caller holds a.mu.
func (a *Agent) welcomeDevice(ip string) (mac string, guest bool, ok bool) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "", false, false
	}
	leases, _ := os.ReadFile(a.paths.DnsmasqLeases())
	for _, lease := range stations.ParseLeases(string(leases)) {
		if lease.IP != addr.String() {
			continue
		}
		mac, err := normalizeMAC(lease.MAC)
		if err != nil {
			return "", false, false
		}
		g := a.guestLAN()
		return mac, g != nil && g.Prefix.Contains(addr), true
	}
	return "", false, false
}

// welcomeNeeded reports whether a device on that network must accept the
// welcome page: only guests do. The caller holds a.mu.
func (a *Agent) welcomeNeeded(guest bool) bool {
	return guest && a.welcomeOn()
}

// WelcomePage is what the welcome page shows the device at ip.
func (a *Agent) WelcomePage(ip string) api.WelcomePage {
	a.mu.Lock()
	defer a.mu.Unlock()
	mac, guest, ok := a.welcomeDevice(ip)
	if !ok || !a.welcomeNeeded(guest) {
		return api.WelcomePage{}
	}
	w := a.welcomeConfig()
	_, accepted := a.acceptedDevices()[mac]
	return api.WelcomePage{
		Active: true, Accepted: accepted,
		Title: w.Title, Message: w.Message, Terms: w.Terms, NeedCode: w.AccessCode != "",
		Color: w.Color, Logo: w.Logo, CustomPage: w.CustomPage,
	}
}

// AcceptWelcome lets the device that accepted the welcome page (at req.IP,
// with the access code if there is one) use the internet.
func (a *Agent) AcceptWelcome(req api.WelcomeAcceptRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	mac, guest, ok := a.welcomeDevice(req.IP)
	if !ok || !a.welcomeNeeded(guest) {
		return &config.ValidationError{Field: "ip", Message: "This device doesn't need to accept anything."}
	}
	w := a.welcomeConfig()
	if w.AccessCode != "" {
		if a.welcome.failures == nil {
			a.welcome.failures = map[string]codeFailures{}
		}
		f := a.welcome.failures[mac]
		if a.now().Before(f.until) {
			return errTooManyCodes
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.Code)), []byte(w.AccessCode)) != 1 {
			f.count++
			if f.count >= welcomeTries {
				f = codeFailures{until: a.now().Add(welcomeWait)}
			}
			a.welcome.failures[mac] = f
			return errWrongCode
		}
		delete(a.welcome.failures, mac)
	}
	until := time.Time{}
	if w.AskAgainHours > 0 {
		until = a.now().Add(time.Duration(w.AskAgainHours) * time.Hour)
	}
	a.acceptedDevices()[mac] = until
	if err := a.saveWelcome(); err != nil {
		return err
	}
	slog.Info("welcome page accepted", "mac", mac, "until", until)
	return a.applyFirewall()
}

// ResetWelcome asks a device to accept the welcome page again (Clients
// page → Ask again).
func (a *Agent) ResetWelcome(mac string) error {
	mac, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.acceptedDevices()[mac]; !ok {
		return nil
	}
	delete(a.welcome.accepted, mac)
	if err := a.saveWelcome(); err != nil {
		return err
	}
	slog.Info("welcome page asked again", "mac", mac)
	return a.applyFirewall()
}

// setWelcome fills in a device's welcome state for the Clients page. The
// caller holds a.mu.
func (a *Agent) setWelcome(c *stations.Station, guest bool) {
	if !a.welcomeNeeded(guest) {
		return
	}
	_, accepted := a.acceptedDevices()[c.MAC]
	if accepted {
		c.Welcome = welcomeAccepted
	} else {
		c.Welcome = welcomeWaiting
	}
}
