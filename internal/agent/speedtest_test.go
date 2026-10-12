package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"rasbeacon/internal/browser"
	"rasbeacon/internal/config"
	"rasbeacon/internal/speedtest"
)

func TestSpeedtest_RunsOnlyWhenAskedAndKeepsTheResult(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	e.agent.SetBrowser("/bin/sh", true) // any program that exists
	e.agent.SetSpeedtestServer("http://127.0.0.1:9")
	runs := 0
	var gotOpts browser.Options
	e.agent.speedtest.run = func(ctx context.Context, opts browser.Options, server string) (speedtest.Result, error) {
		runs++
		gotOpts = opts
		if server != "http://127.0.0.1:9" {
			t.Errorf("server = %q", server)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("the test must have a time limit")
		}
		return speedtest.Result{DownloadMbps: 93.5, UploadMbps: 41.2, PingMs: 12, Server: "San Jose (SJC)"}, nil
	}

	if s := e.agent.Status().Speedtest; s.Provider != "Cloudflare" || s.Last != nil {
		t.Fatalf("before any test: %+v", s)
	}
	// Not without an internet connection.
	var ve *config.ValidationError
	if err := e.agent.RunSpeedtest(); !errors.As(err, &ve) || runs != 0 {
		t.Fatalf("got %v, %d runs", err, runs)
	}

	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	if err := e.agent.RunSpeedtest(); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || gotOpts.Binary != "/bin/sh" || gotOpts.ProfileParent != e.paths.BrowserDir() {
		t.Errorf("runs %d, options %+v", runs, gotOpts)
	}
	c.advance(90 * time.Second)
	last := e.agent.Status().Speedtest.Last
	if last == nil || last.DownloadMbps != 93.5 || last.UploadMbps != 41.2 || last.PingMs != 12 ||
		last.Server != "San Jose (SJC)" || last.AgeSeconds != 90 {
		t.Errorf("last = %+v", last)
	}

	// A failed test keeps the previous result and explains.
	e.agent.speedtest.run = func(context.Context, browser.Options, string) (speedtest.Result, error) {
		return speedtest.Result{}, errors.New("the speed test server didn't answer")
	}
	if err := e.agent.RunSpeedtest(); !errors.As(err, &ve) {
		t.Errorf("got %v", err)
	}
	if e.agent.Status().Speedtest.Last.DownloadMbps != 93.5 {
		t.Error("the last good result should stay")
	}

	// One at a time.
	e.agent.speedtest.running = true
	if err := e.agent.RunSpeedtest(); !errors.Is(err, ErrSpeedtestBusy) {
		t.Errorf("got %v", err)
	}
}
