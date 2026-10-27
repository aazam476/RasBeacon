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
	"net/url"
	pathpkg "path"
	"regexp"
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
	// passwordMu lets one admin password check run at a time (passwords.go).
	passwordMu sync.Mutex

	// Cached copy of rbd's /state, refreshed at most every stateCacheTime,
	// because captive-portal checks happen on almost every request in
	// setup mode.
	stateMu      sync.Mutex
	cachedState  api.SetupState
	cachedLAN    string
	stateFetched time.Time

	// httpsOn is true when the HTTPS setting is on (see UseHTTPS).
	httpsOn bool
}

// UseHTTPS tells the server that HTTPS is on. The settings pages are then
// only used over HTTPS: plain-HTTP visits are sent to HTTPS, and logging in
// over plain HTTP is refused. The setup wizard, the login-page notice and
// the captive portal API stay on plain HTTP, because phones open them in
// their "Log in to network" sheet, which doesn't accept self-signed
// certificates.
func (s *Server) UseHTTPS() { s.httpsOn = true }

// needsHTTPS reports whether r should have used HTTPS.
func (s *Server) needsHTTPS(r *http.Request) bool {
	return s.httpsOn && r.TLS == nil
}

// refuseOverHTTP answers requests for the settings that came over plain
// HTTP while HTTPS is on.
func (s *Server) refuseOverHTTP(w http.ResponseWriter, r *http.Request) {
	_, lan := s.state()
	writeJSON(w, http.StatusForbidden, api.ErrorResponse{Error: "HTTPS is on: use https://" + lan + "/"})
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
	// The public POST routes only take JSON: a form on another website
	// can't send that without the browser asking RasBeacon first (CORS),
	// so it can't claim RasBeacon or log a browser in.
	mux.HandleFunc("GET /api/v1/setup", s.handleSetupState)
	mux.Handle("POST /api/v1/setup/claim", requireJSON(http.HandlerFunc(s.handleClaim)))
	mux.HandleFunc("GET /api/v1/session", s.handleGetSession)
	mux.Handle("POST /api/v1/session", requireJSON(http.HandlerFunc(s.handleLogin)))
	mux.HandleFunc("DELETE /api/v1/session", s.handleLogout)
	mux.HandleFunc("GET /api/v1/captive", s.handleCaptiveAPI)

	// Captive portal login mode: public, because the notice page opens in a
	// phone's "Log in to network" sheet, before anyone has logged in to
	// RasBeacon. Any device on the hotspot may use it (it only opens web
	// and DNS for a few minutes, and only when a login page is detected).
	mux.Handle("GET /api/v1/portal", s.forwardTo("/portal"))
	for _, action := range []string{"start", "extend", "stop", "check"} {
		mux.Handle("POST /api/v1/portal/"+action, requireJSON(s.forwardTo("/portal/"+action)))
	}
	// The zero-leak login: RasBeacon's own browser shows the login page.
	// Public for the same reason. Only devices on the hotspot can reach it
	// (not guests, not the upstream network).
	for _, action := range []string{"start", "input", "stop"} {
		mux.Handle("POST /api/v1/portal/browser/"+action, requireJSON(s.forwardTo("/portal/browser/"+action)))
	}
	mux.HandleFunc("GET /api/v1/portal/browser/frame", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		s.forward(w, http.MethodGet, "/portal/browser/frame?after="+strconv.FormatInt(after, 10), nil)
	})

	// --- Login required: forwarded to rbd unchanged ---
	// Each entry maps a public API route to rbd's internal route.
	for route, rbdPath := range forwardedRoutes {
		mux.Handle(route, s.requireLogin(s.forwardTo(rbdPath)))
	}
	// The status tells rbd who is looking, so the client list can mark
	// "this device".
	mux.Handle("GET /api/v1/status", s.requireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.forward(w, http.MethodGet, "/status?viewer="+url.QueryEscape(clientIP(r)), nil)
	})))
	// The speed test takes about 20 seconds (rbd stops it after 3 minutes),
	// longer than the server's usual time limits.
	mux.Handle("POST /api/v1/speedtest", s.requireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(speedtestTime))
		status, body, err := s.rbd.DoSlow(http.MethodPost, "/speedtest", nil)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
			return
		}
		writeRaw(w, status, "application/json", body)
	})))
	// Update files are large, so they are streamed to rbd.
	mux.Handle("POST /api/v1/system/update", s.requireLogin(http.HandlerFunc(s.handleUpdate)))
	// So are plugin files; and the catalog and plugin downloads take a
	// while (GitHub and image registries, through the VPN).
	mux.Handle("POST /api/v1/plugins/upload", s.requireLogin(http.HandlerFunc(s.handlePluginUpload)))
	// Backups stream both ways: the download as it is made (a link the
	// browser saves; POST /system/backup asks for it first), the upload to
	// rbd. Restoring one takes a while.
	mux.Handle("GET /api/v1/system/backup/{token}", s.requireLogin(http.HandlerFunc(s.handleBackupDownload)))
	mux.Handle("POST /api/v1/system/restore/upload", s.requireLogin(http.HandlerFunc(s.handleRestoreUpload)))
	// Routes that can take longer than the usual time limits: plugin
	// downloads (GitHub and image registries, through the VPN), starting
	// and stopping containers (slow on a Pi Zero), restoring a backup.
	for _, route := range []string{
		"POST /plugins/catalog", "POST /plugins/catalog/download", "POST /plugins/update",
		"POST /plugins/start", "POST /plugins/stop", "POST /plugins/remove", "PUT /plugins/settings",
		"POST /system/restore",
	} {
		method, path, _ := strings.Cut(route, " ")
		mux.Handle(method+" /api/v1"+path, s.requireLogin(s.slowForward(path)))
	}

	// Requests with the admin password go through the password gate
	// (passwords.go), like logins. Factory reset also logs everyone out;
	// a new password logs out every other browser.
	mux.Handle("POST /api/v1/system/factory-reset", s.requireLogin(http.HandlerFunc(s.handleFactoryReset)))
	mux.Handle("POST /api/v1/system/admin-password", s.requireLogin(http.HandlerFunc(s.handleAdminPassword)))

	// Unknown API routes get a JSON 404 instead of the web UI.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, api.ErrorResponse{Error: "no such API route"})
	})

	// Everything else is the web UI.
	mux.HandleFunc("/", s.handleUI)

	return withSecurityHeaders(s.onlyFromHotspot(mux))
}

// onlyFromHotspot rejects requests that did not arrive on RasBeacon's
// hotspot address. The firewall already blocks the web UI from the upstream
// network; this is a second, independent check ("defence in depth").
// Requests from the device itself (loopback) are allowed for local testing.
//
// It also rejects requests sent under another website's name (the Host
// header). A web page can point its own name at RasBeacon's address ("DNS
// rebinding"); the browser then treats RasBeacon as part of that site and
// lets the page send it requests. The one exception is the captive-portal
// redirect (see handleUI): during setup, or while the login-page notice is
// up, phones' connectivity checks (captive.apple.com, …) reach RasBeacon
// and are sent to its own address. They never reach the API.
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
		if !s.isDeviceHost(r.Host) {
			st, _ := s.state()
			captive := st.Mode == api.ModeSetup || st.PortalNotice
			if !captive || strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "open RasBeacon's settings at http://"+hostName+"/", http.StatusMisdirectedRequest)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isDeviceHost reports whether host (a Host header, with or without a
// port) is one of RasBeacon's own names: its hotspot address,
// rasbeacon.lan, the name the owner gave it, or this computer (loopback,
// for the simulated device and tests).
func (s *Server) isDeviceHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	st, lan := s.state()
	switch {
	case host == "" || host == lan || strings.EqualFold(host, hostName) || strings.EqualFold(host, "localhost"):
		return true
	case st.Hostname != "" && strings.EqualFold(host, st.Hostname):
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
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

// handleSetupState says whether RasBeacon is set up yet (no login needed).
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

// currentSession returns the browser's session. A browser without one (or
// whose session ended) but with a valid "Stay logged in" token gets a new
// session.
func (s *Server) currentSession(w http.ResponseWriter, r *http.Request) (*session, bool) {
	if _, sess, ok := s.sessions.fromRequest(r); ok {
		return sess, true
	}
	cookie, err := r.Cookie(rememberCookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	body, _ := json.Marshal(api.RememberedLogin{Token: cookie.Value})
	status, respBody, err := s.rbd.Do(http.MethodPost, "/remembered-logins/check", body)
	var check api.VerifyPasswordResponse
	if err != nil || status != http.StatusOK || json.Unmarshal(respBody, &check) != nil || !check.OK {
		if err == nil {
			clearRememberCookie(w) // expired or forgotten
		}
		return nil, false
	}
	id, _ := s.sessions.create()
	setSessionCookie(w, r, id)
	sess, ok := s.sessions.lookup(id)
	return sess, ok
}

// handleGetSession says whether this browser is logged in, and gives it
// its CSRF token.
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.currentSession(w, r)
	if !ok {
		writeJSON(w, http.StatusOK, api.SessionInfo{LoggedIn: false})
		return
	}
	writeJSON(w, http.StatusOK, api.SessionInfo{LoggedIn: true, CSRFToken: sess.csrfToken})
}

// handleLogin checks the admin password (with rate limiting) and starts a
// session.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.needsHTTPS(r) {
		s.refuseOverHTTP(w, r) // never send the admin password unencrypted
		return
	}
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var login api.LoginRequest
	if err := json.Unmarshal(body, &login); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	done, ok := s.passwordGate(w, r)
	if !ok {
		return
	}
	body, _ = json.Marshal(api.PasswordRequest{Password: login.Password})
	var verify api.VerifyPasswordResponse
	status, respBody, err := s.rbd.Do(http.MethodPost, "/verify-password", body)
	if err != nil || status != http.StatusOK || json.Unmarshal(respBody, &verify) != nil {
		done(passwordUnknown)
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: "could not check the password"})
		return
	}
	if !verify.OK {
		done(passwordWrong)
		writeJSON(w, http.StatusUnauthorized, api.ErrorResponse{Error: "Wrong password.", Field: "password"})
		return
	}
	done(passwordRight)
	if login.StayLoggedIn {
		var remembered api.RememberedLogin
		status, respBody, err := s.rbd.Do(http.MethodPost, "/remembered-logins", []byte("{}"))
		if err == nil && status == http.StatusOK && json.Unmarshal(respBody, &remembered) == nil {
			setRememberCookie(w, r, remembered.Token, remembered.Expires)
		} else {
			slog.Warn("could not remember the login", "error", err, "status", status)
		}
	}
	id, csrf := s.sessions.create()
	setSessionCookie(w, r, id)
	writeJSON(w, http.StatusOK, api.SessionInfo{LoggedIn: true, CSRFToken: csrf})
}

// handleLogout ends this browser's session and forgets its remembered
// login.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if id, _, ok := s.sessions.fromRequest(r); ok {
		s.sessions.remove(id)
	}
	if cookie, err := r.Cookie(rememberCookieName); err == nil && cookie.Value != "" {
		body, _ := json.Marshal(api.RememberedLogin{Token: cookie.Value})
		if _, _, err := s.rbd.Do(http.MethodPost, "/remembered-logins/forget", body); err != nil {
			slog.Warn("could not forget the remembered login", "error", err)
		}
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// speedtestTime is how long a speed test request may take.
const speedtestTime = 4 * time.Minute

// maxUpdateSize limits update uploads. Real ones are a few hundred MB.
const maxUpdateSize = 2 << 30

// handleUpdate streams an uploaded update file to rbd, which checks and
// installs it (see internal/agent/update.go).
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	s.streamUpload(w, r, "/system/update", maxUpdateSize)
}

// handlePluginUpload streams an uploaded plugin file to rbd, which checks
// it and answers what installing it means.
func (s *Server) handlePluginUpload(w http.ResponseWriter, r *http.Request) {
	s.streamUpload(w, r, "/plugins/upload", maxUpdateSize)
}

// handleBackupDownload streams a backup from rbd to the browser while rbd
// makes it. The token in the address is the ticket from POST
// /system/backup; only letters and digits are passed on.
func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !tokenPattern.MatchString(token) {
		writeJSON(w, http.StatusNotFound, api.ErrorResponse{Error: "This backup link has expired. Make the backup again."})
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(transferIdleTime))
	resp, err := s.rbd.Open(http.MethodGet, "/system/backup/"+token, nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
		return
	}
	defer resp.Body.Close()
	for _, header := range []string{"Content-Type", "Content-Disposition"} {
		if value := resp.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(slidingWriter{w: w, rc: rc}, resp.Body)
}

// handleRestoreUpload streams an uploaded backup to rbd, which keeps it
// until its password is given.
func (s *Server) handleRestoreUpload(w http.ResponseWriter, r *http.Request) {
	s.streamUpload(w, r, "/system/restore/upload", maxBackupSize)
}

// maxBackupSize limits backup uploads (plugins' data included).
const maxBackupSize = 32 << 30

// pluginDownloadTime is how long a slow request may take (a large .rba
// downloaded over a slow connection, the longest of them).
const pluginDownloadTime = 30 * time.Minute

// tokenPattern is a backup ticket's token: 32 hex digits.
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// slowForward forwards a JSON request that may take minutes, with the
// request's own method.
func (s *Server) slowForward(rbdPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := readBody(w, r)
		if !ok {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(pluginDownloadTime))
		status, respBody, err := s.rbd.DoSlow(r.Method, rbdPath, body)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: err.Error()})
			return
		}
		writeRaw(w, status, "application/json", respBody)
	})
}

// requireJSON only lets through requests with a JSON body type. Other
// websites can make a browser send form posts to RasBeacon without asking,
// but not JSON (that needs permission, which rbweb never gives), so this
// stops them from using public actions.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeJSON(w, http.StatusUnsupportedMediaType, api.ErrorResponse{Error: "requests must be JSON"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleCaptiveAPI answers RFC 8908 captive-portal queries. During setup it
// tells phones to open the setup page, and while the "this network needs a
// login" notice is up, the notice page.
func (s *Server) handleCaptiveAPI(w http.ResponseWriter, r *http.Request) {
	st, lan := s.state()
	info := api.CaptivePortalInfo{Captive: st.Mode == api.ModeSetup || st.PortalNotice}
	switch {
	case st.Mode == api.ModeSetup:
		info.UserPortalURL = "http://" + lan + "/"
	case st.PortalNotice:
		info.UserPortalURL = "http://" + lan + "/#portal"
	}
	w.Header().Set("Cache-Control", "no-store")
	body, _ := json.Marshal(info)
	writeRaw(w, http.StatusOK, "application/captive+json", body)
}

// --- Login-required handlers ---

// csrfField marks a refusal for a missing or wrong CSRF token.
const csrfField = "csrfToken"

// requireLogin lets the request through only with a valid session, and for
// requests that change something, a matching CSRF token.
func (s *Server) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.needsHTTPS(r) {
			s.refuseOverHTTP(w, r)
			return
		}
		sess, ok := s.currentSession(w, r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, api.ErrorResponse{Error: "Please log in."})
			return
		}
		if r.Method != http.MethodGet && !csrfTokenMatches(sess, r.Header.Get("X-CSRF-Token")) {
			// The field tells the web UI this refusal is about the token:
			// after rbweb restarts, "Stay logged in" starts a new session
			// with a new token, and the open page fetches it and tries
			// again (see request() in web/src/data/real/api.ts).
			writeJSON(w, http.StatusForbidden, api.ErrorResponse{
				Error: "Missing or wrong CSRF token. Reload the page and try again.",
				Field: csrfField,
			})
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

// --- Web UI ---

// handleUI serves the web UI. During setup, requests for any other website
// (a phone checking captive.apple.com, for example) are redirected to the
// setup page, which makes phones show their "Log in to network" screen.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	st, lan := s.state()
	if !s.isDeviceHost(r.Host) && (st.Mode == api.ModeSetup || st.PortalNotice) {
		// A phone's connectivity check (captive.apple.com, ...) reached us
		// because our DNS answered it: send it to the setup page or to the
		// "this network needs a login" notice.
		target := "http://" + lan + "/"
		if st.Mode != api.ModeSetup {
			target += "#portal"
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	// With HTTPS on, the settings pages move to HTTPS, except while the
	// setup wizard or the login-page notice may be showing (see UseHTTPS).
	// r.Host is one of RasBeacon's own names here (onlyFromHotspot).
	if s.needsHTTPS(r) && st.Mode != api.ModeSetup && !st.PortalNotice {
		http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusFound)
		return
	}

	// Serve the file if it exists; otherwise serve index.html so the web UI
	// can show the right screen for addresses like /settings.
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(s.ui, path); err != nil {
		// A missing file (index.php, .env, wp-login.php: what scanners try)
		// is "not found", so they don't report software that isn't here.
		// Only addresses without a file extension are screens.
		if pathpkg.Ext(path) != "" {
			http.NotFound(w, r)
			return
		}
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

// readBody reads a request body of at most 512 KiB (the welcome page's
// settings, with a logo and a page of the owner's own, are the biggest). On
// failure it writes a 400 response and returns false.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512*1024))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "request body too large or unreadable"})
		return nil, false
	}
	return body, true
}

// writeJSON answers with value as JSON.
func writeJSON(w http.ResponseWriter, status int, value any) {
	body, _ := json.Marshal(value)
	writeRaw(w, status, "application/json", body)
}

// writeRaw answers with a status and a body as it is.
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

// forwardedRoutes are the routes that need a login and go to rbd unchanged:
// each public API route and rbd's internal route.
var forwardedRoutes = map[string]string{
	"GET /api/v1/settings/hotspot":                   "/settings/hotspot",
	"PUT /api/v1/settings/hotspot":                   "/settings/hotspot",
	"GET /api/v1/settings/access":                    "/settings/access",
	"PUT /api/v1/settings/access":                    "/settings/access",
	"POST /api/v1/settings/hotspot/reveal-password":  "/settings/hotspot/reveal-password",
	"GET /api/v1/settings/guest":                     "/settings/guest",
	"PUT /api/v1/settings/guest":                     "/settings/guest",
	"GET /api/v1/settings/ssh":                       "/settings/ssh",
	"PUT /api/v1/settings/ssh":                       "/settings/ssh",
	"GET /api/v1/settings/uplink":                    "/settings/uplink",
	"PUT /api/v1/settings/uplink":                    "/settings/uplink",
	"POST /api/v1/uplink/scan":                       "/uplink/scan",
	"POST /api/v1/uplink/disconnect":                 "/uplink/disconnect",
	"POST /api/v1/uplink/renew":                      "/uplink/renew",
	"POST /api/v1/uplink/ethernet/scan":              "/uplink/ethernet/scan",
	"GET /api/v1/settings/dns":                       "/settings/dns",
	"PUT /api/v1/settings/dns":                       "/settings/dns",
	"GET /api/v1/settings/vpn":                       "/settings/vpn",
	"PUT /api/v1/settings/vpn":                       "/settings/vpn",
	"POST /api/v1/settings/vpn/import":               "/settings/vpn/import",
	"POST /api/v1/settings/vpn/profile/save":         "/settings/vpn/profile/save",
	"POST /api/v1/settings/vpn/profile/activate":     "/settings/vpn/profile/activate",
	"POST /api/v1/settings/vpn/profile/remove":       "/settings/vpn/profile/remove",
	"POST /api/v1/settings/vpn/profile/rename":       "/settings/vpn/profile/rename",
	"POST /api/v1/settings/vpn/profile/generate-key": "/settings/vpn/profile/generate-key",
	"GET /api/v1/settings/https":                     "/settings/https",
	"PUT /api/v1/settings/https":                     "/settings/https",
	"GET /api/v1/settings/updates":                   "/settings/updates",
	"PUT /api/v1/settings/updates":                   "/settings/updates",
	"POST /api/v1/settings/updates/test":             "/settings/updates/test",
	"POST /api/v1/system/update/check":               "/system/update/check",
	"POST /api/v1/system/update/restart":             "/system/update/restart",
	"POST /api/v1/system/reboot":                     "/system/reboot",
	"POST /api/v1/system/shutdown":                   "/system/shutdown",
	"POST /api/v1/system/hostname":                   "/system/hostname",
	"GET /api/v1/diagnostics":                        "/diagnostics",
	"POST /api/v1/clients/internet-access":           "/clients/internet-access",
	"POST /api/v1/clients/disconnect":                "/clients/disconnect",
	"POST /api/v1/clients/flush-lease":               "/clients/flush-lease",
	"POST /api/v1/wifi-qr":                           "/wifi-qr",
	"POST /api/v1/settings/hotspot/channels":         "/settings/hotspot/channels",
	"POST /api/v1/clients/name":                      "/clients/name",
	"POST /api/v1/clients/fixed-address":             "/clients/fixed-address",
	"GET /api/v1/settings/welcome":                   "/settings/welcome",
	"PUT /api/v1/settings/welcome":                   "/settings/welcome",
	"POST /api/v1/clients/welcome-reset":             "/clients/welcome-reset",
	"GET /api/v1/settings/port-forwarding":           "/settings/port-forwarding",
	"PUT /api/v1/settings/port-forwarding":           "/settings/port-forwarding",
	"GET /api/v1/settings/usage":                     "/settings/usage",
	"PUT /api/v1/settings/usage":                     "/settings/usage",
	"POST /api/v1/clients/usage/reset":               "/clients/usage/reset",
	"GET /api/v1/settings/swap":                      "/settings/swap",
	"PUT /api/v1/settings/swap":                      "/settings/swap",
	"GET /api/v1/plugins":                            "/plugins",
	"POST /api/v1/plugins/install":                   "/plugins/install",
	"POST /api/v1/plugins/cancel":                    "/plugins/cancel",
	"POST /api/v1/plugins/logs":                      "/plugins/logs",
	"GET /api/v1/plugins/drives":                     "/plugins/drives",
	"PUT /api/v1/plugins/sources":                    "/plugins/sources",
	"POST /api/v1/system/backup":                     "/system/backup",
}
