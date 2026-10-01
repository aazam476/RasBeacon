// Package tunnel decides, every few seconds, where your devices' traffic
// goes when the WireGuard VPN is on: through the tunnel, directly through
// the uplink, or nowhere. It is the "kill switch".
//
// It is a small state machine with no side effects: the agent feeds it
// what it observed (Inputs) and applies the answer (routing, firewall, DNS).
// That keeps the rules in one place and lets the tests run through hours of
// simulated time in milliseconds.
//
// # States
//
//	Off         VPN turned off: traffic goes directly through the uplink.
//	Connecting  VPN on, no working tunnel yet (just started, or no uplink).
//	Up          The tunnel works: traffic goes through it.
//	Down        The tunnel worked but stopped.
//	Direct      Kill switch OFF only: the tunnel has been broken for a while,
//	            so traffic goes directly through the uplink until the
//	            tunnel has worked again for a while.
//
// With the kill switch ON, Connecting and Down send traffic into the tunnel
// anyway, where it is dropped: nothing ever leaves unprotected.
package tunnel

import "time"

// State is where the tunnel is (see the package comment).
type State string

const (
	Off        State = "off"
	Connecting State = "connecting"
	Up         State = "up"
	Down       State = "down"
	Direct     State = "direct"
)

// Timing rules.
const (
	// HandshakeMaxAge: WireGuard renews its session every 2 minutes while
	// traffic (including keepalives) flows. A handshake older than this
	// means the server stopped answering.
	HandshakeMaxAge = 180 * time.Second
	// FallbackDelay: with the kill switch off, how long the tunnel must be
	// broken before traffic goes direct.
	FallbackDelay = 30 * time.Second
	// MinReturnDelay and MaxReturnDelay: how long the tunnel must work again
	// before traffic goes back into it. The delay doubles each time the
	// tunnel breaks again soon after, so a flaky tunnel doesn't make your
	// connection switch back and forth every few seconds.
	MinReturnDelay = 30 * time.Second
	MaxReturnDelay = 5 * time.Minute
	// StableAfter: after working this long, the return delay resets.
	StableAfter = 10 * time.Minute
)

// Inputs is what the agent observed.
type Inputs struct {
	Now time.Time
	// Enabled is the "Use VPN" setting.
	Enabled bool
	// KillSwitch is the kill switch setting (on = never send traffic
	// outside the tunnel).
	KillSwitch bool
	// LastHandshake is the time of WireGuard's latest handshake with the
	// server (zero if none yet).
	LastHandshake time.Time
}

// healthy reports whether the tunnel works right now.
func (in Inputs) healthy() bool {
	return !in.LastHandshake.IsZero() && in.Now.Sub(in.LastHandshake) <= HandshakeMaxAge
}

// Machine remembers the state between updates. The zero value is ready to
// use and starts in Off.
type Machine struct {
	state State
	// brokenSince is when the tunnel was last seen not working (zero while
	// it works).
	brokenSince time.Time
	// workingSince is when the tunnel started working again (zero while it
	// is broken).
	workingSince time.Time
	// upSince is when the state last became Up.
	upSince     time.Time
	returnDelay time.Duration
}

// State returns the current state.
func (m *Machine) State() State {
	if m.state == "" {
		return Off
	}
	return m.state
}

// Update moves the machine on with new observations and returns the state.
func (m *Machine) Update(in Inputs) State {
	if m.returnDelay == 0 {
		m.returnDelay = MinReturnDelay
	}
	if !in.Enabled {
		*m = Machine{state: Off, returnDelay: m.returnDelay}
		return Off
	}

	healthy := in.healthy()
	if healthy {
		m.brokenSince = time.Time{}
		if m.workingSince.IsZero() {
			m.workingSince = in.Now
		}
	} else {
		m.workingSince = time.Time{}
		if m.brokenSince.IsZero() {
			m.brokenSince = in.Now
		}
	}

	switch m.State() {
	case Off:
		m.state = Connecting
		if healthy {
			m.enterUp(in.Now)
		}

	case Connecting, Down:
		switch {
		case healthy:
			m.enterUp(in.Now)
		case !in.KillSwitch && in.Now.Sub(m.brokenSince) >= FallbackDelay:
			m.state = Direct
		}

	case Up:
		if !healthy {
			m.state = Down
			// Broke soon after coming up: wait longer before trusting it
			// again next time.
			if in.Now.Sub(m.upSince) < StableAfter {
				m.returnDelay = min(m.returnDelay*2, MaxReturnDelay)
			}
		} else if in.Now.Sub(m.upSince) >= StableAfter {
			m.returnDelay = MinReturnDelay
		}

	case Direct:
		switch {
		case in.KillSwitch:
			// The owner turned the kill switch on: stop direct traffic now.
			m.state = Down
			if healthy {
				m.enterUp(in.Now)
			}
		case healthy && in.Now.Sub(m.workingSince) >= m.returnDelay:
			m.enterUp(in.Now)
		}
	}
	return m.state
}

func (m *Machine) enterUp(now time.Time) {
	m.state = Up
	m.upSince = now
}

// RoutesThroughTunnel reports whether devices' traffic is sent into the
// tunnel in state s. In Connecting and Down that means it is dropped there
// (the kill switch); only Off and Direct send it straight to the uplink.
func (s State) RoutesThroughTunnel() bool {
	return s == Connecting || s == Up || s == Down
}
