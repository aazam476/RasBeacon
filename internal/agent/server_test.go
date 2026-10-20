package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/plugin"
)

// rbd's HTTP routes (server.go), as rbweb calls them: the answers' status
// codes and bodies. The work behind each route has its own tests; these
// check the routes themselves.

// call sends a request to rbd's routes and returns the status and body.
func (e *testEnv) call(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		data, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	rec := httptest.NewRecorder()
	e.agent.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, reader))
	return rec.Code, rec.Body.Bytes()
}

// Every route that reads a body refuses one it can't read, naming the
// problem, before doing anything.
func TestRoutes_RefuseBodiesTheyCantRead(t *testing.T) {
	e := newClaimedEnv(t, false)
	routes := []string{
		"POST /claim", "POST /verify-password", "POST /remembered-logins/check", "POST /remembered-logins/forget",
		"POST /clients/internet-access", "POST /clients/name", "POST /wifi-qr", "PUT /settings/usage",
		"PUT /settings/hotspot", "PUT /settings/access", "PUT /settings/guest", "PUT /settings/ssh",
		"PUT /settings/uplink", "PUT /settings/swap", "PUT /settings/dns", "PUT /settings/vpn",
		"POST /settings/vpn/import", "POST /settings/vpn/profile/save", "POST /settings/vpn/profile/rename",
		"PUT /settings/https", "POST /portal/browser/start", "POST /portal/browser/input",
		"POST /plugins/catalog", "POST /plugins/catalog/download", "POST /plugins/update", "POST /plugins/install",
		"POST /plugins/cancel", "PUT /plugins/settings", "POST /plugins/remove", "POST /plugins/logs",
		"PUT /plugins/sources", "POST /system/backup", "POST /system/restore", "POST /settings/updates/test",
		"PUT /settings/updates", "POST /system/hostname", "POST /system/factory-reset",
	}
	for _, route := range routes {
		method, path, _ := strings.Cut(route, " ")
		for name, body := range map[string]string{"broken JSON": "{", "an unknown field": `{"surprise": true}`} {
			status, answer := e.call(method, path, body)
			var got api.ErrorResponse
			if status != http.StatusBadRequest || json.Unmarshal(answer, &got) != nil || !strings.HasPrefix(got.Error, "invalid request") {
				t.Errorf("%s with %s = %d %s, want 400 invalid request", route, name, status, answer)
			}
		}
	}
}

// What the settings pages read can be saved back unchanged: the pages
// change one field of the answer and send the rest as it came.
func TestRoutes_SettingsReadCanBeSavedBack(t *testing.T) {
	e := newClaimedEnv(t, false)
	for _, path := range []string{
		"/settings/hotspot", "/settings/access", "/settings/guest", "/settings/ssh", "/settings/uplink",
		"/settings/dns", "/settings/usage", "/settings/updates",
	} {
		status, read := e.call(http.MethodGet, path, nil)
		if status != http.StatusOK {
			t.Errorf("GET %s = %d %s", path, status, read)
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(read, &body); err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		// Read-only fields the router adds to its answer.
		for _, field := range []string{"ethernetPresent", "wifiUplinkAvailable", "hasPassword", "since"} {
			delete(body, field)
		}
		if path == "/settings/uplink" {
			for _, network := range body["networks"].([]any) {
				delete(network.(map[string]any), "hasPassword")
			}
		}
		if status, answer := e.call(http.MethodPut, path, body); status != http.StatusOK {
			t.Errorf("PUT %s with what GET answered = %d %s", path, status, answer)
		}
	}
}

func TestRoutes_ReadsAnswerJSON(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = `{"blockdevices": []}`
	for _, path := range []string{
		"/state", "/status", "/settings/swap", "/settings/vpn", "/settings/https", "/portal", "/plugins",
		"/plugins/drives",
	} {
		status, body := e.call(http.MethodGet, path, nil)
		if status != http.StatusOK || !json.Valid(body) {
			t.Errorf("GET %s = %d %s, want 200 with JSON", path, status, body)
		}
	}
	if status, body := e.call(http.MethodGet, "/diagnostics", nil); status != http.StatusOK || !bytes.Contains(body, []byte("RasBeacon")) {
		t.Errorf("GET /diagnostics = %d, %d bytes", status, len(body))
	}
}

func TestRoutes_RevealHotspotPassword(t *testing.T) {
	e := newClaimedEnv(t, false)
	status, body := e.call(http.MethodPost, "/settings/hotspot/reveal-password", nil)
	var got api.RevealPasswordResponse
	if status != http.StatusOK || json.Unmarshal(body, &got) != nil || got.HotspotPassword != validClaim.HotspotPassword {
		t.Errorf("reveal-password = %d %s", status, body)
	}
}

func TestRoutes_RebootAndShutDownAnswerFirst(t *testing.T) {
	for _, tc := range []struct{ path, command string }{
		{"/system/reboot", "systemctl reboot"},
		{"/system/shutdown", "systemctl poweroff"},
	} {
		e := newClaimedEnv(t, false)
		if status, body := e.call(http.MethodPost, tc.path, nil); status != http.StatusNoContent {
			t.Errorf("POST %s = %d %s, want 204", tc.path, status, body)
		}
		if e.runner.ran(tc.command) {
			t.Errorf("POST %s ran %q before answering", tc.path, tc.command)
		}
		e.timers.runAll()
		if !e.runner.ran(tc.command) {
			t.Errorf("POST %s never ran %q", tc.path, tc.command)
		}
	}
}

func TestRoutes_FactoryResetNeedsTheAdminPassword(t *testing.T) {
	e := newClaimedEnv(t, false)
	status, body := e.call(http.MethodPost, "/system/factory-reset", api.PasswordRequest{Password: "wrong"})
	var refused api.ErrorResponse
	if status != http.StatusForbidden || json.Unmarshal(body, &refused) != nil || refused.Field != "password" {
		t.Errorf("factory reset with a wrong password = %d %s, want 403 on the password", status, body)
	}
	if !e.agent.SetupState().Claimed {
		t.Fatal("a wrong password erased the settings")
	}

	if status, body := e.call(http.MethodPost, "/system/factory-reset", api.PasswordRequest{Password: validClaim.AdminPassword}); status != http.StatusNoContent {
		t.Fatalf("factory reset = %d %s, want 204", status, body)
	}
	e.timers.runAll()
	if !e.runner.ran("systemctl reboot") {
		t.Error("RasBeacon should restart after erasing")
	}
}

func TestRoutes_PluginRoutesNameAMissingPlugin(t *testing.T) {
	e := newClaimedEnv(t, false)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/plugins/logs", api.PluginIDRequest{ID: "missing"}},
		{http.MethodPost, "/plugins/remove", api.PluginRemoveRequest{ID: "missing"}},
		{http.MethodPost, "/plugins/start", api.PluginIDRequest{ID: "missing"}},
	} {
		status, body := e.call(tc.method, tc.path, tc.body)
		var got api.ErrorResponse
		if status != http.StatusBadRequest || json.Unmarshal(body, &got) != nil || got.Error == "" {
			t.Errorf("%s %s = %d %s, want 400 with a message", tc.method, tc.path, status, body)
		}
	}
	// Cancelling an install that isn't there is no error: the sheet just closes.
	if status, body := e.call(http.MethodPost, "/plugins/cancel", api.PluginInstallRequest{StagingID: "nothing"}); status >= 400 {
		t.Errorf("cancel = %d %s", status, body)
	}
}

func TestRoutes_UnknownBackupDownload(t *testing.T) {
	e := newClaimedEnv(t, false)
	// An old or made-up link: a message, not a file.
	status, body := e.call(http.MethodGet, "/system/backup/no-such-token", nil)
	var got api.ErrorResponse
	if status != http.StatusBadRequest || json.Unmarshal(body, &got) != nil || got.Error == "" {
		t.Errorf("an unknown backup token = %d %s, want 400 with a message", status, body)
	}
}
