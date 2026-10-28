package agent

// Data used through phones and modems. Phone plans and SIM cards count
// data by the month, so RasBeacon counts it the same way: every few
// seconds (sampleReadings) it reads the byte counters of each phone or
// modem plugged in and adds what they grew by to this month's total. The
// total is saved now and then (mobile-data.json), so it survives restarts,
// and starts again on the owner's reset day. With a monthly limit set, the
// Status page warns as the total nears it and once it is over. Nothing is
// blocked: going over costs money, not safety, and the owner decides.

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/readings"
)

// mobileWarnShare is how much of the limit used (out of 100) brings the
// "almost used up" warning.
const mobileWarnShare = 90

// mobileDataFile is mobile-data.json.
type mobileDataFile struct {
	// Since is when this month's count started.
	Since time.Time `json:"since"`
	// Bytes is the data down and up since then.
	Bytes uint64 `json:"bytes"`
}

// mobileDataState is the month's total in memory.
type mobileDataState struct {
	loaded  bool
	file    mobileDataFile
	dirty   bool
	savedAt time.Time
	// counters are each phone's or modem's byte counter (received plus
	// sent) at the last sample, by interface; nil before the first one.
	counters map[string]uint64
}

// loadMobileData reads mobile-data.json once. The caller holds a.mu.
func (a *Agent) loadMobileData() {
	if a.mobileData.loaded {
		return
	}
	a.mobileData.loaded = true
	err := config.ReadJSON(a.paths.MobileDataFile(), &a.mobileData.file)
	if err != nil && !errors.Is(err, config.ErrNotFound) {
		slog.Warn("could not read the mobile data total; starting again", "error", err)
		a.mobileData.file = mobileDataFile{}
	}
}

// mobileResetDay is the day of the month the count starts again.
func (a *Agent) mobileResetDay() int {
	if a.cfg.Uplink.MobileResetDay < 1 {
		return 1
	}
	return a.cfg.Uplink.MobileResetDay
}

// monthStart is when the month's count that holds now started: the latest
// reset day (at midnight UTC) that isn't after now.
func monthStart(now time.Time, resetDay int) time.Time {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), resetDay, 0, 0, 0, 0, time.UTC)
	if now.Before(start) {
		// time.Date turns month 0 into December of the year before.
		start = time.Date(now.Year(), now.Month()-1, resetDay, 0, 0, 0, 0, time.UTC)
	}
	return start
}

// startNewMonth starts the count again when this month's started after the
// saved one. The caller holds a.mu.
func (a *Agent) startNewMonth() {
	start := monthStart(a.now(), a.mobileResetDay())
	if a.mobileData.file.Since.Before(start) {
		a.mobileData.file = mobileDataFile{Since: start}
		a.mobileData.dirty = true
	}
}

// mobileInterfaces are the network interfaces of the phones and modems
// plugged in: USB phones and modems, and a ModemManager modem's data port.
func (a *Agent) mobileInterfaces() []string {
	names := a.phoneInterfaces()
	if a.modem.iface != "" {
		names = append(names, a.modem.iface)
	}
	return names
}

// countMobileData adds what the phones and modems carried since the last
// sample to the month's total. The caller holds a.mu.
func (a *Agent) countMobileData() {
	a.loadMobileData()
	a.startNewMonth()
	now := map[string]uint64{}
	for _, iface := range a.mobileInterfaces() {
		now[iface] = a.readCounter(iface, "rx_bytes") + a.readCounter(iface, "tx_bytes")
	}
	// The first sample after a start only notes the counters: they may
	// hold data counted before RasBeacon restarted.
	if before := a.mobileData.counters; before != nil {
		for iface, counter := range now {
			previous, seen := before[iface]
			added := counter
			if seen && counter >= previous {
				added = counter - previous
			}
			// Not seen before: just plugged in, so its counter started at
			// 0. A counter that went down was plugged in again.
			if added > 0 {
				a.mobileData.file.Bytes += added
				a.mobileData.dirty = true
			}
		}
	}
	a.mobileData.counters = now
	if a.mobileData.dirty && a.now().Sub(a.mobileData.savedAt) >= usageSaveInterval {
		a.saveMobileData()
	}
}

// saveMobileData writes mobile-data.json. The caller holds a.mu.
func (a *Agent) saveMobileData() {
	if err := config.WriteJSON(a.paths.MobileDataFile(), a.mobileData.file, 0o600); err != nil {
		slog.Warn("could not save the mobile data total", "error", err)
		return
	}
	a.mobileData.dirty = false
	a.mobileData.savedAt = a.now()
}

// mobileDataNow is the month's total and the limit, for the pages. The
// caller holds a.mu.
func (a *Agent) mobileDataNow() api.MobileData {
	a.loadMobileData()
	a.startNewMonth()
	return api.MobileData{
		UsedBytes: a.mobileData.file.Bytes,
		Since:     a.mobileData.file.Since.UTC().Format(time.RFC3339),
		LimitMB:   a.cfg.Uplink.MobileLimitMB,
		ResetDay:  a.mobileResetDay(),
	}
}

// mobileDataStatus is the month's total for the status, or nil when there
// is nothing to show: no phone or modem plugged in, nothing used this
// month and no limit set. The caller holds a.mu.
func (a *Agent) mobileDataStatus() *api.MobileData {
	data := a.mobileDataNow()
	if data.UsedBytes == 0 && data.LimitMB == 0 && len(a.mobileInterfaces()) == 0 {
		return nil
	}
	return &data
}

// mobileDataWarnings warns on the Status page when the month's data nears
// the limit or is over it. The caller holds a.mu.
func (a *Agent) mobileDataWarnings() []string {
	data := a.mobileDataNow()
	if data.LimitMB <= 0 {
		return nil
	}
	limit := uint64(data.LimitMB) * 1_000_000
	// The limit as the owner typed it: "5 GB" or "750 MB".
	shownLimit := fmt.Sprintf("%d\u00a0MB", data.LimitMB)
	if data.LimitMB%1000 == 0 {
		shownLimit = fmt.Sprintf("%d\u00a0GB", data.LimitMB/1000)
	}
	switch {
	case data.UsedBytes >= limit:
		return []string{fmt.Sprintf("Phones and modems have used %s this month, over your %s limit. Your plan may charge for more or slow down.", readings.Bytes(data.UsedBytes), shownLimit)}
	case data.UsedBytes*100 >= limit*mobileWarnShare:
		return []string{fmt.Sprintf("Phones and modems have used %s of your %s this month.", readings.Bytes(data.UsedBytes), shownLimit)}
	}
	return nil
}

// UpdateMobileData saves the monthly limit and the day the count starts
// again.
func (a *Agent) UpdateMobileData(req api.MobileData) (api.MobileData, error) {
	if req.LimitMB < 0 || req.LimitMB > config.MaxUsageLimitMB {
		return api.MobileData{}, &config.ValidationError{Field: "limitMb", Message: "Enter an amount up to 1,000 GB, or leave it empty for no limit."}
	}
	if req.ResetDay < 1 || req.ResetDay > config.MaxMobileResetDay {
		return api.MobileData{}, &config.ValidationError{Field: "resetDay", Message: fmt.Sprintf("Choose a day from 1 to %d.", config.MaxMobileResetDay)}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return api.MobileData{}, ErrWrongMode
	}
	// A new reset day keeps the count until that day next comes, instead
	// of starting again at once.
	a.loadMobileData()
	if start := monthStart(a.now(), req.ResetDay); a.mobileData.file.Since.Before(start) {
		a.mobileData.file.Since = start
		a.mobileData.dirty = true
	}
	newCfg := a.cfg.Clone()
	newCfg.Uplink.MobileLimitMB = req.LimitMB
	newCfg.Uplink.MobileResetDay = req.ResetDay
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return api.MobileData{}, err
	}
	a.cfg = newCfg
	slog.Info("mobile data limit saved", "limitMB", req.LimitMB, "resetDay", req.ResetDay)
	return a.mobileDataNow(), nil
}

// ResetMobileData starts this month's count again from zero, for a new
// SIM card or a plan topped up.
func (a *Agent) ResetMobileData() (api.MobileData, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return api.MobileData{}, ErrWrongMode
	}
	a.loadMobileData()
	a.mobileData.file = mobileDataFile{Since: a.now()}
	a.saveMobileData()
	slog.Info("mobile data count reset")
	return a.mobileDataNow(), nil
}
