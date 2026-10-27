package agent

// This file exposes the agent through a small HTTP API on a Unix socket
// (/run/rbd/api.sock). Only rbweb talks to it; the socket's file permissions
// stop other programs.
//
// This API trusts its caller: rbweb is responsible for checking that the
// person using the web UI is logged in. Actions that decide who can get in
// (factory reset, changing the admin password) also need the admin
// password in the request, checked here.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// frameWait is how long a request for the login browser's next picture
// waits when the page hasn't changed.
const frameWait = 10 * time.Second

// Handler returns the HTTP routes for the agent's API.
//
// Most routes do the same three steps: read the JSON request, ask the
// agent, then send the answer (or the error). The helpers at the end of
// this file (view, save, call, do, act, result and pluginCall) do those
// steps, so each such route is one line. Routes that do something
// different (streamed files, the login browser's pictures) are written
// out in full.
func (a *Agent) Handler() http.Handler {
	mux := http.NewServeMux()

	// --- First-time setup and logging in -------------------------------
	mux.HandleFunc("GET /state", view(a.SetupState))
	mux.HandleFunc("POST /claim", call(a.Claim))
	mux.HandleFunc("POST /verify-password", call(func(req api.PasswordRequest) (api.VerifyPasswordResponse, error) {
		return api.VerifyPasswordResponse{OK: a.VerifyAdminPassword(req.Password)}, nil
	}))
	// "Stay logged in" (see remember.go). rbweb has checked the password
	// before asking for a token.
	mux.HandleFunc("POST /remembered-logins", result(func() (api.RememberedLogin, error) {
		token, expires, err := a.RememberLogin()
		return api.RememberedLogin{Token: token, Expires: expires}, err
	}))
	mux.HandleFunc("POST /remembered-logins/check", call(func(req api.RememberedLogin) (api.VerifyPasswordResponse, error) {
		return api.VerifyPasswordResponse{OK: a.CheckRememberedLogin(req.Token)}, nil
	}))
	mux.HandleFunc("POST /remembered-logins/forget", do(func(req api.RememberedLogin) error { return a.ForgetLogin(req.Token) }))

	// --- Status and the Clients page -----------------------------------
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		// rbweb says which device is looking (?viewer=<its address>).
		writeJSON(w, http.StatusOK, a.StatusFor(r.URL.Query().Get("viewer")))
	})
	// Per-device controls. Each answers 204.
	mux.HandleFunc("POST /clients/internet-access", do(func(req api.ClientInternetAccessRequest) error {
		return a.SetClientInternetAccess(req.MAC, req.Allowed)
	}))
	mux.HandleFunc("POST /clients/name", do(func(req api.ClientNameRequest) error { return a.SetClientName(req.MAC, req.Name) }))
	mux.HandleFunc("POST /clients/fixed-address", do(func(req api.ClientFixedAddressRequest) error {
		return a.SetFixedAddress(req.MAC, req.Address)
	}))
	mux.HandleFunc("POST /clients/disconnect", do(func(req api.ClientRequest) error { return a.DisconnectClient(req.MAC) }))
	mux.HandleFunc("POST /clients/flush-lease", do(func(req api.ClientRequest) error { return a.FlushClientLease(req.MAC) }))
	mux.HandleFunc("POST /clients/priority", do(func(req api.ClientPriorityRequest) error {
		return a.SetClientPriority(req.MAC, req.Priority)
	}))
	mux.HandleFunc("PUT /settings/radios", do(func(req api.RadioRolesRequest) error { return a.UpdateRadioRoles(req.Roles) }))
	mux.HandleFunc("POST /settings/radios/keep", act(a.KeepRadioRoles))
	mux.HandleFunc("POST /settings/radios/revert", act(a.RevertRadioRoles))
	mux.HandleFunc("GET /settings/qos", view(a.QoSSettings))
	mux.HandleFunc("PUT /settings/qos", call(a.UpdateQoSSettings))
	mux.HandleFunc("GET /settings/usage", view(a.UsageSettings))
	mux.HandleFunc("PUT /settings/usage", call(a.UpdateUsageSettings))
	mux.HandleFunc("POST /clients/usage/reset", result(a.ResetUsage))

	// --- Hotspot, who may join, guests and SSH -------------------------
	mux.HandleFunc("GET /settings/hotspot", view(a.HotspotSettings))
	mux.HandleFunc("PUT /settings/hotspot", save(a.UpdateHotspotSettings, a.HotspotSettings))
	mux.HandleFunc("POST /settings/hotspot/channels", call(a.HotspotChannels))
	mux.HandleFunc("POST /settings/hotspot/reveal-password", result(func() (api.RevealPasswordResponse, error) {
		password, err := a.RevealHotspotPassword()
		return api.RevealPasswordResponse{HotspotPassword: password}, err
	}))
	mux.HandleFunc("POST /wifi-qr", call(a.WiFiQR))
	mux.HandleFunc("GET /settings/access", view(a.AccessSettings))
	mux.HandleFunc("PUT /settings/access", save(a.UpdateAccessSettings, a.AccessSettings))
	mux.HandleFunc("GET /settings/guest", view(a.GuestSettings))
	mux.HandleFunc("PUT /settings/guest", save(a.UpdateGuestSettings, a.GuestSettings))
	mux.HandleFunc("GET /settings/ssh", view(a.SSHSettings))
	mux.HandleFunc("PUT /settings/ssh", save(a.UpdateSSHSettings, a.SSHSettings))

	// --- The Internet page ---------------------------------------------
	mux.HandleFunc("GET /settings/uplink", view(a.UplinkSettings))
	mux.HandleFunc("PUT /settings/uplink", save(a.UpdateUplinkSettings, a.UplinkSettings))
	mux.HandleFunc("POST /uplink/scan", result(a.ScanNearby))
	// Its buttons. Each answers 204.
	mux.HandleFunc("POST /uplink/disconnect", act(a.DisconnectUplink))
	mux.HandleFunc("POST /uplink/renew", act(a.RenewDHCPLease))
	mux.HandleFunc("POST /uplink/ethernet/scan", act(a.RescanEthernetPort))
	// Runs Cloudflare's speed test (about 20 seconds; see speedtest.go) and
	// answers with the result.
	mux.HandleFunc("POST /speedtest", result(func() (api.Speedtest, error) {
		if err := a.RunSpeedtest(); err != nil {
			return api.Speedtest{}, err
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.speedtestStatus(), nil
	}))
	mux.HandleFunc("GET /settings/port-forwarding", view(a.PortForwardingSettings))
	mux.HandleFunc("PUT /settings/port-forwarding", save(a.UpdatePortForwarding, a.PortForwardingSettings))
	mux.HandleFunc("GET /settings/modem", view(a.ModemSettings))
	mux.HandleFunc("PUT /settings/modem", save(a.UpdateModemSettings, a.ModemSettings))
	mux.HandleFunc("GET /settings/welcome", view(a.WelcomeSettings))
	mux.HandleFunc("PUT /settings/welcome", save(a.UpdateWelcomeSettings, a.WelcomeSettings))
	// The welcome page itself (rbweb's welcome server, not the settings
	// pages): what to show the device at ?ip=, and its "I agree".
	mux.HandleFunc("GET /welcome/page", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.WelcomePage(r.URL.Query().Get("ip")))
	})
	mux.HandleFunc("POST /welcome/accept", do(a.AcceptWelcome))
	mux.HandleFunc("POST /clients/welcome-reset", do(func(req api.ClientRequest) error { return a.ResetWelcome(req.MAC) }))
	mux.HandleFunc("GET /settings/dns", view(a.DNSSettings))
	mux.HandleFunc("PUT /settings/dns", save(a.UpdateDNSSettings, a.DNSSettings))

	// --- VPN -----------------------------------------------------------
	// Each change answers with the VPN settings as they are afterwards.
	mux.HandleFunc("GET /settings/vpn", view(a.VPNSettings))
	mux.HandleFunc("PUT /settings/vpn", save(a.SetVPNSwitches, a.VPNSettings))
	mux.HandleFunc("POST /settings/vpn/import", save(func(req api.VPNImportRequest) error {
		return a.ImportVPNProfile(req.Conf, req.Name)
	}, a.VPNSettings))
	mux.HandleFunc("POST /settings/vpn/profile/save", save(a.SaveVPNProfile, a.VPNSettings))
	mux.HandleFunc("POST /settings/vpn/profile/rename", save(func(req api.VPNRenameRequest) error {
		return a.RenameVPNProfile(req.Name, req.NewName)
	}, a.VPNSettings))
	for route, action := range map[string]func(string) error{
		"POST /settings/vpn/profile/activate":     a.SetActiveVPNProfile,
		"POST /settings/vpn/profile/remove":       a.RemoveVPNProfile,
		"POST /settings/vpn/profile/generate-key": a.GenerateVPNKey,
	} {
		mux.HandleFunc(route, save(func(req api.VPNProfileRequest) error { return action(req.Name) }, a.VPNSettings))
	}

	// --- HTTPS and the hotel login (captive portal) --------------------
	mux.HandleFunc("GET /settings/https", view(a.HTTPSSettings))
	mux.HandleFunc("PUT /settings/https", save(a.UpdateHTTPSSettings, a.HTTPSSettings))
	mux.HandleFunc("GET /portal", view(a.PortalStatus))
	// Login mode. Each answers with the new status.
	for route, action := range map[string]func() error{
		"POST /portal/start":  a.StartLoginMode,
		"POST /portal/extend": a.ExtendLoginMode,
		"POST /portal/stop":   a.StopLoginMode,
		"POST /portal/check":  a.RecheckPortal,
	} {
		mux.HandleFunc(route, result(func() (api.PortalStatus, error) {
			err := action() // first, so the status below is the new one
			return a.PortalStatus(), err
		}))
	}
	// The login browser (zero-leak login, see portalbrowser.go).
	mux.HandleFunc("POST /portal/browser/start", save(a.StartPortalBrowser, a.PortalStatus))
	mux.HandleFunc("POST /portal/browser/stop", result(func() (api.PortalStatus, error) {
		a.StopPortalBrowser()
		return a.PortalStatus(), nil
	}))
	mux.HandleFunc("POST /portal/browser/input", do(a.PortalBrowserInput))
	// Waits up to frameWait for a picture newer than ?after=.
	mux.HandleFunc("GET /portal/browser/frame", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		ctx, cancel := context.WithTimeout(r.Context(), frameWait)
		defer cancel()
		writeJSON(w, http.StatusOK, a.PortalBrowserFrame(ctx, after))
	})

	// --- Plugins (plugins.go) ------------------------------------------
	// Their actions' problems are explained to the owner, so they are
	// answered as 400 with the reason (see pluginAnswer).
	mux.HandleFunc("GET /plugins", view(a.PluginsState))
	// The body is the .rba file itself, streamed like updates.
	mux.HandleFunc("POST /plugins/upload", func(w http.ResponseWriter, r *http.Request) {
		preview, err := a.StagePluginFile(r.Body)
		pluginAnswer(w, preview, err)
	})
	mux.HandleFunc("POST /plugins/catalog", call(func(req api.PluginCatalogRequest) (api.PluginCatalog, error) {
		return a.PluginCatalog(req), nil
	}))
	mux.HandleFunc("POST /plugins/catalog/download", pluginCall(a.StagePluginFromCatalog))
	mux.HandleFunc("POST /plugins/update", pluginCall(func(req api.PluginIDRequest) (api.PluginPreview, error) {
		return a.UpdatePluginFromCatalog(req.ID)
	}))
	mux.HandleFunc("POST /plugins/install", pluginCall(a.InstallPlugin))
	mux.HandleFunc("POST /plugins/cancel", do(func(req api.PluginInstallRequest) error {
		a.CancelPluginInstall(req.StagingID)
		return nil
	}))
	mux.HandleFunc("PUT /plugins/settings", pluginCall(a.UpdatePluginSettings))
	for route, action := range map[string]func(string) error{
		"POST /plugins/start": a.StartPlugin,
		"POST /plugins/stop":  a.StopPlugin,
	} {
		mux.HandleFunc(route, pluginCall(func(req api.PluginIDRequest) (api.PluginsState, error) {
			err := action(req.ID)
			return a.PluginsState(), err
		}))
	}
	mux.HandleFunc("POST /plugins/remove", pluginCall(func(req api.PluginRemoveRequest) (api.PluginsState, error) {
		err := a.RemovePlugin(req)
		return a.PluginsState(), err
	}))
	mux.HandleFunc("POST /plugins/kept/forget", pluginCall(func(req api.PluginIDRequest) (api.PluginsState, error) {
		err := a.ForgetKeptData(req.ID)
		return a.PluginsState(), err
	}))
	mux.HandleFunc("POST /plugins/logs", pluginCall(func(req api.PluginIDRequest) (api.PluginLogs, error) {
		return a.PluginLogs(req.ID)
	}))
	mux.HandleFunc("GET /plugins/drives", func(w http.ResponseWriter, r *http.Request) {
		drives, err := a.PluginDrives()
		pluginAnswer(w, drives, err)
	})
	mux.HandleFunc("PUT /plugins/sources", pluginCall(a.SetPluginSources))

	// --- Backups (backup.go) -------------------------------------------
	// Asked for with the passwords, then downloaded with the ticket's
	// token. The download is streamed as it is made: plugin data can be
	// large.
	mux.HandleFunc("POST /system/backup", call(a.PrepareBackup))
	mux.HandleFunc("GET /system/backup/{token}", func(w http.ResponseWriter, r *http.Request) {
		out := &startedWriter{w: w, start: func() {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="`+a.backupFileName()+`"`)
		}}
		if err := a.WriteBackup(out, r.PathValue("token")); err != nil {
			if !out.started {
				writeError(w, err)
				return
			}
			// Too late for an error answer: the file has begun. Cut the
			// download off instead, so it can't be mistaken for a whole
			// backup. Panicking with http.ErrAbortHandler is how Go's HTTP
			// server is told to drop a connection mid-answer (it doesn't
			// treat it as a crash).
			slog.Error("backup failed", "error", err)
			panic(http.ErrAbortHandler)
		}
	})
	// The body is the backup file itself, streamed.
	mux.HandleFunc("POST /system/restore/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := a.StageRestore(r.Body); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /system/restore", call(a.Restore))

	// --- Updates -------------------------------------------------------
	// The body is the update file itself, streamed (it can be hundreds of
	// megabytes), so it isn't read with readJSON.
	mux.HandleFunc("POST /system/update", func(w http.ResponseWriter, r *http.Request) {
		if err := a.InstallUpdate(r.Body); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.lockedUpdateStatus())
	})
	mux.HandleFunc("GET /settings/updates", view(a.UpdateSettings))
	mux.HandleFunc("PUT /settings/updates", save(a.UpdateUpdateSettings, a.lockedUpdateStatus))
	mux.HandleFunc("POST /settings/updates/test", call(func(req api.NotifyTestRequest) (struct{}, error) {
		return struct{}{}, a.SendTestNotification(req.URL)
	}))
	mux.HandleFunc("POST /system/update/restart", act(a.RestartIntoUpdate))
	mux.HandleFunc("POST /system/update/check", result(a.CheckForUpdateNow))

	// --- The System page -----------------------------------------------
	mux.HandleFunc("GET /settings/swap", view(a.SwapSettings))
	mux.HandleFunc("PUT /settings/swap", save(a.UpdateSwapSettings, a.SwapSettings))
	mux.HandleFunc("POST /system/reboot", act(func() error { a.Reboot(); return nil }))
	mux.HandleFunc("POST /system/shutdown", act(func() error { a.Shutdown(); return nil }))
	mux.HandleFunc("POST /system/hostname", do(func(req api.HostnameRequest) error { return a.SetHostname(req.Hostname) }))
	mux.HandleFunc("POST /system/factory-reset", do(func(req api.PasswordRequest) error { return a.FactoryReset(req.Password) }))
	mux.HandleFunc("POST /system/admin-password", do(a.ChangeAdminPassword))
	mux.HandleFunc("GET /diagnostics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(a.Diagnostics()))
	})

	return mux
}

// lockedUpdateStatus is updateStatus for callers that don't hold a.mu.
func (a *Agent) lockedUpdateStatus() api.UpdateStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.updateStatus()
}

// --- Route helpers ---------------------------------------------------------
//
// Each helper makes a route's handler from an agent method. They are
// "generic" functions: the letters in square brackets stand for types that
// are filled in where the helper is used. For example, in
//
//	save(a.UpdateHotspotSettings, a.HotspotSettings)
//
// Req becomes api.HotspotSettings (what UpdateHotspotSettings takes) and
// Resp becomes api.HotspotSettings too (what HotspotSettings returns), so
// the compiler still checks every type.

// view answers a GET with what read returns.
func view[Resp any](read func() Resp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, read())
	}
}

// save reads a Req, saves it with change, then answers with read(): the
// settings as they are after the change.
func save[Req, Resp any](change func(Req) error, read func() Resp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req Req
		if !readJSON(w, r, &req) {
			return
		}
		if err := change(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, read())
	}
}

// call reads a Req, passes it to fn and answers with fn's result.
func call[Req, Resp any](fn func(Req) (Resp, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req Req
		if !readJSON(w, r, &req) {
			return
		}
		resp, err := fn(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// do reads a Req, passes it to fn and answers 204 No Content.
func do[Req any](fn func(Req) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req Req
		if !readJSON(w, r, &req) {
			return
		}
		if err := fn(req); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// act runs fn (the request has no body) and answers 204 No Content.
func act(fn func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// result runs fn (the request has no body) and answers with its result.
func result[Resp any](fn func() (Resp, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := fn()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// pluginCall is call for plugin actions: their errors are answered by
// pluginAnswer.
func pluginCall[Req, Resp any](fn func(Req) (Resp, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req Req
		if !readJSON(w, r, &req) {
			return
		}
		resp, err := fn(req)
		pluginAnswer(w, resp, err)
	}
}

// ListenUnix serves the API on a Unix socket. group is the numeric group ID
// allowed to connect (rbweb's group); -1 leaves the group unchanged.
func (a *Agent) ListenUnix(socketPath string, group int) error {
	// Remove a socket left over from a previous run.
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	// Owner (root) and group (rbweb) may connect; nobody else.
	if err := os.Chmod(socketPath, 0o660); err != nil {
		return err
	}
	if group >= 0 {
		if err := os.Chown(socketPath, 0, group); err != nil {
			return err
		}
	}
	slog.Info("API listening", "socket", socketPath)
	return http.Serve(listener, a.Handler())
}

// maxRequestBody is the largest JSON request: the welcome page's settings,
// with a logo and a page of the owner's own (see config.MaxWelcomeLogo and
// config.MaxWelcomePage), are the biggest.
const maxRequestBody = 512 * 1024

// readJSON decodes the request body into target. On failure it writes a 400
// response and returns false.
func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "invalid request: " + err.Error()})
		return false
	}
	return true
}

// writeJSON sends value as a JSON response.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// startedWriter calls start before the first byte is written, and
// remembers that writing began.
type startedWriter struct {
	w       http.ResponseWriter
	start   func()
	started bool
}

// Write calls start before the first bytes go out, then passes p on.
func (s *startedWriter) Write(p []byte) (int, error) {
	if !s.started {
		s.started = true
		s.start()
	}
	return s.w.Write(p)
}

// pluginAnswer answers a plugin action: the value, or why it failed.
func pluginAnswer(w http.ResponseWriter, value any, err error) {
	var validation *config.ValidationError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, value)
	case errors.As(err, &validation), errors.Is(err, ErrWrongMode):
		writeError(w, err)
	case errors.Is(err, errPluginBusy):
		writeJSON(w, http.StatusConflict, api.ErrorResponse{Error: err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: sentence(err.Error())})
	}
}

// writeError turns an error into the right HTTP status and message.
func writeError(w http.ResponseWriter, err error) {
	var validation *config.ValidationError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: validation.Message, Field: validation.Field})
	case errors.Is(err, ErrWrongPassword):
		writeJSON(w, http.StatusForbidden, api.ErrorResponse{Error: err.Error(), Field: "password"})
	case errors.Is(err, ErrWrongMode), errors.Is(err, ErrNoWiFiUplink), errors.Is(err, ErrUpdateBusy), errors.Is(err, ErrSpeedtestBusy),
		errors.Is(err, ErrRestoreBusy):
		writeJSON(w, http.StatusConflict, api.ErrorResponse{Error: err.Error()})
	default:
		slog.Error("request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, api.ErrorResponse{Error: "internal error: " + err.Error()})
	}
}
