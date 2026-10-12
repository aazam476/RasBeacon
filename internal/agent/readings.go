package agent

// This file collects the readings the Status and System pages show:
// processor load, memory, temperature, traffic, storage, the hotspot's
// network, and details of the Ethernet port. The parsing is in
// internal/readings; this file reads the files and keeps what needs two
// samples (processor load and traffic rates).
//
// sampleReadings runs every few seconds with the uplink check; the rest is
// read when the status is asked for.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/readings"
	"rasbeacon/internal/render"
	"rasbeacon/internal/stations"
	"rasbeacon/internal/system"
	"rasbeacon/internal/version"
)

// readingsState keeps the previous sample of the counters that become
// rates (processor time, bytes).
type readingsState struct {
	sampledAt  time.Time
	cpuBefore  readings.CPUTimes
	cpuPercent float64

	// Bytes through the hotspot and guest bridges at the last sample.
	lanRx, lanTx uint64
	traffic      api.Throughput

	// Per device (by MAC address): byte counters at the last sample, and
	// the rates worked out from them.
	stationBytes map[string][2]uint64
	stationRates map[string][2]float64
}

// sampleReadings takes one sample of the counters. The caller holds a.mu.
func (a *Agent) sampleReadings() {
	now := a.now()
	r := &a.readings
	elapsed := now.Sub(r.sampledAt).Seconds()
	first := r.sampledAt.IsZero()
	r.sampledAt = now

	// Processor load.
	if text, err := os.ReadFile(a.paths.ProcStat()); err == nil {
		if times, err := readings.ParseCPUTimes(string(text)); err == nil {
			if r.cpuBefore.Total > 0 {
				r.cpuPercent = readings.CPUPercent(r.cpuBefore, times)
			}
			r.cpuBefore = times
		}
	}

	// Traffic to and from the devices: what the bridges sent (towards the
	// devices) and received (from them).
	var rx, tx uint64
	for _, bridge := range []string{render.BridgeName, render.GuestBridgeName} {
		rx += a.readCounter(bridge, "rx_bytes")
		tx += a.readCounter(bridge, "tx_bytes")
	}
	if !first && elapsed > 0 && rx >= r.lanRx && tx >= r.lanTx {
		t := &r.traffic
		t.DownMbps = mbps(tx-r.lanTx, elapsed)
		t.UpMbps = mbps(rx-r.lanRx, elapsed)
		t.DownPeakMbps = math.Max(t.DownPeakMbps, t.DownMbps)
		t.UpPeakMbps = math.Max(t.UpPeakMbps, t.UpMbps)
		t.PeakMbps = math.Max(t.PeakMbps, t.DownMbps+t.UpMbps)
	}
	r.lanRx, r.lanTx = rx, tx
	r.traffic.SessionBytes = rx + tx

	// Each device's rate, from the radio's counters.
	rates := map[string][2]float64{}
	bytes := map[string][2]uint64{}
	for _, iface := range []string{"ap0", render.GuestInterface} {
		dump, err := system.StationDump(a.runner, iface)
		if err != nil {
			continue
		}
		for _, s := range stations.ParseStationDump(dump) {
			bytes[s.MAC] = [2]uint64{s.TxBytes, s.RxBytes}
			if before, ok := r.stationBytes[s.MAC]; ok && elapsed > 0 && s.TxBytes >= before[0] && s.RxBytes >= before[1] {
				rates[s.MAC] = [2]float64{mbps(s.TxBytes-before[0], elapsed), mbps(s.RxBytes-before[1], elapsed)}
			}
		}
	}
	r.stationBytes, r.stationRates = bytes, rates
}

// mbps turns a byte count over some seconds into megabits per second.
func mbps(bytes uint64, seconds float64) float64 {
	return float64(bytes) * 8 / seconds / 1e6
}

// readCounter reads one of an interface's statistics counters (0 if the
// interface doesn't exist).
func (a *Agent) readCounter(iface, counter string) uint64 {
	n, _ := strconv.ParseUint(a.readSysNet(iface, filepath.Join("statistics", counter)), 10, 64)
	return n
}

// systemStats reads the hardware readings.
func (a *Agent) systemStats() api.SystemStats {
	s := api.SystemStats{
		CPUPercent:   a.readings.cpuPercent,
		CPUModel:     "Unknown",
		Cooling:      "No fan found",
		SensorStatus: "No sensor found",
		ThermalState: "Unknown",
		Throttling:   "Unknown",
	}
	if text, err := os.ReadFile(a.paths.ProcCPUInfo()); err == nil {
		s.CPUModel = readings.CPUModel(string(text))
	}
	if text, err := os.ReadFile(a.paths.ProcMeminfo()); err == nil {
		if m, err := readings.ParseMemory(string(text)); err == nil {
			s.RAMUsedMB, s.RAMTotalMB = m.UsedMB(), m.TotalMB()
			s.SwapUsedMB, s.SwapTotalMB = m.SwapUsedMB(), m.SwapTotalMB()
		}
	}
	thermal := a.paths.SysClassThermal()
	if text, err := os.ReadFile(filepath.Join(thermal, "thermal_zone0", "temp")); err == nil {
		if c, err := readings.ParseMilliCelsius(string(text)); err == nil {
			s.TemperatureC, s.ThermalState, s.SensorStatus = c, readings.ThermalState(c), "OK"
		}
	}
	devices, _ := filepath.Glob(filepath.Join(thermal, "cooling_device*", "type"))
	for _, path := range devices {
		if kind, _ := os.ReadFile(path); strings.Contains(string(kind), "fan") {
			s.Cooling = "Fan"
		}
	}
	if text, err := os.ReadFile(a.paths.ThrottledFlags()); err == nil {
		if answer, err := readings.Throttled(string(text)); err == nil {
			s.Throttling = answer
		}
	}
	return s
}

// deviceInfo identifies the router.
func (a *Agent) deviceInfo() api.DeviceInfo {
	release := strings.TrimSpace(a.readFile(a.paths.KernelRelease()))
	arch := runtime.GOARCH
	if arch == "arm64" {
		arch = "aarch64" // what `uname -m` says
	}
	host, _ := os.Hostname()
	d := api.DeviceInfo{
		Hostname:      host + ".lan",
		KernelRelease: release,
		Kernel:        strings.Join(strings.Fields("Linux "+release+" "+arch), " "),
		Appliance:     "RasBeacon " + version.Version,
		BootDevice:    a.storageMedium(),
	}
	cpu := readings.CPUModel(a.readFile(a.paths.ProcCPUInfo()))
	if m, err := readings.ParseMemory(a.readFile(a.paths.ProcMeminfo())); err == nil {
		d.ModelSpec = readings.RAMSize(m.TotalKB) + " RAM, " + cpu
	} else {
		d.ModelSpec = cpu
	}
	return d
}

// readFile reads a small file, or returns "".
func (a *Agent) readFile(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

// partitionBytes returns the size of the partition behind an A/B slot link
// (e.g. /dev/disk/by-slot/active/system), and its block device name.
func (a *Agent) partitionBytes(slot, part string) (uint64, string) {
	target, err := filepath.EvalSymlinks(a.paths.SlotDevice(slot, part))
	if err != nil {
		return 0, ""
	}
	name := filepath.Base(target)
	sectors, _ := strconv.ParseUint(strings.TrimSpace(a.readFile(filepath.Join(a.paths.SysClassBlock(), name, "size"))), 10, 64)
	return sectors * 512, name
}

// storageMedium names what RasBeacon starts from: "microSD card", "eMMC",
// "USB drive" or "NVMe drive".
func (a *Agent) storageMedium() string {
	_, name := a.partitionBytes("active", "system")
	switch {
	case strings.HasPrefix(name, "mmcblk"):
		disk := strings.SplitN(name, "p", 2)[0]
		if strings.TrimSpace(a.readFile(filepath.Join(a.paths.SysClassBlock(), disk, "device", "type"))) == "MMC" {
			return "eMMC"
		}
		return "microSD card"
	case strings.HasPrefix(name, "sd"):
		return "USB drive"
	case strings.HasPrefix(name, "nvme"):
		return "NVMe drive"
	default:
		return "Unknown"
	}
}

// storageInfo describes RasBeacon's space: the boot and system partitions
// (both copies) and the data partition.
func (a *Agent) storageInfo() api.StorageInfo {
	const gb = 1e9
	var firmware uint64
	for _, slot := range []string{"active", "other"} {
		for _, part := range []string{"boot", "system"} {
			size, _ := a.partitionBytes(slot, part)
			firmware += size
		}
	}
	s := api.StorageInfo{
		FirmwareGB: float64(firmware) / gb,
		Medium:     a.storageMedium(),
		Filesystem: "Read-only A/B system",
		Health:     "unknown",
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(a.paths.DataDir(), &fs); err == nil {
		total := fs.Blocks * uint64(fs.Bsize)
		used := (fs.Blocks - fs.Bfree) * uint64(fs.Bsize)
		s.UserGB = float64(used) / gb
		s.TotalGB = float64(firmware+total) / gb
	}
	if fsType := a.mountType(a.paths.DataDir()); fsType != "" {
		s.Filesystem += ", " + fsType + " settings"
	}
	return s
}

// mountType returns the filesystem type of the mount that holds path.
func (a *Agent) mountType(path string) string {
	best, fsType := "", ""
	for _, line := range strings.Split(a.readFile(a.paths.ProcMounts()), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mount := fields[1]
		inside := path == mount || strings.HasPrefix(path, strings.TrimSuffix(mount, "/")+"/")
		if inside && len(mount) > len(best) {
			best, fsType = mount, fields[2]
		}
	}
	return fsType
}

// lanInfo describes the hotspot's network.
func (a *Agent) lanInfo() api.LANInfo {
	first, last, count := a.lan.DHCPRange()
	return api.LANInfo{MaxClients: count, Subnet: a.lan.Prefix.Masked().String(), DHCPRange: first + " - " + last}
}

// ethernetDiagnostics describes the Ethernet port while it carries the
// internet (nil otherwise).
func (a *Agent) ethernetDiagnostics() *api.EthernetDiagnostics {
	if a.uplink.status.Kind != "ethernet" {
		return nil
	}
	eth := ethernetInterface
	driver := filepath.Base(a.readLink(filepath.Join(a.paths.SysClassNet(), eth, "device", "driver")))
	d := &api.EthernetDiagnostics{
		CarrierState:    "Link detected",
		Controller:      eth,
		MTU:             a.readSysNet(eth, "mtu") + " bytes",
		DNSServers:      "None",
		DHCPLease:       "No lease",
		PortDescription: "A USB Ethernet adapter.",
		PollingInterval: fmt.Sprintf("%d seconds", int(UplinkCheckInterval.Seconds())),
	}
	if speed, duplex := a.ethernetLink(); speed != "" {
		d.CarrierState = fmt.Sprintf("Link detected (%s, %s)", speed, strings.ToLower(duplex))
	}
	if driver != "" && driver != "." {
		d.Controller = eth + " (" + driver + ")"
	}
	if driver == "bcmgenet" || driver == "macb" {
		d.PortDescription = "The Raspberry Pi's built-in Ethernet port."
	}
	errors := a.readCounter(eth, "rx_errors") + a.readCounter(eth, "tx_errors")
	d.PacketStatistics = fmt.Sprintf("RX %s / TX %s (%d errors)",
		readings.Bytes(a.readCounter(eth, "rx_bytes")), readings.Bytes(a.readCounter(eth, "tx_bytes")), errors)
	if out, err := a.runner.Run("resolvectl", "dns", eth); err == nil {
		if servers := readings.ResolvectlDNS(out); len(servers) > 0 {
			d.DNSServers = strings.Join(servers, ", ")
		}
	}
	leaseFile := filepath.Join(a.paths.NetworkdLeases(), a.readSysNet(eth, "ifindex"))
	if info, err := os.Stat(leaseFile); err == nil {
		if lifetime, ok := readings.LeaseLifetime(a.readFile(leaseFile)); ok {
			left := lifetime - int(a.now().Sub(info.ModTime()).Seconds())
			d.DHCPLease = readings.Duration(left) + " remaining"
		}
	}
	return d
}

// ethernetLink returns the Ethernet link's speed ("1 Gbps") and duplex
// ("Full duplex"), or "" without a link.
func (a *Agent) ethernetLink() (speed, duplex string) {
	if a.readSysNet(ethernetInterface, "carrier") != "1" {
		return "", ""
	}
	mbit, err := strconv.Atoi(a.readSysNet(ethernetInterface, "speed"))
	if err != nil || mbit <= 0 {
		return "", ""
	}
	speed = fmt.Sprintf("%d Mbps", mbit)
	if mbit >= 1000 && mbit%1000 == 0 {
		speed = fmt.Sprintf("%d Gbps", mbit/1000)
	}
	switch a.readSysNet(ethernetInterface, "duplex") {
	case "full":
		duplex = "Full duplex"
	case "half":
		duplex = "Half duplex"
	}
	return speed, duplex
}

// readLink returns a symlink's target, or "".
func (a *Agent) readLink(path string) string {
	target, _ := os.Readlink(path)
	return target
}

// subnetMask turns a prefix length into a dotted mask, e.g. 24 into
// "255.255.255.0".
func subnetMask(bits int) string {
	mask := uint32(0xffffffff) << (32 - bits)
	if bits == 0 {
		mask = 0
	}
	return fmt.Sprintf("%d.%d.%d.%d", byte(mask>>24), byte(mask>>16), byte(mask>>8), byte(mask))
}

// wifiChannel turns a frequency in MHz into its Wi-Fi channel number.
func wifiChannel(mhz int) int {
	switch {
	case mhz == 2484:
		return 14
	case mhz >= 2412 && mhz < 2484:
		return (mhz - 2407) / 5
	case mhz >= 5955:
		return (mhz - 5950) / 5 // 6 GHz
	case mhz >= 5000:
		return (mhz - 5000) / 5
	default:
		return 0
	}
}
