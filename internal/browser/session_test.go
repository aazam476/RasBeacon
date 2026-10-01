package browser

// These tests drive a real headless Chromium. They look for it in
// RASBEACON_TEST_BROWSER, then in Debian's and Playwright's usual places,
// and are skipped if there is none (unless RASBEACON_REQUIRE_BROWSER=1).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func findBrowser(t *testing.T) string {
	t.Helper()
	candidates := []string{os.Getenv("RASBEACON_TEST_BROWSER"), DefaultBinary}
	home, _ := os.UserHomeDir()
	for _, pattern := range []string{
		"/opt/pw-browsers/chromium_headless_shell-*/chrome-linux/headless_shell",
		filepath.Join(home, ".cache/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell"),
	} {
		matches, _ := filepath.Glob(pattern)
		candidates = append(candidates, matches...)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if os.Getenv("RASBEACON_REQUIRE_BROWSER") == "1" {
		t.Fatal("no headless Chromium found")
	}
	t.Skip("no headless Chromium found (set RASBEACON_TEST_BROWSER)")
	return ""
}

// loginSite is a stand-in for a hotel's login page.
func loginSite(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Hotel login</title>
<meta name="viewport" content="width=device-width">
<form action="/welcome">
<input id="room" name="room" style="position:absolute;left:20px;top:20px;width:200px;height:40px">
</form>
<a href="/terms" target="_blank" style="position:absolute;left:20px;top:100px;width:200px;height:40px;display:block">Terms</a>`)
	})
	mux.HandleFunc("/welcome", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<title>Welcome</title>You are online, room %s", r.URL.Query().Get("room"))
	})
	mux.HandleFunc("/terms", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<title>Terms</title>Be nice.")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// waitFor waits until a frame satisfies ok.
func waitFor(t *testing.T, s *Session, what string, ok func(Frame) bool) Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var seq int64
	for {
		frame := s.NextFrame(ctx, seq)
		if ok(frame) {
			return frame
		}
		if ctx.Err() != nil {
			t.Fatalf("timed out waiting for %s; last frame: url=%q title=%q picture=%d bytes",
				what, frame.URL, frame.Title, len(frame.JPEG))
		}
		seq = frame.Seq
	}
}

func TestSession_LogInThroughThePictures(t *testing.T) {
	binary := findBrowser(t)
	site := loginSite(t)
	profiles := t.TempDir()

	s, err := Start(Options{Binary: binary, ProfileParent: profiles, Width: 390, Height: 700,
		NoSandbox: os.Geteuid() == 0})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Navigate(site.URL); err != nil {
		t.Fatal(err)
	}
	frame := waitFor(t, s, "the login page", func(f Frame) bool {
		return f.Title == "Hotel login" && len(f.JPEG) > 0
	})
	if frame.Width != 390 || frame.Height != 700 || string(frame.JPEG[:2]) != "\xff\xd8" {
		t.Errorf("unexpected frame: %dx%d, starts with %x", frame.Width, frame.Height, frame.JPEG[:2])
	}

	// Tap the room field, type, press Enter: as the owner would on the
	// picture.
	if err := s.Click(100, 40); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("412"); err != nil {
		t.Fatal(err)
	}
	if err := s.Key("Enter"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "the welcome page", func(f Frame) bool {
		return f.Title == "Welcome" && strings.Contains(f.URL, "room=412")
	})

	// Back, then a link that would open a new window opens in the page.
	if err := s.Back(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "the login page again", func(f Frame) bool { return f.Title == "Hotel login" })
	if err := s.Click(100, 120); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "the terms (from a new-window link)", func(f Frame) bool { return f.Title == "Terms" })

	// Only web addresses.
	for _, address := range []string{"file:///etc/passwd", "chrome://settings", "javascript:alert(1)"} {
		if err := s.Navigate(address); err != ErrNotWeb {
			t.Errorf("%s: got %v, want ErrNotWeb", address, err)
		}
	}

	// Closing stops the browser and deletes its profile.
	s.Close()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the browser did not stop")
	}
	if left, _ := os.ReadDir(profiles); len(left) != 0 {
		t.Errorf("the profile should be deleted, found %v", left)
	}
}

func TestMobileUserAgent(t *testing.T) {
	got := mobileUserAgent("Mozilla/5.0 (X11; Linux aarch64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/141.0.0.0 Safari/537.36")
	want := "Mozilla/5.0 (X11; Linux aarch64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36"
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestClampSize(t *testing.T) {
	if w, h := ClampSize(10, 99999); w != 320 || h != 1600 {
		t.Errorf("got %dx%d", w, h)
	}
}
