package agent

// This file notices when the hotspot has "stalled": it is running and its
// devices stay connected, but it no longer sends them any data. The
// devices then have no internet, while devices on the Ethernet client port
// keep theirs.
//
// It was seen on the Realtek RTL8812BU USB adapter (driver rtw88_8822bu,
// for example the TP-Link Archer T3U), with WPA2 and WPA3 alike, after the
// hotspot had run for a while. Restarting hostapd brings it back. The cause
// in the driver isn't known yet, so RasBeacon does two things:
//
//  1. It repairs it: the hotspot is restarted (at most once per
//     hotspotRestartPause, like the other repairs in hotspotcheck.go).
//  2. It keeps a record of each stall in /persistent, so it survives a
//     power-off, and the diagnostics report lists the latest ones. Each
//     record says how long the hotspot had been up and what happened
//     just before, which is what finding the cause needs.
//
// How a stall is recognized, once a minute (with the other checks):
//
//   - The radio counts the packets it received from each device ("rx
//     packets") and sent to it ("tx packets"). A device whose rx count grew
//     while its tx count didn't move at all for a whole minute is a
//     suspect: it keeps talking, and the Pi says nothing back.
//   - An idle phone can look like that too, so each suspect gets one ping.
//     A device that answers is fine. So is one whose tx count moved while
//     it was pinged: the radio could send to it.
//   - A suspect that doesn't answer, and to which nothing was sent, means
//     the hotspot has stalled.

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/render"
	"rasbeacon/internal/stations"
	"rasbeacon/internal/system"
)

const (
	// stallsKept is how many stalls the record file keeps.
	stallsKept = 20
	// stallSuspectsPinged is the most devices pinged in one check, so the
	// check stays quick (a ping that gets no answer waits a second).
	stallSuspectsPinged = 3
	// stallHistory is how far back the record's log lines go.
	stallHistory = 10 * time.Minute
	// stallLogLines and stallKernelLines cap the lines kept per record.
	stallLogLines    = 80
	stallKernelLines = 15
)

// stationCounters is what the check remembers of one device between two
// checks.
type stationCounters struct {
	iface            string // "ap0" or the guest network's interface
	connectedSeconds int
	rxPackets        uint64
	txPackets        uint64
	mfp              bool
}

// hotspotStall is one record in the stall file.
type hotspotStall struct {
	Time time.Time `json:"time"`
	// UptimeSeconds is how long the Pi had been running, and
	// HotspotUptimeSeconds how long hostapd had (-1 if unknown).
	UptimeSeconds        int `json:"uptimeSeconds"`
	HotspotUptimeSeconds int `json:"hotspotUptimeSeconds"`
	// The hotspot's radio and settings.
	Driver   string `json:"driver"`
	USBID    string `json:"usbId,omitempty"`
	Security string `json:"security"`
	Band     string `json:"band"`
	// Channel is the saved channel (0 = automatic); RadioChannel is the
	// "channel" line from `iw dev ap0 info`, the one actually in use.
	Channel      int    `json:"channel"`
	RadioChannel string `json:"radioChannel"`
	// Devices lists every connected device, stalled or not.
	Devices []stalledDevice `json:"devices"`
	// Neighbors is `ip neigh show dev br-lan`: whether the Pi can still
	// reach each device's address.
	Neighbors string `json:"neighbors"`
	// Kernel is the last kernel messages; Before is the log of the
	// minutes before (setting saves, devices joining, restarts).
	Kernel []string `json:"kernel"`
	Before []string `json:"before"`
	// Restarted is false when the hotspot had just been restarted, so the
	// check waited instead of restarting it again.
	Restarted bool `json:"restarted"`
}

// stalledDevice is one device in a stall record.
type stalledDevice struct {
	MAC              string `json:"mac"`
	Interface        string `json:"interface"`
	ConnectedSeconds int    `json:"connectedSeconds"`
	RxPackets        uint64 `json:"rxPackets"`
	TxPackets        uint64 `json:"txPackets"`
	MFP              bool   `json:"mfp"`
	// Stalled marks the devices that showed the stall.
	Stalled bool `json:"stalled"`
}

// checkHotspotStall looks for a stall, as described at the top of this
// file, and repairs it. The caller holds a.mu.
func (a *Agent) checkHotspotStall() {
	before := a.hotspotCheck.counters
	now := a.readStationCounters()
	a.hotspotCheck.counters = now

	// 1. Suspects: devices that kept sending for a whole minute while the
	//    Pi sent them nothing.
	var suspects []string
	for mac, current := range now {
		previous, seen := before[mac]
		if !seen || current.connectedSeconds < previous.connectedSeconds {
			continue // new, or it joined again: no full minute to compare
		}
		if current.rxPackets > previous.rxPackets && current.txPackets == previous.txPackets {
			suspects = append(suspects, mac)
		}
	}
	if len(suspects) == 0 {
		return
	}
	slices.Sort(suspects) // the same order every time, for the tests
	if len(suspects) > stallSuspectsPinged {
		suspects = suspects[:stallSuspectsPinged]
	}

	// 2. Ping each suspect once. One that answers is fine.
	addresses := a.leaseAddresses()
	var silent []string
	for _, mac := range suspects {
		address := addresses[mac]
		if address == "" {
			continue // no address to ping: can't tell
		}
		if _, err := a.runner.Run("ping", "-c", "1", "-W", "1", address); err == nil {
			continue
		}
		silent = append(silent, mac)
	}
	if len(silent) == 0 {
		return
	}

	// 3. If the radio sent a silent device anything during the ping, it
	//    still delivers (the device may just be asleep).
	after := a.readStationCounters()
	var stalled []string
	for _, mac := range silent {
		if current, ok := after[mac]; ok && current.txPackets == now[mac].txPackets {
			stalled = append(stalled, mac)
		}
	}
	if len(stalled) == 0 {
		return
	}

	// 4. A stall: record it, then restart the hotspot.
	record := a.describeStall(after, stalled)
	record.Restarted = a.restartHotspot(fmt.Sprintf(
		"the hotspot stopped sending data to %d of its devices, which kept sending to it", len(stalled)))
	a.saveStall(record)
}

// readStationCounters reads each connected device's counters from the
// radio, on the hotspot and (when it is on) the guest network.
func (a *Agent) readStationCounters() map[string]stationCounters {
	interfaces := []string{radio.HotspotInterfaceName}
	if a.guestActive() {
		interfaces = append(interfaces, render.GuestInterface)
	}
	counters := map[string]stationCounters{}
	for _, iface := range interfaces {
		dump, err := system.StationDump(a.runner, iface)
		if err != nil {
			continue
		}
		for _, s := range stations.ParseStationDump(dump) {
			counters[s.MAC] = stationCounters{
				iface:            iface,
				connectedSeconds: s.ConnectedSeconds,
				rxPackets:        s.RxPackets,
				txPackets:        s.TxPackets,
				mfp:              s.MFP,
			}
		}
	}
	return counters
}

// leaseAddresses maps each device's MAC address to the IPv4 address
// dnsmasq gave it.
func (a *Agent) leaseAddresses() map[string]string {
	leases, _ := os.ReadFile(a.paths.DnsmasqLeases())
	addresses := map[string]string{}
	for _, lease := range stations.ParseLeases(string(leases)) {
		addresses[lease.MAC] = lease.IP
	}
	return addresses
}

// describeStall gathers everything a stall record holds. The caller holds
// a.mu.
func (a *Agent) describeStall(counters map[string]stationCounters, stalled []string) hotspotStall {
	h := a.cfg.Hotspot
	record := hotspotStall{
		Time:                 a.now().UTC(),
		UptimeSeconds:        a.uptimeSeconds(),
		HotspotUptimeSeconds: a.hostapdUptimeSeconds(),
		Security:             string(h.Security),
		Band:                 string(h.Band),
		Channel:              h.Channel,
	}
	if r := a.hotspotRadio(); r != nil {
		record.Driver, record.USBID = r.Driver, r.USBID
	}
	info, _ := a.runner.Run("iw", "dev", radio.HotspotInterfaceName, "info")
	record.RadioChannel = channelLine(info)

	macs := make([]string, 0, len(counters))
	for mac := range counters {
		macs = append(macs, mac)
	}
	slices.Sort(macs)
	for _, mac := range macs {
		c := counters[mac]
		record.Devices = append(record.Devices, stalledDevice{
			MAC:              mac,
			Interface:        c.iface,
			ConnectedSeconds: c.connectedSeconds,
			RxPackets:        c.rxPackets,
			TxPackets:        c.txPackets,
			MFP:              c.mfp,
			Stalled:          slices.Contains(stalled, mac),
		})
	}

	record.Neighbors, _ = a.runner.Run("ip", "neigh", "show", "dev", render.BridgeName)
	kernel, _ := a.runner.Run("dmesg")
	record.Kernel = lastLines(kernel, stallKernelLines)
	since := "@" + strconv.FormatInt(a.now().Add(-stallHistory).Unix(), 10)
	log, _ := a.runner.Run("journalctl", "--no-pager", "-o", "short-iso", "--since", since,
		"-u", "rbd.service", "-u", hostapdService, "-u", dnsmasqService)
	record.Before = lastLines(log, stallLogLines)
	return record
}

// hostapdUptimeSeconds says how long hostapd has been running, or -1 if
// that can't be read. systemd gives the time it started in microseconds
// since boot, which is compared with the Pi's uptime.
func (a *Agent) hostapdUptimeSeconds() int {
	out, err := a.runner.Run("systemctl", "show", "-p", "ActiveEnterTimestampMonotonic", "--value", hostapdService)
	if err != nil {
		return -1
	}
	startedMicros, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil || startedMicros <= 0 {
		return -1
	}
	return a.uptimeSeconds() - int(startedMicros/1_000_000)
}

// channelLine finds the "channel 40 (5200 MHz), width: 80 MHz, …" line in
// `iw dev <iface> info`.
func channelLine(info string) string {
	for _, line := range strings.Split(info, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "channel ") {
			return line
		}
	}
	return ""
}

// channelNumber reads the channel number from `iw dev ... info`, e.g. 40
// from "channel 40 (5200 MHz), width: 80 MHz, center1: 5210 MHz". It
// returns 0 when there is no channel line (the interface isn't running).
func channelNumber(info string) int {
	fields := strings.Fields(channelLine(info))
	if len(fields) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(fields[1])
	return n
}

// lastLines returns the last n non-empty lines of text.
func lastLines(text string, n int) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// readStalls reads the stall records, oldest first (none if the file is
// missing or unreadable).
func (a *Agent) readStalls() []hotspotStall {
	var stalls []hotspotStall
	if err := config.ReadJSON(a.paths.HotspotStallsFile(), &stalls); err != nil {
		return nil
	}
	return stalls
}

// saveStall adds a record to the stall file, keeping the latest
// stallsKept.
func (a *Agent) saveStall(record hotspotStall) {
	stalls := append(a.readStalls(), record)
	if extra := len(stalls) - stallsKept; extra > 0 {
		stalls = stalls[extra:]
	}
	if err := config.WriteJSON(a.paths.HotspotStallsFile(), stalls, 0o600); err != nil {
		slog.Warn("couldn't save the hotspot stall record", "err", err)
	}
}

// stallReport writes the stall records for the diagnostics report, newest
// first.
func stallReport(stalls []hotspotStall) string {
	if len(stalls) == 0 {
		return "hotspot stalls: none recorded\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "hotspot stalls: %d recorded (newest first)\n", len(stalls))
	for i := len(stalls) - 1; i >= 0; i-- {
		s := stalls[i]
		fmt.Fprintf(&b, "\n----- stall at %s -----\n", s.Time.Format(time.RFC3339))
		fmt.Fprintf(&b, "uptime=%ds hotspotUptime=%ds restarted=%v\n", s.UptimeSeconds, s.HotspotUptimeSeconds, s.Restarted)
		fmt.Fprintf(&b, "driver=%s usbId=%s security=%s band=%s channel=%d radio: %s\n",
			s.Driver, s.USBID, s.Security, s.Band, s.Channel, s.RadioChannel)
		for _, d := range s.Devices {
			fmt.Fprintf(&b, "device %s on %s: connected=%ds rxPackets=%d txPackets=%d mfp=%v stalled=%v\n",
				d.MAC, d.Interface, d.ConnectedSeconds, d.RxPackets, d.TxPackets, d.MFP, d.Stalled)
		}
		fmt.Fprintf(&b, "neighbors:\n%s", strings.TrimRight(s.Neighbors, "\n")+"\n")
		b.WriteString("kernel:\n")
		for _, line := range s.Kernel {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("before:\n")
		for _, line := range s.Before {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}
