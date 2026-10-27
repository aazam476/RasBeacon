package agent

// This file builds the status page data and the diagnostics export.

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/render"
	"rasbeacon/internal/stations"
	"rasbeacon/internal/system"
	"rasbeacon/internal/version"
)

// Status collects everything shown on the status page.
func (a *Agent) Status() api.Status { return a.StatusFor("") }

// StatusFor is Status as seen by the device at address viewer (the browser
// showing the web UI): that device is marked IsThisDevice in the client
// list, so the web UI can warn before you disconnect yourself.
func (a *Agent) StatusFor(viewer string) api.Status {
	a.mu.Lock()
	defer a.mu.Unlock()

	h := a.cfg.Hotspot
	channel := h.Channel
	if channel == 0 {
		channel = config.DefaultChannel(h.Band)
	}

	// Asked once: the hotspot and the guest network run in the same hostapd.
	hostapdRunning := a.serviceIsActive(hostapdService)
	// The channel the radio really uses, which can differ from the saved
	// one (see api.HotspotStatus.ActualChannel). Only asked while running.
	actualChannel := 0
	if hostapdRunning {
		info, _ := a.runner.Run("iw", "dev", radio.HotspotInterfaceName, "info")
		actualChannel = channelNumber(info)
	}
	status := api.Status{
		Version:       version.Version,
		BoardModel:    a.boardModel(),
		DeviceID:      a.identity.DeviceID,
		UptimeSeconds: a.uptimeSeconds(),
		Mode:          a.mode,
		Plugins:       a.pluginSummaries(),
		Hotspot: api.HotspotStatus{
			Running:       hostapdRunning,
			SSID:          h.SSID,
			Band:          h.Band,
			Channel:       channel,
			ActualChannel: actualChannel,
			Security:      a.hotspotSecurity(), // what hostapd runs
			Interface:     radio.HotspotInterfaceName,
			LANAddress:    a.lan.Address(),
			Metered:       a.metered(), // also while a phone connection is in use
			Standard:      a.hotspotStandard(),
		},
		Guest:             a.guestStatus(hostapdRunning),
		Uplink:            a.uplink.status,
		VPN:               a.vpnStatus(),
		Portal:            a.portalStatus(),
		Update:            a.updateStatus(),
		Radios:            a.radios,
		RadioRolesPending: a.radioRolesSecondsLeft(),
		Clients:           a.connectedClients(),
		Warnings:          slices.Concat(a.warnings, a.vpnWarnings(), a.uplinkWarnings(), a.enterpriseWarnings(), a.serverCertWarnings(), a.guestWarnings(), a.hotspotWarnings()),

		Throughput:          a.readings.traffic,
		System:              a.systemStats(),
		Device:              a.deviceInfo(),
		Storage:             a.storageInfo(),
		LAN:                 a.lanInfo(),
		Speedtest:           a.speedtestStatus(),
		EthernetDiagnostics: a.ethernetDiagnostics(),
		LANPorts:            a.lanPortStatus(),
	}
	// Empty lists are sent as [] rather than null, which is easier for the UI.
	if status.Radios == nil {
		status.Radios = []radio.Radio{}
	}
	if status.Warnings == nil {
		status.Warnings = []string{}
	}
	for i := range status.Clients {
		a.setWelcome(&status.Clients[i], false)
		status.Clients[i].IsThisDevice = viewer != "" && status.Clients[i].IP == viewer
	}
	for i := range status.Guest.Clients {
		a.setWelcome(&status.Guest.Clients[i], true)
		status.Guest.Clients[i].IsThisDevice = viewer != "" && status.Guest.Clients[i].IP == viewer
	}
	return status
}

// connectedClients lists devices on the hotspot right now: on its Wi-Fi,
// and on the Ethernet client port.
func (a *Agent) connectedClients() []stations.Station {
	return append(a.stationsOn(radio.HotspotInterfaceName), a.wiredClients()...)
}

// wiredClients lists devices on the wired ports for devices (the Ethernet
// client port and USB Ethernet adapters), from the devices the hotspot
// bridge has seen behind those ports, with their DHCP leases.
// "Connected for" counts from when RasBeacon first saw each one.
func (a *Agent) wiredClients() []stations.Station {
	ports := a.bridgedWiredPorts()
	if len(ports) == 0 {
		a.uplink.wiredSince = nil
		return nil
	}
	var macs []string
	for _, port := range ports {
		out, err := a.runner.Run("bridge", "-json", "fdb", "show", "br", render.BridgeName, "brport", port)
		if err != nil {
			continue
		}
		found, err := stations.ParseBridgeFDB(out)
		if err != nil {
			slog.Warn("could not list the devices on a wired port", "port", port, "error", err)
			continue
		}
		macs = append(macs, found...)
	}
	seen := map[string]time.Time{}
	var wired []stations.Station
	for _, mac := range macs {
		since, ok := a.uplink.wiredSince[mac]
		if !ok {
			since = a.now()
		}
		seen[mac] = since
		wired = append(wired, stations.Station{MAC: mac, ConnectedSeconds: int(a.now().Sub(since).Seconds())})
	}
	a.uplink.wiredSince = seen
	leases, _ := os.ReadFile(a.paths.DnsmasqLeases())
	wired = stations.Merge(wired, stations.ParseLeases(string(leases)))
	for i := range wired {
		c := &wired[i]
		c.Wired = true
		a.setInternetAccess(c)
		a.setUsage(c)
	}
	return wired
}

// setInternetAccess fills in whether the owner paused the device.
func (a *Agent) setInternetAccess(c *stations.Station) {
	pausedAt, paused := a.cfg.Clients.Paused[c.MAC]
	c.InternetAccess = !paused
	if paused {
		c.PausedSeconds = max(1, int(a.now().Sub(pausedAt).Seconds()))
	}
}

// stationsOn lists devices associated with one of the hotspot radio's
// networks (ap0, or the guest network's interface).
func (a *Agent) stationsOn(iface string) []stations.Station {
	dump, err := system.StationDump(a.runner, iface)
	if err != nil {
		return []stations.Station{}
	}
	leases, _ := os.ReadFile(a.paths.DnsmasqLeases())
	clients := stations.Merge(stations.ParseStationDump(dump), stations.ParseLeases(string(leases)))
	if clients == nil {
		return []stations.Station{}
	}
	for i := range clients {
		c := &clients[i]
		c.Band = string(a.cfg.Hotspot.Band)
		rate := a.readings.stationRates[c.MAC]
		c.DownMbps, c.UpMbps = rate[0], rate[1]
		a.setInternetAccess(c)
		a.setUsage(c)
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
	{"ip", "-6", "route"},
	{"ip", "rule"},
	{"ip", "route", "show", "table", "51820"},
	// "wg show" never prints the private key.
	{"wg", "show"},
	{"networkctl", "--no-pager", "list"},
	{"resolvectl", "--no-pager", "status"},
	{"iw", "dev"},
	{"iw", "reg", "get"},
	{"iw", "dev", "ap0", "station", "dump"},
	{"iw", "dev", "guest0", "station", "dump"},
	// Is the hotspot's Wi-Fi still in its bridge, and what does hostapd
	// say about itself and each device (inactive time, keys)?
	{"bridge", "link"},
	{"hostapd_cli", "-p", "/run/hostapd", "-i", "ap0", "status"},
	{"hostapd_cli", "-p", "/run/hostapd", "-i", "ap0", "all_sta"},
	// The devices RasBeacon can still reach on the hotspot network.
	{"ip", "neigh", "show", "dev", "br-lan"},
	{"nft", "list", "ruleset"},
	{"cat", "/proc/sys/net/ipv4/ip_forward", "/proc/sys/net/ipv6/conf/all/forwarding"},
	{"systemctl", "--no-pager", "status", hostapdService, dnsmasqService, "rbd.service", "rbweb.service"},
	{"journalctl", "--no-pager", "-n", "400", "-u", "rbd.service", "-u", "rbweb.service",
		"-u", hostapdService, "-u", dnsmasqService, "-u", "rasbeacon-wpa@*", "-u", "systemd-networkd.service"},
	{"lsusb"},
}

// Diagnostics returns a plain-text report for troubleshooting. Passwords
// are replaced with <redacted> and MAC addresses are partly hidden, so the
// report is safe to share when asking for help.
func (a *Agent) Diagnostics() string {
	status := a.Status()

	a.mu.Lock()
	secretsToHide := []string{a.secrets.HotspotPassword, a.secrets.GuestPassword, a.setupPassword, a.identity.SuggestedHotspotPassword, a.secrets.UpdateNotifyURL}
	// DoH addresses can hold a personal account ID (and may appear in an
	// error from rbdoh).
	secretsToHide = append(secretsToHide, a.cfg.DNS.DoH...)
	for _, keys := range a.secrets.WireGuardKeys {
		secretsToHide = append(secretsToHide, keys.PrivateKey, keys.PresharedKey)
	}
	for _, password := range a.secrets.UplinkPasswords {
		secretsToHide = append(secretsToHide, password)
	}
	// Plugins' password settings, which an error from Podman could carry.
	for _, values := range a.secrets.Plugins {
		for _, password := range values {
			secretsToHide = append(secretsToHide, password)
		}
	}
	// Enterprise usernames are personal: show only a masked form.
	identities := map[string]string{}
	for _, n := range a.cfg.Uplink.Networks {
		if e := n.Enterprise; e != nil {
			for _, identity := range []string{e.Identity, e.AnonymousIdentity} {
				if identity != "" {
					identities[identity] = MaskIdentity(identity)
				}
			}
		}
	}
	wifiUplink := a.wifiUplinkInterface()
	plugins := a.pluginDiagnostics()
	// Devices the firewall blocks, and what the hotspot check repaired.
	paused := fmt.Sprintf("blocked devices: %d by the owner, %d over the data limit: %s\n",
		len(a.cfg.Clients.Paused), len(a.usage.overLimit), strings.Join(a.pausedMACs(), ", "))
	hotspotEvents := slices.Clone(a.hotspotCheck.events)
	// Times the hotspot stopped sending to its devices (hotspotstall.go).
	stalls := stallReport(a.readStalls())
	// Who may join: an allow list or a limit explains a device that can't.
	access := fmt.Sprintf("access: allow list %s (%d addresses), device limit %s\n",
		onOff(a.cfg.Access.AllowListOn), len(a.cfg.Access.Allowed), limitText(a.cfg.Access.MaxDevices))
	a.mu.Unlock()

	var report strings.Builder
	fmt.Fprintf(&report, "RasBeacon diagnostics\n")
	fmt.Fprintf(&report, "version: %s\nboard: %s\nmode: %s\nuptime: %ds\n", status.Version, status.BoardModel, status.Mode, status.UptimeSeconds)
	fmt.Fprintf(&report, "hotspot: ssid=%q band=%s channel=%d security=%s running=%v clients=%d metered=%v\n",
		status.Hotspot.SSID, status.Hotspot.Band, status.Hotspot.Channel, status.Hotspot.Security,
		status.Hotspot.Running, len(status.Clients), status.Hotspot.Metered)
	fmt.Fprintf(&report, "guest: enabled=%v running=%v ssid=%q clients=%d\n",
		status.Guest.Enabled, status.Guest.Running, status.Guest.SSID, len(status.Guest.Clients))
	report.WriteString(plugins)
	report.WriteString(access)
	report.WriteString(paused)
	if len(hotspotEvents) == 0 {
		report.WriteString("hotspot check: nothing repaired\n")
	}
	for _, event := range hotspotEvents {
		fmt.Fprintf(&report, "hotspot check: %s\n", event)
	}
	report.WriteString(stalls)
	u := status.Uplink
	fmt.Fprintf(&report, "uplink: connected=%v kind=%s interface=%s address=%s gateway=%s ipv6=%v\n",
		u.Connected, u.Kind, u.Interface, u.Address, u.Gateway, u.IPv6)
	fmt.Fprintf(&report, "wifi uplink: interface=%s state=%s ssid=%q band=%s signal=%d\n",
		u.WiFi.Interface, u.WiFi.State, u.WiFi.SSID, u.WiFi.Band, u.WiFi.SignalDBM)
	fmt.Fprintf(&report, "ethernet: present=%v role=%s cable=%v foreignDhcp=%s\n",
		u.Ethernet.Present, u.Ethernet.Role, u.Ethernet.CablePlugged, u.Ethernet.ForeignDHCPServer)
	sys := status.System
	fmt.Fprintf(&report, "hardware: cpu=%.0f%% (%s) ram=%.0f/%.0fMB temp=%.1fC throttling=%q storage=%s\n",
		sys.CPUPercent, sys.CPUModel, sys.RAMUsedMB, sys.RAMTotalMB, sys.TemperatureC, sys.Throttling, status.Storage.Medium)
	for _, r := range status.Radios {
		fmt.Fprintf(&report, "radio: interface=%s driver=%s usb=%v usbId=%s ap=%v maxAps=%d role=%s\n",
			r.Interface, r.Driver, r.IsUSB, r.USBID, r.SupportsAP, r.MaxAPs, r.Role)
	}
	for _, w := range status.Warnings {
		fmt.Fprintf(&report, "warning: %s\n", w)
	}

	commands := diagnosticCommands
	if wifiUplink != "" {
		commands = append(slices.Clone(commands), []string{"wpa_cli", "-p", a.paths.WPAControlDir(), "-i", wifiUplink, "status"})
	}
	for _, cmd := range commands {
		fmt.Fprintf(&report, "\n===== %s =====\n", strings.Join(cmd, " "))
		out, err := a.runner.Run(cmd[0], cmd[1:]...)
		report.WriteString(out)
		if err != nil {
			fmt.Fprintf(&report, "(command failed: %v)\n", err)
		}
	}

	text := report.String()
	for identity, masked := range identities {
		text = strings.ReplaceAll(text, identity, masked)
	}
	return Redact(text, secretsToHide)
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

// onOff writes a switch for the diagnostics report.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// limitText writes the device limit for the diagnostics report.
func limitText(maxDevices int) string {
	if maxDevices == 0 {
		return "none"
	}
	return fmt.Sprint(maxDevices)
}
