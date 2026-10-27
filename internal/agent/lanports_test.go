package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// addWiredPort fakes a network device in /sys/class/net: a wired port
// (type 1) with the given driver, plugged in or not.
func (e *testEnv) addWiredPort(name, driver string, plugged bool, changes string) {
	dir := filepath.Join(e.paths.SysClassNet(), name)
	mustWrite(e.t, filepath.Join(dir, "type"), "1")
	carrier := "0"
	if plugged {
		carrier = "1"
	}
	mustWrite(e.t, filepath.Join(dir, "carrier"), carrier)
	mustWrite(e.t, filepath.Join(dir, "carrier_changes"), changes)
	drivers := filepath.Join(e.t.TempDir(), "drivers", driver)
	if err := os.MkdirAll(drivers, 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "device"), 0o755); err != nil {
		e.t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(dir, "device", "driver"))
	if err := os.Symlink(drivers, filepath.Join(dir, "device", "driver")); err != nil {
		e.t.Fatal(err)
	}
}

// checkLANPorts runs one round of the extra ports' check, as the monitor
// would.
func (e *testEnv) checkLANPorts() {
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	e.agent.checkLANPorts()
}

func TestLANPorts_FoundAmongTheNetworkDevices(t *testing.T) {
	e := newClaimedEnv(t, true)
	e.addWiredPort("enx00e04c680001", "r8152", false, "0")
	e.addWiredPort("eth1", "ax88179_178a", false, "0")
	// A phone sharing its connection and a modem bring the internet: not
	// ports for devices.
	e.addWiredPort("usb0", "rndis_host", true, "1")
	e.addWiredPort("wwan0", "qmi_wwan", true, "1")
	// A Wi-Fi device and a virtual one (no device behind it) neither.
	e.addWiredPort("wlan9", "mt76x2u", true, "1")
	mustWrite(t, filepath.Join(e.paths.SysClassNet(), "wlan9", "phy80211", "name"), "phy9")
	mustWrite(t, filepath.Join(e.paths.SysClassNet(), "veth0", "type"), "1")

	e.agent.mu.Lock()
	ports := e.agent.extraWiredPorts()
	e.agent.mu.Unlock()
	if !slices.Equal(ports, []string{"enx00e04c680001", "eth1"}) {
		t.Errorf("ports = %v", ports)
	}
}

func TestLANPorts_JoinOnlyWhenNoDHCPServerAnswers(t *testing.T) {
	e := newClaimedEnv(t, true)
	const port = "enx00e04c680001"
	e.addWiredPort(port, "r8152", false, "0")

	// Found: taken out of any bridge and brought up; no cable, no check.
	e.checkLANPorts()
	if !e.runner.ran("ip link set dev "+port+" nomaster") || !e.runner.ran("ip link set dev "+port+" up") || e.probes != 0 {
		t.Fatalf("commands = %v, probes = %d", e.runner.commands, e.probes)
	}
	if s := e.agent.Status().LANPorts; len(s) != 1 || s[0].Name != port || s[0].CablePlugged || s[0].Connected {
		t.Errorf("status = %+v", s)
	}

	// A laptop is plugged in: no DHCP server answers, so the port joins.
	e.addWiredPort(port, "r8152", true, "1")
	e.checkLANPorts()
	if e.probes != 1 || !e.runner.ran("ip link set dev "+port+" master br-lan") {
		t.Fatal("the port should be checked and join the hotspot network")
	}
	if s := e.agent.Status().LANPorts; !s[0].CablePlugged || !s[0].Connected {
		t.Errorf("status = %+v", s)
	}
	e.agent.mu.Lock()
	bridged := e.agent.bridgedWiredPorts()
	e.agent.mu.Unlock()
	if !slices.Equal(bridged, []string{port}) {
		t.Errorf("bridged = %v", bridged)
	}

	// Moved to a hotel wall socket: a DHCP server answers, so it leaves.
	e.dhcpServer = "192.168.1.1"
	e.addWiredPort(port, "r8152", true, "3")
	e.checkLANPorts()
	if s := e.agent.Status().LANPorts; s[0].Connected || s[0].ForeignDHCPServer != "192.168.1.1" {
		t.Errorf("status = %+v", s)
	}

	// Unplugged adapter: forgotten.
	if err := os.RemoveAll(filepath.Join(e.paths.SysClassNet(), port)); err != nil {
		t.Fatal(err)
	}
	e.checkLANPorts()
	if s := e.agent.Status().LANPorts; len(s) != 0 {
		t.Errorf("status = %+v", s)
	}
}

func TestLANPorts_DevicesBehindThemAreListed(t *testing.T) {
	e := newClaimedEnv(t, true)
	const port = "enx00e04c680001"
	e.addWiredPort(port, "r8152", true, "1")
	e.runner.outputs["bridge -json fdb show br br-lan brport "+port] = `[{"mac":"aa:bb:cc:00:00:01","flags":[]}]`
	e.checkLANPorts()
	var found bool
	for _, c := range e.agent.Status().Clients {
		if c.MAC == "aa:bb:cc:00:00:01" && c.Wired {
			found = true
		}
	}
	if !found {
		t.Errorf("the laptop on the USB port should be listed as wired: %+v", e.agent.Status().Clients)
	}
}
