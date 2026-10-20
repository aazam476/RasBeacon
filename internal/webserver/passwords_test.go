package webserver

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"rasbeacon/internal/api"
)

// The password gate (passwords.go): logins and the factory reset check the
// admin password one at a time, through the limiter.

// passwordServer is rbweb with a fake rbd that checks the admin password
// for the factory reset, as rbd does (403 when wrong).
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
}
