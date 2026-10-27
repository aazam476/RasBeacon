package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		"PUT /settings/hotspot", "POST /settings/hotspot/channels", "PUT /settings/access", "PUT /settings/guest", "PUT /settings/ssh",
		"PUT /settings/uplink", "PUT /settings/swap", "PUT /settings/dns", "PUT /settings/vpn",
		"POST /settings/vpn/import", "POST /settings/vpn/profile/save", "POST /settings/vpn/profile/rename",
		"PUT /settings/https", "POST /portal/browser/start", "POST /portal/browser/input",
		"POST /plugins/catalog", "POST /plugins/catalog/download", "POST /plugins/update", "POST /plugins/install",
		"POST /plugins/cancel", "PUT /plugins/settings", "POST /plugins/remove", "POST /plugins/logs",
		"PUT /plugins/sources", "POST /system/backup", "POST /system/restore", "POST /settings/updates/test",
		"PUT /settings/updates", "POST /system/hostname", "POST /system/factory-reset", "POST /system/admin-password",
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

func TestRoutes_RestartIntoTheUpdate(t *testing.T) {
	e := newClaimedEnv(t, false)
	if status, body := e.call(http.MethodPost, "/system/update/restart", nil); status != http.StatusBadRequest {
		t.Errorf("with no update waiting = %d %s, want 400", status, body)
	}
	mustWrite(t, e.paths.UpdatePendingFile(), `{"version":"v9.9.9","fromVersion":"dev"}`)
	if status, body := e.call(http.MethodPost, "/system/update/restart", nil); status != http.StatusNoContent {
		t.Errorf("with an update waiting = %d %s, want 204", status, body)
	}
	if e.runner.ran("systemctl reboot") {
		t.Error("it must answer before restarting")
	}
	e.timers.runAll()
	if !e.runner.ran("systemctl reboot --reboot-argument=0 tryboot") {
		t.Errorf("never restarted into the update; ran %q", e.runner.commands)
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

func TestRoutes_BackupDownloadStreamsTheFile(t *testing.T) {
	e := newClaimedEnv(t, false)
	status, body := e.call(http.MethodPost, "/system/backup", api.BackupRequest{Password: "backup-pass-42"})
	var ticket api.BackupTicket
	if status != http.StatusOK || json.Unmarshal(body, &ticket) != nil || ticket.Token == "" {
		t.Fatalf("POST /system/backup = %d %s", status, body)
	}

	rec := httptest.NewRecorder()
	e.agent.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/system/backup/"+ticket.Token, nil))
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("download = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, ticket.FileName) {
		t.Errorf("Content-Disposition = %q, want the file name %q", got, ticket.FileName)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(validClaim.HotspotPassword)) {
		t.Error("the backup must be encrypted")
	}
	// A ticket is used once.
	if status, _ := e.call(http.MethodGet, "/system/backup/"+ticket.Token, nil); status != http.StatusBadRequest {
		t.Errorf("second download = %d, want refused", status)
	}
}

func TestListenUnix_ServesTheRoutesOnlyToItsOwnerAndGroup(t *testing.T) {
	e := newClaimedEnv(t, false)
	socket := filepath.Join(shortTempDir(t), "api.sock")
	go func() { _ = e.agent.ListenUnix(socket, -1) }()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	var resp *http.Response
	var err error
	for range 50 {
		if resp, err = client.Get("http://rbd/state"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /state over the socket = %d", resp.StatusCode)
	}
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err) // reading info below would crash the whole test run
	}
	if info.Mode().Perm() != 0o660 {
		t.Errorf("socket mode = %v; want 0660 (rbd and rbweb's group only)", info.Mode().Perm())
	}
}

func TestRoutes_HotspotChannels(t *testing.T) {
	e := newClaimedEnv(t, false)
	status, body := e.call(http.MethodPost, "/settings/hotspot/channels", api.HotspotChannelsRequest{Country: "JP"})
	var got api.HotspotChannels
	if status != http.StatusOK || json.Unmarshal(body, &got) != nil || len(got.Channels["5GHz"]) != 4 {
		t.Errorf("channels for Japan = %d %s", status, body)
	}
	if status, body := e.call(http.MethodPost, "/settings/hotspot/channels", api.HotspotChannelsRequest{Country: "UK"}); status != http.StatusBadRequest {
		t.Errorf("UK = %d %s, want 400", status, body)
	}
}

// Every route is wired to its action: a simple request reaches it and gets
// an answer that isn't "no such route" (404) or a crash (500). What each
// action does is tested in its own file; this catches a route that was
// left out or points at the wrong action.
func TestRoutes_EachReachesItsAction(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.ImportVPNProfile(testWireGuardConf, "home.conf"); err != nil {
		t.Fatal(err)
	}
	mac := map[string]any{"mac": "3a:1f:22:aa:bb:cc"}
	profile := map[string]any{"name": "home.conf"}
	routes := []struct {
		route string
		body  any
	}{
		{"POST /verify-password", map[string]any{"password": "wrong"}},
		{"POST /remembered-logins", nil},
		{"POST /remembered-logins/check", map[string]any{"token": "x"}},
		{"POST /remembered-logins/forget", map[string]any{"token": "x"}},
		{"POST /clients/internet-access", map[string]any{"mac": "3a:1f:22:aa:bb:cc", "allowed": true}},
		{"POST /clients/name", map[string]any{"mac": "3a:1f:22:aa:bb:cc", "name": "Phone"}},
		{"POST /clients/disconnect", mac},
		{"POST /clients/flush-lease", mac},
		{"POST /uplink/disconnect", nil},
		{"POST /uplink/renew", nil},
		{"POST /uplink/ethernet/scan", nil},
		{"POST /speedtest", nil},
		{"POST /settings/vpn/import", map[string]any{"conf": testWireGuardConf, "name": "office.conf"}},
		{"POST /settings/vpn/profile/rename", map[string]any{"name": "office.conf", "newName": "work.conf"}},
		{"POST /settings/vpn/profile/activate", profile},
		{"POST /settings/vpn/profile/generate-key", profile},
		{"POST /settings/vpn/profile/remove", map[string]any{"name": "work.conf"}},
		{"POST /portal/start", nil},
		{"POST /portal/extend", nil},
		{"POST /portal/stop", nil},
		{"POST /portal/check", nil},
		{"POST /portal/browser/stop", nil},
		{"POST /plugins/upload", "not a plugin"},
		{"POST /plugins/cancel", map[string]any{"stagingId": "x"}},
		{"POST /system/restore/upload", "not a backup"},
		{"POST /system/update", "not an update"},
		{"POST /settings/updates/test", map[string]any{"url": ""}},
		{"POST /system/update/check", nil},
		{"POST /system/hostname", map[string]any{"hostname": "travel"}},
		{"POST /system/admin-password", map[string]any{"currentPassword": "wrong", "newPassword": "a new password"}},
	}
	for _, r := range routes {
		method, path, _ := strings.Cut(r.route, " ")
		status, answer := e.call(method, path, r.body)
		if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status >= 500 {
			t.Errorf("%s = %d %s", r.route, status, answer)
		}
		if len(answer) > 0 && !json.Valid(answer) {
			t.Errorf("%s answered something that isn't JSON: %s", r.route, answer)
		}
		// "invalid request" means the body was refused before the action
		// ran: the test sent a field the route doesn't know.
		if strings.Contains(string(answer), "invalid request") {
			t.Errorf("%s didn't reach its action: %s", r.route, answer)
		}
	}
}
