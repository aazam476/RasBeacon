package agent

// Extra wired ports for devices: a USB Ethernet adapter plugged into
// RasBeacon is a port of the hotspot network, so the built-in Ethernet port
// can carry the internet while wired devices still connect. Each one gets
// the same safety check as the Ethernet client port (checkWiredPort): it
// joins the hotspot network only when no DHCP server answers on it.

import (
	"os"
	"path/filepath"
	"slices"

	"rasbeacon/internal/api"
)

// uplinkDrivers are the drivers of USB network devices that bring the
// internet rather than take devices: phones sharing their connection and
// mobile broadband modems. They are never LAN ports.
var uplinkDrivers = map[string]bool{
	"rndis_host": true, "ipheth": true, "qmi_wwan": true, "cdc_mbim": true, "huawei_cdc_ncm": true,
}

// extraWiredPorts lists the wired network ports other than the built-in
// Ethernet port: physical (with a device behind them), Ethernet (type 1),
// not Wi-Fi, and not a phone or modem (see uplinkDrivers). Sorted by name.
func (a *Agent) extraWiredPorts() []string {
	entries, err := os.ReadDir(a.paths.SysClassNet())
	if err != nil {
		return nil
	}
	var ports []string
	for _, entry := range entries {
		name := entry.Name()
		dir := filepath.Join(a.paths.SysClassNet(), name)
		if name == ethernetInterface || a.readSysNet(name, "type") != "1" || !exists(filepath.Join(dir, "device")) ||
			exists(filepath.Join(dir, "wireless")) || exists(filepath.Join(dir, "phy80211")) {
			continue
		}
		if driver, err := os.Readlink(filepath.Join(dir, "device", "driver")); err == nil && uplinkDrivers[filepath.Base(driver)] {
			continue
		}
		ports = append(ports, name)
	}
	slices.Sort(ports)
	return ports
}

// exists reports whether a file or folder is there.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// checkLANPorts runs the safety check on each extra wired port, and
// forgets ports that were unplugged (the kernel takes them off the bridge
// itself). The uplink monitor calls it; the caller holds a.mu.
func (a *Agent) checkLANPorts() {
	if a.mode != api.ModeNormal {
		return
	}
	ports := a.extraWiredPorts()
	for name := range a.uplink.lanPorts {
		if !slices.Contains(ports, name) {
			delete(a.uplink.lanPorts, name)
		}
	}
	for _, name := range ports {
		if a.uplink.lanPorts == nil {
			a.uplink.lanPorts = map[string]*wiredPort{}
		}
		port, ok := a.uplink.lanPorts[name]
		if !ok {
			port = &wiredPort{}
			a.uplink.lanPorts[name] = port
			// A port seen for the first time may still be in the hotspot
			// network from before rbd restarted: the safety check runs
			// outside it. A new adapter also starts down, and needs to be up
			// to see a cable.
			_, _ = a.runner.Run("ip", "link", "set", "dev", name, "nomaster")
			if _, err := a.runner.Run("ip", "link", "set", "dev", name, "up"); err != nil {
				continue
			}
		}
		a.checkWiredPort(name, port)
	}
}

// bridgedWiredPorts are the wired ports in the hotspot network now: the
// Ethernet client port and the extra ports that passed the safety check.
func (a *Agent) bridgedWiredPorts() []string {
	var ports []string
	if a.uplink.ethernet.inBridge {
		ports = append(ports, ethernetInterface)
	}
	for _, name := range sortedKeys(a.uplink.lanPorts) {
		if a.uplink.lanPorts[name].inBridge {
			ports = append(ports, name)
		}
	}
	return ports
}

// lanPortStatus is the extra wired ports for the status page.
func (a *Agent) lanPortStatus() []api.LANPortStatus {
	list := []api.LANPortStatus{}
	for _, name := range sortedKeys(a.uplink.lanPorts) {
		port := a.uplink.lanPorts[name]
		list = append(list, api.LANPortStatus{
			Name:              name,
			CablePlugged:      a.readSysNet(name, "carrier") == "1",
			Connected:         port.inBridge,
			ForeignDHCPServer: port.foreignDHCPServer,
		})
	}
	return list
}

// sortedKeys are a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
