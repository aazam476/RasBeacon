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
	// BSSID is the access point's MAC address.
	BSSID string
	// FrequencyMHz tells the band: below 3000 is 2.4 GHz, above is 5 GHz.
	FrequencyMHz int
	// KeyMgmt is the security in use, e.g. "WPA2-PSK", "SAE" or "NONE".
	KeyMgmt string
	// NetworkID is wpa_supplicant's number for the network it is using
	// (for enable_network and disable_network); "" if none.
	NetworkID string
	// LoginFailed is true when an Enterprise network rejected the login
	// (wrong username or password, or the server failed RasBeacon's
	// certificate check). wpa_supplicant then waits before trying again.
	LoginFailed bool
	// Generation is the Wi-Fi generation both sides agreed on when joining
	// (4 to 7; 0 when unknown), from wpa_supplicant's "wifi_generation".
	// It is more exact than the rates, which some drivers' rate control
	// reports in an older standard's terms.
	Generation int
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
		case "bssid":
			s.BSSID = value
		case "freq":
			s.FrequencyMHz, _ = strconv.Atoi(value)
		case "key_mgmt":
			s.KeyMgmt = value
		case "id":
			s.NetworkID = value
		case "wifi_generation":
			s.Generation, _ = strconv.Atoi(value)
		case "Supplicant PAE state":
			// HELD means "the last login failed, waiting before retrying".
			s.LoginFailed = s.LoginFailed || value == "HELD"
		case "EAP state":
			s.LoginFailed = s.LoginFailed || value == "FAILURE"
		}
	}
	return s
}

// --- wpa_cli list_networks ---

// ParseTempDisabled reads `wpa_cli list_networks` and returns the names of
// saved networks that wpa_supplicant has paused after failed logins (wrong
// password, or for Enterprise networks also a rejected certificate). It
// tries them again after a pause that grows with each failure.
//
//	network id / ssid / bssid / flags
//	0	eduroam	any	[TEMP-DISABLED]
func ParseTempDisabled(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) >= 4 && strings.Contains(fields[3], "[TEMP-DISABLED]") {
			names = append(names, DecodeSSID(fields[1]))
		}
	}
	return names
}

// --- wpa_cli scan_results ---

// Security values for nearby networks found by a scan.
const (
	SecurityOpen       = "open"
	SecurityPersonal   = "personal"
	SecurityEnterprise = "enterprise" // WPA-Enterprise, e.g. eduroam
	SecurityOther      = "other"      // e.g. old WEP; not supported
)

// NearbyNetwork is one network found by a scan.
type NearbyNetwork struct {
	SSID string `json:"ssid"`
	// SignalDBM is the signal strength: -50 is excellent, -80 is weak.
	SignalDBM    int    `json:"signalDbm"`
	FrequencyMHz int    `json:"frequencyMhz"`
	Security     string `json:"security"`
	// Label names the exact kind of security, e.g. "WPA2/WPA3-Personal".
	Label string `json:"label"`
	// Standard is the newest Wi-Fi standard the network offers ("wifi7",
	// "wifi6e", "wifi6", "wifi5", "wifi4"; "" when unknown or older), from
	// ScanStandards.
	Standard string `json:"standard,omitempty"`
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
		n := NearbyNetwork{
			SSID: ssid, SignalDBM: signal, FrequencyMHz: frequency,
			Security: securityFromFlags(fields[3]), Label: securityLabel(fields[3]),
		}
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

// securityLabel names the security in scan flags the way phones do, e.g.
// "[WPA2-PSK-CCMP][RSN-SAE-CCMP][ESS]" is "WPA2/WPA3-Personal".
//
// In the flags, WPA3 shows up as SAE (personal) or EAP-SHA256 / EAP-SUITE-B
// (enterprise); WPA2 as PSK or plain EAP.
func securityLabel(flags string) string {
	kind := "Personal"
	switch securityFromFlags(flags) {
	case SecurityOpen:
		return "Open"
	case SecurityOther:
		if strings.Contains(flags, "OWE") {
			return "Enhanced Open (OWE)"
		}
		return "WEP (not supported)"
	case SecurityEnterprise:
		kind = "Enterprise"
	}
	wpa2, wpa3 := false, false
	for _, method := range keyManagementMethods(flags) {
		switch method {
		case "PSK", "EAP", "FT/PSK", "FT/EAP", "PSK-SHA256":
			wpa2 = true
		case "SAE", "FT/SAE", "EAP-SHA256", "EAP-SUITE-B", "EAP-SUITE-B-192", "FT/EAP-SHA384":
			wpa3 = true
		}
	}
	switch {
	case wpa2 && wpa3:
		return "WPA2/WPA3-" + kind
	case wpa3:
		return "WPA3-" + kind
	default:
		return "WPA2-" + kind
	}
}

// keyManagementMethods lists the key management methods in scan flags.
// Each flag looks like [protocol-methods-ciphers], with methods joined by
// "+", e.g. "[RSN-PSK+SAE-CCMP]" holds "PSK" and "SAE". Method names can
// contain "-" themselves (EAP-SHA256), so the cipher part is everything
// after the last "-".
func keyManagementMethods(flags string) []string {
	var methods []string
	for _, flag := range strings.Split(flags, "[") {
		flag = strings.TrimSuffix(flag, "]")
		protocol, rest, ok := strings.Cut(flag, "-")
		if !ok || (protocol != "WPA" && protocol != "WPA2" && protocol != "RSN") {
			continue
		}
		if last := strings.LastIndex(rest, "-"); last >= 0 {
			rest = rest[:last] // drop the ciphers, e.g. "-CCMP"
		}
		methods = append(methods, strings.Split(rest, "+")...)
	}
	return methods
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

// --- iw dev <uplink> link ---

// Link is the Wi-Fi standard, channel width and speed of the uplink's
// connection, from `iw dev <uplink> link`, which shows the rates in use:
//
//	freq: 5180
//	rx bitrate: 1200.9 MBit/s 80MHz HE-MCS 11 HE-NSS 2 HE-GI 0 HE-DCM 0
//	tx bitrate: 864.7 MBit/s 80MHz HE-MCS 8 HE-NSS 2 HE-GI 0 HE-DCM 0
//
// The kind of rate names the standard: MCS (Wi-Fi 4), VHT-MCS (Wi-Fi 5),
// HE-MCS (Wi-Fi 6, or 6E on 6 GHz) and EHT-MCS (Wi-Fi 7).
type Link struct {
	// Standard is "wifi7", "wifi6e", "wifi6", "wifi5", "wifi4", or ""
	// for older (802.11a/b/g) rates or when not connected.
	Standard string
	// WidthMHz is the channel width the rates use (20 when not given).
	WidthMHz int
	// RxMbps and TxMbps are the current rates, received and sent.
	RxMbps, TxMbps float64
}

// StandardOf names the standard for a Wi-Fi generation (4 to 7) on a
// frequency: Wi-Fi 6 on 6 GHz is "wifi6e". "" for anything else.
func StandardOf(generation, freqMHz int) string {
	switch {
	case generation == 6 && freqMHz >= 5925:
		return "wifi6e"
	case generation >= 4 && generation <= 7:
		return "wifi" + strconv.Itoa(generation)
	}
	return ""
}

// ParseLink reads `iw dev <uplink> link` output.
func ParseLink(out string) Link {
	var link Link
	freq := 0
	best := 0 // the newest standard seen in either rate
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "freq:"); ok {
			f, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
			freq = int(f)
			continue
		}
		rate, rx := strings.CutPrefix(line, "rx bitrate:")
		if !rx {
			var tx bool
			if rate, tx = strings.CutPrefix(line, "tx bitrate:"); !tx {
				continue
			}
		}
		fields := strings.Fields(rate)
		if len(fields) == 0 {
			continue
		}
		mbps, _ := strconv.ParseFloat(fields[0], 64)
		if rx {
			link.RxMbps = mbps
		} else {
			link.TxMbps = mbps
		}
		for _, field := range fields[1:] {
			switch {
			case field == "EHT-MCS":
				best = max(best, 7)
			case field == "HE-MCS":
				best = max(best, 6)
			case field == "VHT-MCS":
				best = max(best, 5)
			case field == "MCS":
				best = max(best, 4)
			case strings.HasSuffix(field, "MHz"):
				if width, err := strconv.Atoi(strings.TrimSuffix(field, "MHz")); err == nil {
					link.WidthMHz = max(link.WidthMHz, width)
				}
			}
		}
	}
	link.Standard = StandardOf(best, freq)
	if link.WidthMHz == 0 && (link.RxMbps > 0 || link.TxMbps > 0) {
		link.WidthMHz = 20
	}
	return link
}

// --- iw dev <uplink> scan dump ---

// ScanStandards reads `iw dev <uplink> scan dump` (the last scan's results,
// with each access point's capabilities) and returns, by network name,
// the newest Wi-Fi standard any of its access points offers. Each access
// point is a block:
//
//	BSS aa:bb:cc:dd:ee:ff(on wlan1)
//		freq: 5180
//		SSID: Hotel
//		HT capabilities:
//		VHT capabilities:
//		HE capabilities:
//
// HT is Wi-Fi 4, VHT Wi-Fi 5, HE Wi-Fi 6 (6E on 6 GHz) and EHT Wi-Fi 7.
func ScanStandards(out string) map[string]string {
	rank := map[string]int{"wifi4": 1, "wifi5": 2, "wifi6": 3, "wifi6e": 4, "wifi7": 5}
	standards := map[string]string{}
	ssid, freq, best := "", 0, 0
	finish := func() {
		standard := StandardOf(best, freq)
		if ssid != "" && standard != "" && rank[standard] > rank[standards[ssid]] {
			standards[ssid] = standard
		}
		ssid, freq, best = "", 0, 0
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "BSS "):
			finish()
		case strings.HasPrefix(trimmed, "freq:"):
			f, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(trimmed, "freq:")), 64)
			freq = int(f)
		case strings.HasPrefix(trimmed, "SSID: "):
			ssid = DecodeSSID(strings.TrimPrefix(trimmed, "SSID: "))
		case strings.HasPrefix(trimmed, "EHT capabilities"):
			best = max(best, 7)
		case strings.HasPrefix(trimmed, "HE capabilities"):
			best = max(best, 6)
		case strings.HasPrefix(trimmed, "VHT capabilities"):
			best = max(best, 5)
		case strings.HasPrefix(trimmed, "HT capabilities"):
			best = max(best, 4)
		}
	}
	finish()
	return standards
}
