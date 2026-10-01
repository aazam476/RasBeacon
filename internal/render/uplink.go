package render

// This file renders the configuration for the uplink: the connection from
// RasBeacon to the internet, over Ethernet or Wi-Fi.
//
//   - wpa_supplicant joins upstream Wi-Fi networks (WPASupplicant).
//   - systemd-networkd asks the upstream network for an address with DHCP
//     and IPv6 router advertisements (NetworkdUplink, NetworkdUnmanaged).

import (
	"encoding/hex"
	"fmt"
	"strings"

	"rasbeacon/internal/config"
)

// --- wpa_supplicant (joining upstream Wi-Fi) ---

// WPANetwork is one saved upstream Wi-Fi network.
type WPANetwork struct {
	SSID     string
	Security config.UplinkSecurity
	Password string // ignored for open networks
	Hidden   bool
}

// WPAInput is everything needed to write wpa_supplicant.conf.
type WPAInput struct {
	Country string
	// Networks are in order of preference (first = most preferred).
	Networks []WPANetwork
}

type wpaNetworkData struct {
	WPANetwork
	SSIDHex        string
	SSIDForComment string
	Priority       int
}

type wpaData struct {
	Country  string
	Networks []wpaNetworkData
}

// WPASupplicant produces wpa_supplicant.conf text.
func WPASupplicant(in WPAInput) (string, error) {
	data := wpaData{Country: in.Country}
	for i, n := range in.Networks {
		if err := config.ValidateSSID(n.SSID); err != nil {
			return "", fmt.Errorf("network %d name: %w", i+1, err)
		}
		switch n.Security {
		case config.UplinkOpen:
		case config.UplinkPersonal:
			// Re-check here: the password goes into the file as-is, so a
			// newline in it could inject extra settings.
			if err := config.ValidateWiFiPassword(n.Password); err != nil {
				return "", fmt.Errorf("network %q password: %w", n.SSID, err)
			}
		default:
			return "", fmt.Errorf("network %q: unknown security %q", n.SSID, n.Security)
		}
		data.Networks = append(data.Networks, wpaNetworkData{
			WPANetwork:     n,
			SSIDHex:        hex.EncodeToString([]byte(n.SSID)),
			SSIDForComment: commentSafe(n.SSID),
			// wpa_supplicant joins the network with the highest priority
			// number among those in range, so the first network in the list
			// gets the highest number.
			Priority: len(in.Networks) - i,
		})
	}
	return execute("wpa_supplicant.conf.tmpl", data)
}

// --- systemd-networkd (addresses on the uplinks) ---

// Route metrics decide which uplink is used when several are connected:
// the lower number wins, so Ethernet is preferred over Wi-Fi.
const (
	EthernetRouteMetric = 100
	WiFiRouteMetric     = 600
)

// NetworkdUplink produces a systemd-networkd .network file that gets an
// address for an uplink interface by DHCP (IPv4) and router advertisements
// (IPv6).
func NetworkdUplink(iface string, routeMetric int) (string, error) {
	if !isInterfaceName(iface) {
		return "", fmt.Errorf("invalid interface name %q", iface)
	}
	return execute("uplink.network.tmpl", struct {
		Interface   string
		RouteMetric int
	}{iface, routeMetric})
}

// NetworkdUnmanaged produces a .network file telling systemd-networkd to
// leave the given interfaces alone, because rbd configures them itself
// (the hotspot bridge and radio, and Ethernet in client-port mode).
func NetworkdUnmanaged(ifaces []string) (string, error) {
	for _, name := range ifaces {
		if !isInterfaceName(name) {
			return "", fmt.Errorf("invalid interface name %q", name)
		}
	}
	return execute("unmanaged.network.tmpl", struct{ Interfaces string }{strings.Join(ifaces, " ")})
}
