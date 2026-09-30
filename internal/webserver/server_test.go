package webserver

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"rasbeacon/internal/api"
)

// fakeRBD stands in for rbd. It answers the routes rbweb calls and records
// what was forwarded to it.
type fakeRBD struct {
	mode          api.Mode
	adminPassword string
	lastForwarded string
}

func (f *fakeRBD) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, api.SetupState{Mode: f.mode, LANAddress: "10.87.0.1"})
	})
	mux.HandleFunc("POST /verify-password", func(w http.ResponseWriter, r *http.Request) {
		var req api.PasswordRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeJSON(w, http.StatusOK, api.VerifyPasswordResponse{OK: req.Password == f.adminPassword})
	})
	mux.HandleFunc("POST /claim", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, api.ClaimResponse{HotspotSSID: "x"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.lastForwarded = r.Method + " " + r.URL.Path
		writeJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
	})
	return mux
}

// newTestServer returns an rbweb server backed by a fake rbd and a tiny UI.
func newTestServer(mode api.Mode) (*Server, *fakeRBD) {
	rbd := &fakeRBD{mode: mode, adminPassword: "tangerine-orbit-42"}
	ui := fstest.MapFS{
		"index.html":         {Data: []byte("<html>RasBeacon UI</html>")},
		"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
	}
	return New(NewRBDClientForHandler(rbd.handler()), ui), rbd
}

// request sends a request to the server as if it arrived on the hotspot
// address 10.87.0.1 from a phone at 10.87.0.23.
func request(s *Server, method, url, body string, headers map[string]string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.RemoteAddr = "10.87.0.23:50000"
	local := &net.TCPAddr{IP: net.ParseIP("10.87.0.1"), Port: 80}
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, local))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// login logs in and returns the session cookie and CSRF token.
func login(t *testing.T, s *Server) ([]*http.Cookie, string) {
	t.Helper()
	rec := request(s, "POST", "http://10.87.0.1/api/v1/session", `{"password":"tangerine-orbit-42"}`, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body)
	}
	var info api.SessionInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	return rec.Result().Cookies(), info.CSRFToken
}

func TestSetupMode_RedirectsOtherWebsitesToSetupPage(t *testing.T) {
	s, _ := newTestServer(api.ModeSetup)

	// An iPhone checking for internet access.
	rec := request(s, "GET", "http://captive.apple.com/hotspot-detect.html", "", nil, nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "http://10.87.0.1/" {
		t.Errorf("want redirect to setup page, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// The setup page itself is served, not redirected.
	rec = request(s, "GET", "http://10.87.0.1/", "", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "RasBeacon UI") {
		t.Errorf("setup page should be served, got %d", rec.Code)
	}
}

func TestNormalMode_DoesNotRedirect(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	rec := request(s, "GET", "http://rasbeacon.lan/settings", "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("UI routes should serve index.html in normal mode, got %d", rec.Code)
	}
}

func TestUI_HashedAssetsAreCachedLong(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	rec := request(s, "GET", "http://10.87.0.1/assets/app-1a2b.js", "", nil, nil)
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("hashed assets should be cached, got %q", rec.Header().Get("Cache-Control"))
	}
}

func TestCaptiveAPI_TrueOnlyDuringSetup(t *testing.T) {
	for _, tc := range []struct {
		mode    api.Mode
		captive bool
	}{{api.ModeSetup, true}, {api.ModeNormal, false}} {
		s, _ := newTestServer(tc.mode)
		rec := request(s, "GET", "http://10.87.0.1/api/v1/captive", "", nil, nil)
		var info api.CaptivePortalInfo
		_ = json.Unmarshal(rec.Body.Bytes(), &info)
		if info.Captive != tc.captive || rec.Header().Get("Content-Type") != "application/captive+json" {
			t.Errorf("mode %s: got %+v (%s)", tc.mode, info, rec.Header().Get("Content-Type"))
		}
	}
}

func TestProtectedRoutes_NeedLogin(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	rec := request(s, "GET", "http://10.87.0.1/api/v1/status", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status without login = %d, want 401", rec.Code)
	}
}

func TestLogin_ThenChangesNeedCSRFToken(t *testing.T) {
	s, rbd := newTestServer(api.ModeNormal)
	cookies, csrf := login(t, s)

	// Reading works with just the cookie.
	rec := request(s, "GET", "http://10.87.0.1/api/v1/status", "", nil, cookies)
	if rec.Code != http.StatusOK || rbd.lastForwarded != "GET /status" {
		t.Fatalf("status with login: %d, forwarded %q", rec.Code, rbd.lastForwarded)
	}

	// Changing settings without the CSRF header is refused.
	rec = request(s, "PUT", "http://10.87.0.1/api/v1/settings/hotspot", `{}`, nil, cookies)
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT without CSRF token = %d, want 403", rec.Code)
	}

	// With the header it is forwarded to rbd.
	rec = request(s, "PUT", "http://10.87.0.1/api/v1/settings/hotspot", `{}`,
		map[string]string{"X-CSRF-Token": csrf}, cookies)
	if rec.Code != http.StatusOK || rbd.lastForwarded != "PUT /settings/hotspot" {
		t.Errorf("PUT with CSRF token: %d, forwarded %q", rec.Code, rbd.lastForwarded)
	}
}

func TestLogin_SessionCookieIsHttpOnlyAndStrict(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	cookies, _ := login(t, s)
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Errorf("unexpected cookie: %+v", cookies)
	}
}

func TestLogout_EndsSession(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	cookies, _ := login(t, s)
	request(s, "DELETE", "http://10.87.0.1/api/v1/session", "", nil, cookies)
	rec := request(s, "GET", "http://10.87.0.1/api/v1/status", "", nil, cookies)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("after logout status = %d, want 401", rec.Code)
	}
}

func TestLogin_WrongPasswordsAreRateLimited(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	wrong := `{"password":"guess"}`

	for i := 0; i < freeAttempts; i++ {
		rec := request(s, "POST", "http://10.87.0.1/api/v1/session", wrong, nil, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d, want 401", i+1, rec.Code)
		}
	}
	// Attempt 6 sets a delay; attempt 7 arrives too soon.
	request(s, "POST", "http://10.87.0.1/api/v1/session", wrong, nil, nil)
	rec := request(s, "POST", "http://10.87.0.1/api/v1/session", wrong, nil, nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("want 429 with Retry-After, got %d", rec.Code)
	}
}

func TestClaim_LogsTheOwnerIn(t *testing.T) {
	s, _ := newTestServer(api.ModeSetup)
	rec := request(s, "POST", "http://10.87.0.1/api/v1/setup/claim", `{}`, nil, nil)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 1 {
		t.Errorf("successful claim should set a session cookie, got %d %v", rec.Code, rec.Result().Cookies())
	}
}

func TestRequestsOnOtherAddressesAreRejected(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	req := httptest.NewRequest("GET", "http://192.168.1.50/", nil)
	upstream := &net.TCPAddr{IP: net.ParseIP("192.168.1.50"), Port: 80}
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, upstream))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("request on the upstream address = %d, want 403", rec.Code)
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	rec := request(s, "GET", "http://10.87.0.1/api/v1/nope", "", nil, nil)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusNotFound || !strings.Contains(string(body), "no such API route") {
		t.Errorf("got %d %s", rec.Code, body)
	}
}

func TestLoginLimiter_GlobalLockoutAndRecovery(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := newLoginLimiter()
	l.now = func() time.Time { return now }

	// 20 failures from 20 different devices trigger the global lockout.
	for i := 0; i < globalFailureLimit; i++ {
		l.recordFailure("10.87.0." + string(rune('a'+i)))
	}
	if ok, _ := l.allow("10.87.0.200"); ok {
		t.Error("a new device should be blocked during the global lockout")
	}
	now = now.Add(globalLockout + time.Second)
	if ok, _ := l.allow("10.87.0.200"); !ok {
		t.Error("logins should work again after the lockout")
	}
}
