package agent

// This file exposes the agent through a small HTTP API on a Unix socket
// (/run/rbd/api.sock). Only rbweb talks to it; the socket's file permissions
// stop other programs.
//
// This API trusts its caller: rbweb is responsible for checking that the
// person using the web UI is logged in. Actions that are especially
// sensitive (revealing the hotspot password, factory reset) additionally
// require the admin password in the request, checked here.

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
func (a *Agent) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.SetupState())
	})

	mux.HandleFunc("POST /claim", func(w http.ResponseWriter, r *http.Request) {
		var req api.ClaimRequest
		if !readJSON(w, r, &req) {
			return
		}
		resp, err := a.Claim(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("POST /verify-password", func(w http.ResponseWriter, r *http.Request) {
		var req api.PasswordRequest
		if !readJSON(w, r, &req) {
			return
		}
		writeJSON(w, http.StatusOK, api.VerifyPasswordResponse{OK: a.VerifyAdminPassword(req.Password)})
	})

	// "Stay logged in" (see remember.go). rbweb has checked the password
	// before asking for a token.
	mux.HandleFunc("POST /remembered-logins", func(w http.ResponseWriter, r *http.Request) {
		token, expires, err := a.RememberLogin()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, api.RememberedLogin{Token: token, Expires: expires})
	})
	mux.HandleFunc("POST /remembered-logins/check", func(w http.ResponseWriter, r *http.Request) {
		var req api.RememberedLogin
		if !readJSON(w, r, &req) {
			return
		}
		writeJSON(w, http.StatusOK, api.VerifyPasswordResponse{OK: a.CheckRememberedLogin(req.Token)})
	})
	mux.HandleFunc("POST /remembered-logins/forget", func(w http.ResponseWriter, r *http.Request) {
		var req api.RememberedLogin
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.ForgetLogin(req.Token); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		// rbweb says which device is looking (?viewer=<its address>).
		writeJSON(w, http.StatusOK, a.StatusFor(r.URL.Query().Get("viewer")))
	})

	// Per-device controls on the Clients page. Each answers 204.
	mux.HandleFunc("POST /clients/internet-access", func(w http.ResponseWriter, r *http.Request) {
		var req api.ClientInternetAccessRequest
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.SetClientInternetAccess(req.MAC, req.Allowed); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /clients/name", func(w http.ResponseWriter, r *http.Request) {
		var req api.ClientNameRequest
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.SetClientName(req.MAC, req.Name); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /wifi-qr", func(w http.ResponseWriter, r *http.Request) {
		var req api.WiFiQRRequest
		if !readJSON(w, r, &req) {
			return
		}
		code, err := a.WiFiQR(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, code)
	})
	mux.HandleFunc("GET /settings/usage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.UsageSettings())
	})
	mux.HandleFunc("PUT /settings/usage", func(w http.ResponseWriter, r *http.Request) {
		var req api.UsageSettings
		if !readJSON(w, r, &req) {
			return
		}
		settings, err := a.UpdateUsageSettings(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	})
	mux.HandleFunc("POST /clients/usage/reset", func(w http.ResponseWriter, r *http.Request) {
		settings, err := a.ResetUsage()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	})
	clientActions := map[string]func(string) error{
		"POST /clients/disconnect":  a.DisconnectClient,
		"POST /clients/flush-lease": a.FlushClientLease,
	}
	for route, action := range clientActions {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			var req api.ClientRequest
			if !readJSON(w, r, &req) {
				return
			}
			if err := action(req.MAC); err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}

	mux.HandleFunc("GET /settings/hotspot", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.HotspotSettings())
	})

	mux.HandleFunc("PUT /settings/hotspot", func(w http.ResponseWriter, r *http.Request) {
		var req api.HotspotSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateHotspotSettings(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.HotspotSettings())
	})

	mux.HandleFunc("GET /settings/access", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.AccessSettings())
	})

	mux.HandleFunc("PUT /settings/access", func(w http.ResponseWriter, r *http.Request) {
		var req api.AccessSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateAccessSettings(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.AccessSettings())
	})

	mux.HandleFunc("POST /settings/hotspot/reveal-password", func(w http.ResponseWriter, r *http.Request) {
		password, err := a.RevealHotspotPassword()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, api.RevealPasswordResponse{HotspotPassword: password})
	})

	mux.HandleFunc("GET /settings/guest", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.GuestSettings())
	})

	mux.HandleFunc("PUT /settings/guest", func(w http.ResponseWriter, r *http.Request) {
		var req api.GuestSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateGuestSettings(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.GuestSettings())
	})

	mux.HandleFunc("GET /settings/ssh", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.SSHSettings())
	})

	mux.HandleFunc("PUT /settings/ssh", func(w http.ResponseWriter, r *http.Request) {
		var req api.SSHSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateSSHSettings(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.SSHSettings())
	})

	mux.HandleFunc("GET /settings/uplink", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.UplinkSettings())
	})

	mux.HandleFunc("PUT /settings/uplink", func(w http.ResponseWriter, r *http.Request) {
		var req api.UplinkSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateUplinkSettings(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.UplinkSettings())
	})

	mux.HandleFunc("POST /uplink/scan", func(w http.ResponseWriter, r *http.Request) {
		resp, err := a.ScanNearby()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})

	// The Internet page's buttons. Each answers 204.
	uplinkActions := map[string]func() error{
		"POST /uplink/disconnect":    a.DisconnectUplink,
		"POST /uplink/renew":         a.RenewDHCPLease,
		"POST /uplink/ethernet/scan": a.RescanEthernetPort,
	}
	for route, action := range uplinkActions {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			if err := action(); err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}

	// Runs Cloudflare's speed test (about 20 seconds; see speedtest.go) and
	// answers with the result.
	mux.HandleFunc("POST /speedtest", func(w http.ResponseWriter, r *http.Request) {
		if err := a.RunSpeedtest(); err != nil {
			writeError(w, err)
			return
		}
		a.mu.Lock()
		result := a.speedtestStatus()
		a.mu.Unlock()
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("GET /settings/swap", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.SwapSettings())
	})
	mux.HandleFunc("PUT /settings/swap", func(w http.ResponseWriter, r *http.Request) {
		var req api.SwapSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateSwapSettings(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.SwapSettings())
	})

	mux.HandleFunc("GET /settings/dns", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.DNSSettings())
	})
	mux.HandleFunc("PUT /settings/dns", func(w http.ResponseWriter, r *http.Request) {
		var req api.DNSSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateDNSSettings(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.DNSSettings())
	})

	mux.HandleFunc("GET /settings/vpn", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.VPNSettings())
	})

	mux.HandleFunc("PUT /settings/vpn", func(w http.ResponseWriter, r *http.Request) {
		var req api.VPNSwitches
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.SetVPNSwitches(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.VPNSettings())
	})

	mux.HandleFunc("POST /settings/vpn/import", func(w http.ResponseWriter, r *http.Request) {
		var req api.VPNImportRequest
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.ImportVPNProfile(req.Conf, req.Name); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.VPNSettings())
	})

	mux.HandleFunc("POST /settings/vpn/profile/save", func(w http.ResponseWriter, r *http.Request) {
		var req api.VPNProfileDetails
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.SaveVPNProfile(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.VPNSettings())
	})

	mux.HandleFunc("POST /settings/vpn/profile/rename", func(w http.ResponseWriter, r *http.Request) {
		var req api.VPNRenameRequest
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.RenameVPNProfile(req.Name, req.NewName); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.VPNSettings())
	})

	profileActions := map[string]func(string) error{
		"POST /settings/vpn/profile/activate":     a.SetActiveVPNProfile,
		"POST /settings/vpn/profile/remove":       a.RemoveVPNProfile,
		"POST /settings/vpn/profile/generate-key": a.GenerateVPNKey,
	}
	for route, action := range profileActions {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			var req api.VPNProfileRequest
			if !readJSON(w, r, &req) {
				return
			}
			if err := action(req.Name); err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, a.VPNSettings())
		})
	}

	mux.HandleFunc("GET /settings/https", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.HTTPSSettings())
	})

	mux.HandleFunc("PUT /settings/https", func(w http.ResponseWriter, r *http.Request) {
		var req api.HTTPSSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateHTTPSSettings(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.HTTPSSettings())
	})

	mux.HandleFunc("GET /portal", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.PortalStatus())
	})

	// Login mode for captive portals. Each answers with the new status.
	portalActions := map[string]func() error{
		"POST /portal/start":  a.StartLoginMode,
		"POST /portal/extend": a.ExtendLoginMode,
		"POST /portal/stop":   a.StopLoginMode,
		"POST /portal/check":  a.RecheckPortal,
	}
	for route, action := range portalActions {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			if err := action(); err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, a.PortalStatus())
		})
	}

	// The login browser (zero-leak login, see portalbrowser.go).
	mux.HandleFunc("POST /portal/browser/start", func(w http.ResponseWriter, r *http.Request) {
		var req api.PortalBrowserStart
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.StartPortalBrowser(req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.PortalStatus())
	})
	mux.HandleFunc("POST /portal/browser/stop", func(w http.ResponseWriter, r *http.Request) {
		a.StopPortalBrowser()
		writeJSON(w, http.StatusOK, a.PortalStatus())
	})
	mux.HandleFunc("POST /portal/browser/input", func(w http.ResponseWriter, r *http.Request) {
		var req api.PortalBrowserInput
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.PortalBrowserInput(req); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Waits up to frameWait for a picture newer than ?after=.
	mux.HandleFunc("GET /portal/browser/frame", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		ctx, cancel := context.WithTimeout(r.Context(), frameWait)
		defer cancel()
		writeJSON(w, http.StatusOK, a.PortalBrowserFrame(ctx, after))
	})

	// The body is the update file itself, streamed (it can be hundreds of
	// megabytes), so it isn't read with readJSON.
	mux.HandleFunc("POST /system/update", func(w http.ResponseWriter, r *http.Request) {
		if err := a.InstallUpdate(r.Body); err != nil {
			writeError(w, err)
			return
		}
		a.mu.Lock()
		status := a.updateStatus()
		a.mu.Unlock()
		writeJSON(w, http.StatusOK, status)
	})

	// Plugins (plugins.go). Their actions' problems are explained to the
	// owner, so they are answered as 400 with the reason.
	mux.HandleFunc("GET /plugins", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.PluginsState())
	})
	// The body is the .rba file itself, streamed like updates.
	mux.HandleFunc("POST /plugins/upload", func(w http.ResponseWriter, r *http.Request) {
		preview, err := a.StagePluginFile(r.Body)
		pluginAnswer(w, preview, err)
	})
	mux.HandleFunc("POST /plugins/catalog", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginCatalogRequest
		if !readJSON(w, r, &req) {
			return
		}
		writeJSON(w, http.StatusOK, a.PluginCatalog(req))
	})
	mux.HandleFunc("POST /plugins/catalog/download", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginDownloadRequest
		if !readJSON(w, r, &req) {
			return
		}
		preview, err := a.StagePluginFromCatalog(req)
		pluginAnswer(w, preview, err)
	})
	mux.HandleFunc("POST /plugins/update", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginIDRequest
		if !readJSON(w, r, &req) {
			return
		}
		preview, err := a.UpdatePluginFromCatalog(req.ID)
		pluginAnswer(w, preview, err)
	})
	mux.HandleFunc("POST /plugins/install", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginInstallRequest
		if !readJSON(w, r, &req) {
			return
		}
		p, err := a.InstallPlugin(req)
		pluginAnswer(w, p, err)
	})
	mux.HandleFunc("POST /plugins/cancel", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginInstallRequest
		if !readJSON(w, r, &req) {
			return
		}
		a.CancelPluginInstall(req.StagingID)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /plugins/settings", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginSettingsRequest
		if !readJSON(w, r, &req) {
			return
		}
		p, err := a.UpdatePluginSettings(req)
		pluginAnswer(w, p, err)
	})
	for route, action := range map[string]func(string) error{
		"POST /plugins/start": a.StartPlugin,
		"POST /plugins/stop":  a.StopPlugin,
	} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			var req api.PluginIDRequest
			if !readJSON(w, r, &req) {
				return
			}
			err := action(req.ID)
			pluginAnswer(w, a.PluginsState(), err)
		})
	}
	mux.HandleFunc("POST /plugins/remove", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginRemoveRequest
		if !readJSON(w, r, &req) {
			return
		}
		err := a.RemovePlugin(req)
		pluginAnswer(w, a.PluginsState(), err)
	})
	mux.HandleFunc("POST /plugins/logs", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginIDRequest
		if !readJSON(w, r, &req) {
			return
		}
		logs, err := a.PluginLogs(req.ID)
		pluginAnswer(w, logs, err)
	})
	mux.HandleFunc("GET /plugins/drives", func(w http.ResponseWriter, r *http.Request) {
		drives, err := a.PluginDrives()
		pluginAnswer(w, drives, err)
	})
	mux.HandleFunc("PUT /plugins/sources", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginSourcesRequest
		if !readJSON(w, r, &req) {
			return
		}
		state, err := a.SetPluginSources(req)
		pluginAnswer(w, state, err)
	})

	// Backups (backup.go): asked for with the passwords, then downloaded
	// with the ticket's token. The download is streamed as it is made:
	// plugin data can be large.
	mux.HandleFunc("POST /system/backup", func(w http.ResponseWriter, r *http.Request) {
		var req api.BackupRequest
		if !readJSON(w, r, &req) {
			return
		}
		ticket, err := a.PrepareBackup(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ticket)
	})
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
	mux.HandleFunc("POST /system/restore/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := a.StageRestore(r.Body); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /system/restore", func(w http.ResponseWriter, r *http.Request) {
		var req api.RestoreRequest
		if !readJSON(w, r, &req) {
			return
		}
		result, err := a.Restore(req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("GET /settings/updates", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.UpdateSettings())
	})
	mux.HandleFunc("POST /settings/updates/test", func(w http.ResponseWriter, r *http.Request) {
		var req api.NotifyTestRequest
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.SendTestNotification(req.URL); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("POST /system/update/check", func(w http.ResponseWriter, r *http.Request) {
		status, err := a.CheckForUpdateNow()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("PUT /settings/updates", func(w http.ResponseWriter, r *http.Request) {
		var req api.UpdateSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateUpdateSettings(req); err != nil {
			writeError(w, err)
			return
		}
		a.mu.Lock()
		status := a.updateStatus()
		a.mu.Unlock()
		writeJSON(w, http.StatusOK, status)
	})

	mux.HandleFunc("POST /system/reboot", func(w http.ResponseWriter, r *http.Request) {
		a.Reboot()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /system/shutdown", func(w http.ResponseWriter, r *http.Request) {
		a.Shutdown()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /system/hostname", func(w http.ResponseWriter, r *http.Request) {
		var req api.HostnameRequest
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.SetHostname(req.Hostname); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /system/factory-reset", func(w http.ResponseWriter, r *http.Request) {
		var req api.PasswordRequest
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.FactoryReset(req.Password); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /diagnostics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(a.Diagnostics()))
	})

	return mux
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

// readJSON decodes the request body into target. On failure it writes a 400
// response and returns false.
func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
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
	case errors.Is(err, ErrWrongMode), errors.Is(err, ErrNoWiFiUplink), errors.Is(err, ErrUpdateBusy), errors.Is(err, ErrSpeedtestBusy):
		writeJSON(w, http.StatusConflict, api.ErrorResponse{Error: err.Error()})
	default:
		slog.Error("request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, api.ErrorResponse{Error: "internal error: " + err.Error()})
	}
}
