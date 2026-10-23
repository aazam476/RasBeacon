package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"rasbeacon/internal/api"
)

// The password gate (passwords.go): logins, the factory reset and the new
// password all check the admin password one at a time, through the limiter.

// passwordServer is rbweb with a fake rbd that checks the admin password
// for the factory reset and a new password, as rbd does (403 when wrong).
func passwordServer(t *testing.T) (*Server, *fakeRBD) {
	t.Helper()
	rbd := &fakeRBD{mode: api.ModeNormal, adminPassword: "tangerine-orbit-42", remembered: map[string]bool{}}
	base := rbd.handler()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system/factory-reset", "/system/admin-password":
			var req struct {
				Password        string `json:"password"`
				CurrentPassword string `json:"currentPassword"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Password != rbd.adminPassword && req.CurrentPassword != rbd.adminPassword {
				writeJSON(w, http.StatusForbidden, api.ErrorResponse{Error: "Wrong password.", Field: "password"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			base.ServeHTTP(w, r)
		}
	})
	ui := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	return New(NewRBDClientForHandler(handler), ui), rbd
}

func TestPasswordGate_LoginsSentAllAtOnceAreCheckedOneAtATime(t *testing.T) {
	s, rbd := passwordServer(t)
	rbd.verifyDelay = 50 * time.Millisecond

	codes := make(chan int, 30)
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- request(s, "POST", "http://10.87.0.1/api/v1/session", `{"password":"guess"}`, nil, nil).Code
		}()
	}
	wg.Wait()
	close(codes)
	checked, refused := 0, 0
	for code := range codes {
		switch code {
		case http.StatusUnauthorized:
			checked++
		case http.StatusTooManyRequests:
			refused++
		default:
			t.Errorf("unexpected answer %d", code)
		}
	}
	// While one password is being checked, the others are turned away
	// instead of waiting their turn past the limiter.
	if refused < 25 || checked == 0 {
		t.Errorf("%d checked, %d refused; want almost all refused", checked, refused)
	}
}

func TestPasswordGate_TheFactoryResetPasswordCountsAsALogin(t *testing.T) {
	s, _ := passwordServer(t)
	cookies, csrf := login(t, s)
	headers := map[string]string{"X-CSRF-Token": csrf, "Content-Type": "application/json"}
	// Five wrong passwords are free, then the waits begin.
	for i := range 6 {
		rec := request(s, "POST", "http://10.87.0.1/api/v1/system/factory-reset", `{"password":"guess"}`, headers, cookies)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d = %d, want 403", i+1, rec.Code)
		}
	}
	rec := request(s, "POST", "http://10.87.0.1/api/v1/system/factory-reset", `{"password":"guess"}`, headers, cookies)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("after six wrong passwords = %d, want 429", rec.Code)
	}
	// The same limit applies to logging in from that device.
	if rec := request(s, "POST", "http://10.87.0.1/api/v1/session", `{"password":"tangerine-orbit-42"}`, nil, nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("login right after = %d, want 429", rec.Code)
	}
}

func TestAdminPassword_LogsOutEveryOtherBrowser(t *testing.T) {
	s, _ := passwordServer(t)
	mine, csrf := login(t, s)
	other, _ := login(t, s)
	headers := map[string]string{"X-CSRF-Token": csrf, "Content-Type": "application/json"}

	// A wrong current password changes nothing, and counts.
	rec := request(s, "POST", "http://10.87.0.1/api/v1/system/admin-password",
		`{"currentPassword":"guess","newPassword":"plum-harbor-sunrise-7"}`, headers, mine)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong current password = %d", rec.Code)
	}
	if s.limiter.failures["10.87.0.23"] != 1 {
		t.Errorf("failures = %d, want the wrong password counted", s.limiter.failures["10.87.0.23"])
	}

	rec = request(s, "POST", "http://10.87.0.1/api/v1/system/admin-password",
		`{"currentPassword":"tangerine-orbit-42","newPassword":"plum-harbor-sunrise-7"}`, headers, mine)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change = %d %s", rec.Code, rec.Body)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == rememberCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("this browser's Stay logged in cookie should be cleared (rbd forgot them all)")
	}
	if rec := request(s, "GET", "http://10.87.0.1/api/v1/status", "", nil, mine); rec.Code != http.StatusOK {
		t.Errorf("this browser = %d, want still logged in", rec.Code)
	}
	if rec := request(s, "GET", "http://10.87.0.1/api/v1/status", "", nil, other); rec.Code != http.StatusUnauthorized {
		t.Errorf("another browser = %d, want logged out", rec.Code)
	}
	// A request it can't read is refused before any password check.
	if rec := request(s, "POST", "http://10.87.0.1/api/v1/system/admin-password", `{`, headers, mine); rec.Code != http.StatusBadRequest {
		t.Errorf("broken body = %d", rec.Code)
	}
}

func TestPasswordGate_RBDUnreachableRecordsNothing(t *testing.T) {
	if got := resultOf(http.StatusBadGateway); got != passwordUnknown {
		t.Errorf("resultOf(502) = %v", got)
	}
	if got := resultOf(http.StatusBadRequest); got != passwordUnknown {
		t.Errorf("resultOf(400) = %v (a weak new password isn't a wrong one)", got)
	}
}

func TestSessions_ExpiredOnesAreSweptAway(t *testing.T) {
	store := newSessionStore()
	now := time.Now()
	store.now = func() time.Time { return now }
	old, _ := store.create()
	now = now.Add(sessionIdleTimeout + time.Minute)
	fresh, _ := store.create()
	if _, ok := store.sessions[old]; ok {
		t.Error("an expired session should be removed when a new one starts")
	}
	if _, ok := store.sessions[fresh]; !ok {
		t.Error("the new session is missing")
	}
	store.removeAllExcept(fresh)
	if len(store.sessions) != 1 {
		t.Errorf("sessions = %d", len(store.sessions))
	}
}

func TestPasswordGate_NothingIsCountedWhenRBDDoesntAnswer(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	s := New(NewRBDClient(filepath.Join(t.TempDir(), "missing.sock")), ui)
	for path, handler := range map[string]http.HandlerFunc{
		"/api/v1/system/factory-reset":  s.handleFactoryReset,
		"/api/v1/system/admin-password": s.handleAdminPassword,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "http://10.87.0.1"+path, strings.NewReader(`{"password":"x","currentPassword":"x"}`))
		req.RemoteAddr = "10.87.0.23:50000"
		handler(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Errorf("%s without rbd = %d, want 502", path, rec.Code)
		}
	}
	if len(s.limiter.failures) != 0 {
		t.Errorf("failures = %v, want none counted", s.limiter.failures)
	}
	// The gate is free again.
	if !s.passwordMu.TryLock() {
		t.Fatal("the password gate stayed locked")
	}
	s.passwordMu.Unlock()
}
