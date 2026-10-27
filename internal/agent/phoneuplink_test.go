package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
)

// addPhone fakes a phone or modem on USB: a network device with the given
// driver and the name the USB device gives.
func (e *testEnv) addPhone(iface, driver, product string) {
	e.addWiredPort(iface, driver, true, "1")
	// "device/../product": the fake device folder is a plain folder, so
	// its parent is the interface's own.
	mustWrite(e.t, filepath.Join(e.paths.SysClassNet(), iface, "product"), product+"\n")
	mustWrite(e.t, filepath.Join(e.paths.SysClassNet(), iface, "statistics", "rx_bytes"), "123456")
	mustWrite(e.t, filepath.Join(e.paths.SysClassNet(), iface, "statistics", "tx_bytes"), "7890")
}

func TestPhones_FoundByDriver(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.addPhone("usb0", "rndis_host", "Pixel 8")
	e.addPhone("eth1", "ipheth", "iPhone")
	e.addPhone("enx0c5b8f279a64", "cdc_ether", "HUAWEI_MOBILE")
	// A USB Ethernet adapter is a port for devices, not a phone.
	e.addWiredPort("enx00e04c680001", "r8152", false, "0")

	e.agent.mu.Lock()
	phones := e.agent.phoneUplinks()
	ports := e.agent.extraWiredPorts()
	e.agent.mu.Unlock()
	want := []phoneUplink{
		{Interface: "enx0c5b8f279a64", Kind: "modem", Name: "HUAWEI_MOBILE"},
		{Interface: "eth1", Kind: "iphone", Name: "iPhone"},
		{Interface: "usb0", Kind: "android", Name: "Pixel 8"},
	}
	if len(phones) != len(want) {
		t.Fatalf("phones = %+v", phones)
	}
	for i := range want {
		if phones[i] != want[i] {
			t.Errorf("phone %d = %+v, want %+v", i, phones[i], want[i])
		}
	}
	if len(ports) != 1 || ports[0] != "enx00e04c680001" {
		t.Errorf("ports for devices = %v", ports)
	}
}

func TestPhones_UsedAfterEthernetAndWiFiAndMetered(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.addPhone("usb0", "rndis_host", "Pixel 8")
	e.check()

	// networkd asks it for an address, with a route used last.
	network := e.readFile(e.networkdFile("00-rasbeacon-usb0.network"))
	if !strings.Contains(network, "Name=usb0") || !strings.Contains(network, "RouteMetric=900") {
		t.Errorf("networkd file:\n%s", network)
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), `"usb0"`) {
		t.Error("devices' traffic may leave through the phone")
	}

	// In use: the status says so, and devices are told it is metered.
	e.fakeUplink("usb0", "192.168.42.17", "24", false)
	e.check()
	u := e.agent.Status().Uplink
	if u.Kind != "phone" || len(u.Phones) != 1 || !u.Phones[0].Connected || u.Phones[0].ReceivedBytes != 123456 {
		t.Errorf("uplink = %+v", u)
	}
	if !e.agent.Status().Hotspot.Metered || !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "ANDROID_METERED") {
		t.Error("a phone connection should be metered")
	}

	// The owner turns that off.
	settings := e.agent.UplinkSettings()
	if settings.PhoneMetered == nil || !*settings.PhoneMetered {
		t.Fatalf("metered by default: %+v", settings.PhoneMetered)
	}
	off := false
	settings.PhoneMetered = &off
	if err := e.agent.UpdateUplinkSettings(settings); err != nil {
		t.Fatal(err)
	}
	if e.agent.Status().Hotspot.Metered || *e.agent.UplinkSettings().PhoneMetered {
		t.Error("the owner said it isn't metered")
	}
	// A request without the field keeps it.
	settings.PhoneMetered = nil
	if err := e.agent.UpdateUplinkSettings(settings); err != nil || *e.agent.UplinkSettings().PhoneMetered {
		t.Errorf("unchanged without the field: %v", err)
	}

	// Unplugged: its networkd file goes.
	if err := os.RemoveAll(filepath.Join(e.paths.SysClassNet(), "usb0")); err != nil {
		t.Fatal(err)
	}
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	if _, err := os.Stat(e.networkdFile("00-rasbeacon-usb0.network")); !os.IsNotExist(err) {
		t.Error("the phone's networkd file should be removed")
	}
	if u := e.agent.Status().Uplink; u.Kind != "wifi" || len(u.Phones) != 0 {
		t.Errorf("uplink = %+v", u)
	}
}

func TestPhones_MeteredFollowsTheConnection(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.addPhone("usb0", "rndis_host", "Pixel 8")
	e.fakeUplink("usb0", "192.168.42.17", "24", false)
	e.check()
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "ANDROID_METERED") {
		t.Fatal("metered while the phone is in use")
	}
	// Back on Wi-Fi: no longer metered.
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	if strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "ANDROID_METERED") {
		t.Error("not metered on Wi-Fi")
	}
	var status api.Status = e.agent.Status()
	if status.Hotspot.Metered {
		t.Error("the status shouldn't say metered")
	}
}
