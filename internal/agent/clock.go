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
)

const (
	// clockSaveInterval is how often the time floor is saved.
	clockSaveInterval = 15 * time.Minute
	// ntpGrace is how long NTP gets after the uplink connects before
	// RasBeacon asks web servers for the time instead.
	ntpGrace = 30 * time.Second
	// clockCheckInterval is how often RunClockMonitor looks.
	clockCheckInterval = 30 * time.Second
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
	if err := config.WriteFileAtomic(a.paths.ClockFile(), []byte(strconv.FormatInt(now.Unix(), 10)+"\n"), 0o644); err != nil {
		slog.Warn("could not save the time", "error", err)
	}
}

// setClock sets the system clock.
func (a *Agent) setClock(t time.Time) {
	if _, err := a.runner.Run("date", "-u", "-s", fmt.Sprintf("@%d", t.Unix())); err != nil {
		slog.Error("could not set the clock", "error", err)
	}
}

// ntpSynchronized asks systemd-timesyncd whether NTP has set the clock.
func (a *Agent) ntpSynchronized() bool {
	out, err := a.runner.Run("timedatectl", "show", "--property=NTPSynchronized", "--value")
	return err == nil && strings.TrimSpace(out) == "yes"
}

// RunClockMonitor saves the time floor regularly and, if NTP doesn't work
// on the current network, sets the clock from web servers' "Date" headers.
// cmd/rbd starts it in the background (not on the simulated device).
func (a *Agent) RunClockMonitor() {
	lastSaved := time.Time{}
	lastSet := time.Time{}
	client := &http.Client{Timeout: 5 * time.Second}
	for {
		time.Sleep(clockCheckInterval)
		now := time.Now()
		if now.Sub(lastSaved) >= clockSaveInterval {
			a.SaveClock()
			lastSaved = now
		}

		// Only fall back to web servers when NTP hasn't worked, the uplink
		// has been connected for a while, and we haven't just done it.
		a.mu.Lock()
		connected := a.uplink.status.Connected
		connectedFor := a.now().Sub(a.uplink.connectedSince)
		a.mu.Unlock()
		if !connected || connectedFor < ntpGrace || now.Sub(lastSet) < time.Hour || a.ntpSynchronized() {
			continue
		}

		samples := fetchDates(client)
		agreed, ok := clock.AgreedTime(samples, a.timeFloor())
		if !ok {
			slog.Warn("NTP is not working and web servers did not agree on the time")
			continue
		}
		lastSet = now
		if difference := time.Until(agreed); difference > 30*time.Second || difference < -30*time.Second {
			slog.Warn("NTP is not working; setting the clock from web servers", "time", agreed.UTC(), "change", difference)
			a.setClock(agreed)
			a.SaveClock()
		}
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
