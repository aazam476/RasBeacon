// Package readings turns the Linux files that describe the hardware
// (/proc/stat, /proc/meminfo, /proc/cpuinfo, the temperature sensor, the
// Raspberry Pi firmware's throttling flags, ...) into the values shown on
// the Status and System pages.
//
// The functions only parse text, so they can be tested with sample files;
// the agent (internal/agent/readings.go) reads the files.
package readings

import (
	"fmt"
	"strconv"
	"strings"
)

// CPUTimes is the first line of /proc/stat: time spent in each state since
// boot, in clock ticks.
type CPUTimes struct {
	Total uint64
	Idle  uint64
}

// ParseCPUTimes reads the "cpu" line of /proc/stat:
//
//	cpu  4705 356 584 3699 23 23 0 0 0 0
//
// The columns are user, nice, system, idle, iowait, irq, softirq, steal,
// guest, guest_nice. Idle time is idle plus iowait (waiting for the disk
// isn't work either). Guest time is already counted in user, so it is left
// out of the total.
func ParseCPUTimes(procStat string) (CPUTimes, error) {
	for _, line := range strings.Split(procStat, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var t CPUTimes
		for i, field := range fields[1:] {
			if i >= 8 {
				break // guest columns
			}
			n, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return CPUTimes{}, fmt.Errorf("reading /proc/stat: %w", err)
			}
			t.Total += n
			if i == 3 || i == 4 {
				t.Idle += n
			}
		}
		return t, nil
	}
	return CPUTimes{}, fmt.Errorf("no cpu line in /proc/stat")
}

// CPUPercent is how busy the processor was between two readings, from 0 to
// 100 (all cores together).
func CPUPercent(before, after CPUTimes) float64 {
	total := float64(after.Total) - float64(before.Total)
	idle := float64(after.Idle) - float64(before.Idle)
	if total <= 0 {
		return 0
	}
	busy := 100 * (total - idle) / total
	return max(0, min(100, busy))
}

// Memory is what /proc/meminfo says about memory and swap, in kibibytes.
type Memory struct {
	TotalKB, AvailableKB    uint64
	SwapTotalKB, SwapFreeKB uint64
}

// UsedMB is memory in use (total minus what is available to programs), in
// mebibytes.
func (m Memory) UsedMB() float64 { return float64(m.TotalKB-m.AvailableKB) / 1024 }

// TotalMB is the memory size in mebibytes.
func (m Memory) TotalMB() float64 { return float64(m.TotalKB) / 1024 }

// SwapUsedMB is the swap space in use, in mebibytes.
func (m Memory) SwapUsedMB() float64 { return float64(m.SwapTotalKB-m.SwapFreeKB) / 1024 }

// SwapTotalMB is the swap space's size, in mebibytes.
func (m Memory) SwapTotalMB() float64 { return float64(m.SwapTotalKB) / 1024 }

// ParseMemory reads /proc/meminfo ("MemTotal:  3884332 kB" lines).
func ParseMemory(meminfo string) (Memory, error) {
	var m Memory
	found := 0
	for _, line := range strings.Split(meminfo, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(value), " kB"), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			m.TotalKB = n
			found++
		case "MemAvailable":
			m.AvailableKB = n
			found++
		case "SwapTotal":
			m.SwapTotalKB = n
		case "SwapFree":
			m.SwapFreeKB = n
		}
	}
	if found < 2 || m.AvailableKB > m.TotalKB || m.SwapFreeKB > m.SwapTotalKB {
		return Memory{}, fmt.Errorf("unexpected /proc/meminfo")
	}
	return m, nil
}

// RAMSize rounds the memory the kernel reports up to the size printed on
// the board (the firmware keeps some for itself): 3884332 kB is "4 GB".
func RAMSize(totalKB uint64) string {
	mb := float64(totalKB) / 1024
	for _, size := range []float64{512, 1024, 2048, 4096, 8192, 16384} {
		if mb <= size {
			if size < 1024 {
				return fmt.Sprintf("%.0f MB", size)
			}
			return fmt.Sprintf("%.0f GB", size/1024)
		}
	}
	return fmt.Sprintf("%.0f GB", mb/1024)
}

// cpuParts names the ARM processor cores Raspberry Pis use, by the "CPU
// part" number in /proc/cpuinfo.
var cpuParts = map[string]string{
	"0xd03": "Cortex-A53", // Pi 3, Zero 2 W
	"0xd08": "Cortex-A72", // Pi 4, 400, CM4
	"0xd0b": "Cortex-A76", // Pi 5, 500, CM5
}

// CPUModel describes the processor from /proc/cpuinfo, e.g. "4 × Cortex-A72".
func CPUModel(cpuinfo string) string {
	cores := 0
	part := ""
	for _, line := range strings.Split(cpuinfo, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "processor":
			cores++
		case "CPU part":
			part = strings.TrimSpace(value)
		}
	}
	name, known := cpuParts[part]
	if !known {
		name = "ARM"
	}
	if cores == 0 {
		return name
	}
	return fmt.Sprintf("%d × %s", cores, name)
}

// ParseMilliCelsius reads a thermal zone's "temp" file, e.g. "48750" for
// 48.75 °C.
func ParseMilliCelsius(text string) (float64, error) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("unexpected temperature %q", strings.TrimSpace(text))
	}
	return float64(n) / 1000, nil
}

// ThermalState describes a temperature in words. The Pi's firmware slows
// the processor down from 80 °C and more from 85 °C.
func ThermalState(celsius float64) string {
	switch {
	case celsius >= 80:
		return "Hot: slowing down"
	case celsius >= 70:
		return "Warm"
	default:
		return "Normal"
	}
}

// Throttled describes the Raspberry Pi firmware's throttling flags
// (/sys/devices/platform/soc/soc:firmware/get_throttled, the same number
// `vcgencmd get_throttled` prints) in words:
//
//	bit 0  under-voltage now        bit 16  under-voltage has happened
//	bit 1  frequency capped now     bit 17  frequency cap has happened
//	bit 2  throttled now            bit 18  throttling has happened
//	bit 3  soft temperature limit   bit 19  soft temperature limit has happened
//
// The answer is short: "No", "Yes, now", "Earlier, not now", with "(power
// too weak)" when under-voltage is involved, since that needs a better
// power supply rather than cooling.
func Throttled(text string) (string, error) {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "0x"))
	flags, err := strconv.ParseUint(text, 16, 32)
	if err != nil {
		return "", fmt.Errorf("unexpected throttling flags %q", text)
	}
	const now, before = 0b1110, 0b1110 << 16
	const powerNow, powerBefore = 1, 1 << 16
	answer := "No"
	switch {
	case flags&(now|powerNow) != 0:
		answer = "Yes, now"
	case flags&(before|powerBefore) != 0:
		answer = "Earlier, not now"
	}
	if flags&(powerNow|powerBefore) != 0 {
		answer += " (power too weak)"
	}
	return answer, nil
}

// LeaseLifetime reads the lease time (seconds) from a systemd-networkd DHCP
// lease file (/run/systemd/netif/leases/<interface number>), which has
// lines like "LIFETIME=86400". ok is false if there is none.
func LeaseLifetime(leaseFile string) (seconds int, ok bool) {
	for _, line := range strings.Split(leaseFile, "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "LIFETIME="); found {
			n, err := strconv.Atoi(value)
			return n, err == nil
		}
	}
	return 0, false
}

// ResolvectlDNS reads the servers from `resolvectl dns <interface>`, which
// prints "Link 3 (eth0): 192.168.1.1 fd00::1".
func ResolvectlDNS(output string) []string {
	_, servers, found := strings.Cut(output, "):")
	if !found {
		return nil
	}
	return strings.Fields(servers)
}

// Bytes formats a byte count as people read it, e.g. "1.48 GB", "820 MB".
func Bytes(n uint64) string {
	units := []string{"bytes", "KB", "MB", "GB", "TB"}
	value := float64(n)
	unit := 0
	for value >= 1000 && unit < len(units)-1 {
		value /= 1000
		unit++
	}
	switch {
	case unit == 0:
		return fmt.Sprintf("%d bytes", n)
	case value >= 100:
		return fmt.Sprintf("%.0f %s", value, units[unit])
	case value >= 10:
		return fmt.Sprintf("%.1f %s", value, units[unit])
	default:
		return fmt.Sprintf("%.2f %s", value, units[unit])
	}
}

// Duration formats a number of seconds as "23h 48m", "12m" or "40s".
func Duration(seconds int) string {
	switch {
	case seconds >= 3600:
		return fmt.Sprintf("%dh %02dm", seconds/3600, seconds%3600/60)
	case seconds >= 60:
		return fmt.Sprintf("%dm", seconds/60)
	default:
		return fmt.Sprintf("%ds", max(0, seconds))
	}
}
