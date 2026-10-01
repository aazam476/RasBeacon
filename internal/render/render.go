// Package render turns RasBeacon's settings into the configuration files
// that the real network programs read: hostapd (Wi-Fi hotspot), dnsmasq
// (DHCP and DNS) and nftables (firewall).
//
// Each output file has a template in the templates/ directory. The templates
// look almost exactly like the finished files, with comments explaining every
// line, so the easiest way to understand what RasBeacon configures is to read
// the templates (or the example outputs in testdata/).
//
// This package only produces text. Writing the files and restarting programs
// is done by the agent (internal/agent).
package render

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
	"text/template"

	"rasbeacon/internal/config"
)

//go:embed templates/*.tmpl
var templateFiles embed.FS

// templates holds all parsed templates, loaded once when the program starts.
// template.Must stops the program immediately if a template has a syntax
// error, which the unit tests catch long before a release.
var templates = template.Must(template.ParseFS(templateFiles, "templates/*.tmpl"))

// BridgeName is the network bridge that holds the hotspot's IP address.
// The hotspot radio (and, later, a wired client port) are attached to it.
const BridgeName = "br-lan"

// LAN describes the hotspot-side network: RasBeacon's own address and the
// range of addresses handed to devices.
type LAN struct {
	// Prefix is RasBeacon's address with the network size, e.g. 10.87.0.1/24.
	Prefix netip.Prefix
	// IPv6Prefix is RasBeacon's private IPv6 address on the hotspot with the
	// network size, e.g. fd12:3456:789a:1::1/64 (see ULALANAddress). It is
	// not set (IsValid() is false) when IPv6 is not used, as in setup mode.
	IPv6Prefix netip.Prefix
}

// DefaultLAN is the hotspot network chosen by the owner: 10.87.0.0/24, with
// RasBeacon at 10.87.0.1.
var DefaultLAN = LAN{Prefix: netip.MustParsePrefix("10.87.0.1/24")}

// LANCandidates are the hotspot networks RasBeacon can use, in order of
// preference. It uses the first one that does not overlap the upstream
// network: if a hotel also used 10.87.0.0/24, devices could not tell
// RasBeacon's addresses from the hotel's.
var LANCandidates = []netip.Prefix{
	netip.MustParsePrefix("10.87.0.1/24"),
	netip.MustParsePrefix("10.88.0.1/24"),
	netip.MustParsePrefix("172.31.87.1/24"),
	netip.MustParsePrefix("192.168.87.1/24"),
}

// ChooseLANPrefix returns the first of LANCandidates that overlaps none of
// the upstream networks. If all of them overlap (very unlikely), it returns
// the first one.
func ChooseLANPrefix(upstream []netip.Prefix) netip.Prefix {
	for _, candidate := range LANCandidates {
		overlaps := false
		for _, u := range upstream {
			if u.Masked().Overlaps(candidate.Masked()) {
				overlaps = true
				break
			}
		}
		if !overlaps {
			return candidate
		}
	}
	return LANCandidates[0]
}

// ULALANAddress turns the device's random IPv6 prefix (a /48 such as
// fd12:3456:789a::/48, see config.Identity) into RasBeacon's address on the
// hotspot: the first /64 inside it, address ::1, e.g. fd12:3456:789a:1::1/64.
func ULALANAddress(ula48 string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(ula48)
	if err != nil || !prefix.Addr().Is6() || prefix.Bits() != 48 {
		return netip.Prefix{}, fmt.Errorf("invalid ULA prefix %q", ula48)
	}
	a := prefix.Masked().Addr().As16()
	a[7] = 1  // subnet number 1 (the 4th group of the address)
	a[15] = 1 // host ::1
	return netip.PrefixFrom(netip.AddrFrom16(a), 64), nil
}

// Address returns RasBeacon's own IP address on the hotspot, e.g. 10.87.0.1.
func (l LAN) Address() string { return l.Prefix.Addr().String() }

// Netmask returns the classic dotted netmask, e.g. 255.255.255.0 for a /24.
func (l LAN) Netmask() string {
	bits := l.Prefix.Bits()
	mask := uint32(0xffffffff) << (32 - bits)
	return fmt.Sprintf("%d.%d.%d.%d", byte(mask>>24), byte(mask>>16), byte(mask>>8), byte(mask))
}

// hostAddress returns the address with the given last number in this /24,
// e.g. hostAddress(20) = 10.87.0.20.
func (l LAN) hostAddress(last byte) string {
	a := l.Prefix.Masked().Addr().As4()
	a[3] = last
	return netip.AddrFrom4(a).String()
}

// --- hostapd (Wi-Fi hotspot) ---

// HotspotInput is everything needed to write hostapd.conf.
type HotspotInput struct {
	Interface string // the hotspot radio, normally "ap0"
	SSID      string
	Band      config.Band
	Channel   int // 0 means the default for the band
	Country   string
	Security  config.Security
	// Open means no password at all. Only used for the setup network when
	// no setup.txt was provided.
	Open     bool
	Password string // ignored when Open is true
}

// hostapdData is what the hostapd template actually sees: the input plus a
// few values computed here so the template itself stays simple.
type hostapdData struct {
	HotspotInput
	Bridge         string
	SSIDHex        string
	SSIDForComment string
	// VHT80 is true when the 5 GHz channel can be part of an 80 MHz block.
	VHT80            bool
	VHTCenterChannel int
	// HT40Direction is "HT40+" or "HT40-" (see ht40Direction).
	HT40Direction string
}

// Hostapd produces hostapd.conf text for the hotspot.
func Hostapd(in HotspotInput) (string, error) {
	if in.Channel == 0 {
		in.Channel = config.DefaultChannel(in.Band)
	}
	if !in.Open {
		// Re-check here: the password goes into the file as-is, so a
		// newline in it could inject extra settings.
		if err := config.ValidateWiFiPassword(in.Password); err != nil {
			return "", fmt.Errorf("hotspot password: %w", err)
		}
	}
	if err := config.ValidateSSID(in.SSID); err != nil {
		return "", fmt.Errorf("hotspot name: %w", err)
	}

	data := hostapdData{
		HotspotInput:     in,
		Bridge:           BridgeName,
		SSIDHex:          hex.EncodeToString([]byte(in.SSID)),
		SSIDForComment:   commentSafe(in.SSID),
		VHTCenterChannel: vhtCenterChannel(in.Channel),
		VHT80:            vhtCenterChannel(in.Channel) != in.Channel,
		HT40Direction:    ht40Direction(in.Channel),
	}
	return execute("hostapd.conf.tmpl", data)
}

// vhtCenterChannel returns the centre of the 80 MHz block that contains a
// 5 GHz channel. hostapd needs this for 80 MHz (802.11ac) operation.
// Channels 36-48 share centre 42; 149-161 share 155. Channel 165 cannot be
// part of an 80 MHz block, so it stays on its own centre.
func vhtCenterChannel(channel int) int {
	switch {
	case channel >= 36 && channel <= 48:
		return 42
	case channel >= 149 && channel <= 161:
		return 155
	default:
		return channel
	}
}

// ht40Direction says where a 5 GHz channel's 40 MHz partner is. Channels
// pair up as 36+40, 44+48, 149+153, 157+161: the first of each pair looks up
// ("HT40+"), the second looks down ("HT40-").
func ht40Direction(channel int) string {
	switch channel {
	case 40, 48, 153, 161:
		return "HT40-"
	default:
		return "HT40+"
	}
}

// commentSafe makes text safe to place inside a "# ..." comment line by
// replacing anything that is not a plain printable character.
func commentSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

// --- dnsmasq (DHCP and DNS) ---

// DnsmasqInput is everything needed to write dnsmasq.conf.
type DnsmasqInput struct {
	LAN       LAN
	LeaseFile string
	// Captive is true during first-boot setup: every name points to
	// RasBeacon so phones open the setup page automatically.
	Captive bool
	// Upstream lists the DNS servers that questions about internet names are
	// passed on to (normally 127.0.0.53, the device's own resolver, which
	// uses the DNS servers of the current uplink). Ignored when Captive.
	Upstream []string
	// IPv6DefaultRoute tells devices that RasBeacon can carry their IPv6
	// internet traffic. When false, devices still get private IPv6
	// addresses (if LAN.IPv6Prefix is set) but no IPv6 route to the
	// internet, so they use IPv4 instead of waiting for IPv6 to time out.
	IPv6DefaultRoute bool
}

type dnsmasqData struct {
	Bridge           string
	LANAddress       string
	Netmask          string
	DHCPStart        string
	DHCPEnd          string
	LeaseFile        string
	Captive          bool
	CaptivePortalAPI string
	Upstream         []string
	IPv6             bool
	IPv6DefaultRoute bool
}

// Dnsmasq produces dnsmasq.conf text.
func Dnsmasq(in DnsmasqInput) (string, error) {
	data := dnsmasqData{
		Bridge:     BridgeName,
		LANAddress: in.LAN.Address(),
		Netmask:    in.LAN.Netmask(),
		// Addresses .1 to .19 are left free for RasBeacon and future use.
		DHCPStart:        in.LAN.hostAddress(20),
		DHCPEnd:          in.LAN.hostAddress(250),
		LeaseFile:        in.LeaseFile,
		Captive:          in.Captive,
		CaptivePortalAPI: "http://" + in.LAN.Address() + "/api/v1/captive",
		Upstream:         in.Upstream,
		IPv6:             in.LAN.IPv6Prefix.IsValid(),
		IPv6DefaultRoute: in.IPv6DefaultRoute,
	}
	for _, server := range in.Upstream {
		if _, err := netip.ParseAddr(server); err != nil {
			return "", fmt.Errorf("upstream DNS server %q is not an IP address", server)
		}
	}
	return execute("dnsmasq.conf.tmpl", data)
}

// --- nftables (firewall) ---

// FirewallInput is everything needed to write the firewall rules.
type FirewallInput struct {
	SSHEnabled bool
	// Uplinks are the interfaces that lead to the internet, e.g. "eth0" and
	// "wlan0". Devices' traffic is forwarded (with NAT) to these interfaces
	// only. Empty means nothing is forwarded, as during first-boot setup.
	Uplinks []string
}

type firewallData struct {
	Bridge     string
	SSHEnabled bool
	Uplinks    []string
}

// Firewall produces the nftables ruleset.
func Firewall(in FirewallInput) (string, error) {
	for _, name := range in.Uplinks {
		if !isInterfaceName(name) {
			return "", fmt.Errorf("invalid interface name %q", name)
		}
	}
	return execute("firewall.nft.tmpl", firewallData{
		Bridge:     BridgeName,
		SSHEnabled: in.SSHEnabled,
		Uplinks:    in.Uplinks,
	})
}

// isInterfaceName reports whether name is a safe Linux interface name
// (letters, digits, "-", "_", "."; at most 15 characters), so it can be
// placed into config files without quoting problems.
func isInterfaceName(name string) bool {
	if name == "" || len(name) > 15 {
		return false
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

// execute fills in one template.
func execute(name string, data any) (string, error) {
	var out bytes.Buffer
	if err := templates.ExecuteTemplate(&out, name, data); err != nil {
		return "", fmt.Errorf("rendering %s: %w", name, err)
	}
	return out.String(), nil
}
