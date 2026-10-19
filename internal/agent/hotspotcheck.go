package agent

// This file checks, every minute, that the hotspot can still carry
// devices' traffic, and repairs it when it can't.
//
// Devices on the hotspot reach the internet through the "br-lan" bridge:
// hostapd puts the hotspot's Wi-Fi interface (ap0) into the bridge when it
// starts. If the interface falls out of the bridge, or hostapd stops, Wi-Fi
// devices can stay connected yet reach nothing, while devices on the
// Ethernet client port (also in the bridge) keep working. The check puts
// the interface back, or restarts the hotspot, and keeps a short record of
// what it did for the diagnostics report.

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/render"
	"rasbeacon/internal/system"
)

const (
	// hotspotCheckInterval is how often the hotspot is checked.
	hotspotCheckInterval = time.Minute
	// hotspotRestartPause is the least time between two restarts by the
	// check, so a hotspot that can't start isn't restarted over and over.
	hotspotRestartPause = 5 * time.Minute
	// hotspotEventsKept is how many repairs the diagnostics report lists.
	hotspotEventsKept = 20
)

// hotspotCheckState is what the check remembers between runs.
type hotspotCheckState struct {
	lastCheck   time.Time
	lastRestart time.Time
	// events are the latest repairs, oldest first, for diagnostics.
	events []string
}

// checkHotspot runs the check if it is due. The caller holds a.mu.
func (a *Agent) checkHotspot() {
	if a.mode != api.ModeNormal || a.now().Sub(a.hotspotCheck.lastCheck) < hotspotCheckInterval {
		return
	}
	a.hotspotCheck.lastCheck = a.now()

	// 1. hostapd itself. "systemctl is-active" prints the state; only a
	//    clear "inactive" or "failed" counts (anything else, like
	//    "activating", is left alone).
	state, _ := a.runner.Run("systemctl", "is-active", hostapdService)
	switch strings.TrimSpace(state) {
	case "inactive", "failed":
		a.restartHotspot("the hotspot program (hostapd) had stopped")
		return
	}

	// 2. Each Wi-Fi interface must be in its bridge.
	wanted := map[string]string{radio.HotspotInterfaceName: render.BridgeName}
	if a.guestActive() {
		wanted[render.GuestInterface] = render.GuestBridgeName
	}
	for iface, bridge := range wanted {
		if _, err := os.Stat(filepath.Join(a.paths.SysClassNet(), iface)); err != nil {
			continue // not there (yet): hostapd creates the guest one
		}
		if got := a.bridgeOf(iface); got != bridge {
			a.noteHotspotEvent(fmt.Sprintf("%s was not in %s (it was in %q); put it back", iface, bridge, got))
			if _, err := a.runner.Run("ip", "link", "set", "dev", iface, "master", bridge); err != nil {
				a.restartHotspot(fmt.Sprintf("%s could not be put back into %s: %v", iface, bridge, err))
				return
			}
		}
	}
}

// bridgeOf returns the bridge an interface is in ("" for none).
func (a *Agent) bridgeOf(iface string) string {
	target, err := os.Readlink(filepath.Join(a.paths.SysClassNet(), iface, "master"))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// restartHotspot restarts hostapd for the reason given, at most once per
// hotspotRestartPause. The caller holds a.mu.
func (a *Agent) restartHotspot(reason string) {
	if !a.hotspotCheck.lastRestart.IsZero() && a.now().Sub(a.hotspotCheck.lastRestart) < hotspotRestartPause {
		a.noteHotspotEvent(reason + "; not restarting again so soon")
		return
	}
	a.hotspotCheck.lastRestart = a.now()
	a.noteHotspotEvent(reason + "; restarting the hotspot")
	if err := system.ServiceRestart(a.runner, hostapdService); err != nil {
		a.noteHotspotEvent("the restart failed: " + err.Error())
	}
}

// noteHotspotEvent logs a repair and keeps it for the diagnostics report.
func (a *Agent) noteHotspotEvent(text string) {
	slog.Warn("hotspot check: " + text)
	a.hotspotCheck.events = append(a.hotspotCheck.events, a.now().UTC().Format(time.RFC3339)+" "+text)
	if extra := len(a.hotspotCheck.events) - hotspotEventsKept; extra > 0 {
		a.hotspotCheck.events = a.hotspotCheck.events[extra:]
	}
}
