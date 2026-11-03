package agent

// A sample that can't read the radios' counters keeps the old ones, so the
// next sample's rates must be worked out over the time since those were
// read, not since the failed sample.

import (
	"math"
	"testing"
	"time"
)

func TestReadings_FailedStationReadDoesNotDoubleTheNextRate(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	sample := func() {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		e.agent.sampleReadings()
	}
	const dumpCommand = "iw dev ap0 station dump"
	dump := func(txBytes string) string {
		return "Station 3a:1f:22:aa:bb:cc (on ap0)\n\tsignal:\t-48 dBm\n\trx bytes:\t1000\n\ttx bytes:\t" + txBytes + "\n"
	}

	// First sample: the counters start at 0.
	e.runner.outputs[dumpCommand] = dump("0")
	sample()

	// Five seconds later the counters can't be read.
	c.advance(5 * time.Second)
	delete(e.runner.outputs, dumpCommand)
	e.runner.failing = []string{dumpCommand}
	sample()

	// Five seconds after that: 5 MB sent to the device in 10 seconds is
	// 4 Mbit/s (not 8, as if it had all come in the last 5 seconds).
	c.advance(5 * time.Second)
	e.runner.failing = nil
	e.runner.outputs[dumpCommand] = dump("5000000")
	sample()

	s := e.agent.Status()
	if len(s.Clients) != 1 {
		t.Fatalf("clients = %+v", s.Clients)
	}
	if got := math.Round(s.Clients[0].DownMbps); got != 4 {
		t.Errorf("down = %v Mbit/s, want 4", s.Clients[0].DownMbps)
	}
}
