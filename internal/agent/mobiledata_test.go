package agent

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// setPhoneBytes sets a phone's received and sent byte counters.
func (e *testEnv) setPhoneBytes(iface string, rx, tx uint64) {
	dir := filepath.Join(e.paths.SysClassNet(), iface, "statistics")
	mustWrite(e.t, filepath.Join(dir, "rx_bytes"), strconv.FormatUint(rx, 10))
	mustWrite(e.t, filepath.Join(dir, "tx_bytes"), strconv.FormatUint(tx, 10))
}

// sampleMobile takes one sample of the phones' counters.
func (e *testEnv) sampleMobile() {
	e.agent.mu.Lock()
	e.agent.countMobileData()
	e.agent.mu.Unlock()
}

func TestMonthStart(t *testing.T) {
	at := func(text string) time.Time {
		v, err := time.Parse(time.DateOnly, text)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		now      string
		resetDay int
		want     string
	}{
		{"2026-10-28", 1, "2026-10-01"},
		{"2026-10-01", 1, "2026-10-01"},
		{"2026-10-14", 15, "2026-09-15"},
		{"2026-10-15", 15, "2026-10-15"},
		// Across the new year.
		{"2026-01-03", 20, "2025-12-20"},
		// March 1st with the 28th: February's.
		{"2026-03-01", 28, "2026-02-28"},
	}
	for _, c := range cases {
		if got := monthStart(at(c.now).Add(13*time.Hour), c.resetDay); !got.Equal(at(c.want)) {
			t.Errorf("monthStart(%s, %d) = %s, want %s", c.now, c.resetDay, got.Format(time.DateOnly), c.want)
		}
	}
}

func TestMobileData_CountsWhatPhonesCarry(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }

	// Nothing plugged in or used: nothing to show.
	if got := e.agent.Status().Uplink.MobileData; got != nil {
		t.Errorf("without a phone: %+v", got)
	}

	// The first sample only notes the counters (they may hold data from
	// before a restart).
	e.addPhone("usb0", "rndis_host", "Pixel 8")
	e.setPhoneBytes("usb0", 1000, 500)
	e.sampleMobile()
	if got := e.agent.Status().Uplink.MobileData; got == nil || got.UsedBytes != 0 || got.ResetDay != 1 || got.Since != "2026-10-01T00:00:00Z" {
		t.Fatalf("after the first sample: %+v", got)
	}

	// Growth counts; a second phone plugged in counts from zero.
	e.setPhoneBytes("usb0", 3000, 1500)
	e.sampleMobile()
	e.addPhone("eth1", "ipheth", "iPhone")
	e.setPhoneBytes("eth1", 200, 100)
	e.sampleMobile()
	if got := e.agent.Status().Uplink.MobileData.UsedBytes; got != 3000+300 {
		t.Errorf("used = %d, want 3300", got)
	}

	// A counter that went down belongs to a phone plugged in again.
	e.setPhoneBytes("usb0", 100, 0)
	e.sampleMobile()
	if got := e.agent.Status().Uplink.MobileData.UsedBytes; got != 3400 {
		t.Errorf("after replugging: used = %d, want 3400", got)
	}

	// Saved at shutdown, and kept after a restart.
	e.agent.SaveUsage()
	var saved mobileDataFile
	if err := config.ReadJSON(e.paths.MobileDataFile(), &saved); err != nil || saved.Bytes != 3400 {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	e.restart()
	e.agent.now = func() time.Time { return c.now }
	e.sampleMobile()
	if got := e.agent.Status().Uplink.MobileData.UsedBytes; got != 3400 {
		t.Errorf("after a restart: used = %d, want 3400", got)
	}

	// A new month starts again from zero.
	c.now = time.Date(2026, 11, 1, 0, 0, 1, 0, time.UTC)
	e.sampleMobile()
	if got := e.agent.Status().Uplink.MobileData; got.UsedBytes != 0 || got.Since != "2026-11-01T00:00:00Z" {
		t.Errorf("in November: %+v", got)
	}
}

func TestMobileData_CountsTheModemsDataPort(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.addPhone("wwan0", "qmi_wwan", "")
	e.agent.modem.iface = "wwan0"
	e.setPhoneBytes("wwan0", 0, 0)
	e.sampleMobile()
	e.setPhoneBytes("wwan0", 4000, 1000)
	e.sampleMobile()
	if got := e.agent.Status().Uplink.MobileData; got == nil || got.UsedBytes != 5000 {
		t.Errorf("modem data = %+v", got)
	}
}

func TestMobileData_SavedEveryTenMinutes(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	e.addPhone("usb0", "rndis_host", "Pixel 8")
	e.setPhoneBytes("usb0", 0, 0)
	e.sampleMobile() // saves the month's start: never saved before
	e.setPhoneBytes("usb0", 10, 0)
	c.advance(time.Minute)
	e.sampleMobile() // too soon to save again
	var saved mobileDataFile
	if err := config.ReadJSON(e.paths.MobileDataFile(), &saved); err != nil || saved.Bytes != 0 {
		t.Fatalf("after a minute: saved = %+v, %v", saved, err)
	}
	e.setPhoneBytes("usb0", 20, 0)
	c.advance(usageSaveInterval)
	e.sampleMobile()
	if err := config.ReadJSON(e.paths.MobileDataFile(), &saved); err != nil || saved.Bytes != 20 {
		t.Errorf("after ten minutes: saved = %+v, %v", saved, err)
	}
}

func TestMobileData_LimitWarnsAndCanBeReset(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }

	// Wrong amounts and days are refused.
	for _, bad := range []api.MobileData{
		{LimitMB: -1, ResetDay: 1},
		{LimitMB: config.MaxUsageLimitMB + 1, ResetDay: 1},
		{LimitMB: 100, ResetDay: 0},
		{LimitMB: 100, ResetDay: 29},
	} {
		var invalid *config.ValidationError
		if _, err := e.agent.UpdateMobileData(bad); !errors.As(err, &invalid) {
			t.Errorf("UpdateMobileData(%+v) = %v", bad, err)
		}
	}

	// A limit alone shows the count, with nothing plugged in.
	got, err := e.agent.UpdateMobileData(api.MobileData{LimitMB: 1000, ResetDay: 1})
	if err != nil || got.LimitMB != 1000 || e.agent.Status().Uplink.MobileData == nil {
		t.Fatalf("saved = %+v, %v", got, err)
	}
	warnings := func() string { return strings.Join(e.agent.Status().Warnings, "\n") }
	if strings.Contains(warnings(), "Phones and modems") {
		t.Errorf("warned with nothing used: %s", warnings())
	}

	e.agent.mu.Lock()
	e.agent.mobileData.file.Bytes = 900_000_000
	e.agent.mu.Unlock()
	if !strings.Contains(warnings(), "have used 900 MB of your 1 GB this month") {
		t.Errorf("at 90%%: %s", warnings())
	}
	e.agent.mu.Lock()
	e.agent.mobileData.file.Bytes = 1_200_000_000
	e.agent.mu.Unlock()
	if !strings.Contains(warnings(), "1.20 GB this month, over your 1 GB limit") {
		t.Errorf("over the limit: %s", warnings())
	}

	// A later reset day keeps the count until that day comes.
	got, err = e.agent.UpdateMobileData(api.MobileData{LimitMB: 1000, ResetDay: 15})
	if err != nil || got.UsedBytes != 1_200_000_000 || got.ResetDay != 15 || got.Since != "2026-10-15T00:00:00Z" {
		t.Errorf("new reset day: %+v, %v", got, err)
	}

	// Reset: from zero, and no more warning.
	got, err = e.agent.ResetMobileData()
	if err != nil || got.UsedBytes != 0 || strings.Contains(warnings(), "Phones and modems") {
		t.Errorf("after the reset: %+v, %v, %s", got, err, warnings())
	}
	var saved mobileDataFile
	if err := config.ReadJSON(e.paths.MobileDataFile(), &saved); err != nil || saved.Bytes != 0 || !saved.Since.Equal(c.now) {
		t.Errorf("saved after the reset: %+v, %v", saved, err)
	}
	var cfg config.Config
	if err := config.ReadJSON(e.paths.ConfigFile(), &cfg); err != nil || cfg.Uplink.MobileLimitMB != 1000 || cfg.Uplink.MobileResetDay != 15 {
		t.Errorf("config = %+v, %v", cfg.Uplink, err)
	}
}

func TestMobileData_OnlyInNormalMode(t *testing.T) {
	e := newTestEnv(t)
	if _, err := e.agent.UpdateMobileData(api.MobileData{ResetDay: 1}); !errors.Is(err, ErrWrongMode) {
		t.Errorf("UpdateMobileData in setup = %v", err)
	}
	if _, err := e.agent.ResetMobileData(); !errors.Is(err, ErrWrongMode) {
		t.Errorf("ResetMobileData in setup = %v", err)
	}
}
