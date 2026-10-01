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
// notice page that opens in a phone's "Log in to network" sheet.

import (
	"context"
	"encoding/base64"
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

// startRealBrowser starts Chromium. If its sandbox can't work on this
// system (it needs user namespaces), it starts again without it: the
// browser still runs as its own user, behind the firewall rule.
func startRealBrowser(opts browser.Options) (loginBrowser, error) {
	s, err := browser.Start(opts)
	if err != nil && !opts.NoSandbox && strings.Contains(strings.ToLower(err.Error()), "sandbox") {
		slog.Warn("login browser: Chromium's sandbox is not available here; starting without it", "error", err)
		opts.NoSandbox = true
		s, err = browser.Start(opts)
	}
	if err != nil {
		return nil, err
	}
	return s, nil
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
	a.mu.Lock()
	if !a.canBrowse() || a.portal.browserStarting {
		a.mu.Unlock()
		return ErrWrongMode
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

// StopPortalBrowser closes the login browser.
func (a *Agent) StopPortalBrowser() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopBrowser("closed from the web UI")
}

// stopBrowser closes the browser, if one is open. The caller holds a.mu.
func (a *Agent) stopBrowser(reason string) {
	b := a.portal.browser
	if b == nil {
		return
	}
	a.portal.browser = nil
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
// than after, and returns the newest one.
func (a *Agent) PortalBrowserFrame(ctx context.Context, after int64) api.PortalBrowserFrame {
	a.mu.Lock()
	b := a.portal.browser
	a.mu.Unlock()
	if b == nil {
		return api.PortalBrowserFrame{}
	}
	f := b.NextFrame(ctx, after)
	return api.PortalBrowserFrame{
		Running: true, Seq: f.Seq, Image: base64.StdEncoding.EncodeToString(f.JPEG),
		Width: f.Width, Height: f.Height, URL: f.URL, Title: f.Title,
	}
}

// PortalBrowserInput passes a tap, scroll or typing to the browser.
func (a *Agent) PortalBrowserInput(in api.PortalBrowserInput) error {
	a.mu.Lock()
	b := a.portal.browser
	if b != nil {
		a.portal.browserUsed = a.now()
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
