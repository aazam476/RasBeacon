package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hotspotClock is a clock the hotspot check tests move by hand.
type hotspotClock struct{ now time.Time }

func newHotspotCheckEnv(t *testing.T) (*testEnv, *hotspotClock) {
	t.Helper()
	e := newClaimedEnv(t, false)
	clock := &hotspotClock{now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return clock.now }
	e.runner.commands = nil
	return e, clock
}

// check runs the hotspot check a minute later than the last one.
func (e *testEnv) checkHotspotAt(clock *hotspotClock) {
	clock.now = clock.now.Add(hotspotCheckInterval)
	e.agent.mu.Lock()
	e.agent.checkHotspot()
	e.agent.mu.Unlock()
}

func TestCheckHotspot_PutsTheWiFiBackIntoTheBridge(t *testing.T) {
	e, clock := newHotspotCheckEnv(t)
	sysNet := e.paths.SysClassNet()
	if err := os.MkdirAll(filepath.Join(sysNet, "ap0"), 0o755); err != nil {
		t.Fatal(err)
	}
	// ap0 is in no bridge: devices on Wi-Fi would reach nothing.
	e.checkHotspotAt(clock)
	if !e.runner.ran("ip link set dev ap0 master br-lan") {
		t.Errorf("ap0 should be put back into br-lan; commands: %v", e.runner.commands)
	}
	if !strings.Contains(e.agent.Diagnostics(), "ap0 was not in br-lan") {
		t.Error("the repair should be in the diagnostics report")
	}

	// In the bridge: nothing to do.
	if err := os.Symlink("../br-lan", filepath.Join(sysNet, "ap0", "master")); err != nil {
		t.Fatal(err)
	}
	e.runner.commands = nil
	e.checkHotspotAt(clock)
	if e.runner.ran("ip link set") || e.runner.ran("systemctl restart") {
		t.Errorf("a healthy hotspot must be left alone; commands: %v", e.runner.commands)
	}
}

func TestCheckHotspot_RestartsAStoppedHotspotButNotInALoop(t *testing.T) {
	e, clock := newHotspotCheckEnv(t)
	e.runner.outputs["systemctl is-active rasbeacon-hostapd.service"] = "failed\n"

	e.checkHotspotAt(clock)
	if !e.runner.ran("systemctl restart rasbeacon-hostapd.service") {
		t.Fatalf("a stopped hotspot should be restarted; commands: %v", e.runner.commands)
	}
	// Still failed a minute later: no second restart yet.
	e.runner.commands = nil
	e.checkHotspotAt(clock)
	if e.runner.ran("systemctl restart") {
		t.Error("the hotspot must not be restarted again within a few minutes")
	}
	// Five minutes on, it may try again.
	clock.now = clock.now.Add(hotspotRestartPause)
	e.checkHotspotAt(clock)
	if !e.runner.ran("systemctl restart rasbeacon-hostapd.service") {
		t.Error("after the pause, the hotspot should be restarted again")
	}
}
