package agent

// The MAC address an uplink shows its network: the Pi's own, a random one
// RasBeacon made, or a cloned one (another device's, so the network takes
// RasBeacon for it). Ethernet gets it from systemd-networkd (MACAddress=,
// see applyNetworkdConfig); the Wi-Fi radio from rbd, before wpa_supplicant
// starts (setWiFiMAC).

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"rasbeacon/internal/config"
)

// checkMACChoice checks an uplink's new MAC address choice, making a
// random address when one is asked for (and keeping the one already made
// when the choice stays random). field names it in errors.
func (a *Agent) checkMACChoice(choice, old config.MACChoice, field string) (config.MACChoice, error) {
	choice.Address = strings.ToLower(strings.TrimSpace(choice.Address))
	if choice.Mode == config.MACRandom && choice.Address == "" {
		choice.Address = old.Address
		if old.Mode != config.MACRandom || old.Address == "" {
			choice.Address = config.RandomMAC()
		}
	}
	if choice.Mode == config.MACOwn {
		choice.Address = ""
	}
	if err := choice.Validate(field); err != nil {
		return choice, err
	}
	if choice.Mode == config.MACCloned && a.isOwnMAC(choice.Address) {
		return choice, &config.ValidationError{Field: field, Message: "That is one of RasBeacon's own addresses."}
	}
	return choice, nil
}

// isOwnMAC reports whether mac is one of RasBeacon's network interfaces'
// own (permanent) addresses.
func (a *Agent) isOwnMAC(mac string) bool {
	entries, _ := os.ReadDir(a.paths.SysClassNet())
	for _, entry := range entries {
		if a.permanentMAC(entry.Name()) == mac {
			return true
		}
	}
	return false
}

// permanentMAC is an interface's own address, even while another one is
// set: `ip -json link` names it "permaddr" then. "" if unknown.
func (a *Agent) permanentMAC(iface string) string {
	out, err := a.runner.Run("ip", "-json", "link", "show", "dev", iface)
	if err == nil {
		var links []struct {
			Address  string `json:"address"`
			PermAddr string `json:"permaddr"`
		}
		if json.Unmarshal([]byte(out), &links) == nil && len(links) == 1 {
			if links[0].PermAddr != "" {
				return strings.ToLower(links[0].PermAddr)
			}
			return strings.ToLower(links[0].Address)
		}
	}
	data, _ := os.ReadFile(filepath.Join(a.paths.SysClassNet(), iface, "address"))
	return strings.ToLower(strings.TrimSpace(string(data)))
}

// setWiFiMAC gives the Wi-Fi uplink radio the address chosen for it (its
// own, random or cloned), and reports whether it changed (wpa_supplicant
// must start again to use it). It acts only when the choice changed: a
// network set to "random MAC address" makes wpa_supplicant change the
// address by itself, which must not be undone at every save. The caller
// holds a.mu.
func (a *Agent) setWiFiMAC(iface string) bool {
	want := a.cfg.Uplink.WiFiMAC.Address
	if want == "" {
		want = a.permanentMAC(iface)
	}
	if want == "" || want == a.uplink.wifiMAC {
		return false
	}
	a.uplink.wifiMAC = want
	if a.readSysNet(iface, "address") == want {
		return false
	}
	// The address can only change while the interface is down.
	for _, args := range [][]string{
		{"link", "set", "dev", iface, "down"},
		{"link", "set", "dev", iface, "address", want},
		{"link", "set", "dev", iface, "up"},
	} {
		if _, err := a.runner.Run("ip", args...); err != nil {
			slog.Error("could not set the Wi-Fi uplink's MAC address", "address", want, "error", err)
			_, _ = a.runner.Run("ip", "link", "set", "dev", iface, "up")
			return false
		}
	}
	slog.Info("Wi-Fi uplink MAC address set", "address", want, "mode", a.cfg.Uplink.WiFiMAC.Mode)
	return true
}
