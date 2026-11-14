package agent

// This file keeps the update log: what RasBeacon is doing while it
// installs an update, in plain words, for the log box on the System page
// (Status.update.log). It also watches the update file as it arrives (an
// upload, or a download from GitHub), so the log says when data stops
// coming, and a file that is cut off fails with that reason instead of a
// confusing one about the file's contents.

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/update"
)

// updateLogLines is the most lines the log keeps; older ones are dropped.
const updateLogLines = 60

// updateLogEvery is the longest the log goes without a change while an
// update installs: a step that takes longer gets a "Still …" line, so
// the owner sees it hasn't stalled. A variable so tests can make it short.
var updateLogEvery = 5 * time.Second

// transferQuietAfter is how long the update file may stop arriving
// before the log says so (and says so again each time after). A variable
// so tests can make it short.
var transferQuietAfter = 5 * time.Second

// errTransferCut means the update file stopped arriving before all of it
// was there; the message says whether it was an upload or a download.
type errTransferCut struct{ what string }

// Error says what happened and what to do, for the owner.
func (e errTransferCut) Error() string {
	if e.what == "download" {
		return "the download from GitHub stopped before the whole file arrived (the connection dropped). Try again"
	}
	return "the upload stopped before the whole file arrived (the page was closed or reloaded, or the " +
		"connection dropped). Install the file again"
}

// errInvalidUpdate marks a refusal because of the update file itself (its
// signature, board, version or checksum), so the log can say the file is
// not valid. Its message is the refusal's own.
type errInvalidUpdate struct{ err error }

// Error is the refusal's message.
func (e errInvalidUpdate) Error() string { return e.err.Error() }

// Unwrap lets errors.As find the refusal (update.NeedsReflashError).
func (e errInvalidUpdate) Unwrap() error { return e.err }

// updateLogState is the log and what keeps it moving (updateState.log).
type updateLogState struct {
	lines []api.UpdateLogLine
	// changed is when the log last changed; step is the step it shows
	// now (its last line that isn't a "Still …" one), started when.
	changed     time.Time
	step        string
	stepStarted time.Time
	// transfer is the update file arriving, while it does.
	transfer *transferWatch
	// every and quietAfter are updateLogEvery and transferQuietAfter as
	// the install started; run tells this install's checks from an
	// earlier one's.
	every, quietAfter time.Duration
	run               int
}

// startUpdateLog empties the log for a new install, and starts checking
// every second that it changes at least every updateLogEvery. The caller
// holds a.mu.
func (a *Agent) startUpdateLog() {
	run := a.update.log.run + 1
	a.update.log = updateLogState{changed: time.Now(), every: updateLogEvery, quietAfter: transferQuietAfter, run: run}
	time.AfterFunc(updateLogEvery/5, func() { a.keepUpdateLogMoving(run) })
}

// keepUpdateLogMoving adds a "Still …" line when the log hasn't changed
// for updateLogEvery, then looks again a moment later, as long as the
// install goes on. A file that stopped arriving is left to its own
// "Nothing has arrived" lines (transferWatch). run is the install it
// watches.
func (a *Agent) keepUpdateLogMoving(run int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	log := &a.update.log
	if !a.update.installing || log.run != run {
		return
	}
	waiting := log.transfer != nil && log.transfer.quietFor() >= log.quietAfter/2
	if !waiting && log.step != "" && time.Since(log.changed) >= log.every {
		a.addUpdateLogLine(api.UpdateLogLine{Text: a.stillLine()})
	}
	time.AfterFunc(log.every/5, func() { a.keepUpdateLogMoving(run) })
}

// stillLine says the current step goes on: "Still saving it to the SD
// card (10 s so far)", or for a progress bar how far it got ("Still
// uploading and installing update: 182 of 404 MB | ~3m left").
// The caller holds a.mu.
func (a *Agent) stillLine() string {
	log := &a.update.log
	doing := "Still " + strings.ToLower(log.step[:1]) + log.step[1:]
	if n := len(log.lines); n > 0 && log.lines[n-1].Percent != nil && log.lines[n-1].Detail != "" {
		return doing + ": " + log.lines[n-1].Detail
	}
	return fmt.Sprintf("%s (%s so far)", doing, time.Since(log.stepStarted).Round(time.Second))
}

// logUpdate adds a line to the log. It takes a.mu itself.
func (a *Agent) logUpdate(format string, args ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.addUpdateStep(api.UpdateLogLine{Text: fmt.Sprintf(format, args...)})
}

// logUpdateProgress shows how far a step is: percent (0 to 100) and
// detail ("182 of 404 MB"). The step's line is changed in place, like a
// progress bar in a terminal, rather than added again each time; after a
// "Still …" line it starts a new one. It takes a.mu itself.
func (a *Agent) logUpdateProgress(step string, percent int, detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	lines := a.update.log.lines
	if n := len(lines); n > 0 && lines[n-1].Text == step && lines[n-1].Percent != nil {
		*lines[n-1].Percent = percent
		lines[n-1].Detail = detail
		a.update.log.changed = time.Now()
		return
	}
	if a.update.log.step == step {
		// The same step goes on after a "Still …" line: its time goes on too.
		a.addUpdateLogLine(api.UpdateLogLine{Text: step, Percent: &percent, Detail: detail})
		return
	}
	a.addUpdateStep(api.UpdateLogLine{Text: step, Percent: &percent, Detail: detail})
}

// addUpdateStep appends the line of a new step. The caller holds a.mu.
func (a *Agent) addUpdateStep(line api.UpdateLogLine) {
	a.update.log.step = line.Text
	a.update.log.stepStarted = time.Now()
	a.addUpdateLogLine(line)
}

// addUpdateLogLine appends line, keeping at most updateLogLines. The
// caller holds a.mu.
func (a *Agent) addUpdateLogLine(line api.UpdateLogLine) {
	log := &a.update.log
	log.lines = append(log.lines, line)
	if extra := len(log.lines) - updateLogLines; extra > 0 {
		log.lines = log.lines[extra:]
	}
	log.changed = time.Now()
}

// updateLogCopy is the log for the status, copied so it can't change
// while it is sent. The caller holds a.mu.
func (a *Agent) updateLogCopy() []api.UpdateLogLine {
	lines := make([]api.UpdateLogLine, len(a.update.log.lines))
	for i, line := range a.update.log.lines {
		lines[i] = line
		if line.Percent != nil {
			percent := *line.Percent
			lines[i].Percent = &percent
		}
	}
	return lines
}

// logUpdateResult ends the log of an install that failed: why, in the
// owner's words, and that nothing was changed. It takes a.mu itself.
func (a *Agent) logUpdateResult(err error) {
	var cut errTransferCut
	var invalid errInvalidUpdate
	switch {
	case errors.As(err, &cut) && cut.what == "download":
		a.logUpdate("Download stopped before the whole file arrived")
	case errors.As(err, &cut):
		a.logUpdate("Upload stopped before the whole file arrived")
	case errors.As(err, &invalid):
		a.logUpdate("Update file is not valid: %v", err)
	default:
		a.logUpdate("The update failed: %v", err)
	}
	a.logUpdate("Discarded the update. The running RasBeacon was not changed.")
}

// transferWatch reads the update file as it arrives. When nothing comes
// for transferQuietAfter, the log says so (and again when data comes
// back); and it remembers whether the file was cut off: the connection
// broke (a read error), or it ended before writeUpdate had all of it.
type transferWatch struct {
	r     io.Reader
	agent *Agent
	// what is "upload" or "download", for the messages.
	what string
	// quiet fires when nothing arrived for quietAfter (transferQuietAfter
	// when the transfer started).
	quiet      *time.Timer
	quietAfter time.Duration

	mu       sync.Mutex // the timer's function runs on its own
	lastData time.Time  // when data last arrived
	waiting  bool       // the log said that nothing arrives
	stopped  bool       // the install is over: no more lines
	broke    bool       // a read failed (not just the end of the file)
	ended    bool       // the end of the file was reached
}

// watchTransfer starts watching r, an update file arriving by what
// ("upload" or "download"). Call stop when the install is over.
func (a *Agent) watchTransfer(r io.Reader, what string) *transferWatch {
	w := &transferWatch{r: r, agent: a, what: what, lastData: time.Now(), quietAfter: transferQuietAfter}
	w.quiet = time.AfterFunc(w.quietAfter, w.wentQuiet)
	a.mu.Lock()
	a.update.log.transfer = w
	a.mu.Unlock()
	return w
}

// wentQuiet tells the log that nothing has arrived for a while, and looks
// again after transferQuietAfter more.
func (w *transferWatch) wentQuiet() {
	quiet := w.quietFor()
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.waiting = true
	w.mu.Unlock()
	w.agent.mu.Lock()
	w.agent.addUpdateLogLine(api.UpdateLogLine{Text: fmt.Sprintf(
		"Nothing has arrived for %.0f seconds. Still waiting: the %s stops after 2 minutes without data.",
		quiet.Seconds(), w.what)})
	w.agent.mu.Unlock()
	w.quiet.Reset(w.quietAfter)
}

// quietFor is how long it has been since data last arrived.
func (w *transferWatch) quietFor() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return time.Since(w.lastData)
}

// Read reads the file, noting data that arrives again after a pause, a
// broken connection, and the file's end.
func (w *transferWatch) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	w.mu.Lock()
	resumed := n > 0 && w.waiting
	if n > 0 {
		w.waiting = false
		w.lastData = time.Now()
	}
	switch {
	case errors.Is(err, io.EOF):
		w.ended = true
	case err != nil:
		w.broke = true
	}
	w.mu.Unlock()
	if n > 0 {
		w.quiet.Reset(w.quietAfter)
	}
	if resumed {
		w.agent.mu.Lock()
		w.agent.addUpdateLogLine(api.UpdateLogLine{Text: "Data is arriving again"})
		w.agent.mu.Unlock()
	}
	return n, err
}

// stop stops watching.
func (w *transferWatch) stop() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
	w.quiet.Stop()
	w.agent.mu.Lock()
	if w.agent.update.log.transfer == w {
		w.agent.update.log.transfer = nil
	}
	w.agent.mu.Unlock()
}

// cutOff reports whether the file stopped arriving before all of it was
// there: the connection broke, or the file ended while incomplete
// (missing is how many bytes writeUpdate still expected).
func (w *transferWatch) cutOff(missing int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.broke || (w.ended && missing > 0)
}

// progressReader reads an update's bundle (the compressed partitions) and
// shows in the log how much of it has been read and written: step, with
// its percentage, megabytes and the time left ("182 of 404 MB | ~3m
// left"). The line changes with each
// megabyte, so even a slow upload shows it is moving.
type progressReader struct {
	pkg   *update.Package
	agent *Agent
	step  string
	left  timeLeft
	// What the log shows: the percentage, and the megabytes, e.g. "182 of
	// 404 MB".
	shownPercent int
	shownDetail  string
}

// Read reads the bundle and moves the step's progress bar on.
func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.pkg.Read(b)
	size := p.pkg.Manifest.BundleSize
	done := size - p.pkg.Remaining()
	percent, detail := int(done*100/size), megabytes(done, size)
	if left := p.left.update(done, size, time.Now()); left != "" && done < size {
		detail += " | " + left
	}
	if percent != p.shownPercent || detail != p.shownDetail {
		p.shownPercent, p.shownDetail = percent, detail
		p.agent.logUpdateProgress(p.step, percent, detail)
	}
	return n, err
}

// megabytes is how much of total is done, in MB: "182 of 404 MB", with a
// decimal for small files ("0.4 of 2.5 MB").
func megabytes(done, total int64) string {
	if total >= 100e6 {
		return fmt.Sprintf("%d of %d MB", done/1e6, total/1e6)
	}
	return fmt.Sprintf("%.1f of %.1f MB", float64(done)/1e6, float64(total)/1e6)
}

// timeLeftEvery is how often the time left is worked out again: often
// enough to follow the connection's speed as it changes, not so often
// that the estimate jumps around and looks unreliable.
const timeLeftEvery = 10 * time.Second

// timeLeft estimates how long a transfer still takes from its recent
// speed. Every timeLeftEvery it measures the speed over that time and
// blends it with the earlier speed, so one slow or fast moment moves the
// estimate only a little.
type timeLeft struct {
	windowStart time.Time // when the current measurement began
	windowDone  int64     // the bytes done then
	speed       float64   // bytes per second, smoothed; 0 until measured
	text        string    // what the log shows, e.g. "~3m left"
}

// update notes that done of total bytes are there at now, and returns the
// time left to show ("" until the first measurement is over).
func (t *timeLeft) update(done, total int64, now time.Time) string {
	if t.windowStart.IsZero() {
		t.windowStart, t.windowDone = now, done
		return ""
	}
	elapsed := now.Sub(t.windowStart)
	if elapsed < timeLeftEvery {
		return t.text
	}
	measured := float64(done-t.windowDone) / elapsed.Seconds()
	if t.speed == 0 {
		t.speed = measured
	} else {
		// The newest measurement counts for 40%, the earlier speed for 60%.
		t.speed = 0.6*t.speed + 0.4*measured
	}
	t.windowStart, t.windowDone = now, done
	if t.speed > 0 {
		t.text = roundTimeLeft(float64(total-done) / t.speed)
	}
	return t.text
}

// roundTimeLeft says a number of seconds briefly, for the end of a
// progress line: "<10s left", "~40s left", "~3m left", "~1h 20m left" (~
// is "about"). Coarse on purpose: an estimate shouldn't look more exact
// than it is.
func roundTimeLeft(seconds float64) string {
	switch {
	case seconds < 10:
		return "<10s left"
	case seconds < 60:
		return fmt.Sprintf("~%ds left", int(math.Round(seconds/10))*10)
	case seconds < 3600:
		return fmt.Sprintf("~%dm left", max(1, int(math.Round(seconds/60))))
	}
	minutes := int(math.Round(seconds / 60))
	return fmt.Sprintf("~%dh %dm left", minutes/60, minutes%60)
}
