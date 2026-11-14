package agent

// The update log never stands still for more than updateLogEvery while an
// update installs: a long step gets "Still …" lines, a progress bar moves
// with each megabyte, and a stalled upload says so again and again.

import (
	"strings"
	"testing"
	"time"
)

// fastUpdateLog makes the log's pace short for a test, and starts an
// install's log as beginInstall does.
func fastUpdateLog(t *testing.T, e *testEnv, every time.Duration) {
	t.Helper()
	beforeEvery, beforeQuiet := updateLogEvery, transferQuietAfter
	updateLogEvery, transferQuietAfter = every, every
	e.agent.mu.Lock()
	e.agent.update.installing = true
	e.agent.startUpdateLog()
	e.agent.mu.Unlock()
	t.Cleanup(func() {
		e.agent.mu.Lock()
		e.agent.update.installing = false
		e.agent.mu.Unlock()
		updateLogEvery, transferQuietAfter = beforeEvery, beforeQuiet
	})
}

// logLines is the log's text, progress details included.
func logLines(e *testEnv) string {
	var lines []string
	for _, line := range e.agent.Status().Update.Log {
		text := line.Text
		if line.Detail != "" {
			text += " [" + line.Detail + "]"
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func TestUpdateLog_ALongStepGetsStillLines(t *testing.T) {
	e, _ := newUpdateEnv(t)
	fastUpdateLog(t, e, 50*time.Millisecond)
	e.agent.logUpdate("Saving it to the SD card")
	time.Sleep(180 * time.Millisecond)
	log := logLines(e)
	if strings.Count(log, "Still saving it to the SD card (") < 2 {
		t.Errorf("a step that goes on should get a line each time the log would stand still:\n%s", log)
	}
}

func TestUpdateLog_ASlowProgressBarIsSplitIntoNewLines(t *testing.T) {
	e, _ := newUpdateEnv(t)
	fastUpdateLog(t, e, 50*time.Millisecond)
	const step = "Uploading and installing update"
	e.agent.logUpdateProgress(step, 45, "182 of 404 MB")
	time.Sleep(80 * time.Millisecond)
	e.agent.logUpdateProgress(step, 46, "186 of 404 MB")
	log := logLines(e)
	want := step + " [182 of 404 MB]\nStill uploading and installing update: 182 of 404 MB so far\n" + step + " [186 of 404 MB]"
	if !strings.Contains(log, want) {
		t.Errorf("log:\n%s\nwant it to contain:\n%s", log, want)
	}
}

func TestUpdateLog_AStalledUploadIsSaidAgainAndAgain(t *testing.T) {
	e, key := newUpdateEnv(t)
	beforeEvery, beforeQuiet := updateLogEvery, transferQuietAfter
	updateLogEvery, transferQuietAfter = 40*time.Millisecond, 40*time.Millisecond
	t.Cleanup(func() { updateLogEvery, transferQuietAfter = beforeEvery, beforeQuiet })
	file := goodUpdateFile(t, key)
	half := len(file) / 2
	if err := e.agent.InstallUpdate(&pausingUpload{first: file[:half], second: file[half:], pause: 300 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	log := logLines(e)
	if strings.Count(log, "Nothing has arrived for") < 3 {
		t.Errorf("a stall should be said every time the log would stand still:\n%s", log)
	}
	if strings.Contains(log, "Still uploading") {
		t.Errorf("while nothing arrives, only the stall is said:\n%s", log)
	}
}

func TestUpdateLog_MegabytesAreShortAndReadable(t *testing.T) {
	for _, c := range []struct {
		done, total int64
		want        string
	}{
		{182_400_000, 404_000_000, "182 of 404 MB"},
		{400_000, 2_500_000, "0.4 of 2.5 MB"},
	} {
		if got := megabytes(c.done, c.total); got != c.want {
			t.Errorf("megabytes(%d, %d) = %q, want %q", c.done, c.total, got, c.want)
		}
	}
}
