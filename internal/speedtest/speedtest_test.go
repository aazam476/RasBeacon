package speedtest

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/browser"
)

func TestScript_DefinesTheEngine(t *testing.T) {
	script, err := Script()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "export {") || !strings.HasSuffix(strings.TrimSpace(script), "globalThis.RasBeaconSpeedTest = SpeedTestEngine;") {
		t.Error("the library should become a plain script")
	}
}

func TestParseOutcome(t *testing.T) {
	r, err := parseOutcome([]byte(`{"download":93500000,"upload":41200000,"latency":12.5,"colo":"SJC","city":"San Jose"}`))
	if err != nil || r.DownloadMbps != 93.5 || r.UploadMbps != 41.2 || r.PingMs != 12.5 || r.Server != "San Jose (SJC)" {
		t.Errorf("got %+v %v", r, err)
	}
	if r, _ := parseOutcome([]byte(`{"download":1,"upload":1,"latency":1,"colo":"SJC"}`)); r.Server != "SJC" {
		t.Errorf("got %q", r.Server)
	}
	if _, err := parseOutcome([]byte(`{"download":1,"latency":1}`)); err == nil {
		t.Error("a test without an upload result must fail")
	}
}

// findBrowser looks for a headless Chromium like internal/browser's tests.
func findBrowser(t *testing.T) string {
	t.Helper()
	candidates := []string{os.Getenv("RASBEACON_TEST_BROWSER"), browser.DefaultBinary}
	home, _ := os.UserHomeDir()
	for _, pattern := range []string{
		"/opt/pw-browsers/chromium_headless_shell-*/chrome-linux/headless_shell",
		filepath.Join(home, ".cache/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell"),
	} {
		matches, _ := filepath.Glob(pattern)
		candidates = append(candidates, matches...)
	}
	for _, c := range candidates {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	if os.Getenv("RASBEACON_REQUIRE_BROWSER") == "1" {
		t.Fatal("no headless Chromium found")
	}
	t.Skip("no headless Chromium found (set RASBEACON_TEST_BROWSER)")
	return ""
}

// TestRun_InARealBrowser runs Cloudflare's library in headless Chromium
// against the stand-in server, limited to 80 Mbps.
func TestRun_InARealBrowser(t *testing.T) {
	binary := findBrowser(t)
	standIn := &StandIn{RateMbps: 80}
	server := httptest.NewServer(standIn.Handler())
	defer server.Close()

	session, err := browser.Start(browser.Options{Binary: binary, ProfileParent: t.TempDir(),
		Width: 400, Height: 600, NoSandbox: os.Geteuid() == 0, NoPictures: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	started := time.Now()
	result, err := Run(ctx, session, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v in %s", result, time.Since(started).Round(time.Second))
	if result.DownloadMbps < 40 || result.DownloadMbps > 120 || result.UploadMbps < 40 || result.UploadMbps > 120 {
		t.Errorf("the stand-in sends and receives at about 80 Mbps, got %+v", result)
	}
	if result.PingMs <= 0 || result.Server != "Simulated city (DEV)" {
		t.Errorf("got %+v", result)
	}
	if standIn.ResultsLogged.Load() != 1 {
		t.Error("the results should be sent to the test service")
	}
}
