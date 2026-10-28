package agent

// This file names devices and counts the data each one uses.
//
// Names: the owner can give a device a name on the Clients page. It is kept
// by MAC address in the settings, so it stays across reconnects and
// restarts (a phone with a private address per network keeps the same
// address on RasBeacon's network).
//
// Data use: every few seconds the radio's byte counters for each Wi-Fi
// device are read (see sampleReadings); what they grew by since the last
// sample is added to that device's total. The totals are saved now and then
// (usage.json), so they survive restarts, until the owner resets them. With
// a data limit set, a device over the limit is blocked by the firewall
// until the counters are reset or the limit is raised.

import (
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/stations"
)

// usageSaveInterval is how often changed totals are written to flash.
const usageSaveInterval = 10 * time.Minute

// usageFile is usage.json.
type usageFile struct {
	// Since is when the counters were last reset (or first started).
	Since time.Time `json:"since"`
	// Devices are the totals by MAC address: [down, up] in bytes.
	Devices map[string][2]uint64 `json:"devices"`
}

// usageState is the totals in memory.
type usageState struct {
	loaded  bool
	file    usageFile
	dirty   bool
	savedAt time.Time
	// overLimit are the devices paused for their data use, as last given
	// to the firewall.
	overLimit []string
}

// loadUsage reads usage.json once. The caller holds a.mu.
func (a *Agent) loadUsage() {
	if a.usage.loaded {
		return
	}
	a.usage.loaded = true
	err := config.ReadJSON(a.paths.UsageFile(), &a.usage.file)
	if err != nil && !errors.Is(err, config.ErrNotFound) {
		slog.Warn("could not read the data use totals; starting again", "error", err)
	}
	if err != nil || a.usage.file.Since.IsZero() {
		a.usage.file = usageFile{Since: a.now()}
	}
	if a.usage.file.Devices == nil {
		a.usage.file.Devices = map[string][2]uint64{}
	}
}

// addUsage adds what devices used since the last sample. bytes are the
// radio's counters now, before at the last sample ([down, up] by MAC); a
// counter that went down belongs to a new connection, so all of it counts.
// The caller holds a.mu.
func (a *Agent) addUsage(before, now map[string][2]uint64) {
	a.loadUsage()
	for mac, counters := range now {
		previous, seen := before[mac]
		var added [2]uint64
		for i := range 2 {
			switch {
			case !seen:
				// A device that just joined: its counters started at 0.
				added[i] = counters[i]
			case counters[i] >= previous[i]:
				added[i] = counters[i] - previous[i]
			default:
				added[i] = counters[i]
			}
		}
		if added[0] == 0 && added[1] == 0 {
			continue
		}
		total := a.usage.file.Devices[mac]
		a.usage.file.Devices[mac] = [2]uint64{total[0] + added[0], total[1] + added[1]}
		a.usage.dirty = true
	}
	if a.usage.dirty && a.now().Sub(a.usage.savedAt) >= usageSaveInterval {
		a.saveUsage()
	}
	a.applyUsageLimit()
}

// SaveUsage saves each device's data use, and the month's data through
// phones and modems, if they changed since the last save. Totals are
// otherwise saved at most every usageSaveInterval, so cmd/rbd calls this
// when the device shuts down: without it, the last few minutes of use
// would be lost at every reboot or update.
func (a *Agent) SaveUsage() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.usage.dirty {
		a.saveUsage()
	}
	if a.mobileData.dirty {
		a.saveMobileData()
	}
}

// saveUsage writes usage.json. The caller holds a.mu.
func (a *Agent) saveUsage() {
	if err := config.WriteJSON(a.paths.UsageFile(), a.usage.file, 0o600); err != nil {
		slog.Warn("could not save the data use totals", "error", err)
		return
	}
	a.usage.dirty = false
	a.usage.savedAt = a.now()
}

// usageLimitBytes is the data limit in force now (0: none), metered
// connection or not.
func (a *Agent) usageLimitBytes() uint64 {
	if a.cfg.Clients.UsageLimitMB <= 0 {
		return 0
	}
	return uint64(a.cfg.Clients.UsageLimitMB) * 1_000_000
}

// overUsageLimit lists the devices over the data limit, sorted.
func (a *Agent) overUsageLimit() []string {
	limit := a.usageLimitBytes()
	if limit == 0 {
		return nil
	}
	a.loadUsage()
	var over []string
	for mac, total := range a.usage.file.Devices {
		if total[0]+total[1] >= limit {
			over = append(over, mac)
		}
	}
	slices.Sort(over)
	return over
}

// applyUsageLimit reloads the firewall when the devices over the limit
// changed. The caller holds a.mu.
func (a *Agent) applyUsageLimit() {
	over := a.overUsageLimit()
	if slices.Equal(over, a.usage.overLimit) {
		return
	}
	for _, mac := range over {
		if !slices.Contains(a.usage.overLimit, mac) {
			slog.Info("device paused: it used its data limit", "mac", mac, "limitMB", a.cfg.Clients.UsageLimitMB)
		}
	}
	a.usage.overLimit = over
	if a.mode == api.ModeNormal {
		if err := a.applyFirewall(); err != nil {
			slog.Error("could not apply the data limit", "error", err)
		}
	}
}

// setUsage fills in a device's name, data use and whether the limit paused
// it. The caller holds a.mu.
func (a *Agent) setUsage(c *stations.Station) {
	a.loadUsage()
	c.Name = a.cfg.Clients.Names[c.MAC]
	c.FixedAddress = a.cfg.Clients.FixedAddresses[c.MAC]
	c.Priority = slices.Contains(a.cfg.QoS.Priority, c.MAC)
	total := a.usage.file.Devices[c.MAC]
	c.DownBytes, c.UpBytes = total[0], total[1]
	if slices.Contains(a.usage.overLimit, c.MAC) {
		c.LimitReached = true
		c.InternetAccess = false
	}
}

// SetClientName names a device; an empty name removes it.
func (a *Agent) SetClientName(mac, name string) error {
	mac, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > config.MaxDeviceNameLength {
		return &config.ValidationError{Field: "name", Message: "Use at most 40 characters."}
	}
	if strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return &config.ValidationError{Field: "name", Message: "Use letters, numbers and punctuation only."}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	if name == "" {
		delete(newCfg.Clients.Names, mac)
	} else {
		if newCfg.Clients.Names == nil {
			newCfg.Clients.Names = map[string]string{}
		}
		newCfg.Clients.Names[mac] = name
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("device renamed", "mac", mac)
	return nil
}

// UsageSettings returns the data limit and since when data is counted.
func (a *Agent) UsageSettings() api.UsageSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.loadUsage()
	return api.UsageSettings{
		LimitMB:       a.cfg.Clients.UsageLimitMB,
		Since:         a.usage.file.Since.UTC().Format(time.RFC3339),
		DevicesPaused: len(a.usage.overLimit),
	}
}

// UpdateUsageSettings saves the data limit (applied at once).
func (a *Agent) UpdateUsageSettings(req api.UsageSettings) (api.UsageSettings, error) {
	if req.LimitMB < 0 || req.LimitMB > config.MaxUsageLimitMB {
		return api.UsageSettings{}, &config.ValidationError{Field: "limitMb", Message: "Enter an amount up to 1,000 GB, or leave it empty for no limit."}
	}
	a.mu.Lock()
	if a.mode != api.ModeNormal {
		a.mu.Unlock()
		return api.UsageSettings{}, ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	newCfg.Clients.UsageLimitMB = req.LimitMB
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		a.mu.Unlock()
		return api.UsageSettings{}, err
	}
	a.cfg = newCfg
	slog.Info("data limit saved", "limitMB", req.LimitMB)
	a.applyUsageLimit()
	a.mu.Unlock()
	return a.UsageSettings(), nil
}

// ResetUsage starts counting again from zero; devices paused by the limit
// are allowed again.
func (a *Agent) ResetUsage() (api.UsageSettings, error) {
	a.mu.Lock()
	if a.mode != api.ModeNormal {
		a.mu.Unlock()
		return api.UsageSettings{}, ErrWrongMode
	}
	a.loadUsage()
	a.usage.file = usageFile{Since: a.now(), Devices: map[string][2]uint64{}}
	a.saveUsage()
	slog.Info("data use counters reset")
	a.applyUsageLimit()
	a.mu.Unlock()
	return a.UsageSettings(), nil
}
