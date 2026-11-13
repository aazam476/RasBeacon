package agent

// The update log (updatelog.go): each step of an install in plain words,
// and an upload that stops or pauses said as such.

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// logTexts is the update log as the System page shows it.
func logTexts(e *testEnv) []string {
	var texts []string
	for _, line := range e.agent.Status().Update.Log {
		text := line.Text
		if line.Percent != nil {
			text += " " + strings.Repeat("#", *line.Percent/10)
		}
		texts = append(texts, text)
	}
	return texts
}

// goodUpdateFile is a valid update file for the test device.
func goodUpdateFile(t *testing.T, key ed25519.PrivateKey) []byte {
	return updateFile(t, key, "pi4", testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64)))
}

func TestUpdateLog_SaysEachStepOfAnInstall(t *testing.T) {
	e, key := newUpdateEnv(t)
	if err := e.agent.InstallUpdate(bytes.NewReader(goodUpdateFile(t, key))); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Receiving the update file",
		"Checking the update file's signature",
		"Signature is valid: this is RasBeacon v9.9.9",
		"Uploading and installing update ##########",
		"Verifying the update against its signature",
		"Verified the update",
		"Saving it to the SD card",
		"Installed RasBeacon v9.9.9",
		"Restarting to finish the install (RasBeacon is offline for about 2 minutes)",
	}
	if got := logTexts(e); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestUpdateLog_AnInvalidFileIsDiscarded(t *testing.T) {
	e, _ := newUpdateEnv(t)
	_, other, _ := ed25519.GenerateKey(nil)
	if err := e.agent.InstallUpdate(bytes.NewReader(goodUpdateFile(t, other))); err == nil {
		t.Fatal("a file signed by an unknown key must be refused")
	}
	log := strings.Join(logTexts(e), "\n")
	if !strings.Contains(log, "Update file is not valid: this update is not signed by a key this RasBeacon trusts") ||
		!strings.HasSuffix(log, "Discarded the update. The running RasBeacon was not changed.") {
		t.Errorf("log:\n%s", log)
	}
}

// brokenUpload sends part of a file, then fails the way a closed page or
// a dropped connection does.
type brokenUpload struct {
	data []byte
	err  error
}

// Read sends what is left of data, then err.
func (b *brokenUpload) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, b.err
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func TestUpdateLog_AnUploadThatStopsSaysSo(t *testing.T) {
	for name, end := range map[string]error{"connection dropped": io.ErrUnexpectedEOF, "file ended early": io.EOF} {
		t.Run(name, func(t *testing.T) {
			e, key := newUpdateEnv(t)
			file := goodUpdateFile(t, key)
			err := e.agent.InstallUpdate(&brokenUpload{data: file[:len(file)*3/4], err: end})
			if err == nil || !strings.Contains(err.Error(), "the upload stopped before the whole file arrived") {
				t.Fatalf("error = %v", err)
			}
			log := strings.Join(logTexts(e), "\n")
			if !strings.Contains(log, "Upload stopped before the whole file arrived\nDiscarded the update. The running RasBeacon was not changed.") {
				t.Errorf("log:\n%s", log)
			}
			if s := e.agent.Status().Update; !s.Failed || s.Installing {
				t.Errorf("failed %v, installing %v", s.Failed, s.Installing)
			}
		})
	}
}

// pausingUpload sends a file in two halves with a pause between them.
type pausingUpload struct {
	first, second []byte
	pause         time.Duration
	paused        bool
}

// Read sends the first half, waits, then sends the second.
func (u *pausingUpload) Read(p []byte) (int, error) {
	if len(u.first) > 0 {
		n := copy(p, u.first)
		u.first = u.first[n:]
		return n, nil
	}
	if !u.paused {
		u.paused = true
		time.Sleep(u.pause)
	}
	if len(u.second) == 0 {
		return 0, io.EOF
	}
	n := copy(p, u.second)
	u.second = u.second[n:]
	return n, nil
}

func TestUpdateLog_SaysWhenTheUploadPausesAndComesBack(t *testing.T) {
	e, key := newUpdateEnv(t)
	before := transferQuietAfter
	transferQuietAfter = 20 * time.Millisecond
	t.Cleanup(func() { transferQuietAfter = before })
	file := goodUpdateFile(t, key)
	half := len(file) / 2
	upload := &pausingUpload{first: file[:half], second: file[half:], pause: 200 * time.Millisecond}
	if err := e.agent.InstallUpdate(upload); err != nil {
		t.Fatal(err)
	}
	log := strings.Join(logTexts(e), "\n")
	if !strings.Contains(log, "Nothing has arrived for 0 seconds. Still waiting: the upload stops after 2 minutes without data.") ||
		!strings.Contains(log, "Data is arriving again") || !strings.Contains(log, "Installed RasBeacon v9.9.9") {
		t.Errorf("log:\n%s", log)
	}
}

// An upload that stops is not the file's fault, and a refused file isn't
// a stopped upload.
func TestUpdateLog_TellsAStoppedTransferFromAnInvalidFile(t *testing.T) {
	cut := errTransferCut{"download"}
	if !strings.Contains(cut.Error(), "download from GitHub stopped") {
		t.Errorf("download: %q", cut.Error())
	}
	var invalid errInvalidUpdate
	if errors.As(error(cut), &invalid) {
		t.Error("a stopped download is not an invalid file")
	}
}
