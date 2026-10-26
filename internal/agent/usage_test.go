package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// stationWithBytes is one device in `iw dev ap0 station dump`, with its
// byte counters (rx: from the device, tx: towards it).
func stationWithBytes(mac string, rx, tx uint64) string {
	return fmt.Sprintf("Station %s (on ap0)\n\trx bytes:\t%d\n\ttx bytes:\t%d\n\tsignal:\t-48 dBm\n", mac, rx, tx)
}

func TestClients_NamesAreKeptByAddress(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.runner.outputs["iw dev ap0 station dump"] = stationWithBytes("3a:1f:22:aa:bb:cc", 0, 0)

	if err := e.agent.SetClientName("3A:1F:22:AA:BB:CC", "  Ali's iPhone "); err != nil {
		t.Fatal(err)
	}
	if c := e.agent.Status().Clients[0]; c.Name != "Ali's iPhone" {
		t.Errorf("client = %+v", c)
	}
	var saved config.Config
	if err := config.ReadJSON(e.paths.ConfigFile(), &saved); err != nil || saved.Clients.Names["3a:1f:22:aa:bb:cc"] != "Ali's iPhone" {
		t.Errorf("the name should be saved: %+v %v", saved.Clients, err)
	}

	var invalid *config.ValidationError
	for _, bad := range []string{strings.Repeat("x", 41), "tab\there"} {
		if err := e.agent.SetClientName("3a:1f:22:aa:bb:cc", bad); !errors.As(err, &invalid) || invalid.Field != "name" {
			t.Errorf("%q: got %v", bad, err)
		}
	}
	// An empty name goes back to the device's own.
	if err := e.agent.SetClientName("3a:1f:22:aa:bb:cc", ""); err != nil {
		t.Fatal(err)
	}
	if c := e.agent.Status().Clients[0]; c.Name != "" {
		t.Errorf("client = %+v", c)
	}
}

func TestClients_DataUseIsCountedAndTheLimitBlocksADevice(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	phone, laptop := "3a:1f:22:aa:bb:cc", "3a:1f:22:aa:bb:dd"
	sample := func(dump string) {
		e.runner.outputs["iw dev ap0 station dump"] = dump
		c.advance(5 * time.Second)
		e.agent.mu.Lock()
		e.agent.sampleReadings()
		e.agent.mu.Unlock()
	}

	// The first sample only sets the starting point: the counters then
	// include whatever the devices used before RasBeacon (re)started.
	sample(stationWithBytes(phone, 900, 9_000))
	sample(stationWithBytes(phone, 1_000, 10_000))
	// The laptop joins (its counters start at 0); the phone reconnects (its
	// counters start again).
	sample(stationWithBytes(phone, 50, 400) + stationWithBytes(laptop, 2_000, 30_000))

	got := map[string][2]uint64{}
	for _, cl := range e.agent.Status().Clients {
		got[cl.MAC] = [2]uint64{cl.DownBytes, cl.UpBytes}
	}
	if got[phone] != [2]uint64{1_000 + 400, 100 + 50} || got[laptop] != [2]uint64{30_000, 2_000} {
		t.Errorf("data use = %v", got)
	}

	// A 1 MB limit, on a connection that isn't metered: it applies anyway.
	if _, err := e.agent.UpdateUsageSettings(api.UsageSettings{LimitMB: 1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "ether saddr") {
		t.Error("nobody is over the limit yet")
	}
	e.agent.mu.Lock()
	e.agent.usage.file.Devices[laptop] = [2]uint64{900_000, 200_000}
	e.agent.mu.Unlock()
	sample(stationWithBytes(phone, 50, 400) + stationWithBytes(laptop, 2_000, 30_000))
	if rules := e.readFile(e.paths.FirewallRules()); !strings.Contains(rules, laptop) || strings.Contains(rules, phone) {
		t.Errorf("the laptop is over 1 MB and should be paused, the phone not:\n%s", rules)
	}
	for _, cl := range e.agent.Status().Clients {
		if cl.MAC == laptop && (!cl.LimitReached || cl.InternetAccess) {
			t.Errorf("laptop = %+v", cl)
		}
	}
	if s := e.agent.UsageSettings(); s.DevicesPaused != 1 || s.LimitMB != 1 {
		t.Errorf("settings = %+v", s)
	}

	// Resetting the counters allows it again, and the totals are saved.
	if _, err := e.agent.ResetUsage(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), laptop) {
		t.Error("after a reset the laptop is allowed again")
	}
	var saved usageFile
	if err := config.ReadJSON(e.paths.UsageFile(), &saved); err != nil || len(saved.Devices) != 0 || !saved.Since.Equal(c.now) {
		t.Errorf("saved = %+v, %v", saved, err)
	}

	// A limit beyond 1 TB is refused.
	var invalid *config.ValidationError
	if _, err := e.agent.UpdateUsageSettings(api.UsageSettings{LimitMB: -1}); !errors.As(err, &invalid) {
		t.Errorf("got %v", err)
	}
}

func TestClients_AFailedRadioReadDoesNotCountDataTwice(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	phone := "3a:1f:22:aa:bb:cc"
	sample := func() {
		c.advance(5 * time.Second)
		e.agent.mu.Lock()
		e.agent.sampleReadings()
		e.agent.mu.Unlock()
	}
	read := func(rx, tx uint64) {
		e.runner.failing = nil
		e.runner.outputs["iw dev ap0 station dump"] = stationWithBytes(phone, rx, tx)
		sample()
	}

	read(0, 0)
	read(100, 1_000)
	// One read fails (a slow USB adapter) while the phone stays connected.
	delete(e.runner.outputs, "iw dev ap0 station dump")
	e.runner.failing = []string{"iw dev ap0 station dump"}
	sample()
	// The next read must add only the growth since the last good read.
	read(150, 1_500)

	cl := e.agent.Status().Clients[0]
	if cl.DownBytes != 1_500 || cl.UpBytes != 150 {
		t.Errorf("data use = %d down, %d up; want 1500 and 150", cl.DownBytes, cl.UpBytes)
	}
}
