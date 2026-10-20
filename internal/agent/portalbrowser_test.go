package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/browser"
	"rasbeacon/internal/config"
	"rasbeacon/internal/portal"
)

// fakeBrowser stands in for the real login browser.
type fakeBrowser struct {
	mu      sync.Mutex
	opts    browser.Options
	actions []string
	done    chan struct{}
	closed  bool
}

func (f *fakeBrowser) record(action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	return nil
}

func (f *fakeBrowser) did(action string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.actions {
		if a == action {
			return true
		}
	}
	return false
}

func (f *fakeBrowser) NextFrame(ctx context.Context, after int64) browser.Frame {
	return browser.Frame{Seq: after + 1, JPEG: []byte{0xff, 0xd8}, URL: "http://login.hotel.example/", Width: 390, Height: 700}
}
func (f *fakeBrowser) Navigate(address string) error {
	if !strings.HasPrefix(address, "http") {
		return browser.ErrNotWeb
	}
	return f.record("navigate " + address)
}
func (f *fakeBrowser) Click(x, y float64) error     { return f.record("click") }
func (f *fakeBrowser) Scroll(x, y, d float64) error { return f.record("scroll") }
func (f *fakeBrowser) Type(text string) error       { return f.record("type " + text) }
func (f *fakeBrowser) Key(name string) error        { return f.record("key " + name) }
func (f *fakeBrowser) Back() error                  { return f.record("back") }
func (f *fakeBrowser) Reload() error                { return f.record("reload") }
func (f *fakeBrowser) Done() <-chan struct{}        { return f.done }
func (f *fakeBrowser) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.done)
	}
}

func (f *fakeBrowser) isClosed() bool {
	for i := 0; i < 100; i++ { // Close runs in the background
		f.mu.Lock()
		closed := f.closed
		f.mu.Unlock()
		if closed {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// withFakeBrowser makes the agent use a fake browser "installed" at a
// temporary path, and returns a function that gives the last one started.
func withFakeBrowser(t *testing.T, e *testEnv) func() *fakeBrowser {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "chromium-headless-shell")
	mustWrite(t, binary, "")
	var last *fakeBrowser
	var mu sync.Mutex
	e.agent.browser = browserSettings{binary: binary, user: "rbbrowser", uid: 997,
		start: func(opts browser.Options) (loginBrowser, error) {
			mu.Lock()
			defer mu.Unlock()
			last = &fakeBrowser{opts: opts, done: make(chan struct{})}
			return last, nil
		}}
	// On the device the browser user is known from the start.
	e.agent.mu.Lock()
	if err := e.agent.applyFirewall(); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Unlock()
	return func() *fakeBrowser {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

func waitForAction(t *testing.T, b *fakeBrowser, action string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if b.did(action) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("the browser never did %q (did %v)", action, b.actions)
}

func TestPortalBrowser_OpensLoginPageAndClosesWhenLoggedIn(t *testing.T) {
	e, _, p := newPortalEnv(t)
	last := withFakeBrowser(t, e)

	if s := e.agent.PortalStatus(); !s.CanBrowse || s.Browsing {
		t.Fatalf("status = %+v", s)
	}
	if err := e.agent.StartPortalBrowser(api.PortalBrowserStart{Width: 390, Height: 700}); err != nil {
		t.Fatal(err)
	}
	b := last()
	if b.opts.User != "rbbrowser" || b.opts.Width != 390 || b.opts.ProfileParent != e.paths.BrowserDir() {
		t.Errorf("unexpected options: %+v", b.opts)
	}
	waitForAction(t, b, "navigate http://login.hotel.example/")
	if !e.agent.PortalStatus().Browsing {
		t.Error("status should say the browser is open")
	}

	// The browser is RasBeacon's own traffic: devices stay in the tunnel.
	if got := e.firewallEgress(); got != `elements = { "wg0" }` {
		t.Errorf("devices' traffic must stay in the tunnel, egress = %s", got)
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), `meta skuid 997 oifname != { "wlan0" } reject`) {
		t.Error("the browser user may only reach the uplink")
	}

	// Taps and typing reach the browser.
	for _, in := range []api.PortalBrowserInput{
		{Action: "click", X: 10, Y: 20}, {Action: "type", Text: "Room 412"}, {Action: "key", Key: "Enter"},
	} {
		if err := e.agent.PortalBrowserInput(in); err != nil {
			t.Fatal(err)
		}
	}
	for _, action := range []string{"click", "type Room 412", "key Enter"} {
		if !b.did(action) {
			t.Errorf("the browser didn't get %q", action)
		}
	}
	frame := e.agent.PortalBrowserFrame(context.Background(), 0)
	if !frame.Running || frame.Seq != 1 || frame.Image != "/9g=" || frame.Width != 390 {
		t.Errorf("frame = %+v", frame)
	}

	// The login worked: the browser closes by itself.
	p.result = portal.Result{State: portal.Open}
	e.agent.CheckPortal(p.probe)
	if !b.isClosed() || e.agent.PortalStatus().Browsing {
		t.Error("the browser should close once the login worked")
	}
	if f := e.agent.PortalBrowserFrame(context.Background(), 0); f.Running {
		t.Error("no frames after closing")
	}
}

func TestPortalBrowser_ClosesWhenUnused(t *testing.T) {
	e, c, p := newPortalEnv(t)
	last := withFakeBrowser(t, e)
	if err := e.agent.StartPortalBrowser(api.PortalBrowserStart{Width: 390, Height: 700}); err != nil {
		t.Fatal(err)
	}
	c.advance(BrowserIdleTimeout - time.Minute)
	if err := e.agent.PortalBrowserInput(api.PortalBrowserInput{Action: "scroll", DeltaY: 100}); err != nil {
		t.Fatal(err)
	}
	c.advance(BrowserIdleTimeout - time.Minute)
	e.agent.CheckPortal(p.probe)
	if !e.agent.PortalStatus().Browsing {
		t.Fatal("a tap should keep the browser open")
	}
	c.advance(2 * time.Minute)
	e.agent.CheckPortal(p.probe)
	if !last().isClosed() {
		t.Error("the browser should close when unused")
	}
}

func TestPortalBrowser_RefusedWithoutLoginPageOrBrowser(t *testing.T) {
	e := newClaimedEnv(t, false)
	withFakeBrowser(t, e)
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	e.agent.CheckPortal((&fakeProbe{result: portal.Result{State: portal.Open}}).probe)
	if err := e.agent.StartPortalBrowser(api.PortalBrowserStart{}); !errors.Is(err, ErrWrongMode) {
		t.Errorf("no login page and no VPN problem: got %v", err)
	}

	e2, _, _ := newPortalEnv(t)
	e2.agent.browser.binary = filepath.Join(t.TempDir(), "missing")
	if e2.agent.PortalStatus().CanBrowse {
		t.Error("the browser isn't installed")
	}
}

func TestPortalBrowser_InputIsChecked(t *testing.T) {
	e, _, _ := newPortalEnv(t)
	withFakeBrowser(t, e)
	if err := e.agent.PortalBrowserInput(api.PortalBrowserInput{Action: "click"}); !errors.Is(err, ErrWrongMode) {
		t.Errorf("no browser open: got %v", err)
	}
	if err := e.agent.StartPortalBrowser(api.PortalBrowserStart{}); err != nil {
		t.Fatal(err)
	}
	for field, in := range map[string]api.PortalBrowserInput{
		"url":    {Action: "navigate", URL: "file:///etc/passwd"},
		"key":    {Action: "key", Key: "F12"},
		"action": {Action: "devtools"},
		"text":   {Action: "type", Text: strings.Repeat("x", 1001)},
	} {
		var ve *config.ValidationError
		if err := e.agent.PortalBrowserInput(in); !errors.As(err, &ve) || ve.Field != field {
			t.Errorf("%s: got %v", field, err)
		}
	}
}

func TestPortalBrowser_NoFirewallRuleWithoutBrowserUser(t *testing.T) {
	e := newClaimedEnv(t, false)
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "skuid") {
		t.Error("no browser user on this machine, so no rule")
	}
}

func TestPortalBrowser_CloseFromThePage(t *testing.T) {
	e, _, _ := newPortalEnv(t)
	last := withFakeBrowser(t, e)
	if err := e.agent.StartPortalBrowser(api.PortalBrowserStart{Width: 390, Height: 700}); err != nil {
		t.Fatal(err)
	}
	e.agent.StopPortalBrowser()
	if !last().isClosed() || e.agent.PortalStatus().Browsing {
		t.Error("Close should close RasBeacon's browser")
	}
	// Closing again is fine.
	e.agent.StopPortalBrowser()
}
