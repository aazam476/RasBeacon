package agent

// This file keeps the clock sane on a device without a battery-backed
// clock (see internal/clock for why that matters).

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"rasbeacon/internal/clock"
	"rasbeacon/internal/config"
	"rasbeacon/internal/portal"
)

const (
	// clockSaveInterval is how often the time floor is saved.
	clockSaveInterval = 15 * time.Minute
	// clockCheckInterval is how often RunClockMonitor looks. Often, so
	// the clock is set soon after the uplink connects: until then the
	// VPN's first handshakes may look old to its server (the time floor
	// can be 15 minutes behind after a power cut).
	clockCheckInterval = 10 * time.Second
	// clockAskInterval is how often web servers are asked for the time
	// once they have agreed on it, and clockRetryInterval how soon again
	// when they haven't.
	clockAskInterval   = time.Hour
	clockRetryInterval = time.Minute
	// clockTolerance is how far the clock may be from the servers' time
	// before it is set.
	clockTolerance = 30 * time.Second
)

// applyTimeFloor moves the clock forward to the last saved time, if the
// clock is behind it. Called first thing at startup.
func (a *Agent) applyTimeFloor() {
	floor := a.timeFloor()
	if floor.IsZero() || !a.now().Before(floor) {
		return
	}
	slog.Warn("clock is behind the last saved time; moving it forward", "clock", a.now().UTC(), "saved", floor.UTC())
	a.setClock(floor)
}

// timeFloor reads the saved time (zero if there is none).
func (a *Agent) timeFloor() time.Time {
	data, err := os.ReadFile(a.paths.ClockFile())
	if err != nil {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

// SaveClock saves the current time as the floor for the next boot. cmd/rbd
// also calls it when the device shuts down.
func (a *Agent) SaveClock() {
	now := a.now()
	if now.Before(a.timeFloor()) {
		return // never save an earlier time
	}
	a.saveTimeFloor(now)
}

// saveTimeFloor saves t as the time floor, whatever was saved before.
// Only a time that web servers agreed on may replace a later one (see
// checkClock).
func (a *Agent) saveTimeFloor(t time.Time) {
	if err := config.WriteFileAtomic(a.paths.ClockFile(), []byte(strconv.FormatInt(t.Unix(), 10)+"\n"), 0o644); err != nil {
		slog.Warn("could not save the time", "error", err)
	}
}

// setClock sets the system clock.
func (a *Agent) setClock(t time.Time) {
	if _, err := a.runner.Run("date", "-u", "-s", fmt.Sprintf("@%d", t.Unix())); err != nil {
		slog.Error("could not set the clock", "error", err)
	}
}

// stopNTP stops the system's NTP client, if one runs. RasBeacon takes the
// time from web servers over HTTPS only (see internal/clock for why): an
// NTP answer can't be checked, and the network names the server. The
// image has systemd-timesyncd masked (image/layer/rasbeacon.yaml); this
// is for a system where it runs all the same.
func (a *Agent) stopNTP() {
	if _, err := a.runner.Run("systemctl", "stop", "systemd-timesyncd.service"); err != nil {
		slog.Debug("no NTP client to stop", "error", err)
	}
}

// RunClockMonitor saves the time floor regularly and sets the clock from
// web servers' "Date" headers. cmd/rbd starts it in the background (not on
// the simulated device).
func (a *Agent) RunClockMonitor() {
	a.stopNTP()
	client := clock.Client(nil)
	watch := &clockWatch{fetch: func() []time.Time { return fetchDates(client) }}
	for {
		time.Sleep(clockCheckInterval)
		a.checkClock(watch, time.Now())
	}
}

// clockWatch is what the clock monitor remembers from one round to the
// next.
type clockWatch struct {
	// lastSaved is when it last saved the time floor, lastAsked when it
	// last asked web servers for the time, and lastAgreed when they last
	// agreed on it.
	lastSaved, lastAsked, lastAgreed time.Time
	// fetch asks web servers for the time (fetchDates on the device).
	fetch func() []time.Time
}

// checkClock is one round of the clock monitor, at now.
func (a *Agent) checkClock(w *clockWatch, now time.Time) {
	if now.Sub(w.lastSaved) >= clockSaveInterval {
		a.SaveClock()
		w.lastSaved = now
	}

	// Web servers are asked as soon as this uplink has been checked for a
	// login page and none is in the way, and then once an hour. With a
	// login page the network answers in the servers' place, so there is
	// no time to get.
	a.mu.Lock()
	connected := a.uplink.status.Connected
	open := a.portal.result.State == portal.Open && a.portal.probedUplinkSince.Equal(a.uplink.connectedSince)
	a.mu.Unlock()
	if !connected || !open {
		return
	}
	if now.Sub(w.lastAgreed) < clockAskInterval || now.Sub(w.lastAsked) < clockRetryInterval {
		return
	}
	w.lastAsked = now

	agreed, ok := clock.AgreedTime(w.fetch())
	if !ok {
		slog.Warn("web servers did not agree on the time")
		a.setClockUnsure(true)
		return
	}
	a.setClockUnsure(false)
	w.lastAgreed = now
	if difference := agreed.Sub(now); difference > clockTolerance || difference < -clockTolerance {
		slog.Warn("setting the clock from web servers", "time", agreed.UTC(), "change", difference)
		a.setClock(agreed)
		// The floor follows, also back: a clock that was ahead (a
		// battery-backed clock gone wrong, or a time an older version
		// took from a network's time server) must not come back at the
		// next boot.
		a.saveTimeFloor(agreed)
	}
}

// fetchDates asks all clock.DateSources at the same time.
func fetchDates(client *http.Client) []time.Time {
	samples := make([]time.Time, len(clock.DateSources))
	var wg sync.WaitGroup
	for i, url := range clock.DateSources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			samples[i], _ = clock.FetchDate(ctx, client, url)
		}()
	}
	wg.Wait()
	return samples
}

// setClockUnsure notes whether the clock could be set from the internet,
// for the warning on the Status page (clockWarnings).
func (a *Agent) setClockUnsure(unsure bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clockUnsure = unsure
}

// clockWarnings warns when web servers couldn't set the clock on a
// network that otherwise works. The clock then runs from the last saved
// time, which keeps the VPN's handshakes valid across a reboot: if that
// time was wrong, secure websites and the VPN may fail until the clock is
// set. The caller holds a.mu.
func (a *Agent) clockWarnings() []string {
	if !a.clockUnsure {
		return nil
	}
	return []string{fmt.Sprintf("RasBeacon couldn't set its clock from the internet (it reads %s UTC). If that's wrong, secure websites and the VPN may not work: try another network, where the clock is set by itself.", a.now().UTC().Format("Jan 2, 2006 15:04"))}
}
