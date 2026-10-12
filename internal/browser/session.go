// Package browser runs a web browser on RasBeacon itself, for logging in
// to hotel and café networks without letting any of your devices' traffic
// leave the VPN ("zero-leak login", see internal/agent/portalbrowser.go).
//
// The browser is Chromium's headless shell: Chromium without a window. It
// opens the network's login page; RasBeacon sends pictures of the page to
// your phone, and your taps and typing back to the browser. Only the
// browser on RasBeacon talks to the login page, so your devices' traffic
// can stay locked inside the VPN the whole time.
//
// How the browser is kept in its box:
//   - It runs as its own unprivileged user (rbbrowser) with a fresh,
//     empty profile that is deleted afterwards, so it can't read
//     RasBeacon's settings, and nothing is remembered between logins.
//   - The firewall only lets that user reach the uplink (see
//     render.FirewallInput.BrowserUID): not the VPN, not your devices,
//     not RasBeacon's own web UI.
//   - It is controlled through a pipe, not a network port, so no other
//     program can take it over.
//   - Downloads, pop-up windows and file uploads are refused; only
//     http:// and https:// addresses can be opened.
package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Options says how to start the browser.
type Options struct {
	// Binary is the browser program, normally DefaultBinary.
	Binary string
	// User runs the browser as this user ("" = the current user). On the
	// device this is "rbbrowser".
	User string
	// ProfileParent is where the browser's temporary profile is created.
	ProfileParent string
	// Width and Height are the page's size in CSS pixels, normally the
	// phone screen's size (see ClampSize).
	Width, Height int
	// NoSandbox turns off Chromium's own sandbox. Chromium refuses to
	// start as root with its sandbox, so tests running as root need this;
	// on the device the browser runs as rbbrowser with the sandbox on.
	NoSandbox bool
	// NoPictures skips the stream of pictures of the page, for a browser
	// nobody looks at (the speed test).
	NoPictures bool
}

// DefaultBinary is where Debian's chromium-headless-shell package puts the
// browser.
const DefaultBinary = "/usr/bin/chromium-headless-shell"

// ClampSize keeps a requested page size within sensible limits.
func ClampSize(width, height int) (int, int) {
	clamp := func(v, low, high int) int { return max(low, min(high, v)) }
	return clamp(width, 320, 1280), clamp(height, 400, 1600)
}

// Frame is a picture of the page.
type Frame struct {
	// Seq goes up by one with every new picture.
	Seq   int64
	JPEG  []byte
	URL   string
	Title string
	// Width and Height are the page's size in CSS pixels (taps use these
	// coordinates).
	Width, Height int
}

// Session is one running browser with one page.
type Session struct {
	cmd        *exec.Cmd
	conn       *conn
	page       string // the CDP session of the page
	targetID   string
	profileDir string
	width      int
	height     int
	noPictures bool
	stderr     *tailBuffer

	mu      sync.Mutex
	frame   Frame
	changed chan struct{} // closed and replaced when frame changes
	closed  bool
	done    chan struct{} // closed when the browser has exited
}

// startTimeout limits how long the browser may take to start.
const startTimeout = 30 * time.Second

// Start launches the browser with an empty page.
func Start(opts Options) (*Session, error) {
	s := &Session{changed: make(chan struct{}), done: make(chan struct{}), stderr: &tailBuffer{}, noPictures: opts.NoPictures}
	s.width, s.height = ClampSize(opts.Width, opts.Height)

	var cred *syscall.Credential
	if opts.User != "" {
		u, err := user.Lookup(opts.User)
		if err != nil {
			return nil, fmt.Errorf("browser user: %w", err)
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		cred = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

	// A fresh profile, readable only by the browser's user.
	if err := os.MkdirAll(opts.ProfileParent, 0o711); err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp(opts.ProfileParent, "profile-")
	if err != nil {
		return nil, err
	}
	s.profileDir = profile
	if cred != nil {
		if err := os.Chown(profile, int(cred.Uid), int(cred.Gid)); err != nil {
			os.RemoveAll(profile)
			return nil, err
		}
	}

	// Pipes: the browser reads commands from fd 3 and writes to fd 4.
	toBrowserR, toBrowserW, err := os.Pipe()
	if err != nil {
		os.RemoveAll(profile)
		return nil, err
	}
	fromBrowserR, fromBrowserW, err := os.Pipe()
	if err != nil {
		toBrowserR.Close()
		toBrowserW.Close()
		os.RemoveAll(profile)
		return nil, err
	}

	s.cmd = exec.Command(opts.Binary, browserArgs(profile, s.width, s.height, opts.NoSandbox)...)
	s.cmd.ExtraFiles = []*os.File{toBrowserR, fromBrowserW}
	s.cmd.Env = []string{"HOME=" + profile, "XDG_CONFIG_HOME=" + profile, "XDG_CACHE_HOME=" + profile,
		"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	s.cmd.Dir = profile
	s.cmd.Stdout = s.stderr
	s.cmd.Stderr = s.stderr
	s.cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: cred,
		Setpgid:    true, // its helper processes form one group, stopped together
		Pdeathsig:  syscall.SIGKILL,
	}
	if err := s.cmd.Start(); err != nil {
		toBrowserR.Close()
		toBrowserW.Close()
		fromBrowserR.Close()
		fromBrowserW.Close()
		os.RemoveAll(profile)
		return nil, fmt.Errorf("starting the browser: %w", err)
	}
	// The browser has its own copies of its ends now.
	toBrowserR.Close()
	fromBrowserW.Close()
	go func() {
		s.cmd.Wait()
		toBrowserW.Close()
		close(s.done)
	}()

	s.conn = newConn(toBrowserW, fromBrowserR, s.handleEvent)
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	if err := s.setUp(ctx); err != nil {
		s.Close()
		if tail := s.stderr.String(); tail != "" {
			err = fmt.Errorf("%w (browser said: %s)", err, tail)
		}
		return nil, err
	}
	return s, nil
}

// browserArgs are Chromium's command-line options.
func browserArgs(profile string, width, height int, noSandbox bool) []string {
	args := []string{
		"--remote-debugging-pipe",
		"--user-data-dir=" + profile,
		fmt.Sprintf("--window-size=%d,%d", width, height),
		// Nothing but the login page: no background requests to Google
		// (updates, safe browsing, metrics, ...), no extensions.
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-domain-reliability",
		"--disable-extensions",
		"--disable-sync",
		"--disable-breakpad",
		"--metrics-recording-only",
		"--no-default-browser-check",
		"--no-first-run",
		"--no-pings",
		"--disable-features=Translate,OptimizationHints,MediaRouter,InterestFeedContentSuggestions",
		// Pop-up windows are opened in the same page instead (handleEvent).
		"--block-new-web-contents",
		// Small device: software drawing only, and no sound.
		"--disable-gpu",
		"--mute-audio",
		"--deny-permission-prompts",
	}
	if noSandbox {
		args = append(args, "--no-sandbox")
	}
	return append(args, "about:blank")
}

// setUp opens the page and starts the picture stream.
func (s *Session) setUp(ctx context.Context) error {
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := s.conn.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		return err
	}
	s.targetID = created.TargetID
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := s.conn.call(ctx, "", "Target.attachToTarget",
		map[string]any{"targetId": s.targetID, "flatten": true}, &attached); err != nil {
		return err
	}
	s.page = attached.SessionID

	var version struct {
		UserAgent string `json:"userAgent"`
	}
	if err := s.conn.call(ctx, "", "Browser.getVersion", nil, &version); err != nil {
		return err
	}
	browserCalls := []struct {
		method string
		params any
	}{
		{"Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}},
		{"Target.setDiscoverTargets", map[string]any{"discover": true}},
	}
	for _, c := range browserCalls {
		if err := s.conn.call(ctx, "", c.method, c.params, nil); err != nil {
			return err
		}
	}
	pageCalls := []struct {
		method string
		params any
	}{
		{"Page.enable", nil},
		{"Page.setInterceptFileChooserDialog", map[string]any{"enabled": true}},
		{"Emulation.setDeviceMetricsOverride", map[string]any{
			"width": s.width, "height": s.height, "deviceScaleFactor": 1, "mobile": true}},
		// Login pages are made for real phones, so look like one.
		{"Emulation.setUserAgentOverride", map[string]any{"userAgent": mobileUserAgent(version.UserAgent)}},
		{"Page.startScreencast", map[string]any{
			"format": "jpeg", "quality": 70, "maxWidth": s.width, "maxHeight": s.height}},
	}
	if s.noPictures {
		pageCalls = pageCalls[:len(pageCalls)-1] // no Page.startScreencast
	}
	for _, c := range pageCalls {
		if err := s.conn.call(ctx, s.page, c.method, c.params, nil); err != nil {
			return err
		}
	}
	return nil
}

// mobileUserAgent turns the headless browser's name into an ordinary
// phone browser's, e.g. "... HeadlessChrome/141.0.0.0 Safari/537.36"
// becomes "... Chrome/141.0.0.0 Mobile Safari/537.36". Some login pages
// refuse "headless" browsers or show a desktop layout.
func mobileUserAgent(ua string) string {
	ua = strings.Replace(ua, "HeadlessChrome/", "Chrome/", 1)
	if !strings.Contains(ua, "Mobile") {
		ua = strings.Replace(ua, " Safari/", " Mobile Safari/", 1)
	}
	return ua
}

// handleEvent reacts to the browser's events. It runs on the connection's
// reading goroutine, so anything that sends a command runs in its own
// goroutine (the answer is read by this same goroutine).
func (s *Session) handleEvent(e event) {
	switch e.Method {
	case "Page.screencastFrame":
		var p struct {
			Data      string `json:"data"`
			SessionID int    `json:"sessionId"`
		}
		if json.Unmarshal(e.Params, &p) != nil {
			return
		}
		go s.conn.call(context.Background(), s.page, "Page.screencastFrameAck", map[string]any{"sessionId": p.SessionID}, nil)
		picture, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			return
		}
		s.update(func(f *Frame) { f.JPEG = picture })

	case "Target.targetInfoChanged":
		var p struct {
			TargetInfo struct {
				TargetID string `json:"targetId"`
				URL      string `json:"url"`
				Title    string `json:"title"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(e.Params, &p) == nil && p.TargetInfo.TargetID == s.targetID {
			s.update(func(f *Frame) { f.URL, f.Title = p.TargetInfo.URL, p.TargetInfo.Title })
		}

	case "Page.frameNavigated", "Page.loadEventFired", "Page.navigatedWithinDocument":
		// The address or title may have changed (the title event above
		// doesn't come for every change).
		go s.refreshInfo()

	case "Page.windowOpen":
		// A pop-up or a link to a new window: open it in our page instead.
		var p struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(e.Params, &p) == nil {
			go func() {
				if err := s.Navigate(p.URL); err != nil {
					slog.Info("login browser: pop-up not opened", "error", err)
				}
			}()
		}

	case "Page.javascriptDialogOpening":
		// An alert or "are you sure?" box would freeze the page: accept it.
		go s.conn.call(context.Background(), s.page, "Page.handleJavaScriptDialog", map[string]any{"accept": true}, nil)
	}
}

// refreshInfo reads the page's address and title.
func (s *Session) refreshInfo() {
	var info struct {
		TargetInfo struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"targetInfo"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if s.conn.call(ctx, "", "Target.getTargetInfo", map[string]any{"targetId": s.targetID}, &info) != nil {
		return
	}
	s.mu.Lock()
	same := s.frame.URL == info.TargetInfo.URL && s.frame.Title == info.TargetInfo.Title
	s.mu.Unlock()
	if !same {
		s.update(func(f *Frame) { f.URL, f.Title = info.TargetInfo.URL, info.TargetInfo.Title })
	}
}

// update changes the current frame and wakes everyone waiting for it.
func (s *Session) update(change func(*Frame)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(&s.frame)
	s.frame.Seq++
	s.frame.Width, s.frame.Height = s.width, s.height
	close(s.changed)
	s.changed = make(chan struct{})
}

// NextFrame returns the page's picture once there is one newer than
// after, or the current one when ctx ends first.
func (s *Session) NextFrame(ctx context.Context, after int64) Frame {
	for {
		s.mu.Lock()
		frame, changed := s.frame, s.changed
		s.mu.Unlock()
		if frame.Seq > after {
			return frame
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return frame
		case <-s.done:
			return frame
		}
	}
}

// --- What the owner does on the page ---

// commandTimeout limits each action.
const commandTimeout = 10 * time.Second

func (s *Session) pageCall(method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return s.conn.call(ctx, s.page, method, params, result)
}

// ErrNotWeb is returned for addresses that aren't web pages (file:,
// chrome:, javascript:, ...): only http and https may be opened.
var ErrNotWeb = errors.New("only http:// and https:// addresses can be opened")

// Navigate opens an address in the page.
func (s *Session) Navigate(address string) error {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrNotWeb
	}
	return s.pageCall("Page.navigate", map[string]any{"url": u.String()}, nil)
}

// Click taps the page at (x, y), in CSS pixels from the top left.
func (s *Session) Click(x, y float64) error {
	for _, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		params := map[string]any{"type": kind, "x": x, "y": y}
		if kind != "mouseMoved" {
			params["button"], params["clickCount"] = "left", 1
		}
		if err := s.pageCall("Input.dispatchMouseEvent", params, nil); err != nil {
			return err
		}
	}
	return nil
}

// Scroll scrolls the page at (x, y) by deltaY pixels (positive: down).
func (s *Session) Scroll(x, y, deltaY float64) error {
	return s.pageCall("Input.dispatchMouseEvent", map[string]any{
		"type": "mouseWheel", "x": x, "y": y, "deltaX": 0, "deltaY": deltaY}, nil)
}

// Type enters text into whatever is focused on the page.
func (s *Session) Type(text string) error {
	return s.pageCall("Input.insertText", map[string]any{"text": text}, nil)
}

// keys are the special keys the owner can press.
var keys = map[string]struct {
	code int
	text string
}{
	"Enter":     {13, "\r"},
	"Backspace": {8, ""},
	"Tab":       {9, ""},
	"Escape":    {27, ""},
}

// Key presses a special key: "Enter", "Backspace", "Tab" or "Escape".
func (s *Session) Key(name string) error {
	k, ok := keys[name]
	if !ok {
		return fmt.Errorf("unknown key %q", name)
	}
	for _, kind := range []string{"keyDown", "keyUp"} {
		params := map[string]any{"type": kind, "key": name, "code": name,
			"windowsVirtualKeyCode": k.code, "nativeVirtualKeyCode": k.code}
		if kind == "keyDown" && k.text != "" {
			params["text"] = k.text
		}
		if err := s.pageCall("Input.dispatchKeyEvent", params, nil); err != nil {
			return err
		}
	}
	return nil
}

// Back goes to the previous page.
func (s *Session) Back() error {
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := s.pageCall("Page.getNavigationHistory", nil, &history); err != nil {
		return err
	}
	if history.CurrentIndex <= 0 || history.CurrentIndex >= len(history.Entries) {
		return nil // nothing to go back to
	}
	return s.pageCall("Page.navigateToHistoryEntry",
		map[string]any{"entryId": history.Entries[history.CurrentIndex-1].ID}, nil)
}

// Reload loads the page again.
func (s *Session) Reload() error {
	return s.pageCall("Page.reload", nil, nil)
}

// Evaluate runs JavaScript in the page and returns its result as JSON. If
// the code gives a promise, Evaluate waits for it (until ctx ends). Only
// RasBeacon's own code is run this way (the speed test), never anything
// from the web UI.
func (s *Session) Evaluate(ctx context.Context, expression string) (json.RawMessage, error) {
	var reply struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	err := s.conn.call(ctx, s.page, "Runtime.evaluate", map[string]any{
		"expression": expression, "awaitPromise": true, "returnByValue": true}, &reply)
	if err != nil {
		return nil, err
	}
	if e := reply.ExceptionDetails; e != nil {
		message := e.Exception.Description
		if message == "" {
			message = e.Text
		}
		return nil, fmt.Errorf("script failed: %s", message)
	}
	return reply.Result.Value, nil
}

// Done is closed when the browser has exited.
func (s *Session) Done() <-chan struct{} { return s.done }

// Close stops the browser and deletes its profile (cookies, cache).
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	if s.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.conn.call(ctx, "", "Browser.close", nil, nil)
		cancel()
	}
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}
	// Stop the whole process group (the browser's helper processes too).
	if s.cmd != nil && s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		<-s.done
	}
	if s.profileDir != "" {
		if err := os.RemoveAll(s.profileDir); err != nil {
			slog.Warn("could not delete the login browser's profile", "error", err)
		}
	}
}

// tailBuffer keeps the last few hundred bytes the browser printed, to
// explain a failed start.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 600 {
		b.data = b.data[len(b.data)-600:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
