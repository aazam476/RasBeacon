package agent

// The welcome page: the owner's own captive portal (Guests page → Welcome
// page). Until a device accepts it, its web requests show the page (served
// by rbweb on render.WelcomePort) and nothing else of it is forwarded; see
// welcome_ok in firewall.nft.tmpl. Devices that accepted are kept, with
// when to ask again, in WelcomeAcceptedFile.

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
	// wired are devices on wired ports, let through without the page
	// unless the owner includes them (see refreshWelcome).
	wired map[string]bool
	// failures counts wrong codes per device, and until when it must wait.
	failures map[string]codeFailures
}

type codeFailures struct {
	count int
	until time.Time
}

// welcomeConfig is the welcome page's settings, or the defaults before the
// owner saved any (settings from before the page existed). The caller
// holds a.mu.
func (a *Agent) welcomeConfig() config.WelcomeConfig {
	if a.cfg.Welcome.Networks == "" {
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
		Enabled: w.Enabled, Networks: w.Networks, Title: w.Title, Message: w.Message, Terms: w.Terms,
		AccessCode: w.AccessCode, AskAgainHours: w.AskAgainHours, IncludeWired: w.IncludeWired,
		Color: w.Color, Logo: w.Logo, CustomPage: w.CustomPage,
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
		Enabled: req.Enabled, Networks: req.Networks, Title: strings.TrimSpace(req.Title),
		Message: strings.TrimSpace(req.Message), Terms: strings.TrimSpace(req.Terms),
		AccessCode: strings.TrimSpace(req.AccessCode), AskAgainHours: req.AskAgainHours,
		IncludeWired: req.IncludeWired, Color: req.Color, Logo: req.Logo, CustomPage: req.CustomPage,
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
	slog.Info("welcome page saved", "enabled", w.Enabled, "networks", w.Networks)
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

// welcomeInterfaces are the bridges the welcome page is on now (none while
// it is off). The caller holds a.mu.
func (a *Agent) welcomeInterfaces() []string {
	w := a.welcomeConfig()
	if !w.Enabled || a.mode != api.ModeNormal {
		return nil
	}
	var names []string
	if w.Networks != config.WelcomeOnGuest {
		names = append(names, render.BridgeName)
	}
	if w.Networks != config.WelcomeOnHotspot && a.guestActive() {
		names = append(names, render.GuestBridgeName)
	}
	return names
}

// welcomeRule is the welcome page for the firewall, or nil while it is
// off. The caller holds a.mu.
func (a *Agent) welcomeRule() *render.WelcomeRule {
	interfaces := a.welcomeInterfaces()
	if len(interfaces) == 0 {
		return nil
	}
	rule := &render.WelcomeRule{Interfaces: interfaces, OwnAddresses: []netip.Addr{a.lan.Prefix.Addr()}}
	if g := a.guestLAN(); g != nil && a.guestActive() {
		rule.OwnAddresses = append(rule.OwnAddresses, g.Prefix.Addr())
	}
	now := a.now()
	for _, mac := range sortedKeys(a.acceptedDevices()) {
		device := render.WelcomeDevice{MAC: mac}
		if until := a.welcome.accepted[mac]; !until.IsZero() {
			device.Seconds = max(1, int(until.Sub(now).Seconds()))
		}
		rule.Accepted = append(rule.Accepted, device)
	}
	for _, mac := range sortedKeys(a.welcome.wired) {
		if _, done := a.welcome.accepted[mac]; !done {
			rule.Accepted = append(rule.Accepted, render.WelcomeDevice{MAC: mac})
		}
	}
	return rule
}

// refreshWelcome lets devices on wired ports through without the page
// (unless the owner includes them), loading the firewall again when one
// appears. The uplink monitor calls it; the caller holds a.mu.
func (a *Agent) refreshWelcome() {
	w := a.welcomeConfig()
	wired := map[string]bool{}
	if w.Enabled && !w.IncludeWired && w.Networks != config.WelcomeOnGuest {
		for _, c := range a.wiredClients() {
			wired[c.MAC] = true
		}
	}
	changed := len(wired) != len(a.welcome.wired)
	for mac := range wired {
		changed = changed || !a.welcome.wired[mac]
	}
	if !changed {
		return
	}
	a.welcome.wired = wired
	if err := a.applyFirewall(); err != nil {
		slog.Error("could not update the welcome page's devices", "error", err)
	}
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
// welcome page. The caller holds a.mu.
func (a *Agent) welcomeNeeded(guest bool) bool {
	bridge := render.BridgeName
	if guest {
		bridge = render.GuestBridgeName
	}
	for _, name := range a.welcomeInterfaces() {
		if name == bridge {
			return true
		}
	}
	return false
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
		Active: true, Accepted: accepted || a.welcome.wired[mac],
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
	if accepted || a.welcome.wired[c.MAC] {
		c.Welcome = welcomeAccepted
	} else {
		c.Welcome = welcomeWaiting
	}
}
