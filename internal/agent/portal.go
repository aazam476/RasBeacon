package agent

// This file handles captive portals: hotel or café networks that show a
// login page before letting anyone online (see internal/portal and
// docs/design.md section 4.6).
//
// The problem: with the VPN's kill switch on, devices may only use the
// tunnel. But the hotel blocks the tunnel until someone logs in, and nobody
// can reach the login page through the tunnel. So:
//
//  1. RunPortalMonitor checks for a login page over the uplink.
//  2. While one is in the way and the kill switch keeps devices offline,
//     RasBeacon shows a NOTICE: its DNS answers phones' connectivity checks
//     with its own address, so the phone's "Log in to network" sheet opens
//     on RasBeacon's page (the web UI's #portal screen).
//  3. The owner taps "Start" there: LOGIN MODE. For 10 minutes (extendable
//     to 30), devices may reach the uplink directly, but only for web and
//     DNS (render.FirewallInput.PortalOnly). The kill switch setting itself
//     stays on.
//  4. As soon as the check passes (logged in), or time runs out, or the
//     owner stops it, login mode ends and traffic goes back into the
//     tunnel. The firewall only lets established connections continue on
//     the current path, so nothing opened during login mode keeps going.

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/portal"
	"rasbeacon/internal/tunnel"
)

const (
	// LoginWindow is how long login mode lasts, and how much "extend" adds.
	LoginWindow = 10 * time.Minute
	// MaxLoginWindow is the longest login mode can run in total.
	MaxLoginWindow = 30 * time.Minute
)

// portalState is what rbd knows about captive portals.
type portalState struct {
	// result is the last check's answer.
	result   portal.Result
	probedAt time.Time
	// probedUplinkSince is the uplink connection the last check was for;
	// a new connection gets checked at once.
	probedUplinkSince time.Time

	// Login mode.
	loginMode    bool
	loginStarted time.Time
	loginUntil   time.Time

	// The login browser (portalbrowser.go).
	browser          loginBrowser
	browserStarting  bool
	browserStarted   time.Time
	browserUsed      time.Time
	browserSawPortal bool
}

// noticeActive reports whether devices should be shown the "login needed"
// notice: a login page is in the way, the tunnel isn't working, and devices
// can't reach the page because their traffic is kept in the tunnel.
func (a *Agent) noticeActive() bool {
	return a.mode == api.ModeNormal && !a.portal.loginMode &&
		a.portal.result.State == portal.Portal &&
		a.clientsThroughTunnel() && a.vpn.machine.State() != tunnel.Up
}

// portalStatus describes captive portals for the UI.
func (a *Agent) portalStatus() api.PortalStatus {
	s := api.PortalStatus{
		Detected:  a.portal.result.State == portal.Portal,
		LoginURL:  a.portal.result.LoginURL,
		LoginMode: a.portal.loginMode,
		CanStart:  a.noticeActive(),
		CanBrowse: a.canBrowse(),
		Browsing:  a.portal.browser != nil,
	}
	if a.portal.loginMode {
		s.SecondsLeft = max(0, int(a.portal.loginUntil.Sub(a.now()).Seconds()))
	}
	return s
}

// portalCheckInterval is how long to wait between checks.
func (a *Agent) portalCheckInterval() time.Duration {
	switch {
	case a.portal.loginMode, a.portal.browser != nil:
		return 5 * time.Second // notice the login quickly
	case a.vpnActive() && a.vpn.machine.State() != tunnel.Up:
		return time.Minute // a portal may be why the tunnel is down
	default:
		return 10 * time.Minute
	}
}

// ProbeInternet is the real captive portal check (see internal/portal).
func ProbeInternet(ctx context.Context) portal.Result {
	client := &http.Client{Timeout: 5 * time.Second}
	return portal.Probe(ctx, client, portal.Checks)
}

// RunPortalMonitor checks for captive portals in the background, using
// probe (ProbeInternet on the device, a stand-in on the simulated device).
func (a *Agent) RunPortalMonitor(probe func(context.Context) portal.Result) {
	for {
		time.Sleep(5 * time.Second)
		a.CheckPortal(probe)
	}
}

// CheckPortal runs one round: check for a login page if it's time, then
// update login mode and the notice. probe does the actual check; it runs
// without holding the lock because it takes a few seconds.
func (a *Agent) CheckPortal(probe func(context.Context) portal.Result) {
	a.mu.Lock()
	connected := a.uplink.status.Connected
	due := connected && (a.uplink.connectedSince != a.portal.probedUplinkSince ||
		a.now().Sub(a.portal.probedAt) >= a.portalCheckInterval())
	uplinkSince := a.uplink.connectedSince
	a.mu.Unlock()

	var result portal.Result
	if due {
		result = probe(context.Background())
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	wasNotice := a.noticeActive()
	if due {
		if result.State != a.portal.result.State {
			slog.Info("captive portal check", "result", result.State, "loginUrl", result.LoginURL)
		}
		a.portal.result = result
		a.portal.probedAt = a.now()
		a.portal.probedUplinkSince = uplinkSince
	}

	if a.portal.loginMode {
		switch {
		case a.portal.result.State == portal.Open:
			a.endLoginMode("the login worked")
		case !a.now().Before(a.portal.loginUntil):
			a.endLoginMode("time is up")
		}
	}
	a.checkBrowser()
	if a.noticeActive() != wasNotice {
		slog.Info("captive portal notice", "shown", a.noticeActive())
		a.applyClientPath()
	}
}

// RecheckPortal makes the next check for a login page happen within a few
// seconds (for "Check again" on the notice page, for example after logging
// in some other way).
func (a *Agent) RecheckPortal() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.portal.probedAt = time.Time{}
	a.portal.probedUplinkSince = time.Time{}
	return nil
}

// StartLoginMode lets devices reach the network's login page for
// LoginWindow. Any device on the hotspot may start it (from the notice).
func (a *Agent) StartLoginMode() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.noticeActive() {
		return ErrWrongMode
	}
	slog.Warn("login mode started: devices' web and DNS traffic bypasses the VPN for a few minutes")
	a.portal.loginMode = true
	a.portal.loginStarted = a.now()
	a.portal.loginUntil = a.now().Add(LoginWindow)
	a.portal.probedAt = time.Time{} // check again soon
	a.applyClientPath()
	return nil
}

// ExtendLoginMode adds LoginWindow, up to MaxLoginWindow in total.
func (a *Agent) ExtendLoginMode() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.portal.loginMode {
		return ErrWrongMode
	}
	limit := a.portal.loginStarted.Add(MaxLoginWindow)
	a.portal.loginUntil = a.portal.loginUntil.Add(LoginWindow)
	if a.portal.loginUntil.After(limit) {
		a.portal.loginUntil = limit
	}
	return nil
}

// StopLoginMode ends login mode early.
func (a *Agent) StopLoginMode() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.portal.loginMode {
		return nil
	}
	a.endLoginMode("stopped from the web UI")
	return nil
}

// endLoginMode puts devices' traffic back into the tunnel. The caller
// holds a.mu.
func (a *Agent) endLoginMode(reason string) {
	slog.Info("login mode ended", "reason", reason)
	a.portal.loginMode = false
	a.applyClientPath()
}

// PortalStatus returns the captive portal state (public: the notice page
// shows it before login).
func (a *Agent) PortalStatus() api.PortalStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.portalStatus()
}
