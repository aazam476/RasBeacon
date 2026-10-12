// Package speedtest measures the internet connection with Cloudflare's
// speed test (github.com/cloudflare/speedtest, vendored in cloudflare/).
//
// The test is Cloudflare's own JavaScript library, so it runs in a
// browser: RasBeacon's headless Chromium, as the rbbrowser user, which the
// firewall only lets out through the uplink (see internal/browser). So it
// measures the connection RasBeacon uses, as the web UI says. It downloads
// and uploads test files from speed.cloudflare.com for about 20 seconds,
// and, like Cloudflare's own speed test page, sends the results to
// Cloudflare afterwards (the owner agreed to that).
//
// It only runs when the owner presses "Run speed test".
package speedtest

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Provider is the service's name, shown as "Uses Cloudflare Speedtest".
const Provider = "Cloudflare"

// DefaultServer is Cloudflare's speed test service.
const DefaultServer = "https://speed.cloudflare.com"

// LibraryVersion is the vendored library's version (see cloudflare/README.md).
const LibraryVersion = "1.14.1"

//go:embed cloudflare/speedtest.js
var library string

// libraryExport is how the library's file ends. Script replaces it, so the
// library can run as a plain script in the page.
const libraryExport = "export { SpeedTestEngine as default };"

// Script returns the library as a script that defines
// globalThis.RasBeaconSpeedTest (the library's SpeedTestEngine class).
func Script() (string, error) {
	if strings.Count(library, libraryExport) != 1 {
		return "", errors.New("the speed test library doesn't end as expected (see internal/speedtest/cloudflare/README.md)")
	}
	script := strings.Replace(library, libraryExport, "globalThis.RasBeaconSpeedTest = SpeedTestEngine;", 1)
	// The source map isn't shipped.
	if i := strings.LastIndex(script, "//# sourceMappingURL="); i >= 0 {
		script = script[:i]
	}
	return script, nil
}

// Result is one test's outcome, in the units the web UI shows.
type Result struct {
	DownloadMbps float64
	UploadMbps   float64
	// PingMs is the latency while the line is otherwise quiet.
	PingMs float64
	// Server is the Cloudflare location tested against, e.g.
	// "San Jose (SJC)"; "" if unknown.
	Server string
}

// Page is the browser page the test runs in (*browser.Session).
type Page interface {
	Navigate(address string) error
	Evaluate(ctx context.Context, expression string) (json.RawMessage, error)
}

// pageLoadTimeout limits how long the first page may take to load.
const pageLoadTimeout = 20 * time.Second

// Run runs the speed test against server (DefaultServer, or a stand-in in
// tests) until it finishes or ctx ends.
func Run(ctx context.Context, page Page, server string) (Result, error) {
	script, err := Script()
	if err != nil {
		return Result{}, err
	}
	server = strings.TrimRight(server, "/")

	// The test needs a page from the test server itself (the library's
	// requests go to the same site). /cdn-cgi/trace is a tiny text page.
	if err := page.Navigate(server + "/cdn-cgi/trace"); err != nil {
		return Result{}, fmt.Errorf("opening the speed test server: %w", err)
	}
	if err := waitForPage(ctx, page, server); err != nil {
		return Result{}, err
	}
	if _, err := page.Evaluate(ctx, script); err != nil {
		return Result{}, fmt.Errorf("loading the speed test: %w", err)
	}
	config, _ := json.Marshal(engineConfig(server))
	raw, err := page.Evaluate(ctx, fmt.Sprintf(runScript, config))
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, errors.New("the speed test took too long")
		}
		return Result{}, fmt.Errorf("the speed test failed: %w", err)
	}
	return parseOutcome(raw)
}

// waitForPage waits until the page from server has loaded.
func waitForPage(ctx context.Context, page Page, server string) error {
	ctx, cancel := context.WithTimeout(ctx, pageLoadTimeout)
	defer cancel()
	check := fmt.Sprintf("location.origin === %q && document.readyState === 'complete'", server)
	for {
		raw, err := page.Evaluate(ctx, check)
		if err == nil && string(raw) == "true" {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("the speed test server didn't answer")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// engineConfig is the library's configuration: its default measurements
// (dist/speedtest.js, defaultConfig) without the packet loss one (which needs a WebRTC
// connection to Cloudflare's TURN server, and isn't shown), with all
// addresses on server.
func engineConfig(server string) map[string]any {
	type m = map[string]any
	return m{
		"autoStart":      false,
		"downloadApiUrl": server + "/__down",
		"uploadApiUrl":   server + "/__up",
		"logAimApiUrl":   server + "/__results",
		"measurements": []m{
			{"type": "latency", "numPackets": 2},
			{"type": "download", "bytes": 1e5, "count": 1, "bypassMinDuration": true},
			{"type": "latency", "numPackets": 20},
			{"type": "download", "bytes": 1e5, "count": 9},
			{"type": "latency", "numPackets": 2},
			{"type": "download", "bytes": 1e6, "count": 8},
			{"type": "latency", "numPackets": 2},
			{"type": "upload", "bytes": 1e5, "count": 8},
			{"type": "latency", "numPackets": 2},
			// (Cloudflare's packet loss measurement comes here.)
			{"type": "upload", "bytes": 1e6, "count": 6},
			{"type": "latency", "numPackets": 2},
			{"type": "download", "bytes": 1e7, "count": 6},
			{"type": "latency", "numPackets": 2},
			{"type": "upload", "bytes": 1e7, "count": 4},
			{"type": "latency", "numPackets": 2},
			{"type": "download", "bytes": 2.5e7, "count": 4},
			{"type": "latency", "numPackets": 2},
			{"type": "upload", "bytes": 2.5e7, "count": 4},
			{"type": "latency", "numPackets": 2},
			{"type": "download", "bytes": 1e8, "count": 3},
			{"type": "latency", "numPackets": 2},
			{"type": "upload", "bytes": 5e7, "count": 3},
			{"type": "latency", "numPackets": 2},
			{"type": "download", "bytes": 2.5e8, "count": 2},
		},
	}
}

// runScript runs the test in the page and returns its summary. It waits
// (up to 5 seconds) for the results to reach Cloudflare before answering,
// so closing the browser doesn't cut that off. Then it looks up the
// location: /cdn-cgi/trace says which one answered ("colo=SJC"), and
// /locations gives its city.
const runScript = `(async () => {
  const engine = new globalThis.RasBeaconSpeedTest(%s);
  const logged = new Promise((resolve) => { engine.onResultsLogged = resolve; });
  const summary = await new Promise((resolve, reject) => {
    engine.onFinish = (results) => resolve(results.getSummary());
    engine.onError = (error) => reject(new Error(String(error)));
    engine.play();
  });
  await Promise.race([logged, new Promise((resolve) => setTimeout(resolve, 5000))]);
  let colo = '', city = '';
  try {
    const trace = await (await fetch('/cdn-cgi/trace')).text();
    colo = (trace.match(/^colo=(.+)$/m) || ['', ''])[1].trim();
    const locations = await (await fetch('/locations')).json();
    const here = locations.find((l) => l.iata === colo);
    if (here && here.city) city = here.city;
  } catch (e) {}
  return { download: summary.download, upload: summary.upload, latency: summary.latency, colo, city };
})()`

// parseOutcome reads runScript's answer.
func parseOutcome(raw json.RawMessage) (Result, error) {
	var out struct {
		Download *float64 `json:"download"` // bits per second
		Upload   *float64 `json:"upload"`
		Latency  *float64 `json:"latency"` // milliseconds
		Colo     string   `json:"colo"`
		City     string   `json:"city"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, fmt.Errorf("unexpected speed test result: %w", err)
	}
	if out.Download == nil || out.Upload == nil || out.Latency == nil {
		return Result{}, errors.New("the speed test finished without a result")
	}
	r := Result{DownloadMbps: *out.Download / 1e6, UploadMbps: *out.Upload / 1e6, PingMs: *out.Latency}
	switch {
	case out.City != "" && out.Colo != "":
		r.Server = fmt.Sprintf("%s (%s)", out.City, out.Colo)
	case out.Colo != "":
		r.Server = out.Colo
	}
	return r, nil
}
