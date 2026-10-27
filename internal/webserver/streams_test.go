package webserver

import (
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"rasbeacon/internal/api"
)

// Requests rbweb passes to rbd in their own way: uploads streamed as they
// arrive, the backup download, slow requests, and the factory reset that
// logs everyone out.

// recordingRBD answers like fakeRBD and also keeps each forwarded request's
// method, path and body. answers replaces the answer of "METHOD /path".
type recordingRBD struct {
	fakeRBD
	got     []string
	answers map[string]func(w http.ResponseWriter)
}

func (f *recordingRBD) handler() http.Handler {
	base := f.fakeRBD.handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			r.Body = http.NoBody // requests without one, called in memory
		}
		route := r.Method + " " + r.URL.Path
		if answer, ok := f.answers[route]; ok {
			body, _ := io.ReadAll(r.Body)
			f.got = append(f.got, route+" "+string(body))
			answer(w)
			return
		}
		if r.URL.Path != "/state" && !strings.HasPrefix(r.URL.Path, "/verify-password") {
			body, _ := io.ReadAll(r.Body)
			f.got = append(f.got, route+" "+string(body))
			writeJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
			return
		}
		base.ServeHTTP(w, r)
	})
}

func newRecordingServer(answers map[string]func(w http.ResponseWriter)) (*Server, *recordingRBD) {
	rbd := &recordingRBD{
		fakeRBD: fakeRBD{mode: api.ModeNormal, adminPassword: "tangerine-orbit-42", remembered: map[string]bool{}},
		answers: answers,
	}
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>RasBeacon UI</html>")}}
	return New(NewRBDClientForHandler(rbd.handler()), ui), rbd
}

// loggedIn sends a request with the session's cookie and CSRF token.
func loggedIn(s *Server, cookies []*http.Cookie, csrf, method, url, body string) *http.Response {
	headers := map[string]string{"X-CSRF-Token": csrf, "Content-Type": "application/json"}
	return request(s, method, url, body, headers, cookies).Result()
}

func TestUploads_AreStreamedToRBD(t *testing.T) {
	for _, path := range []string{"/system/update", "/plugins/upload", "/system/restore/upload"} {
		s, rbd := newRecordingServer(nil)
		cookies, csrf := login(t, s)
		resp := loggedIn(s, cookies, csrf, "POST", "http://10.87.0.1/api/v1"+path, "FILE-BYTES")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("POST %s = %d", path, resp.StatusCode)
		}
		if want := "POST " + path + " FILE-BYTES"; len(rbd.got) != 1 || rbd.got[0] != want {
			t.Errorf("POST %s reached rbd as %q, want %q", path, rbd.got, want)
		}
	}
}

func TestUploads_NeedALogin(t *testing.T) {
	s, rbd := newRecordingServer(nil)
	for _, path := range []string{"/system/update", "/plugins/upload", "/system/restore/upload"} {
		rec := request(s, "POST", "http://10.87.0.1/api/v1"+path, "FILE", nil, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without a login = %d, want 401", path, rec.Code)
		}
	}
	if len(rbd.got) != 0 {
		t.Errorf("rbd got %q", rbd.got)
	}
}

func TestBackupDownload(t *testing.T) {
	token := strings.Repeat("ab", 16)
	s, _ := newRecordingServer(map[string]func(w http.ResponseWriter){
		"GET /system/backup/" + token: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="rasbeacon.rbbackup"`)
			w.Header().Set("X-Internal", "not passed on")
			_, _ = w.Write([]byte("BACKUP"))
		},
	})
	cookies, csrf := login(t, s)

	resp := loggedIn(s, cookies, csrf, "GET", "http://10.87.0.1/api/v1/system/backup/"+token, "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "BACKUP" {
		t.Fatalf("download = %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="rasbeacon.rbbackup"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Internal") != "" {
		t.Errorf("headers = %v", resp.Header)
	}

	// Only a ticket's 32 hex digits are passed on.
	for _, bad := range []string{"short", strings.Repeat("AB", 16), "..%2F..%2Fstate"} {
		resp := loggedIn(s, cookies, csrf, "GET", "http://10.87.0.1/api/v1/system/backup/"+bad, "")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("token %q = %d, want 404", bad, resp.StatusCode)
		}
	}
}

func TestSlowRequests_AreForwardedWithTheirMethod(t *testing.T) {
	s, rbd := newRecordingServer(nil)
	cookies, csrf := login(t, s)
	for _, route := range []string{"POST /plugins/catalog", "PUT /plugins/settings", "POST /system/restore"} {
		method, path, _ := strings.Cut(route, " ")
		rbd.got = nil
		resp := loggedIn(s, cookies, csrf, method, "http://10.87.0.1/api/v1"+path, `{"id":"x"}`)
		if resp.StatusCode != http.StatusOK || len(rbd.got) != 1 || rbd.got[0] != route+` {"id":"x"}` {
			t.Errorf("%s = %d, rbd got %q", route, resp.StatusCode, rbd.got)
		}
	}
}

func TestFactoryReset_LogsEveryoneOutOnlyWhenAccepted(t *testing.T) {
	refused := func(w http.ResponseWriter) {
		writeJSON(w, http.StatusForbidden, api.ErrorResponse{Error: "Wrong password.", Field: "password"})
	}
	s, _ := newRecordingServer(map[string]func(w http.ResponseWriter){"POST /system/factory-reset": refused})
	cookies, csrf := login(t, s)
	if resp := loggedIn(s, cookies, csrf, "POST", "http://10.87.0.1/api/v1/system/factory-reset", `{"password":"wrong"}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("refused reset = %d", resp.StatusCode)
	}
	if resp := loggedIn(s, cookies, csrf, "GET", "http://10.87.0.1/api/v1/status", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("after a refused reset, the session should stay: %d", resp.StatusCode)
	}

	accepted := func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }
	s, _ = newRecordingServer(map[string]func(w http.ResponseWriter){"POST /system/factory-reset": accepted})
	cookies, csrf = login(t, s)
	other, _ := login(t, s)
	if resp := loggedIn(s, cookies, csrf, "POST", "http://10.87.0.1/api/v1/system/factory-reset", `{"password":"tangerine-orbit-42"}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("accepted reset = %d", resp.StatusCode)
	}
	for name, session := range map[string][]*http.Cookie{"this browser": cookies, "another browser": other} {
		if resp := loggedIn(s, session, csrf, "GET", "http://10.87.0.1/api/v1/status", ""); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s after the reset = %d, want 401", name, resp.StatusCode)
		}
	}
}

func TestRBDUnreachable_FailsClosed(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	// Without rbd, rbweb can't tell which network a request came from, so
	// it serves nothing rather than guess.
	s := New(NewRBDClient(filepath.Join(t.TempDir(), "missing.sock")), ui)
	for _, path := range []string{"/api/v1/setup", "/api/v1/status", "/"} {
		if rec := request(s, "GET", "http://10.87.0.1"+path, "", nil, nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s without rbd = %d %s, want 403", path, rec.Code, rec.Body)
		}
	}
}

func TestRBDClient_TalksOverTheUnixSocket(t *testing.T) {
	// A short folder: macOS limits socket paths to 104 characters, and
	// t.TempDir() is often longer there.
	dir, err := os.MkdirTemp("/tmp", "rb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "rbd.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skip("no Unix sockets here:", err)
	}
	defer listener.Close()
	go func() {
		_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			writeJSON(w, http.StatusOK, map[string]string{"path": r.URL.Path, "body": string(body)})
		}))
	}()

	client := NewRBDClient(socket)
	status, body, err := client.Do(http.MethodPost, "/claim", []byte(`{}`))
	if err != nil || status != http.StatusOK || !strings.Contains(string(body), `"path":"/claim"`) {
		t.Errorf("Do = %d %s %v", status, body, err)
	}
	status, _, err = client.DoSlow(http.MethodPost, "/speedtest", nil)
	if err != nil || status != http.StatusOK {
		t.Errorf("DoSlow = %d %v", status, err)
	}
	resp, err := client.Open(http.MethodGet, "/system/backup/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}
