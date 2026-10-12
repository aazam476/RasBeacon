package agent

// The speed test on the Internet page (Cloudflare's, see
// internal/speedtest). It only runs when the owner asks, in RasBeacon's
// headless browser as the rbbrowser user, so the firewall keeps it on the
// uplink: it measures the connection RasBeacon uses (not the VPN).

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/browser"
	"rasbeacon/internal/config"
	"rasbeacon/internal/speedtest"
)

// speedtestTimeout limits one test. Cloudflare's test takes about 20
// seconds on a good line, longer on a slow one.
const speedtestTimeout = 3 * time.Minute

// ErrSpeedtestBusy means a speed test is running already.
var ErrSpeedtestBusy = errors.New("A speed test is already running.")

// speedtestState is the last result, kept until RasBeacon restarts.
type speedtestState struct {
	running bool
	last    *speedtest.Result
	at      time.Time
	// server is the test service (a stand-in on the simulated device).
	server string
	// run starts the browser and runs the test (replaced in tests).
	run func(ctx context.Context, opts browser.Options, server string) (speedtest.Result, error)
}

// runSpeedtest starts a browser without pictures, runs the test, and
// closes the browser (deleting its profile).
func runSpeedtest(ctx context.Context, opts browser.Options, server string) (speedtest.Result, error) {
	opts.NoPictures = true
	session, err := startChromium(opts)
	if err != nil {
		return speedtest.Result{}, err
	}
	defer session.Close()
	return speedtest.Run(ctx, session, server)
}

// SetSpeedtestServer makes the speed test use another service (the
// simulated device's stand-in). Call it before Start.
func (a *Agent) SetSpeedtestServer(server string) { a.speedtest.server = server }

// RunSpeedtest runs a speed test now and keeps its result. It takes about
// 20 seconds; the agent isn't blocked meanwhile.
func (a *Agent) RunSpeedtest() error {
	a.mu.Lock()
	switch {
	case a.mode != api.ModeNormal:
		a.mu.Unlock()
		return ErrWrongMode
	case a.speedtest.running:
		a.mu.Unlock()
		return ErrSpeedtestBusy
	case !a.uplink.status.Connected:
		a.mu.Unlock()
		return errNoUplink
	case !a.browserAvailable():
		a.mu.Unlock()
		return &config.ValidationError{Message: "The speed test needs RasBeacon's browser (chromium-headless-shell), which isn't installed."}
	}
	a.speedtest.running = true
	opts := browser.Options{
		Binary: a.browser.binary, User: a.browser.user, NoSandbox: a.browser.noSandbox,
		ProfileParent: a.paths.BrowserDir(), Width: 800, Height: 600,
	}
	run, server := a.speedtest.run, a.speedtest.server
	a.mu.Unlock()

	slog.Info("speed test started", "server", server)
	ctx, cancel := context.WithTimeout(context.Background(), speedtestTimeout)
	defer cancel()
	result, err := run(ctx, opts, server)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.speedtest.running = false
	if err != nil {
		slog.Warn("speed test failed", "error", err)
		return &config.ValidationError{Message: "The speed test didn't work: " + err.Error()}
	}
	a.speedtest.last, a.speedtest.at = &result, a.now()
	slog.Info("speed test finished", "downloadMbps", result.DownloadMbps, "uploadMbps", result.UploadMbps,
		"pingMs", result.PingMs, "server", result.Server)
	return nil
}

// speedtestStatus describes the last speed test.
func (a *Agent) speedtestStatus() api.Speedtest {
	s := api.Speedtest{Provider: speedtest.Provider}
	if last := a.speedtest.last; last != nil {
		s.Last = &api.SpeedtestResult{
			Server:       last.Server,
			AgeSeconds:   max(0, int(a.now().Sub(a.speedtest.at).Seconds())),
			DownloadMbps: last.DownloadMbps,
			UploadMbps:   last.UploadMbps,
			PingMs:       last.PingMs,
		}
	}
	return s
}
