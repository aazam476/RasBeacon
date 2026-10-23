package webserver

// Login sessions for the web UI.
//
// After a successful login, the browser receives a random session ID in a
// cookie. Every later request sends the cookie back, and we look the ID up in
// a table kept in memory. Sessions are lost when rbweb restarts; you then
// simply log in again. Nothing about sessions is written to disk.
//
// CSRF protection: a malicious web page could try to make your browser send
// a request to RasBeacon (a "cross-site request forgery"). Two defences:
//   - The cookie is SameSite=Strict, so browsers don't attach it to requests
//     started by other websites.
//   - Every request that changes something must carry the session's CSRF
//     token in the X-CSRF-Token header. Other websites can't read the token.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

// sessionCookieName is the cookie that carries the session ID.
const sessionCookieName = "rasbeacon_session"

// rememberCookieName carries the "Stay logged in" token (see
// agent/remember.go), which starts a new session without the password
// after the old one ended (for example because RasBeacon restarted).
const rememberCookieName = "rasbeacon_remember"

// sessionIdleTimeout logs you out after this long without any request.
const sessionIdleTimeout = 12 * time.Hour

// session is one logged-in browser.
type session struct {
	csrfToken string
	lastSeen  time.Time
}

// sessionStore holds all sessions. It is safe to use from many requests at
// once because every method takes the lock.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	now      func() time.Time
}

// newSessionStore makes an empty store on the real clock.
func newSessionStore() *sessionStore {
	return &sessionStore{sessions: map[string]*session{}, now: time.Now}
}

// create starts a new session and returns its ID and CSRF token.
func (s *sessionStore) create() (id, csrfToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Sessions are otherwise only dropped when they are looked up, so the
	// ones nobody comes back to go here.
	for oldID, old := range s.sessions {
		if s.now().Sub(old.lastSeen) > sessionIdleTimeout {
			delete(s.sessions, oldID)
		}
	}
	id = randomToken()
	csrfToken = randomToken()
	s.sessions[id] = &session{csrfToken: csrfToken, lastSeen: s.now()}
	return id, csrfToken
}

// lookup returns the session for id if it exists and has not expired, and
// marks it as used now.
func (s *sessionStore) lookup(id string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if s.now().Sub(sess.lastSeen) > sessionIdleTimeout {
		delete(s.sessions, id)
		return nil, false
	}
	sess.lastSeen = s.now()
	return sess, true
}

// remove ends one session (logout).
func (s *sessionStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// removeAll ends every session, for example after a factory reset.
func (s *sessionStore) removeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]*session{}
}

// removeAllExcept ends every session but keep (the admin password changed
// in that one).
func (s *sessionStore) removeAllExcept(keep string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.sessions {
		if id != keep {
			delete(s.sessions, id)
		}
	}
}

// fromRequest finds the session for the cookie in r, if any.
func (s *sessionStore) fromRequest(r *http.Request) (id string, sess *session, ok bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", nil, false
	}
	sess, ok = s.lookup(cookie.Value)
	return cookie.Value, sess, ok
}

// csrfTokenMatches compares tokens in constant time (see auth.VerifyPassword
// for why that matters).
func csrfTokenMatches(sess *session, header string) bool {
	return header != "" && subtle.ConstantTimeCompare([]byte(sess.csrfToken), []byte(header)) == 1
}

// setSessionCookie sends the session cookie to the browser.
func setSessionCookie(w http.ResponseWriter, r *http.Request, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true, // JavaScript can't read it, so a script bug can't leak it
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil, // only sent over HTTPS when HTTPS is in use
	})
}

// clearSessionCookie deletes the cookies in the browser.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	clearRememberCookie(w)
}

// setRememberCookie sends the "Stay logged in" token to the browser. It
// lasts until the token expires, also when the browser is closed.
func setRememberCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     rememberCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	})
}

// clearRememberCookie tells the browser to delete its remembered-login
// cookie.
func clearRememberCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: rememberCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

// randomToken returns 32 random bytes as URL-safe text.
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// The operating system's random generator never fails in practice;
		// if it does, continuing would create guessable sessions.
		panic("reading random bytes: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
