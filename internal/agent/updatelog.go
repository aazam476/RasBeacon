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
	"sync"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/update"
)

// updateLogLines is the most lines the log keeps; older ones are dropped.
const updateLogLines = 60

// transferQuietAfter is how long the update file may stop arriving
// before the log says so. A variable so tests can make it short.
var transferQuietAfter = 15 * time.Second

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

// startUpdateLog empties the log for a new install. The caller holds a.mu.
func (a *Agent) startUpdateLog() {
	a.update.log = nil
}

// logUpdate adds a line to the log. It takes a.mu itself.
func (a *Agent) logUpdate(format string, args ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.addUpdateLogLine(api.UpdateLogLine{Text: fmt.Sprintf(format, args...)})
}

// logUpdateProgress shows how far a step is (0 to 100 percent). The step's
// line is changed in place, like a progress bar in a terminal, rather
// than added again for each percent. It takes a.mu itself.
func (a *Agent) logUpdateProgress(step string, percent int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	log := a.update.log
	if n := len(log); n > 0 && log[n-1].Text == step && log[n-1].Percent != nil {
		*log[n-1].Percent = percent
		return
	}
	a.addUpdateLogLine(api.UpdateLogLine{Text: step, Percent: &percent})
}

// addUpdateLogLine appends line, keeping at most updateLogLines. The
// caller holds a.mu.
func (a *Agent) addUpdateLogLine(line api.UpdateLogLine) {
	a.update.log = append(a.update.log, line)
	if extra := len(a.update.log) - updateLogLines; extra > 0 {
		a.update.log = a.update.log[extra:]
	}
}

// updateLogCopy is the log for the status, copied so it can't change
// while it is sent. The caller holds a.mu.
func (a *Agent) updateLogCopy() []api.UpdateLogLine {
	lines := make([]api.UpdateLogLine, len(a.update.log))
	for i, line := range a.update.log {
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
	// quiet fires when nothing arrived for transferQuietAfter.
	quiet *time.Timer

	mu      sync.Mutex // the timer's function runs on its own
	waiting bool       // the log said that nothing arrives
	broke   bool       // a read failed (not just the end of the file)
	ended   bool       // the end of the file was reached
}

// watchTransfer starts watching r, an update file arriving by what
// ("upload" or "download"). Call stop when the install is over.
func (a *Agent) watchTransfer(r io.Reader, what string) *transferWatch {
	w := &transferWatch{r: r, agent: a, what: what}
	w.quiet = time.AfterFunc(transferQuietAfter, w.wentQuiet)
	return w
}

// wentQuiet tells the log that nothing has arrived for a while.
func (w *transferWatch) wentQuiet() {
	w.mu.Lock()
	w.waiting = true
	w.mu.Unlock()
	w.agent.logUpdate("Nothing has arrived for %.0f seconds. Still waiting: the %s stops after 2 minutes without data.",
		transferQuietAfter.Seconds(), w.what)
}

// Read reads the file, noting data that arrives again after a pause, a
// broken connection, and the file's end.
func (w *transferWatch) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	w.mu.Lock()
	resumed := n > 0 && w.waiting
	if n > 0 {
		w.waiting = false
	}
	switch {
	case errors.Is(err, io.EOF):
		w.ended = true
	case err != nil:
		w.broke = true
	}
	w.mu.Unlock()
	if n > 0 {
		w.quiet.Reset(transferQuietAfter)
	}
	if resumed {
		w.agent.logUpdate("Data is arriving again")
	}
	return n, err
}

// stop stops watching.
func (w *transferWatch) stop() {
	w.quiet.Stop()
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
// its percentage.
type progressReader struct {
	pkg   *update.Package
	agent *Agent
	step  string
	shown int // the percentage the log shows
}

// Read reads the bundle and moves the step's progress bar on.
func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.pkg.Read(b)
	size := p.pkg.Manifest.BundleSize
	if percent := int((size - p.pkg.Remaining()) * 100 / size); percent != p.shown {
		p.shown = percent
		p.agent.logUpdateProgress(p.step, percent)
	}
	return n, err
}
