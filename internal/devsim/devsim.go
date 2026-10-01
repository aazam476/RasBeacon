// Package devsim makes rbd run on a normal computer without a Raspberry Pi,
// for developing and testing the web UI.
//
// Start rbd with `-dev /tmp/rasbeacon-dev` and it will:
//   - keep all its files under that directory instead of /,
//   - pretend there is a built-in radio, a USB Archer T3U and an Ethernet
//     port with no cable,
//   - pretend the built-in radio is connected to a café's Wi-Fi with
//     internet (IPv4 only), and that a scan finds a few networks,
//   - pretend a WireGuard tunnel, once set up, works,
//   - log the commands it would run (ip, iw, systemctl, ...) instead of
//     running them, and answer with made-up output (two connected phones).
//
// Nothing on your computer's network is changed.
package devsim

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rasbeacon/internal/dhcpprobe"
	"rasbeacon/internal/paths"
	"rasbeacon/internal/portal"
)

// Setup prepares a fake device under root and returns its paths.
func Setup(root string) (paths.Paths, error) {
	p := paths.Paths{Root: root}

	// Fake /sys/class/net with a built-in radio (wlan0) and a USB radio (wlan1).
	radios := []struct{ iface, phy, device string }{
		{"wlan0", "phy0", "platform/mmc1/mmc1:0001:1"},
		{"wlan1", "phy1", "usb1/1-1/1-1:1.0"},
	}
	for _, r := range radios {
		ifaceDir := filepath.Join(p.SysClassNet(), r.iface)
		if err := writeFile(filepath.Join(ifaceDir, "phy80211", "name"), r.phy); err != nil {
			return p, err
		}
		device := filepath.Join(root, "sys", "devices", r.device)
		if err := os.MkdirAll(device, 0o755); err != nil {
			return p, err
		}
		link := filepath.Join(ifaceDir, "device")
		if _, err := os.Lstat(link); err != nil {
			if err := os.Symlink(device, link); err != nil {
				return p, err
			}
		}
	}
	usbDevice := filepath.Join(root, "sys", "devices", "usb1", "1-1")
	if err := writeFile(filepath.Join(usbDevice, "idVendor"), "2357"); err != nil {
		return p, err
	}
	if err := writeFile(filepath.Join(usbDevice, "idProduct"), "012d"); err != nil {
		return p, err
	}
	// Ethernet port, no cable plugged in.
	if err := writeFile(filepath.Join(p.SysClassNet(), "eth0", "carrier"), "0"); err != nil {
		return p, err
	}
	if err := writeFile(filepath.Join(p.SysClassNet(), "eth0", "carrier_changes"), "1"); err != nil {
		return p, err
	}
	if err := writeFile(p.DeviceTreeModel(), "Simulated Raspberry Pi (devsim)\x00"); err != nil {
		return p, err
	}
	if err := writeFile(p.ProcUptime(), "3725.00 1000.00"); err != nil {
		return p, err
	}
	if err := os.MkdirAll(p.BootConfigDir(), 0o755); err != nil {
		return p, err
	}
	// Two phones with DHCP leases.
	leases := "1790800000 3a:1f:22:aa:bb:cc 10.87.0.23 Alis-iPhone *\n" +
		"1790800000 8e:00:11:22:33:44 10.87.0.24 Pixel-8 *\n"
	if err := writeFile(p.DnsmasqLeases(), leases); err != nil {
		return p, err
	}
	return p, nil
}

// Runner pretends to run commands: it logs them and returns canned output.
type Runner struct{}

const iwPhyInfo = "Wiphy phy1\n\tSupported interface modes:\n\t\t * managed\n\t\t * AP\n\t\t * monitor\n"

const stationDump = `Station 3a:1f:22:aa:bb:cc (on ap0)
	signal:  	-48 [-50, -52] dBm
	connected time:	367 seconds
Station 8e:00:11:22:33:44 (on ap0)
	signal:  	-71 [-73, -74] dBm
	connected time:	42 seconds
`

// Made-up uplink: wlan0 is connected to "Corner Café" with 192.168.4.20.
const (
	defaultRoutes = `[{"dst":"default","gateway":"192.168.4.1","dev":"wlan0","protocol":"dhcp","metric":600,"flags":[]}]`
	addresses     = `[{"ifname":"wlan0","addr_info":[{"family":"inet","local":"192.168.4.20","prefixlen":24,"scope":"global"}]}]`
	wpaStatus     = "bssid=aa:bb:cc:dd:ee:ff\nfreq=2437\nssid=Corner Caf\\xc3\\xa9\nkey_mgmt=WPA2-PSK\nwpa_state=COMPLETED\n"
	signalPoll    = "RSSI=-58\nLINKSPEED=72\n"
	scanResults   = "bssid / frequency / signal level / flags / ssid\n" +
		"aa:bb:cc:dd:ee:ff\t2437\t-58\t[WPA2-PSK-CCMP][ESS]\tCorner Caf\\xc3\\xa9\n" +
		"aa:bb:cc:dd:ee:01\t5180\t-63\t[ESS]\tHotel Guest\n" +
		"aa:bb:cc:dd:ee:02\t5500\t-71\t[WPA2-EAP-CCMP][ESS]\teduroam\n" +
		"aa:bb:cc:dd:ee:03\t2462\t-80\t[RSN-SAE-CCMP][ESS]\tNeighbour 5G\n"
)

// DHCPProbe pretends no DHCP server answers on the Ethernet port, so the
// simulated client port connects. Nothing is sent on a real network.
func DHCPProbe(iface string, timeout time.Duration) (dhcpprobe.Result, error) {
	slog.Info("devsim: would check for a DHCP server", "interface", iface)
	return dhcpprobe.Result{}, nil
}

// PortalProbe stands in for the captive portal check: the simulated
// network has no login page.
func PortalProbe(ctx context.Context) portal.Result {
	return portal.Result{State: portal.Open}
}

// Run logs the command and returns made-up output where it matters.
func (Runner) Run(name string, args ...string) (string, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	slog.Info("devsim: would run", "command", line)
	switch {
	case strings.HasPrefix(line, "iw phy") && strings.HasSuffix(line, "info"):
		return iwPhyInfo, nil
	case strings.HasPrefix(line, "iw dev ap0 station dump"):
		return stationDump, nil
	case strings.HasPrefix(line, "systemctl is-active"):
		return "active\n", nil
	case line == "ip -json route show default":
		return defaultRoutes, nil
	case line == "ip -json -6 route show default":
		return "[]", nil
	case line == "ip -json address show":
		return addresses, nil
	case strings.HasPrefix(line, "wpa_cli") && strings.HasSuffix(line, " status"):
		return wpaStatus, nil
	case strings.HasPrefix(line, "wpa_cli") && strings.HasSuffix(line, " signal_poll"):
		return signalPoll, nil
	case strings.HasPrefix(line, "wpa_cli") && strings.HasSuffix(line, " scan_results"):
		return scanResults, nil
	case line == "wg show wg0 dump":
		// A tunnel that just completed a handshake.
		return fmt.Sprintf("priv\tpub\t0\toff\nxTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\t(none)\t203.0.113.5:51820\t0.0.0.0/0,::/0\t%d\t48213\t20981\t25\n",
			time.Now().Unix()-20), nil
	case strings.HasPrefix(line, "ip link show"):
		return "", fmt.Errorf("devsim: link does not exist")
	}
	return "", nil
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
