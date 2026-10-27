package agent

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/portal"
)

// The clock monitor's rounds (checkClock): saving the time floor, and
// setting the clock from web servers when NTP doesn't work on a network
// (hotel networks often block it).

// clockEnv is a set-up RasBeacon whose uplink has been connected for an
// hour, with no login page in the way and NTP not working, and web servers
// answering `servers`.
func clockEnv(t *testing.T, servers ...time.Time) (*testEnv, *clockWatch) {
	t.Helper()
	e := newClaimedEnv(t, false)
	e.agent.mu.Lock()
	e.agent.uplink.status.Connected = true
	e.agent.uplink.connectedSince = time.Now().Add(-time.Hour)
	e.agent.portal.result.State = portal.Open
	e.agent.mu.Unlock()
	return e, &clockWatch{fetch: func() []time.Time { return servers }}
}

// clockSets lists the clock changes rbd made ("date -u -s @…").
func clockSets(e *testEnv) []string {
	var sets []string
	for _, c := range e.runner.commands {
		if strings.HasPrefix(c, "date -u -s") {
			sets = append(sets, c)
		}
	}
	return sets
}

func TestClock_SetFromWebServersWhenNTPDoesntWork(t *testing.T) {
	now := time.Now()
	right := now.Add(3 * time.Hour) // the clock is three hours behind
	e, watch := clockEnv(t, right, right.Add(2*time.Second), right.Add(-time.Second))

	e.agent.checkClock(watch, now)
	sets := clockSets(e)
	if len(sets) != 1 {
		t.Fatalf("clock changes = %q, want one", sets)
	}
	// It is also saved, so the next boot starts from it.
	saved, _ := os.ReadFile(e.paths.ClockFile())
	if strings.TrimSpace(string(saved)) == "" {
		t.Error("the time floor should be saved")
	}

	// Not again within the hour.
	e.agent.checkClock(watch, now.Add(30*time.Minute))
	if len(clockSets(e)) != 1 {
		t.Errorf("the clock was set again within the hour: %q", clockSets(e))
	}
}

func TestClock_LeftAloneWhen(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		servers []time.Time
		setup   func(e *testEnv)
	}{
		{"NTP works", []time.Time{now.Add(time.Hour), now.Add(time.Hour)}, func(e *testEnv) {
			e.runner.outputs["timedatectl show --property=NTPSynchronized --value"] = "yes\n"
		}},
		{"there is no internet", []time.Time{now.Add(time.Hour), now.Add(time.Hour)}, func(e *testEnv) {
			e.agent.uplink.status.Connected = false
		}},
		{"the uplink only just connected (NTP gets a moment)", []time.Time{now.Add(time.Hour), now.Add(time.Hour)}, func(e *testEnv) {
			e.agent.uplink.connectedSince = time.Now()
		}},
		{"the servers don't agree (a login page answering, say)", []time.Time{now.Add(time.Hour), now.Add(-5 * time.Hour)}, nil},
		{"only one server answers", []time.Time{now.Add(time.Hour), {}}, nil},
		{"the clock is right to within 30 seconds", []time.Time{now.Add(10 * time.Second), now.Add(12 * time.Second)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, watch := clockEnv(t, tc.servers...)
			if tc.setup != nil {
				e.agent.mu.Lock()
				tc.setup(e)
				e.agent.mu.Unlock()
			}
			e.agent.checkClock(watch, now)
			if sets := clockSets(e); len(sets) != 0 {
				t.Errorf("clock changes = %q, want none", sets)
			}
		})
	}
}

func TestClock_SavesTheTimeFloorRegularly(t *testing.T) {
	e, watch := clockEnv(t)
	e.agent.mu.Lock()
	e.agent.uplink.status.Connected = false
	e.agent.mu.Unlock()
	now := time.Now()

	e.agent.checkClock(watch, now)
	first, err := os.ReadFile(e.paths.ClockFile())
	if err != nil {
		t.Fatal("the first round should save the time floor:", err)
	}
	_ = os.Remove(e.paths.ClockFile())
	e.agent.checkClock(watch, now.Add(5*time.Minute))
	if _, err := os.Stat(e.paths.ClockFile()); err == nil {
		t.Error("saved again before 15 minutes")
	}
	e.agent.checkClock(watch, now.Add(clockSaveInterval))
	if _, err := os.Stat(e.paths.ClockFile()); err != nil {
		t.Errorf("not saved after 15 minutes (first save was %q)", first)
	}
}

func TestClock_NeverSavesAnEarlierTime(t *testing.T) {
	e := newClaimedEnv(t, false)
	later := time.Now().Add(24 * time.Hour).Unix()
	mustWrite(t, e.paths.ClockFile(), strconv.FormatInt(later, 10)+"\n")
	e.agent.SaveClock()
	if got, _ := os.ReadFile(e.paths.ClockFile()); strings.TrimSpace(string(got)) != strconv.FormatInt(later, 10) {
		t.Errorf("clock file = %q, want the later time kept", got)
	}
}

// Behind a login page, the network answers in the servers' place, so the
// clock isn't set from what it says.
func TestClock_NotSetWhileALoginPageIsInTheWay(t *testing.T) {
	now := time.Now()
	future := now.Add(5 * 365 * 24 * time.Hour)
	e, watch := clockEnv(t, future, future, future)
	e.agent.mu.Lock()
	e.agent.portal.result.State = portal.Portal
	e.agent.mu.Unlock()

	e.agent.checkClock(watch, now)
	if sets := clockSets(e); len(sets) != 0 {
		t.Errorf("the clock was set behind a login page: %q", sets)
	}
}

// The saved time floor: a damaged file counts as none, an earlier time is
// never saved over a later one, and the clock only ever moves forward to
// it at start.
func TestTimeFloor(t *testing.T) {
	e := newClaimedEnv(t, false)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	e.agent.now = func() time.Time { return now }

	os.WriteFile(e.paths.ClockFile(), []byte("garbage\n"), 0o644)
	if !e.agent.timeFloor().IsZero() {
		t.Error("a damaged file means no saved time")
	}
	e.agent.SaveClock()
	if got := e.agent.timeFloor(); !got.Equal(now) {
		t.Errorf("saved %v, want %v", got, now)
	}

	// The clock went back (no battery, no network yet): nothing is saved,
	// and at start the clock moves forward to the saved time.
	earlier := now.Add(-48 * time.Hour)
	e.agent.now = func() time.Time { return earlier }
	e.agent.SaveClock()
	if got := e.agent.timeFloor(); !got.Equal(now) {
		t.Errorf("an earlier time was saved: %v", got)
	}
	e.runner.commands = nil
	e.agent.applyTimeFloor()
	if sets := clockSets(e); len(sets) != 1 || sets[0] != "date -u -s @"+strconv.FormatInt(now.Unix(), 10) {
		t.Errorf("clock changes: %v", sets)
	}

	// A clock already past the floor stays as it is.
	e.agent.now = func() time.Time { return now.Add(time.Hour) }
	e.runner.commands = nil
	e.agent.applyTimeFloor()
	if sets := clockSets(e); len(sets) != 0 {
		t.Errorf("the clock was moved back: %v", sets)
	}
	// A failure to set the clock is logged, not fatal.
	e.agent.now = func() time.Time { return earlier }
	e.runner.failing = append(e.runner.failing, "date")
	e.agent.applyTimeFloor()
}

// NTP reports itself through timedatectl.
func TestNTPSynchronized(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.runner.outputs["timedatectl show --property=NTPSynchronized --value"] = "yes\n"
	if !e.agent.ntpSynchronized() {
		t.Error("yes means synchronized")
	}
	e.runner.outputs["timedatectl show --property=NTPSynchronized --value"] = "no\n"
	if e.agent.ntpSynchronized() {
		t.Error("no means not synchronized")
	}
}
