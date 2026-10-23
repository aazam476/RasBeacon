package tunnel

import (
	"testing"
	"time"
)

// sim drives a Machine through simulated time. The tunnel "works" while
// working is true: the last handshake then follows the clock.
type sim struct {
	t          *testing.T
	m          Machine
	now        time.Time
	working    bool
	handshake  time.Time
	killSwitch bool
}

func newSim(t *testing.T, killSwitch bool) *sim {
	return &sim{t: t, now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), killSwitch: killSwitch}
}

// run advances time in 5-second steps (like the agent's check) and returns
// the final state.
func (s *sim) run(d time.Duration) State {
	var state State
	for end := s.now.Add(d); !s.now.After(end); s.now = s.now.Add(5 * time.Second) {
		if s.working {
			s.handshake = s.now
		}
		state = s.m.Update(Inputs{Now: s.now, Enabled: true, KillSwitch: s.killSwitch, LastHandshake: s.handshake})
	}
	return state
}

func (s *sim) expect(want State, d time.Duration) {
	s.t.Helper()
	if got := s.run(d); got != want {
		s.t.Fatalf("after %s: state %s, want %s", d, got, want)
	}
}

func TestKillSwitchOn_NeverGoesDirect(t *testing.T) {
	s := newSim(t, true)
	s.expect(Connecting, time.Hour) // the server never answers: traffic stays in the dead tunnel

	s.working = true
	s.expect(Up, 10*time.Second)

	// The server stops answering: traffic stays in the tunnel, forever.
	s.working = false
	s.expect(Up, HandshakeMaxAge-10*time.Second)
	s.expect(Down, time.Hour)
}

func TestKillSwitchOff_FallsBackAfterDelayAndReturns(t *testing.T) {
	s := newSim(t, false)
	s.working = true
	s.expect(Up, 10*time.Second)

	s.working = false
	s.expect(Down, HandshakeMaxAge+10*time.Second) // handshake now too old
	s.expect(Direct, FallbackDelay)                // broken for 30 s: go direct

	// The tunnel works again: wait the return delay before going back.
	// (It broke soon after coming up, so the delay doubled to 60 s.)
	s.working = true
	s.expect(Direct, 50*time.Second)
	s.expect(Up, 20*time.Second)
}

func TestKillSwitchOff_FlakyTunnelWaitsLongerEachTime(t *testing.T) {
	s := newSim(t, false)
	delays := []time.Duration{}
	s.working = true
	s.run(10 * time.Second)
	for i := 0; i < 6; i++ {
		s.working = false
		s.run(HandshakeMaxAge + FallbackDelay + 10*time.Second) // -> Direct
		delays = append(delays, s.m.returnDelay)
		s.working = true
		s.run(s.m.returnDelay + 10*time.Second) // -> Up
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("return delays = %v, want %v", delays, want)
		}
	}

	// Once the tunnel has been stable for a while, the delay resets.
	s.run(StableAfter + time.Minute)
	if s.m.returnDelay != MinReturnDelay {
		t.Errorf("return delay = %s after a stable period, want %s", s.m.returnDelay, MinReturnDelay)
	}
}

func TestTurningKillSwitchOnStopsDirectTrafficImmediately(t *testing.T) {
	s := newSim(t, false)
	s.expect(Direct, time.Minute) // never connected, kill switch off
	s.killSwitch = true
	// Only Off and Direct send devices' traffic straight to the uplink.
	if got := s.run(0); got == Direct || got == Off {
		t.Fatalf("state %s: traffic must go back into the tunnel at once", got)
	}
}

func TestTurningVPNOff(t *testing.T) {
	var m Machine
	now := time.Now()
	if m.Update(Inputs{Now: now, Enabled: true, KillSwitch: true}) != Connecting {
		t.Fatal("turning on starts in Connecting")
	}
	if got := m.Update(Inputs{Now: now, Enabled: false}); got != Off {
		t.Fatalf("turning off: %s", got)
	}
}
