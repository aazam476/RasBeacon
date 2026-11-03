package agent

// What the System page says when installing an update goes wrong in ways
// that are not the update file's fault.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdate_StalledDownloadBlamesTheConnectionNotTheFile(t *testing.T) {
	e, key := newUpdateEnv(t)
	file := updateFile(t, key, "pi4", testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64)))
	// GitHub sends half the file, then nothing more.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(file[:len(file)/2])
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	before := downloadStall
	downloadStall = 100 * time.Millisecond
	t.Cleanup(func() { downloadStall = before })

	_, err := e.agent.downloadUpdate(server.URL)
	if err == nil || !strings.Contains(err.Error(), "nothing arrived from GitHub") ||
		!strings.Contains(err.Error(), "Check the internet connection") {
		t.Errorf("error = %v, want one that says the download stalled", err)
	}
}

func TestUpdate_WholeDownloadStillInstalls(t *testing.T) {
	e, key := newUpdateEnv(t)
	file := updateFile(t, key, "pi4", testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(file)
	}))
	t.Cleanup(server.Close)

	version, err := e.agent.downloadUpdate(server.URL)
	if err != nil || version != "v9.9.9" {
		t.Errorf("downloadUpdate = %q, %v", version, err)
	}
}

func TestUpdate_UnsavedRestartNoteIsReportedNotLeftInstalling(t *testing.T) {
	e, _ := newUpdateEnv(t)
	// A non-empty directory where the note goes: it can't be written.
	pending := e.paths.UpdatePendingFile()
	if err := os.MkdirAll(filepath.Join(pending, "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	e.agent.update.installing = true
	e.agent.update.message = "Downloading and installing RasBeacon v9.9.9…"
	e.agent.mu.Unlock()

	if err := e.agent.finishInstall("v9.9.9", nil); err == nil {
		t.Fatal("finishInstall should fail when the note can't be saved")
	}
	s := e.agent.Status().Update
	if s.Installing || !strings.HasPrefix(s.Message, "The update was not installed: ") {
		t.Errorf("update status = installing %v, message %q", s.Installing, s.Message)
	}
}
