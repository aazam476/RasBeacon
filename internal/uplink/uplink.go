// Package uplink reads the state of RasBeacon's connection to the internet
// ("the uplink"): which interface is in use, its addresses, and what the
// Wi-Fi uplink is connected to.
//
// It only parses the output of commands (ip, wpa_cli); running them is the
// agent's job. That keeps every parser easy to test with saved output.
package uplink

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// --- ip -json route / address ---

// Route is one default route, from `ip -json route show default`.
type Route struct {
	Interface string `json:"dev"`
	Gateway   string `json:"gateway"`
	Metric    int    `json:"metric"`
}

// ParseDefaultRoutes reads `ip -json [-6] route show default` output and
// returns the routes, best (lowest metric) first.
func ParseDefaultRoutes(ipJSON string) ([]Route, error) {
	if strings.TrimSpace(ipJSON) == "" {
		return nil, nil
	}
	var routes []Route
	if err := json.Unmarshal([]byte(ipJSON), &routes); err != nil {
		return nil, fmt.Errorf("reading routes: %w", err)
	}
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].Metric < routes[j].Metric })
	return routes, nil
}

// ipAddrEntry matches one interface in `ip -json address show`.
type ipAddrEntry struct {
	Interface string `json:"ifname"`
	Addresses []struct {
		Family    string `json:"family"` // "inet" (IPv4) or "inet6"
		Local     string `json:"local"`
		PrefixLen int    `json:"prefixlen"`
		Scope     string `json:"scope"` // "global", "link", "host"
	} `json:"addr_info"`
}

// ParseAddresses reads `ip -json address show` output and returns each
// interface's global addresses (with network size, e.g. 192.168.1.57/24).
// Link-local and loopback addresses are left out.
func ParseAddresses(ipJSON string) (map[string][]netip.Prefix, error) {
	var entries []ipAddrEntry
	if err := json.Unmarshal([]byte(ipJSON), &entries); err != nil {
		return nil, fmt.Errorf("reading addresses: %w", err)
	}
	result := map[string][]netip.Prefix{}
	for _, e := range entries {
		for _, a := range e.Addresses {
			if a.Scope != "global" {
				continue
			}
			addr, err := netip.ParseAddr(a.Local)
			if err != nil {
				continue
			}
			result[e.Interface] = append(result[e.Interface], netip.PrefixFrom(addr, a.PrefixLen))
		}
	}
	return result, nil
}

// --- wpa_cli status ---

// WiFiStatus is the Wi-Fi uplink's connection, from `wpa_cli status`.
type WiFiStatus struct {
	// State is wpa_supplicant's state, e.g. "COMPLETED" (connected),
	// "SCANNING", "ASSOCIATING", "DISCONNECTED" or "INACTIVE".
	State string
	SSID  string
	// FrequencyMHz tells the band: below 3000 is 2.4 GHz, above is 5 GHz.
	FrequencyMHz int
	// KeyMgmt is the security in use, e.g. "WPA2-PSK", "SAE" or "NONE".
	KeyMgmt string
}

// Connected reports whether the Wi-Fi uplink has joined a network.
func (s WiFiStatus) Connected() bool { return s.State == "COMPLETED" }

// ParseWPAStatus reads `wpa_cli status` output (lines of key=value).
func ParseWPAStatus(out string) WiFiStatus {
	var s WiFiStatus
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "wpa_state":
			s.State = value
		case "ssid":
			s.SSID = DecodeSSID(value)
		case "freq":
			s.FrequencyMHz, _ = strconv.Atoi(value)
		case "key_mgmt":
			s.KeyMgmt = value
		}
	}
	return s
}

// --- wpa_cli scan_results ---

// Security values for nearby networks found by a scan.
const (
	SecurityOpen       = "open"
	SecurityPersonal   = "personal"
	SecurityEnterprise = "enterprise" // e.g. eduroam; supported from milestone 3
	SecurityOther      = "other"      // e.g. old WEP; not supported
)

// NearbyNetwork is one network found by a scan.
type NearbyNetwork struct {
	SSID string `json:"ssid"`
	// SignalDBM is the signal strength: -50 is excellent, -80 is weak.
	SignalDBM    int    `json:"signalDbm"`
	FrequencyMHz int    `json:"frequencyMhz"`
	Security     string `json:"security"`
}

// ParseScanResults reads `wpa_cli scan_results` output. Each network name
// appears once (with its strongest signal), strongest first. Hidden
// networks (no name) are left out.
//
// The output is a header line followed by tab-separated lines:
//
//	bssid / frequency / signal level / flags / ssid
//	aa:bb:cc:dd:ee:ff	2437	-52	[WPA2-PSK-CCMP][ESS]	Home
func ParseScanResults(out string) []NearbyNetwork {
	best := map[string]NearbyNetwork{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, "\t", 5)
		if len(fields) != 5 {
			continue // header or empty line
		}
		frequency, err1 := strconv.Atoi(fields[1])
		signal, err2 := strconv.Atoi(fields[2])
		ssid := DecodeSSID(fields[4])
		if err1 != nil || err2 != nil || ssid == "" {
			continue
		}
		n := NearbyNetwork{SSID: ssid, SignalDBM: signal, FrequencyMHz: frequency, Security: securityFromFlags(fields[3])}
		if existing, seen := best[ssid]; !seen || n.SignalDBM > existing.SignalDBM {
			best[ssid] = n
		}
	}
	result := make([]NearbyNetwork, 0, len(best))
	for _, n := range best {
		result = append(result, n)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SignalDBM != result[j].SignalDBM {
			return result[i].SignalDBM > result[j].SignalDBM
		}
		return result[i].SSID < result[j].SSID
	})
	return result
}

// securityFromFlags turns scan flags like "[WPA2-PSK-CCMP][ESS]" into one
// of the Security values.
func securityFromFlags(flags string) string {
	switch {
	case strings.Contains(flags, "EAP"):
		return SecurityEnterprise
	case strings.Contains(flags, "PSK"), strings.Contains(flags, "SAE"):
		return SecurityPersonal
	case strings.Contains(flags, "WEP"), strings.Contains(flags, "OWE"):
		return SecurityOther
	default:
		return SecurityOpen
	}
}

// DecodeSSID undoes the escaping wpa_cli uses for network names: \xNN for
// unusual bytes, and \\, \", \n, \r, \t, \e for special characters.
func DecodeSSID(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			out = append(out, s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'e':
			out = append(out, 0x1b)
		case 'x':
			if i+2 < len(s) { // two hex digits follow
				if b, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					out = append(out, byte(b))
					i += 2
					continue
				}
			}
			out = append(out, '\\', 'x')
		default: // \\ and \"
			out = append(out, s[i])
		}
	}
	return string(out)
}

// BandName turns a frequency into "2.4GHz", "5GHz" or "6GHz".
func BandName(frequencyMHz int) string {
	switch {
	case frequencyMHz == 0:
		return ""
	case frequencyMHz < 3000:
		return "2.4GHz"
	case frequencyMHz < 5925:
		return "5GHz"
	default:
		return "6GHz"
	}
}

// ParseSignalPoll reads the signal strength (in dBm) from `wpa_cli
// signal_poll` output, which contains a line like "RSSI=-52". It returns 0
// when the line is missing (for example when not connected).
func ParseSignalPoll(out string) int {
	for _, line := range strings.Split(out, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "RSSI="); ok {
			rssi, _ := strconv.Atoi(value)
			return rssi
		}
	}
	return 0
}
