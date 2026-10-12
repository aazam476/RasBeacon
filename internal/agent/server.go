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

	mux.HandleFunc("POST /settings/hotspot/reveal-password", func(w http.ResponseWriter, r *http.Request) {
		var req api.PasswordRequest
		if !readJSON(w, r, &req) {
			return
		}
		password, err := a.RevealHotspotPassword(req.Password)
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

	mux.HandleFunc("GET /settings/vpn", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.VPNSettings())
	})

	mux.HandleFunc("PUT /settings/vpn", func(w http.ResponseWriter, r *http.Request) {
		var req api.VPNSettings
		if !readJSON(w, r, &req) {
			return
		}
		if err := a.UpdateVPNSettings(req); err != nil {
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
		if err := a.ImportVPNConfig(req.Conf); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.VPNSettings())
	})

	mux.HandleFunc("POST /settings/vpn/generate-key", func(w http.ResponseWriter, r *http.Request) {
		if err := a.GenerateVPNKey(); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.VPNSettings())
	})

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

// writeError turns an error into the right HTTP status and message.
func writeError(w http.ResponseWriter, err error) {
	var validation *config.ValidationError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: validation.Message, Field: validation.Field})
	case errors.Is(err, ErrWrongPassword):
		writeJSON(w, http.StatusForbidden, api.ErrorResponse{Error: err.Error(), Field: "password"})
	case errors.Is(err, ErrWrongMode), errors.Is(err, ErrNoWiFiUplink), errors.Is(err, ErrUpdateBusy):
		writeJSON(w, http.StatusConflict, api.ErrorResponse{Error: err.Error()})
	default:
		slog.Error("request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, api.ErrorResponse{Error: "internal error: " + err.Error()})
	}
}
