// Package devsim makes rbd run on a normal computer without a Raspberry Pi,
// for developing and testing the web UI.
//
// Start rbd with `-dev /tmp/rasbeacon-dev` and it will:
//   - keep all its files under that directory instead of /,
//   - pretend there is a built-in radio and a USB Archer T3U,
//   - log the commands it would run (ip, iw, systemctl, ...) instead of
//     running them, and answer with made-up output (two connected phones).
//
// Nothing on your computer's network is changed.
package devsim

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"rasbeacon/internal/paths"
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
