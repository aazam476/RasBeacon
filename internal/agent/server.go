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
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

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
		writeJSON(w, http.StatusOK, a.Status())
	})

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
	case errors.Is(err, ErrWrongMode), errors.Is(err, ErrNoWiFiUplink):
		writeJSON(w, http.StatusConflict, api.ErrorResponse{Error: err.Error()})
	default:
		slog.Error("request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, api.ErrorResponse{Error: "internal error: " + err.Error()})
	}
}
