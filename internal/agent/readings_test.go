package agent

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCounters fakes an interface's byte counters.
func (e *testEnv) writeCounters(iface string, rx, tx string) {
	dir := filepath.Join(e.paths.SysClassNet(), iface, "statistics")
	mustWrite(e.t, filepath.Join(dir, "rx_bytes"), rx)
	mustWrite(e.t, filepath.Join(dir, "tx_bytes"), tx)
}

func TestReadings_HardwareTrafficAndDevices(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }

	// The hardware.
	mustWrite(t, e.paths.ProcCPUInfo(), "processor\t: 0\nCPU part\t: 0xd08\n\nprocessor\t: 1\nCPU part\t: 0xd08\n")
	mustWrite(t, e.paths.ProcMeminfo(), "MemTotal: 3884332 kB\nMemAvailable: 3491116 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	mustWrite(t, filepath.Join(e.paths.SysClassThermal(), "thermal_zone0", "temp"), "48750\n")
	mustWrite(t, filepath.Join(e.paths.SysClassThermal(), "cooling_device0", "type"), "pwm-fan\n")
	mustWrite(t, e.paths.ThrottledFlags(), "0x0\n")
	mustWrite(t, e.paths.KernelRelease(), "6.12.34+rpt-rpi-v8\n")

	// First sample: counters at some value.
	mustWrite(t, e.paths.ProcStat(), "cpu  100 0 100 800 0 0 0 0 0 0\n")
	e.writeCounters("br-lan", "1000000", "5000000")
	e.runner.outputs["iw dev ap0 station dump"] = "Station 3a:1f:22:aa:bb:cc (on ap0)\n\tsignal:\t-48 dBm\n\trx bytes:\t1000\n\ttx bytes:\t2000\n"
	e.agent.mu.Lock()
	e.agent.sampleReadings()
	e.agent.mu.Unlock()

	// Five seconds later: 2.5 MB more to the devices, 0.625 MB from them.
	c.advance(5 * time.Second)
	mustWrite(t, e.paths.ProcStat(), "cpu  200 0 200 1400 0 0 0 0 0 0\n")
	e.writeCounters("br-lan", "1625000", "7500000")
	e.runner.outputs["iw dev ap0 station dump"] = "Station 3a:1f:22:aa:bb:cc (on ap0)\n\tsignal:\t-48 dBm\n\trx bytes:\t626000\n\ttx bytes:\t2502000\n"
	e.agent.mu.Lock()
	e.agent.sampleReadings()
	e.agent.mu.Unlock()

	s := e.agent.Status()
	sys := s.System
	if math.Round(sys.CPUPercent) != 25 || sys.CPUModel != "2 × Cortex-A72" || math.Round(sys.RAMUsedMB) != 384 ||
		sys.TemperatureC != 48.75 || sys.ThermalState != "Normal" || sys.Cooling != "Fan" || sys.SensorStatus != "OK" || sys.Throttling != "No" {
		t.Errorf("system = %+v", sys)
	}
	tr := s.Throughput
	if math.Round(tr.DownMbps) != 4 || math.Round(tr.UpMbps) != 1 || math.Round(tr.PeakMbps) != 5 || tr.SessionBytes != 9125000 {
		t.Errorf("throughput = %+v", tr)
	}
	if len(s.Clients) != 1 || math.Round(s.Clients[0].DownMbps) != 4 || math.Round(s.Clients[0].UpMbps) != 1 ||
		s.Clients[0].Band != "2.4GHz" || !s.Clients[0].InternetAccess {
		t.Errorf("clients = %+v", s.Clients)
	}
	if s.LAN.MaxClients != 231 || s.LAN.Subnet != "10.87.0.0/24" || s.LAN.DHCPRange != "10.87.0.20 - 10.87.0.250" {
		t.Errorf("lan = %+v", s.LAN)
	}
	if !strings.HasPrefix(s.Device.Kernel, "Linux 6.12.34+rpt-rpi-v8 ") || s.Device.ModelSpec != "4 GB RAM, 2 × Cortex-A72" {
		t.Errorf("device = %+v", s.Device)
	}
	if s.Speedtest.Provider != "Cloudflare" || s.Speedtest.Last != nil {
		t.Errorf("speedtest = %+v", s.Speedtest)
	}
	if s.EthernetDiagnostics != nil {
		t.Error("no Ethernet details while Wi-Fi or nothing carries the internet")
	}
}

func TestReadings_EthernetDetailsAndUplinkExtras(t *testing.T) {
	e := newClaimedEnv(t, true)
	e.setEthernetCable(true, "1")
	eth := filepath.Join(e.paths.SysClassNet(), "eth0")
	for file, value := range map[string]string{"speed": "1000", "duplex": "full", "mtu": "1500", "ifindex": "2", "address": "dc:a6:32:01:02:03"} {
		mustWrite(t, filepath.Join(eth, file), value)
	}
	e.writeCounters("eth0", "1480000000", "820000000")
	mustWrite(t, filepath.Join(eth, "statistics", "rx_errors"), "0")
	mustWrite(t, filepath.Join(eth, "statistics", "tx_errors"), "0")
	if err := os.Symlink("/sys/bus/platform/drivers/bcmgenet", filepath.Join(eth, "device", "driver")); err != nil {
		if err := os.MkdirAll(filepath.Join(eth, "device"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/sys/bus/platform/drivers/bcmgenet", filepath.Join(eth, "device", "driver")); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(e.paths.NetworkdLeases(), "2"), "ADDRESS=192.168.1.20\nLIFETIME=86400\n")
	e.runner.outputs["resolvectl dns eth0"] = "Link 2 (eth0): 192.168.1.1\n"
	e.fakeUplink("eth0", "192.168.1.20", "24", false)
	e.check()

	s := e.agent.Status()
	u := s.Uplink
	if u.SubnetMask != "255.255.255.0" || u.InterfaceMAC != "dc:a6:32:01:02:03" || u.Ethernet.LinkSpeed != "1 Gbps" || u.Ethernet.Duplex != "Full duplex" {
		t.Errorf("uplink = %+v", u)
	}
	d := s.EthernetDiagnostics
	if d == nil {
		t.Fatal("expected Ethernet details")
	}
	if d.CarrierState != "Link detected (1 Gbps, full duplex)" || d.Controller != "eth0 (bcmgenet)" || d.MTU != "1500 bytes" ||
		d.PacketStatistics != "RX 1.48\u00a0GB / TX 820\u00a0MB (0 errors)" || d.DNSServers != "192.168.1.1" ||
		!strings.HasSuffix(d.DHCPLease, "remaining") || d.PortDescription != "The Raspberry Pi's built-in Ethernet port." {
		t.Errorf("details = %+v", d)
	}
}

func TestReadings_WiFiAccessPoint(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 status"] =
		"bssid=aa:bb:cc:dd:ee:ff\nwpa_state=COMPLETED\nssid=Hotel\nfreq=5180\nkey_mgmt=WPA2-PSK\n"
	e.runner.outputs["iw dev wlan0 link"] = "\tfreq: 5180\n\trx bitrate: 1200.9 MBit/s 80MHz HE-MCS 11 HE-NSS 2\n" +
		"\ttx bitrate: 864.7 MBit/s 80MHz HE-MCS 8 HE-NSS 2\n"
	e.check()
	w := e.agent.Status().Uplink.WiFi
	if w.BSSID != "aa:bb:cc:dd:ee:ff" || w.FrequencyMHz != 5180 || w.Channel != 36 {
		t.Errorf("wifi = %+v", w)
	}
	if w.Standard != "wifi6" || w.WidthMHz != 80 || w.RxMbps != 1200.9 || w.TxMbps != 864.7 {
		t.Errorf("the connection's standard and rates = %+v", w)
	}

	// wpa_supplicant's agreed generation wins over the rates (here plain
	// 802.11n rates on a Wi-Fi 6E connection).
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 status"] =
		"bssid=aa:bb:cc:dd:ee:ff\nwpa_state=COMPLETED\nssid=Hotel\nfreq=6135\nkey_mgmt=SAE\nwifi_generation=6\n"
	e.runner.outputs["iw dev wlan0 link"] = "\tfreq: 6135\n\trx bitrate: 65.0 MBit/s MCS 7\n"
	e.check()
	if w := e.agent.Status().Uplink.WiFi; w.Standard != "wifi6e" || w.RxMbps != 65 {
		t.Errorf("with wifi_generation: %+v", w)
	}
	if wifiChannel(2437) != 6 || wifiChannel(2484) != 14 || wifiChannel(5975) != 5 {
		t.Error("channel numbers")
	}
}
