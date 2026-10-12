package readings

import (
	"math"
	"testing"
)

func TestCPUPercent(t *testing.T) {
	before, err := ParseCPUTimes("cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 2 3 4\n")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := ParseCPUTimes("cpu  200 0 200 1300 200 0 0 0 50 0\n")
	// 200 busy ticks out of 900, guest (50) ignored: 22.2%.
	if got := CPUPercent(before, after); math.Abs(got-200.0/900*100) > 0.01 {
		t.Errorf("got %.2f", got)
	}
	if CPUPercent(after, after) != 0 {
		t.Error("no time passed should read 0")
	}
	if _, err := ParseCPUTimes("nothing"); err == nil {
		t.Error("expected an error")
	}
}

func TestParseMemory(t *testing.T) {
	m, err := ParseMemory("MemTotal:        3884332 kB\nMemFree:          100000 kB\nMemAvailable:    3491116 kB\n" +
		"SwapTotal:        524284 kB\nSwapFree:         512000 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if math.Round(m.UsedMB()) != 384 || math.Round(m.TotalMB()) != 3793 || math.Round(m.SwapUsedMB()) != 12 || math.Round(m.SwapTotalMB()) != 512 {
		t.Errorf("got %+v", m)
	}
	if RAMSize(m.TotalKB) != "4 GB" || RAMSize(950000) != "1 GB" || RAMSize(400000) != "512 MB" {
		t.Error("RAM sizes")
	}
	if _, err := ParseMemory("MemTotal: 10 kB\n"); err == nil {
		t.Error("expected an error without MemAvailable")
	}
}

func TestCPUModel(t *testing.T) {
	info := "processor\t: 0\nCPU part\t: 0xd08\n\nprocessor\t: 1\nCPU part\t: 0xd08\n\nprocessor\t: 2\n\nprocessor\t: 3\n"
	if got := CPUModel(info); got != "4 × Cortex-A72" {
		t.Errorf("got %q", got)
	}
	if got := CPUModel("processor : 0\n"); got != "1 × ARM" {
		t.Errorf("got %q", got)
	}
}

func TestTemperatureAndThrottling(t *testing.T) {
	c, err := ParseMilliCelsius("48750\n")
	if err != nil || c != 48.75 || ThermalState(c) != "Normal" || ThermalState(82) != "Hot: slowing down" {
		t.Errorf("got %v %v", c, err)
	}
	for flags, want := range map[string]string{
		"0x0":     "No",
		"0x4":     "Yes, now",
		"0x40000": "Earlier, not now",
		"0x50005": "Yes, now (power too weak)",
		"0x10000": "Earlier, not now (power too weak)",
	} {
		if got, err := Throttled(flags); err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", flags, got, err, want)
		}
	}
	if _, err := Throttled("zz"); err == nil {
		t.Error("expected an error")
	}
}

func TestLeaseDNSAndFormatting(t *testing.T) {
	if s, ok := LeaseLifetime("# This is private data.\nADDRESS=192.168.1.20\nLIFETIME=86400\n"); !ok || s != 86400 {
		t.Errorf("got %d %v", s, ok)
	}
	if _, ok := LeaseLifetime("ADDRESS=1.2.3.4\n"); ok {
		t.Error("no lifetime")
	}
	if got := ResolvectlDNS("Link 3 (eth0): 192.168.1.1 fd00::1\n"); len(got) != 2 || got[1] != "fd00::1" {
		t.Errorf("got %v", got)
	}
	for n, want := range map[uint64]string{512: "512 bytes", 1_480_000_000: "1.48 GB", 820_000_000: "820 MB", 14_200_000: "14.2 MB"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
	if Duration(85680) != "23h 48m" || Duration(720) != "12m" || Duration(40) != "40s" {
		t.Error("durations")
	}
}
