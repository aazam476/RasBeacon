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
	want := step + " [182 of 404 MB]\nStill uploading and installing update: 182 of 404 MB\n" + step + " [186 of 404 MB]"
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

// The time left follows the speed, but only every timeLeftEvery, and
// smoothed: one slow moment doesn't make it jump.
func TestUpdateLog_TimeLeftFollowsTheSpeedCalmly(t *testing.T) {
	const total = 400_000_000
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	var left timeLeft
	if got := left.update(0, total, start); got != "" {
		t.Errorf("before any speed is known: %q", got)
	}
	// 1 MB/s for the first 10 s: 390 MB left at 1 MB/s is 6.5 min.
	if got := left.update(10_000_000, total, start.Add(10*time.Second)); got != "~7m left" {
		t.Errorf("after 10 s at 1 MB/s: %q", got)
	}
	// Faster for a moment: the estimate waits for the next 10 s.
	if got := left.update(30_000_000, total, start.Add(15*time.Second)); got != "~7m left" {
		t.Errorf("between measurements the estimate stays: %q", got)
	}
	// 3 MB/s over the next 10 s: blended with the earlier 1 MB/s (1.8 MB/s),
	// 360 MB left is about 3.3 min, not the 2 min 3 MB/s alone would say.
	if got := left.update(40_000_000, total, start.Add(20*time.Second)); got != "~3m left" {
		t.Errorf("after a faster stretch: %q", got)
	}
}

func TestUpdateLog_TimeLeftIsRoundedTheWayPeopleSayIt(t *testing.T) {
	for seconds, want := range map[float64]string{
		4:    "<10s left",
		37:   "~40s left",
		95:   "~2m left",
		4800: "~1h 20m left",
	} {
		if got := roundTimeLeft(seconds); got != want {
			t.Errorf("%v s: %q, want %q", seconds, got, want)
		}
	}
}
