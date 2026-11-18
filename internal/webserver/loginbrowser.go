package webserver

// The login browser (zero-leak login, see agent/portalbrowser.go) is open
// to any device on the hotspot, as it starts from the page a phone's "Log
// in to network" sheet shows, before anyone has logged in to RasBeacon. But
// what is typed into a hotel's login page (a room number, a name, a card)
// is the typing device's alone. So the browser that opens it gets a secret
// in a cookie, and rbd lets only that secret see the page and use it.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"rasbeacon/internal/api"
)

// browserCookieName holds a device's secret for the login browser.
const browserCookieName = "rb_login_browser"

// browserSession is the secret in r's cookie ("" without one).
func browserSession(r *http.Request) string {
	if c, err := r.Cookie(browserCookieName); err == nil {
		return c.Value
	}
	return ""
}

// handleBrowserStart opens the login browser for this device: with the
// secret its cookie has, or a new one, which the cookie gets once rbd has
// opened the browser.
func (s *Server) handleBrowserStart(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req api.PortalBrowserStart
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "invalid JSON"})
		return
	}
	req.Session = browserSession(r)
	if len(req.Session) != 64 {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			writeJSON(w, http.StatusInternalServerError, api.ErrorResponse{Error: "no randomness"})
			return
		}
		req.Session = hex.EncodeToString(secret)
	}
	forwarded, _ := json.Marshal(req)
	status, respBody, err := s.rbd.Do(http.MethodPost, "/portal/browser/start", forwarded)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
		return
	}
	if status == http.StatusOK {
		http.SetCookie(w, &http.Cookie{
			Name:     browserCookieName,
			Value:    req.Session,
			Path:     "/api/v1/portal/browser",
			HttpOnly: true, // JavaScript can't read it
			SameSite: http.SameSiteStrictMode,
			Secure:   r.TLS != nil,
		})
	}
	writeRaw(w, status, "application/json", respBody)
}

// handleBrowserInput passes a tap or typing to the login browser, with
// this device's secret.
func (s *Server) handleBrowserInput(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var in api.PortalBrowserInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "invalid JSON"})
		return
	}
	in.Session = browserSession(r)
	forwarded, _ := json.Marshal(in)
	s.forward(w, http.MethodPost, "/portal/browser/input", forwarded)
}

// handleBrowserStop closes the login browser, if this device opened it.
func (s *Server) handleBrowserStop(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	forwarded, _ := json.Marshal(api.PortalBrowserStop{Session: browserSession(r)})
	s.forward(w, http.MethodPost, "/portal/browser/stop", forwarded)
}
