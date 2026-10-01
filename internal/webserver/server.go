// Package webserver is rbweb: the web server your phone or laptop talks to.
//
// It does three jobs:
//
//  1. Serves the web UI (HTML, JavaScript, CSS) built from web/.
//  2. Handles logins: it keeps sessions, checks CSRF tokens and slows down
//     password guessing (see sessions.go and ratelimit.go).
//  3. Forwards API requests from logged-in users to rbd, which does the real
//     work. rbweb itself runs without special privileges and never reads the
//     settings files.
//
// All API routes are under /api/v1/ and are listed in docs/api.md.
package webserver

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"rasbeacon/internal/api"
)

// Server is the rbweb HTTP server.
type Server struct {
	rbd      *RBDClient
	ui       fs.FS // the built web UI files
	sessions *sessionStore
	limiter  *loginLimiter

	// Cached copy of rbd's /state, refreshed at most every stateCacheTime,
	// because captive-portal checks happen on almost every request in
	// setup mode.
	stateMu      sync.Mutex
	cachedState  api.SetupState
	cachedLAN    string
	stateFetched time.Time
}

const stateCacheTime = 2 * time.Second

// hostName is the friendly name for the device on the hotspot.
const hostName = "rasbeacon.lan"

// New creates the server. ui holds the built web UI files.
func New(rbd *RBDClient, ui fs.FS) *Server {
	return &Server{rbd: rbd, ui: ui, sessions: newSessionStore(), limiter: newLoginLimiter()}
}

// Handler returns all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// --- Public: no login needed ---
	mux.HandleFunc("GET /api/v1/setup", s.handleSetupState)
	mux.HandleFunc("POST /api/v1/setup/claim", s.handleClaim)
	mux.HandleFunc("GET /api/v1/session", s.handleGetSession)
	mux.HandleFunc("POST /api/v1/session", s.handleLogin)
	mux.HandleFunc("DELETE /api/v1/session", s.handleLogout)
	mux.HandleFunc("GET /api/v1/captive", s.handleCaptiveAPI)

	// --- Login required: forwarded to rbd unchanged ---
	// Each entry maps a public API route to rbd's internal route.
	forwarded := map[string]string{
		"GET /api/v1/status":                            "/status",
		"GET /api/v1/settings/hotspot":                  "/settings/hotspot",
		"PUT /api/v1/settings/hotspot":                  "/settings/hotspot",
		"POST /api/v1/settings/hotspot/reveal-password": "/settings/hotspot/reveal-password",
		"GET /api/v1/settings/ssh":                      "/settings/ssh",
		"PUT /api/v1/settings/ssh":                      "/settings/ssh",
		"GET /api/v1/settings/uplink":                   "/settings/uplink",
		"PUT /api/v1/settings/uplink":                   "/settings/uplink",
		"POST /api/v1/uplink/scan":                      "/uplink/scan",
		"GET /api/v1/settings/vpn":                      "/settings/vpn",
		"PUT /api/v1/settings/vpn":                      "/settings/vpn",
		"POST /api/v1/settings/vpn/import":              "/settings/vpn/import",
		"POST /api/v1/settings/vpn/generate-key":        "/settings/vpn/generate-key",
		"POST /api/v1/system/reboot":                    "/system/reboot",
		"GET /api/v1/diagnostics":                       "/diagnostics",
	}
	for route, rbdPath := range forwarded {
		mux.Handle(route, s.requireLogin(s.forwardTo(rbdPath)))
	}
	// Factory reset also logs everyone out.
	mux.Handle("POST /api/v1/system/factory-reset", s.requireLogin(s.handleFactoryReset()))

	// Unknown API routes get a JSON 404 instead of the web UI.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, api.ErrorResponse{Error: "no such API route"})
	})

	// Everything else is the web UI.
	mux.HandleFunc("/", s.handleUI)

	return s.onlyFromHotspot(mux)
}

// onlyFromHotspot rejects requests that did not arrive on RasBeacon's
// hotspot address. The firewall already blocks the web UI from the upstream
// network; this is a second, independent check ("defence in depth").
// Requests from the device itself (loopback) are allowed for local testing.
func (s *Server) onlyFromHotspot(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
		if local != nil {
			host, _, _ := net.SplitHostPort(local.String())
			ip := net.ParseIP(host)
			_, lan := s.state()
			if ip != nil && !ip.IsLoopback() && host != lan {
				http.Error(w, "not available on this network", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// state returns rbd's setup state and the hotspot address, cached briefly.
func (s *Server) state() (api.SetupState, string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if time.Since(s.stateFetched) < stateCacheTime && s.cachedLAN != "" {
		return s.cachedState, s.cachedLAN
	}
	var st api.SetupState
	if err := s.rbd.GetJSON("/state", &st); err != nil {
		slog.Warn("could not reach rbd", "error", err)
		return s.cachedState, s.cachedLAN
	}
	s.cachedState = st
	s.cachedLAN = st.LANAddress
	s.stateFetched = time.Now()
	return s.cachedState, s.cachedLAN
}

// forgetState makes the next state() call ask rbd again.
func (s *Server) forgetState() {
	s.stateMu.Lock()
	s.stateFetched = time.Time{}
	s.stateMu.Unlock()
}

// --- Public handlers ---

func (s *Server) handleSetupState(w http.ResponseWriter, r *http.Request) {
	s.forward(w, http.MethodGet, "/state", nil)
}

// handleClaim forwards the setup wizard to rbd and, if it succeeds, logs the
// browser in so the owner lands on the status page after reconnecting.
func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	status, respBody, err := s.rbd.Do(http.MethodPost, "/claim", body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
		return
	}
	if status == http.StatusOK {
		id, _ := s.sessions.create()
		setSessionCookie(w, r, id)
		s.forgetState()
	}
	writeRaw(w, status, "application/json", respBody)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.sessions.fromRequest(r)
	if !ok {
		writeJSON(w, http.StatusOK, api.SessionInfo{LoggedIn: false})
		return
	}
	writeJSON(w, http.StatusOK, api.SessionInfo{LoggedIn: true, CSRFToken: sess.csrfToken})
}

// handleLogin checks the admin password (with rate limiting) and starts a
// session.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if allowed, wait := s.limiter.allow(ip); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, api.ErrorResponse{
			Error: "Too many wrong passwords. Please wait " + wait.Round(time.Second).String() + " and try again.",
		})
		return
	}

	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var verify api.VerifyPasswordResponse
	status, respBody, err := s.rbd.Do(http.MethodPost, "/verify-password", body)
	if err != nil || status != http.StatusOK || json.Unmarshal(respBody, &verify) != nil {
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: "could not check the password"})
		return
	}
	if !verify.OK {
		s.limiter.recordFailure(ip)
		writeJSON(w, http.StatusUnauthorized, api.ErrorResponse{Error: "Wrong password.", Field: "password"})
		return
	}

	s.limiter.recordSuccess(ip)
	id, csrf := s.sessions.create()
	setSessionCookie(w, r, id)
	writeJSON(w, http.StatusOK, api.SessionInfo{LoggedIn: true, CSRFToken: csrf})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if id, _, ok := s.sessions.fromRequest(r); ok {
		s.sessions.remove(id)
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleCaptiveAPI answers RFC 8908 captive-portal queries. During setup it
// tells phones to open the setup page.
func (s *Server) handleCaptiveAPI(w http.ResponseWriter, r *http.Request) {
	st, lan := s.state()
	info := api.CaptivePortalInfo{Captive: st.Mode == api.ModeSetup}
	if info.Captive {
		info.UserPortalURL = "http://" + lan + "/"
	}
	w.Header().Set("Cache-Control", "no-store")
	body, _ := json.Marshal(info)
	writeRaw(w, http.StatusOK, "application/captive+json", body)
}

// --- Login-required handlers ---

// requireLogin lets the request through only with a valid session, and for
// requests that change something, a matching CSRF token.
func (s *Server) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sess, ok := s.sessions.fromRequest(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, api.ErrorResponse{Error: "Please log in."})
			return
		}
		if r.Method != http.MethodGet && !csrfTokenMatches(sess, r.Header.Get("X-CSRF-Token")) {
			writeJSON(w, http.StatusForbidden, api.ErrorResponse{Error: "Missing or wrong CSRF token. Reload the page and try again."})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// forwardTo returns a handler that sends the request to rbdPath on rbd and
// copies rbd's answer back.
func (s *Server) forwardTo(rbdPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Method != http.MethodGet {
			var ok bool
			if body, ok = readBody(w, r); !ok {
				return
			}
		}
		s.forward(w, r.Method, rbdPath, body)
	})
}

// forward sends one request to rbd and copies the response.
func (s *Server) forward(w http.ResponseWriter, method, rbdPath string, body []byte) {
	status, respBody, err := s.rbd.Do(method, rbdPath, body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
		return
	}
	contentType := "application/json"
	if rbdPath == "/diagnostics" {
		contentType = "text/plain; charset=utf-8"
		w.Header().Set("Content-Disposition", `attachment; filename="rasbeacon-diagnostics.txt"`)
	}
	writeRaw(w, status, contentType, respBody)
}

func (s *Server) handleFactoryReset() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := readBody(w, r)
		if !ok {
			return
		}
		status, respBody, err := s.rbd.Do(http.MethodPost, "/system/factory-reset", body)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
			return
		}
		if status == http.StatusNoContent {
			s.sessions.removeAll()
			clearSessionCookie(w)
		}
		writeRaw(w, status, "application/json", respBody)
	})
}

// --- Web UI ---

// handleUI serves the web UI. During setup, requests for any other website
// (a phone checking captive.apple.com, for example) are redirected to the
// setup page, which makes phones show their "Log in to network" screen.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	st, lan := s.state()
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	isDevice := host == lan || host == hostName || host == "localhost" || host == "127.0.0.1"
	if st.Mode == api.ModeSetup && !isDevice {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "http://"+lan+"/", http.StatusFound)
		return
	}

	// Serve the file if it exists; otherwise serve index.html so the web UI
	// can show the right screen for addresses like /settings.
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(s.ui, path); err != nil {
		path = "index.html"
	}
	if _, err := fs.Stat(s.ui, "index.html"); err != nil {
		// Happens in a development build made before `make ui`.
		http.Error(w, "The web UI has not been built. Run `make ui`, then rebuild rbweb.", http.StatusServiceUnavailable)
		return
	}
	if path == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	} else if strings.HasPrefix(path, "assets/") {
		// Built asset names contain a content hash, so they never change.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, s.ui, path)
}

// --- Small helpers ---

// readBody reads a request body of at most 64 KiB. On failure it writes a
// 400 response and returns false.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "request body too large or unreadable"})
		return nil, false
	}
	return body, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, _ := json.Marshal(value)
	writeRaw(w, status, "application/json", body)
}

func writeRaw(w http.ResponseWriter, status int, contentType string, body []byte) {
	if len(body) > 0 {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// clientIP returns the address of the device that sent the request.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
