package agent

// This file builds the status page data and the diagnostics export.

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/stations"
	"rasbeacon/internal/system"
	"rasbeacon/internal/version"
)

// Status collects everything shown on the status page.
func (a *Agent) Status() api.Status {
	a.mu.Lock()
	defer a.mu.Unlock()

	h := a.cfg.Hotspot
	channel := h.Channel
	if channel == 0 {
		channel = config.DefaultChannel(h.Band)
	}

	status := api.Status{
		Version:       version.Version,
		BoardModel:    a.boardModel(),
		DeviceID:      a.identity.DeviceID,
		UptimeSeconds: a.uptimeSeconds(),
		Mode:          a.mode,
		Hotspot: api.HotspotStatus{
			Running:    a.serviceIsActive(hostapdService),
			SSID:       h.SSID,
			Band:       h.Band,
			Channel:    channel,
			Security:   h.Security,
			Interface:  radio.HotspotInterfaceName,
			LANAddress: a.lan.Address(),
		},
		Radios:   a.radios,
		Clients:  a.connectedClients(),
		Warnings: a.warnings,
	}
	// Empty lists are sent as [] rather than null, which is easier for the UI.
	if status.Radios == nil {
		status.Radios = []radio.Radio{}
	}
	if status.Warnings == nil {
		status.Warnings = []string{}
	}
	return status
}

// connectedClients lists devices associated with the hotspot right now.
func (a *Agent) connectedClients() []stations.Station {
	dump, err := system.StationDump(a.runner, radio.HotspotInterfaceName)
	if err != nil {
		return []stations.Station{}
	}
	leases, _ := os.ReadFile(a.paths.DnsmasqLeases())
	clients := stations.Merge(stations.ParseStationDump(dump), stations.ParseLeases(string(leases)))
	if clients == nil {
		return []stations.Station{}
	}
	return clients
}

// serviceIsActive asks systemd whether a service is running.
func (a *Agent) serviceIsActive(unit string) bool {
	out, err := a.runner.Run("systemctl", "is-active", unit)
	return err == nil && strings.TrimSpace(out) == "active"
}

// boardModel reads the board name, e.g. "Raspberry Pi 4 Model B Rev 1.5".
func (a *Agent) boardModel() string {
	data, err := os.ReadFile(a.paths.DeviceTreeModel())
	if err != nil {
		return "unknown"
	}
	// The file ends with a zero byte, which we remove.
	return strings.TrimRight(string(data), "\x00\n")
}

// uptimeSeconds reads how long the device has been running.
func (a *Agent) uptimeSeconds() int {
	data, err := os.ReadFile(a.paths.ProcUptime())
	if err != nil {
		return 0
	}
	// The file looks like "12345.67 45678.90"; the first number is uptime.
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	seconds, _ := strconv.ParseFloat(fields[0], 64)
	return int(seconds)
}

// diagnosticCommands are included in the diagnostics export. Each helps
// answer a common "why isn't it working?" question.
var diagnosticCommands = [][]string{
	{"ip", "-brief", "address"},
	{"ip", "route"},
	{"iw", "dev"},
	{"iw", "reg", "get"},
	{"iw", "dev", "ap0", "station", "dump"},
	{"nft", "list", "ruleset"},
	{"systemctl", "--no-pager", "status", hostapdService, dnsmasqService, "rbd.service", "rbweb.service"},
	{"journalctl", "--no-pager", "-n", "400", "-u", "rbd.service", "-u", "rbweb.service",
		"-u", hostapdService, "-u", dnsmasqService},
	{"lsusb"},
}

// Diagnostics returns a plain-text report for troubleshooting. Passwords
// are replaced with <redacted> and MAC addresses are partly hidden, so the
// report is safe to share when asking for help.
func (a *Agent) Diagnostics() string {
	status := a.Status()

	a.mu.Lock()
	secretsToHide := []string{a.secrets.HotspotPassword, a.setupPassword, a.identity.SuggestedHotspotPassword}
	a.mu.Unlock()

	var report strings.Builder
	fmt.Fprintf(&report, "RasBeacon diagnostics\n")
	fmt.Fprintf(&report, "version: %s\nboard: %s\nmode: %s\nuptime: %ds\n", status.Version, status.BoardModel, status.Mode, status.UptimeSeconds)
	fmt.Fprintf(&report, "hotspot: ssid=%q band=%s channel=%d security=%s running=%v clients=%d\n",
		status.Hotspot.SSID, status.Hotspot.Band, status.Hotspot.Channel, status.Hotspot.Security,
		status.Hotspot.Running, len(status.Clients))
	for _, r := range status.Radios {
		fmt.Fprintf(&report, "radio: interface=%s driver=%s usb=%v usbId=%s ap=%v role=%s\n",
			r.Interface, r.Driver, r.IsUSB, r.USBID, r.SupportsAP, r.Role)
	}
	for _, w := range status.Warnings {
		fmt.Fprintf(&report, "warning: %s\n", w)
	}

	for _, cmd := range diagnosticCommands {
		fmt.Fprintf(&report, "\n===== %s =====\n", strings.Join(cmd, " "))
		out, err := a.runner.Run(cmd[0], cmd[1:]...)
		report.WriteString(out)
		if err != nil {
			fmt.Fprintf(&report, "(command failed: %v)\n", err)
		}
	}

	return Redact(report.String(), secretsToHide)
}

// macAddress matches MAC addresses like 3a:1f:22:aa:bb:cc.
var macAddress = regexp.MustCompile(`(?i)\b([0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}):[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}\b`)

// Redact hides secrets and the last half of MAC addresses in text.
func Redact(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "<redacted>")
		}
	}
	return macAddress.ReplaceAllString(text, "$1:xx:xx:xx")
}
