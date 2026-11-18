package agent

// This file runs the "zero-leak" way to log in to a hotel or café network:
// RasBeacon opens the login page in its own browser (internal/browser) and
// shows it on your phone as pictures. Your taps and typing are sent back to
// that browser. Your devices' traffic never leaves the VPN, unlike login
// mode (portal.go), which lets devices reach the network directly for a
// few minutes.
//
// The browser runs as the unprivileged user rbbrowser, and the firewall
// only lets that user reach the uplink (render.FirewallInput.Browser). It
// closes by itself once the login worked, after BrowserIdleTimeout without
// a tap, or after BrowserMaxTime.
//
// Like login mode, it is available to any device on your own hotspot
// (not guests) without logging in to RasBeacon, because it starts from the
// notice page that opens in a phone's "Log in to network" sheet. But only
// the device that opened it may see the page and use it: what is typed
// there (a room number, a name, a card) is that device's. rbweb gives it a
// secret in a cookie (api.PortalBrowserStart.Session); another device may
// open the browser for itself once nobody has used it for browserTakeover.

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/browser"
	"rasbeacon/internal/config"
	"rasbeacon/internal/portal"
	"rasbeacon/internal/tunnel"
)

const (
	// BrowserIdleTimeout closes the browser when nobody tapped or typed
	// for this long.
	BrowserIdleTimeout = 5 * time.Minute
	// BrowserMaxTime closes the browser after this long in any case.
	BrowserMaxTime = 30 * time.Minute
	// browserTakeover is how long the login browser must have gone unused
	// (no picture asked for, no tap or typing) before another device may
	// open it for itself.
	browserTakeover = 20 * time.Second
	// browserUser is the unprivileged user the browser runs as.
	browserUser = "rbbrowser"
)

// loginBrowser is what the agent needs from a browser session
// (*browser.Session, or a stand-in in tests).
type loginBrowser interface {
	NextFrame(ctx context.Context, after int64) browser.Frame
	Navigate(address string) error
	Click(x, y float64) error
	Scroll(x, y, deltaY float64) error
	Type(text string) error
	Key(name string) error
	Back() error
	Reload() error
	Done() <-chan struct{}
	Close()
}

// browserSettings says how to start the login browser.
type browserSettings struct {
	binary string
	// user runs the browser ("" = rbd's own user, for the simulated device).
	user string
	// uid is user's number for the firewall rule, or -1 if there is no
	// such user (then the browser can't be used).
	uid       int
	noSandbox bool
	start     func(browser.Options) (loginBrowser, error)
}

// defaultBrowserSettings is the device's setup: Debian's headless Chromium
// running as rbbrowser.
func defaultBrowserSettings() browserSettings {
	return browserSettings{
		binary: browser.DefaultBinary,
		user:   browserUser,
		uid:    lookupUserID(browserUser),
		start:  startRealBrowser,
	}
}

// startRealBrowser starts Chromium as the login browser.
func startRealBrowser(opts browser.Options) (loginBrowser, error) {
	s, err := startChromium(opts)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// startChromium starts Chromium, with its sandbox. If the sandbox can't
// work on this system (it needs user namespaces), the browser doesn't
// start: it shows pages from the network it logs in to, and without the
// sandbox a flaw in Chromium would reach rbbrowser's files and programs
// directly. (The simulated device turns the sandbox off itself, as it runs
// as root; see SetBrowser.)
func startChromium(opts browser.Options) (*browser.Session, error) {
	s, err := browser.Start(opts)
	if err != nil && !opts.NoSandbox && strings.Contains(strings.ToLower(err.Error()), "sandbox") {
		return nil, fmt.Errorf("Chromium's sandbox can't start here, and RasBeacon doesn't run it without: %w", err)
	}
	return s, err
}

// errBrowserInUse refuses to open the login browser for a device while
// another one uses it.
var errBrowserInUse = &config.ValidationError{Field: "session", Message: "Another device is using the login browser. It can be opened here once it has been left alone for 20 seconds."}

// errNotYourBrowser refuses to show or work the login browser for a device
// that didn't open it.
var errNotYourBrowser = &config.ValidationError{Field: "session", Message: "The login browser was opened on another device."}

// validBrowserSession reports whether s looks like rbweb's secret for a
// device: 64 hex digits.
func validBrowserSession(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// ownsBrowser reports whether session is the secret of the device that
// opened the login browser. The caller holds a.mu.
func (a *Agent) ownsBrowser(session string) bool {
	return a.portal.browser != nil && a.portal.browserSession != "" &&
		subtle.ConstantTimeCompare([]byte(session), []byte(a.portal.browserSession)) == 1
}

// SetBrowser changes how the login browser is started (for the simulated
// device: another binary, rbd's own user, and no sandbox when that user is
// root). Call it before Start.
func (a *Agent) SetBrowser(binary string, noSandbox bool) {
	a.browser = browserSettings{binary: binary, uid: -1, noSandbox: noSandbox, start: startRealBrowser}
}

// lookupUserID returns a user's number, or -1 if there is no such user.
func lookupUserID(name string) int {
	u, err := user.Lookup(name)
	if err != nil {
		return -1
	}
	id, err := strconv.Atoi(u.Uid)
	if err != nil {
		return -1
	}
	return id
}

// browserAvailable reports whether the login browser is installed and can
// run as its own user.
func (a *Agent) browserAvailable() bool {
	if a.browser.binary == "" {
		return false
	}
	if _, err := os.Stat(a.browser.binary); err != nil {
		return false
	}
	return a.browser.user == "" || a.browser.uid > 0
}

// canBrowse reports whether the login browser may be opened now: a login
// page is in the way (or the VPN can't connect, perhaps because of one
// the check missed), or the browser is already open.
func (a *Agent) canBrowse() bool {
	if a.portal.browser != nil {
		return true
	}
	if a.mode != api.ModeNormal || !a.uplink.status.Connected || !a.browserAvailable() {
		return false
	}
	return a.portal.result.State == portal.Portal || (a.vpnActive() && a.vpn.machine.State() != tunnel.Up)
}

// StartPortalBrowser opens the network's login page in RasBeacon's own
// browser, sized for the phone screen in req.
func (a *Agent) StartPortalBrowser(req api.PortalBrowserStart) error {
	if !validBrowserSession(req.Session) {
		return &config.ValidationError{Field: "session", Message: "This browser has no session for the login browser."}
	}
	a.mu.Lock()
	if !a.canBrowse() || a.portal.browserStarting {
		a.mu.Unlock()
		return ErrWrongMode
	}
	// Another device's, still in use: not taken from it.
	if a.portal.browser != nil && !a.ownsBrowser(req.Session) && a.now().Sub(a.portal.browserSeen) < browserTakeover {
		a.mu.Unlock()
		return errBrowserInUse
	}
	previous := a.portal.browser
	a.portal.browser = nil
	a.portal.browserStarting = true
	target := a.portal.result.LoginURL
	if target == "" {
		// Opening any plain http:// address makes the network show its
		// login page.
		target = portal.Checks[0].URL
	}
	opts := browser.Options{
		Binary: a.browser.binary, User: a.browser.user, NoSandbox: a.browser.noSandbox,
		ProfileParent: a.paths.BrowserDir(), Width: req.Width, Height: req.Height,
	}
	start := a.browser.start
	a.mu.Unlock()

	// Starting the browser takes a few seconds; don't block everything
	// else meanwhile.
	if previous != nil {
		previous.Close()
	}
	b, err := start(opts)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.portal.browserStarting = false
	if err != nil {
		slog.Error("login browser could not start", "error", err)
		return fmt.Errorf("the login browser could not start: %w", err)
	}
	a.portal.browser = b
	a.portal.browserSession = req.Session
	a.portal.browserSeen = a.now()
	a.portal.browserStarted = a.now()
	a.portal.browserUsed = a.now()
	a.portal.browserSawPortal = a.portal.result.State == portal.Portal
	a.portal.probedAt = time.Time{} // check again soon
	slog.Info("login browser opened", "url", target)
	go func() {
		if err := b.Navigate(target); err != nil {
			slog.Warn("login browser: could not open the login page", "error", err)
		}
	}()
	return nil
}

// StopPortalBrowser closes the login browser, for the device that opened
// it.
func (a *Agent) StopPortalBrowser(req api.PortalBrowserStop) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.portal.browser == nil {
		return nil
	}
	if !a.ownsBrowser(req.Session) {
		return errNotYourBrowser
	}
	a.stopBrowser("closed from the web UI")
	return nil
}

// stopBrowser closes the browser, if one is open. The caller holds a.mu.
func (a *Agent) stopBrowser(reason string) {
	b := a.portal.browser
	if b == nil {
		return
	}
	a.portal.browser, a.portal.browserSession = nil, ""
	slog.Info("login browser closed", "reason", reason)
	go b.Close() // takes a moment; don't hold the lock meanwhile
}

// checkBrowser closes the browser when the login worked, it wasn't used
// for a while, its time is up, or it stopped by itself. CheckPortal calls
// it every few seconds. The caller holds a.mu.
func (a *Agent) checkBrowser() {
	b := a.portal.browser
	if b == nil {
		return
	}
	if a.portal.result.State == portal.Portal {
		a.portal.browserSawPortal = true
	}
	select {
	case <-b.Done():
		a.portal.browser = nil
		slog.Warn("login browser stopped by itself")
		return
	default:
	}
	switch {
	case a.portal.browserSawPortal && a.portal.result.State == portal.Open:
		a.stopBrowser("the login worked")
	case a.now().Sub(a.portal.browserUsed) >= BrowserIdleTimeout:
		a.stopBrowser("not used for a while")
	case a.now().Sub(a.portal.browserStarted) >= BrowserMaxTime:
		a.stopBrowser("time is up")
	}
}

// PortalBrowserFrame waits (until ctx ends) for a picture of the page newer
// than after, and returns the newest one, to the device that opened the
// browser (session).
func (a *Agent) PortalBrowserFrame(ctx context.Context, after int64, session string) (api.PortalBrowserFrame, error) {
	a.mu.Lock()
	b := a.portal.browser
	if b != nil && !a.ownsBrowser(session) {
		a.mu.Unlock()
		return api.PortalBrowserFrame{}, errNotYourBrowser
	}
	a.portal.browserSeen = a.now()
	a.mu.Unlock()
	if b == nil {
		return api.PortalBrowserFrame{}, nil
	}
	f := b.NextFrame(ctx, after)
	// Still watched (the wait can take a while).
	a.mu.Lock()
	if a.portal.browser == b {
		a.portal.browserSeen = a.now()
	}
	a.mu.Unlock()
	return api.PortalBrowserFrame{
		Running: true, Seq: f.Seq, Image: base64.StdEncoding.EncodeToString(f.JPEG),
		Width: f.Width, Height: f.Height, URL: f.URL, Title: f.Title,
	}, nil
}

// PortalBrowserInput passes a tap, scroll or typing to the browser, from
// the device that opened it.
func (a *Agent) PortalBrowserInput(in api.PortalBrowserInput) error {
	a.mu.Lock()
	b := a.portal.browser
	if b != nil && !a.ownsBrowser(in.Session) {
		a.mu.Unlock()
		return errNotYourBrowser
	}
	if b != nil {
		a.portal.browserUsed = a.now()
		a.portal.browserSeen = a.now()
	}
	a.mu.Unlock()
	if b == nil {
		return ErrWrongMode
	}

	var err error
	switch in.Action {
	case "click":
		err = b.Click(in.X, in.Y)
	case "scroll":
		err = b.Scroll(in.X, in.Y, in.DeltaY)
	case "type":
		if len(in.Text) > 1000 {
			return &config.ValidationError{Field: "text", Message: "Type at most 1000 characters at a time."}
		}
		err = b.Type(in.Text)
	case "key":
		switch in.Key {
		case "Enter", "Backspace", "Tab", "Escape":
		default:
			return &config.ValidationError{Field: "key", Message: "Must be Enter, Backspace, Tab or Escape."}
		}
		err = b.Key(in.Key)
	case "back":
		err = b.Back()
	case "reload":
		err = b.Reload()
	case "navigate":
		if err = b.Navigate(in.URL); err == browser.ErrNotWeb {
			return &config.ValidationError{Field: "url", Message: "Only http:// and https:// addresses can be opened."}
		}
	default:
		return &config.ValidationError{Field: "action", Message: fmt.Sprintf("Unknown action %q.", in.Action)}
	}
	if err != nil {
		return &config.ValidationError{Field: "action", Message: "The browser didn't respond: " + err.Error()}
	}
	return nil
}
