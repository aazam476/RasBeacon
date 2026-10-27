package agent

// Internet from a phone or a USB modem: a phone sharing its connection
// over USB ("USB tethering" on Android, "Personal Hotspot" over USB on an
// iPhone) or a "HiLink" modem shows up as a USB network card with a DHCP
// server of its own. RasBeacon uses it like a cable to the internet, after
// Ethernet and Wi-Fi (PhoneRouteMetric), and treats it as metered.

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"rasbeacon/internal/api"
)

// phoneDrivers are the drivers of USB network devices that bring the
// internet, with what kind of device each usually is.
var phoneDrivers = map[string]string{
	"ipheth":         "iphone",
	"rndis_host":     "android",
	"cdc_ncm":        "android",
	"cdc_ether":      "modem",
	"huawei_cdc_ncm": "modem",
}

// phoneUplink is one phone or modem found among the network devices.
type phoneUplink struct {
	Interface string
	Kind      string
	Name      string
}

// phoneUplinks lists the phones and modems plugged in, by interface name.
func (a *Agent) phoneUplinks() []phoneUplink {
	entries, err := os.ReadDir(a.paths.SysClassNet())
	if err != nil {
		return nil
	}
	var list []phoneUplink
	for _, entry := range entries {
		dir := filepath.Join(a.paths.SysClassNet(), entry.Name())
		driver, err := os.Readlink(filepath.Join(dir, "device", "driver"))
		if err != nil {
			continue
		}
		kind, ok := phoneDrivers[filepath.Base(driver)]
		if !ok {
			continue
		}
		// The USB device's own name is next to its network interface's
		// folder ("device/.." without cleaning the path, so the kernel
		// follows the link first).
		name, _ := os.ReadFile(dir + "/device/../product")
		list = append(list, phoneUplink{Interface: entry.Name(), Kind: kind, Name: strings.TrimSpace(string(name))})
	}
	slices.SortFunc(list, func(x, y phoneUplink) int { return strings.Compare(x.Interface, y.Interface) })
	return list
}

// phoneInterfaces are the phones' and modems' interface names.
func (a *Agent) phoneInterfaces() []string {
	var names []string
	for _, p := range a.phoneUplinks() {
		names = append(names, p.Interface)
	}
	return names
}

// observePhones describes the phones and modems for the status page.
// addressed is the uplinks that have an IPv4 address.
func (a *Agent) observePhones(addressed map[string]bool) []api.PhoneUplinkStatus {
	list := []api.PhoneUplinkStatus{}
	for _, p := range a.phoneUplinks() {
		rx, _ := strconv.ParseUint(a.readSysNet(p.Interface, "statistics/rx_bytes"), 10, 64)
		tx, _ := strconv.ParseUint(a.readSysNet(p.Interface, "statistics/tx_bytes"), 10, 64)
		list = append(list, api.PhoneUplinkStatus{
			Interface: p.Interface, Kind: p.Kind, Name: p.Name, Connected: addressed[p.Interface],
			ReceivedBytes: rx, SentBytes: tx,
		})
	}
	return list
}

// checkPhones notices phones and modems being plugged in or out, and
// updates networkd (to ask them for an address) and the firewall (which
// lets devices' traffic out through them). The caller holds a.mu.
func (a *Agent) checkPhones() {
	now := strings.Join(a.phoneInterfaces(), " ")
	if now == a.uplink.phones {
		return
	}
	a.uplink.phones = now
	if err := a.applyNetworkdConfig(); err != nil {
		slog.Error("could not set up the phone or modem connection", "error", err)
	}
	if err := a.applyFirewall(); err != nil {
		slog.Error("could not update the firewall for the phone or modem", "error", err)
	}
}

// metered reports whether devices are asked to hold back big downloads:
// the owner's setting, or a phone or modem connection in use (unless the
// owner turned that off). The caller holds a.mu.
func (a *Agent) metered() bool {
	return a.cfg.Hotspot.Metered || (mobile(a.uplink.status.Kind) && !a.cfg.Uplink.PhoneNotMetered)
}

// mobile reports whether an uplink kind is a phone or a modem, which
// usually count data.
func mobile(kind string) bool { return kind == "phone" || kind == "modem" }
