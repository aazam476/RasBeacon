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
// setting the clock from web servers.

// clockEnv is a set-up RasBeacon whose uplink has been connected for an
// hour, with no login page in the way, and web servers answering
// `servers`.
func clockEnv(t *testing.T, servers ...time.Time) (*testEnv, *clockWatch) {
	t.Helper()
	e := newClaimedEnv(t, false)
	e.agent.mu.Lock()
	e.agent.uplink.status.Connected = true
	e.agent.uplink.connectedSince = time.Now().Add(-time.Hour)
	e.agent.portal.result.State = portal.Open
	e.agent.portal.probedUplinkSince = e.agent.uplink.connectedSince
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

func TestClock_SetFromWebServers(t *testing.T) {
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

// Servers that don't agree are asked again after a minute, not at every
// round; once they agree, after an hour.
func TestClock_AsksAgainAfterAMinuteThenAnHour(t *testing.T) {
	now := time.Now()
	e, watch := clockEnv(t)
	asked := 0
	answers := []time.Time{now, now.Add(time.Hour)} // they don't agree
	watch.fetch = func() []time.Time { asked++; return answers }
	for _, after := range []time.Duration{0, 10 * time.Second, 50 * time.Second} {
		e.agent.checkClock(watch, now.Add(after))
	}
	if asked != 1 {
		t.Errorf("asked %d times within a minute, want once", asked)
	}
	answers = []time.Time{now, now.Add(time.Second)}
	for _, after := range []time.Duration{time.Minute, 2 * time.Minute, 30 * time.Minute, time.Hour} {
		e.agent.checkClock(watch, now.Add(after))
	}
	if asked != 2 {
		t.Errorf("asked %d times, want once more after a minute and not again within the hour", asked)
	}
	e.agent.checkClock(watch, now.Add(time.Hour+time.Minute))
	if asked != 3 {
		t.Errorf("asked %d times, want again after an hour", asked)
	}
}

func TestClock_LeftAloneWhen(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		servers []time.Time
		setup   func(e *testEnv)
	}{
		{"there is no internet", []time.Time{now.Add(time.Hour), now.Add(time.Hour)}, func(e *testEnv) {
			e.agent.uplink.status.Connected = false
		}},
		{"the uplink only just connected (it hasn't been checked for a login page yet)", []time.Time{now.Add(time.Hour), now.Add(time.Hour)}, func(e *testEnv) {
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

// When web servers can't set the clock, the Status page says so; once
// they can, the warning goes.
func TestClock_WarnsWhenTheClockCantBeSet(t *testing.T) {
	now := time.Now()
	// Web servers that don't agree on the time.
	e, watch := clockEnv(t, now.Add(time.Hour), now.Add(-5*time.Hour))
	warned := func() bool {
		return strings.Contains(strings.Join(e.agent.Status().Warnings, " "), "couldn't set its clock")
	}
	if warned() {
		t.Fatal("no warning before the clock was checked")
	}
	e.agent.checkClock(watch, now)
	if !warned() {
		t.Errorf("warnings = %q, want the clock warning", e.agent.Status().Warnings)
	}
	watch.fetch = func() []time.Time { return []time.Time{now.Add(time.Minute), now.Add(time.Minute)} }
	e.agent.checkClock(watch, now.Add(time.Minute))
	if warned() {
		t.Error("once the servers agree, the warning goes")
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

// The time comes from web servers over HTTPS only. A network names its own
// time server, and an NTP answer can't be checked: RasBeacon doesn't ask
// one, whatever the system would do.
func TestClock_TakesNoTimeFromNTP(t *testing.T) {
	now := time.Now()
	e, watch := clockEnv(t, now, now)
	e.agent.stopNTP()
	if !e.runner.ran("systemctl stop systemd-timesyncd.service") {
		t.Error("the system's NTP client should be stopped")
	}
	// Even an NTP client that says it has set the clock changes nothing:
	// it isn't asked, and the servers are.
	e.runner.outputs["timedatectl show --property=NTPSynchronized --value"] = "yes\n"
	asked := 0
	fetch := watch.fetch
	watch.fetch = func() []time.Time { asked++; return fetch() }
	e.agent.checkClock(watch, now)
	if asked != 1 || e.runner.ran("timedatectl") {
		t.Errorf("web servers asked %d times, timedatectl run: %v; want the servers only", asked, e.runner.ran("timedatectl"))
	}
	// No NTP client to stop is fine.
	e.runner.failing = append(e.runner.failing, "systemctl stop")
	e.agent.stopNTP()
}

// A clock that is ahead is put back, and the saved time with it: one that
// came from a network's time server (in an older version) or from a
// battery-backed clock gone wrong would otherwise be back at every boot,
// with secure connections and the VPN failing each time.
func TestClock_AClockThatIsAheadIsPutBack(t *testing.T) {
	right := time.Now()
	ahead := right.Add(5 * 365 * 24 * time.Hour)
	e, watch := clockEnv(t, right, right.Add(time.Second))
	mustWrite(t, e.paths.ClockFile(), strconv.FormatInt(ahead.Unix(), 10)+"\n")

	e.agent.checkClock(watch, ahead) // the clock reads five years ahead
	want := right.Add(time.Second).Unix()
	if sets := clockSets(e); len(sets) != 1 || sets[0] != "date -u -s @"+strconv.FormatInt(want, 10) {
		t.Errorf("clock changes: %v, want it set to the servers' time", sets)
	}
	if got := e.agent.timeFloor(); got.Unix() != want {
		t.Errorf("saved time = %v, want the servers' %v", got, time.Unix(want, 0))
	}
	// Without the servers, the saved time still never goes back.
	e.agent.SaveClock()
	e.agent.now = func() time.Time { return right.Add(-time.Hour) }
	e.agent.SaveClock()
	if got := e.agent.timeFloor(); got.Before(time.Unix(want, 0)) {
		t.Errorf("an earlier time was saved: %v", got)
	}
}
