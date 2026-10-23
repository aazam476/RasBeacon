package webserver

// Every request that checks the admin password (logging in, changing the
// password, factory reset) goes through one gate: one check at a time, and
// only from a device the login limiter (ratelimit.go) allows.
//
// One at a time matters: asking the limiter and recording a wrong password
// are separate steps, and rbd takes about half a second per check on a Pi.
// Without the gate, a thousand logins sent at once would all be allowed
// before the first failure is recorded. rbd checks one password at a time
// anyway, so a person loses nothing.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"rasbeacon/internal/api"
)

// passwordResult is what a password check found.
type passwordResult int

const (
	// passwordUnknown: no answer (rbd unreachable, a refusal for another
	// reason). Nothing is recorded.
	passwordUnknown passwordResult = iota
	passwordWrong
	passwordRight
)

// passwordGate lets one password check through, or answers 429 itself and
// returns false. The caller must call done with what the check found.
func (s *Server) passwordGate(w http.ResponseWriter, r *http.Request) (done func(passwordResult), ok bool) {
	if !s.passwordMu.TryLock() {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, api.ErrorResponse{Error: "Another password is being checked. Try again in a moment."})
		return nil, false
	}
	ip := clientIP(r)
	if allowed, wait := s.limiter.allow(ip); !allowed {
		s.passwordMu.Unlock()
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, api.ErrorResponse{
			Error: "Too many wrong passwords. Please wait " + wait.Round(time.Second).String() + " and try again.",
		})
		return nil, false
	}
	return func(result passwordResult) {
		switch result {
		case passwordWrong:
			s.limiter.recordFailure(ip)
		case passwordRight:
			s.limiter.recordSuccess(ip)
		}
		s.passwordMu.Unlock()
	}, true
}

// resultOf reads rbd's answer to a request with a password: 403 is a
// wrong password, success a right one.
func resultOf(status int) passwordResult {
	switch {
	case status == http.StatusForbidden:
		return passwordWrong
	case status >= 200 && status < 300:
		return passwordRight
	}
	return passwordUnknown
}

// handleFactoryReset forwards a factory reset to rbd and, when it is
// accepted, logs every browser out.
func (s *Server) handleFactoryReset(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	done, ok := s.passwordGate(w, r)
	if !ok {
		return
	}
	status, respBody, err := s.rbd.Do(http.MethodPost, "/system/factory-reset", body)
	if err != nil {
		done(passwordUnknown)
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
		return
	}
	done(resultOf(status))
	if status == http.StatusNoContent {
		s.sessions.removeAll()
		clearSessionCookie(w)
	}
	writeRaw(w, status, "application/json", respBody)
}

// handleAdminPassword changes the admin password. When it is changed, every
// other browser is logged out and "Stay logged in" ends everywhere (rbd
// forgets the remembered logins); this browser stays logged in.
func (s *Server) handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req api.AdminPasswordRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	done, ok := s.passwordGate(w, r)
	if !ok {
		return
	}
	body, _ = json.Marshal(req)
	status, respBody, err := s.rbd.Do(http.MethodPost, "/system/admin-password", body)
	if err != nil {
		done(passwordUnknown)
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
		return
	}
	done(resultOf(status))
	if status == http.StatusNoContent {
		if id, _, ok := s.sessions.fromRequest(r); ok {
			s.sessions.removeAllExcept(id)
		}
		clearRememberCookie(w)
	}
	writeRaw(w, status, "application/json", respBody)
}
