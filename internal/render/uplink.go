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
	"net/netip"
	"strings"

	"rasbeacon/internal/config"
)

// --- wpa_supplicant (joining upstream Wi-Fi) ---

// WPANetwork is one saved upstream Wi-Fi network.
type WPANetwork struct {
	SSID     string
	Security config.UplinkSecurity
	// Password is the network password (personal), the user's password
	// (enterprise TTLS/PEAP) or the client key's passphrase (enterprise
	// TLS). Ignored for open networks.
	Password string
	Hidden   bool
	// Enterprise is set for enterprise networks.
	Enterprise *WPAEnterprise
}

// WPAEnterprise holds an enterprise network's login settings.
type WPAEnterprise struct {
	Method            config.EAPMethod
	Identity          string
	AnonymousIdentity string
	// ServerDomain is required in the server certificate's name, e.g.
	// "ruw.rutgers.edu" (see config.EnterpriseConfig.ServerDomain). Empty
	// only when the certificate check was turned off.
	ServerDomain string
	// Files written by rbd. CACertFile is empty only when the certificate
	// check was turned off; the client files are for EAP-TLS.
	CACertFile     string
	ClientCertFile string
	ClientKeyFile  string
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
	// Enterprise values, written in hexadecimal so that no character in
	// them can change the meaning of the file.
	EAP                  string // "TTLS", "PEAP" or "TLS"
	Phase2               string // e.g. "auth=PAP"
	IdentityHex          string
	AnonymousIdentityHex string
	PasswordHex          string
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
		case config.UplinkEnterprise:
			if err := checkEnterprise(n); err != nil {
				return "", fmt.Errorf("network %q: %w", n.SSID, err)
			}
		default:
			return "", fmt.Errorf("network %q: unknown security %q", n.SSID, n.Security)
		}
		network := wpaNetworkData{
			WPANetwork:     n,
			SSIDHex:        hex.EncodeToString([]byte(n.SSID)),
			SSIDForComment: commentSafe(n.SSID),
			// wpa_supplicant joins the network with the highest priority
			// number among those in range, so the first network in the list
			// gets the highest number.
			Priority: len(in.Networks) - i,
		}
		if e := n.Enterprise; e != nil && n.Security == config.UplinkEnterprise {
			network.EAP, network.Phase2 = eapNames(e.Method)
			network.IdentityHex = hex.EncodeToString([]byte(e.Identity))
			network.AnonymousIdentityHex = hex.EncodeToString([]byte(e.AnonymousIdentity))
			network.PasswordHex = hex.EncodeToString([]byte(n.Password))
		}
		data.Networks = append(data.Networks, network)
	}
	return execute("wpa_supplicant.conf.tmpl", data)
}

// eapNames returns wpa_supplicant's names for a login method: the outer
// method and, for tunnelled methods, the inner ("phase 2") one.
func eapNames(method config.EAPMethod) (eap, phase2 string) {
	switch method {
	case config.EAPTTLSPAP:
		return "TTLS", "auth=PAP"
	case config.EAPPEAP:
		return "PEAP", "auth=MSCHAPV2"
	default:
		return "TLS", ""
	}
}

// checkEnterprise re-checks the values that are written into the file as
// they are (method, server domain, file paths).
func checkEnterprise(n WPANetwork) error {
	e := n.Enterprise
	if e == nil {
		return fmt.Errorf("missing enterprise settings")
	}
	switch e.Method {
	case config.EAPTTLSPAP, config.EAPPEAP, config.EAPTLS:
	default:
		return fmt.Errorf("unknown login method %q", e.Method)
	}
	if e.Method == config.EAPTLS && (e.ClientCertFile == "" || e.ClientKeyFile == "") {
		return fmt.Errorf("EAP-TLS needs a client certificate and key")
	}
	for _, value := range []string{e.ServerDomain, e.CACertFile, e.ClientCertFile, e.ClientKeyFile} {
		if strings.ContainsAny(value, "\"\n\r\\") {
			return fmt.Errorf("unsafe value %q", value)
		}
	}
	if e.CACertFile != "" && e.ServerDomain == "" {
		return fmt.Errorf("a CA certificate needs a server name")
	}
	return nil
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

// --- WireGuard (the VPN tunnel) ---

// WireGuardInput is everything needed to write wg0.conf.
type WireGuardInput struct {
	// Keys in base64.
	PrivateKey    string
	PeerPublicKey string
	PresharedKey  string // optional
	Endpoint      string // "host:port"
	AllowedIPs    []netip.Prefix
	Keepalive     int
}

// WireGuard produces the `wg setconf` file for the tunnel.
func WireGuard(in WireGuardInput) (string, error) {
	for _, key := range []string{in.PrivateKey, in.PeerPublicKey, in.PresharedKey} {
		if strings.ContainsAny(key, "\n\r") || (key != "" && len(key) != 44) {
			return "", fmt.Errorf("invalid WireGuard key")
		}
	}
	if strings.ContainsAny(in.Endpoint, "\n\r") || in.Endpoint == "" {
		return "", fmt.Errorf("invalid endpoint %q", in.Endpoint)
	}
	var allowed []string
	for _, p := range in.AllowedIPs {
		allowed = append(allowed, p.String())
	}
	return execute("wg0.conf.tmpl", struct {
		WireGuardInput
		AllowedIPs string
	}{in, strings.Join(allowed, ", ")})
}
