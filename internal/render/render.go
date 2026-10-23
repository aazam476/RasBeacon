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
	"net"
	"net/netip"
	"slices"
	"strconv"
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
// The hotspot radio (and the Ethernet port in client-port mode) are
// attached to it.
const BridgeName = "br-lan"

// The guest network (see config.GuestConfig) has its own radio interface,
// which hostapd creates, and its own bridge holding RasBeacon's guest-side
// address. Nothing else is ever attached to the guest bridge.
const (
	GuestInterface  = "guest0"
	GuestBridgeName = "br-guest"
)

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
// the upstream networks, counting the guest network that goes with it (see
// GuestPrefixFor). If all of them overlap (very unlikely), it returns the
// first one.
func ChooseLANPrefix(upstream []netip.Prefix) netip.Prefix {
	for _, candidate := range LANCandidates {
		if !LANOverlaps(candidate, upstream) {
			return candidate
		}
	}
	return LANCandidates[0]
}

// LANOverlaps reports whether a hotspot network, or the guest network that
// goes with it, overlaps any of the upstream networks.
func LANOverlaps(lan netip.Prefix, upstream []netip.Prefix) bool {
	for _, u := range upstream {
		if u.Masked().Overlaps(lan.Masked()) || u.Masked().Overlaps(GuestPrefixFor(lan).Masked()) {
			return true
		}
	}
	return false
}

// GuestPrefixFor returns the guest network that goes with a hotspot
// network: the next /24, e.g. 10.87.1.1/24 for 10.87.0.1/24.
func GuestPrefixFor(lan netip.Prefix) netip.Prefix {
	a := lan.Addr().As4()
	a[2]++
	return netip.PrefixFrom(netip.AddrFrom4(a), lan.Bits())
}

// Guest returns the guest network that goes with this hotspot network
// (IPv4 only: guests get no IPv6).
func (l LAN) Guest() LAN {
	return LAN{Prefix: GuestPrefixFor(l.Prefix)}
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
func (l LAN) Netmask() string { return Netmask(l.Prefix.Bits()) }

// Netmask turns an IPv4 prefix length into a dotted netmask: 24 is
// 255.255.255.0, 0 is 0.0.0.0.
func Netmask(bits int) string { return net.IP(net.CIDRMask(bits, 32)).String() }

// DHCP hands out the addresses from .20 to .250 (.1 to .19 are left free
// for RasBeacon and future use).
const (
	dhcpFirst = 20
	dhcpLast  = 250
)

// DHCPRange returns the first and last address DHCP hands out on this
// network, and how many that is.
func (l LAN) DHCPRange() (first, last string, count int) {
	return l.hostAddress(dhcpFirst), l.hostAddress(dhcpLast), dhcpLast - dhcpFirst + 1
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
	// AcceptMACFile, when set, lets only the devices listed in that file
	// join (one MAC address per line). It applies to this network, not the
	// guest network.
	AcceptMACFile string
	// MaxDevices is the most devices on each network at once (0: hostapd's
	// own limit).
	MaxDevices int
	// Guest adds the guest network on the same radio (nil: no guest
	// network). The radio must support two access points (see
	// radio.MaxAPInterfaces).
	Guest *GuestNetwork
}

// GuestNetwork is the guest network's part of hostapd.conf. It uses the
// main network's band, channel, country and security setting.
type GuestNetwork struct {
	SSID     string
	Password string
	// BSSID is the guest network's own MAC address (see GuestBSSID).
	BSSID string
}

// GuestBSSID makes a MAC address for the guest network from the hotspot
// radio's address. Every network on a radio needs its own; this one sets
// the "locally administered" bit (or, if the radio's address already has
// it, changes the last bit), so it can't clash with a real device.
func GuestBSSID(radioMAC string) (string, error) {
	hw, err := net.ParseMAC(radioMAC)
	if err != nil || len(hw) != 6 {
		return "", fmt.Errorf("invalid MAC address %q", radioMAC)
	}
	guest := slices.Clone(hw)
	if guest[0]&0x02 == 0 {
		guest[0] |= 0x02
	} else {
		guest[5] ^= 0x01
	}
	return guest.String(), nil
}

// hostapdData is what the hostapd template actually sees: the input plus a
// few values computed here so the template itself stays simple.
type hostapdData struct {
	HotspotInput
	Bridge         string
	SSIDHex        string
	SSIDForComment string
	// The guest network, if any.
	GuestInterface      string
	GuestBridge         string
	GuestSSIDHex        string
	GuestSSIDForComment string
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
	if strings.ContainsAny(in.AcceptMACFile, "\n\r") {
		return "", fmt.Errorf("invalid allow list file name %q", in.AcceptMACFile)
	}
	if in.MaxDevices < 0 || in.MaxDevices > config.MaxDevicesLimit {
		return "", fmt.Errorf("device limit %d is out of range", in.MaxDevices)
	}
	if g := in.Guest; g != nil {
		if in.Open {
			return "", fmt.Errorf("the open setup network has no guest network")
		}
		if err := config.ValidateWiFiPassword(g.Password); err != nil {
			return "", fmt.Errorf("guest network password: %w", err)
		}
		if err := config.ValidateSSID(g.SSID); err != nil {
			return "", fmt.Errorf("guest network name: %w", err)
		}
		if _, err := net.ParseMAC(g.BSSID); err != nil {
			return "", fmt.Errorf("guest network address: %w", err)
		}
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
	if g := in.Guest; g != nil {
		data.GuestInterface = GuestInterface
		data.GuestBridge = GuestBridgeName
		data.GuestSSIDHex = hex.EncodeToString([]byte(g.SSID))
		data.GuestSSIDForComment = commentSafe(g.SSID)
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

// PluginSubnet is the network RasBeacon's plugins run in (see
// internal/plugins). Each plugin has a fixed address in it.
var PluginSubnet = netip.MustParsePrefix("10.89.0.0/24")

// --- dnsmasq (DHCP and DNS) ---

// DnsmasqInput is everything needed to write dnsmasq.conf.
type DnsmasqInput struct {
	LAN       LAN
	LeaseFile string
	// Hostname is RasBeacon's own name; <Hostname>.lan points to it, as
	// does rasbeacon.lan.
	Hostname string
	// Captive is true during first-boot setup: every name points to
	// RasBeacon so phones open the setup page automatically.
	Captive bool
	// Upstream lists the DNS servers that questions about internet names are
	// passed on to (normally 127.0.0.53, the device's own resolver, which
	// uses the DNS servers of the current uplink): addresses, or address#port
	// (the DoH forwarder, 127.0.0.1#5053). Ignored when Captive.
	Upstream []string
	// NoticeHostnames, when set, are answered with RasBeacon's own address,
	// so devices' connectivity checks open RasBeacon's "this network needs
	// a login" notice (see internal/agent/portal.go). Ignored when Captive.
	NoticeHostnames []string
	// IPv6DefaultRoute tells devices that RasBeacon can carry their IPv6
	// internet traffic. When false, devices still get private IPv6
	// addresses (if LAN.IPv6Prefix is set) but no IPv6 route to the
	// internet, so they use IPv4 instead of waiting for IPv6 to time out.
	IPv6DefaultRoute bool
	// Guest is the guest network, if it is on (see LAN.Guest).
	Guest *LAN
	// Metered tells Android devices the connection is limited (see
	// config.HotspotConfig.Metered).
	Metered bool
	// Plugins makes dnsmasq answer plugins' DNS questions too, on the
	// plugins' network (PluginBridge must exist). Ignored when Captive.
	Plugins bool
}

type dnsmasqData struct {
	Metered          bool
	GuestBridge      string
	GuestAddress     string
	GuestNetmask     string
	GuestDHCPStart   string
	GuestDHCPEnd     string
	Bridge           string
	LANAddress       string
	Netmask          string
	DHCPStart        string
	DHCPEnd          string
	LeaseFile        string
	Hostname         string
	Captive          bool
	CaptivePortalAPI string
	Upstream         []string
	IPv6             bool
	IPv6DefaultRoute bool
	NoticeHostnames  []string
	PluginBridge     string
}

// Dnsmasq produces dnsmasq.conf text.
func Dnsmasq(in DnsmasqInput) (string, error) {
	data := dnsmasqData{
		Bridge:           BridgeName,
		LANAddress:       in.LAN.Address(),
		Netmask:          in.LAN.Netmask(),
		DHCPStart:        in.LAN.hostAddress(dhcpFirst),
		DHCPEnd:          in.LAN.hostAddress(dhcpLast),
		LeaseFile:        in.LeaseFile,
		Captive:          in.Captive,
		CaptivePortalAPI: "http://" + in.LAN.Address() + "/api/v1/captive",
		Upstream:         in.Upstream,
		IPv6:             in.LAN.IPv6Prefix.IsValid(),
		IPv6DefaultRoute: in.IPv6DefaultRoute,
	}
	if !in.Captive {
		data.NoticeHostnames = in.NoticeHostnames
		data.Metered = in.Metered
		if in.Plugins {
			data.PluginBridge = PluginBridge
		}
		if g := in.Guest; g != nil {
			data.GuestBridge = GuestBridgeName
			data.GuestAddress = g.Address()
			data.GuestNetmask = g.Netmask()
			data.GuestDHCPStart = g.hostAddress(dhcpFirst)
			data.GuestDHCPEnd = g.hostAddress(dhcpLast)
		}
	}
	if in.Hostname != "" && in.Hostname != "rasbeacon" {
		if strings.ContainsAny(in.Hostname, "/.\n\r ") {
			return "", fmt.Errorf("invalid host name %q", in.Hostname)
		}
		data.Hostname = in.Hostname
	}
	for _, name := range data.NoticeHostnames {
		if strings.ContainsAny(name, "/\n\r ") || name == "" {
			return "", fmt.Errorf("invalid host name %q", name)
		}
	}
	for _, server := range in.Upstream {
		// An address, or an address and port in dnsmasq's form (127.0.0.1#5053).
		host, port, hasPort := strings.Cut(server, "#")
		if _, err := netip.ParseAddr(host); err != nil {
			return "", fmt.Errorf("upstream DNS server %q is not an IP address", server)
		}
		if n, err := strconv.Atoi(port); hasPort && (err != nil || n < 1 || n > 65535) {
			return "", fmt.Errorf("upstream DNS server %q has no valid port", server)
		}
	}
	return execute("dnsmasq.conf.tmpl", data)
}

// --- nftables (firewall) ---

// FirewallInput is everything needed to write the firewall rules.
type FirewallInput struct {
	SSHEnabled bool
	// Egress lists the interfaces devices' traffic may leave through (with
	// NAT): the uplinks, e.g. "eth0" and "wlan0", or only "wg0" when the
	// VPN is in use. Empty means nothing is forwarded, as during first-boot
	// setup.
	Egress []string
	// TunnelDNS are the VPN's DNS servers. RasBeacon may only reach them
	// through the tunnel (TunnelInterface), so lookups never leak to the
	// uplink, even if a route is missing.
	TunnelDNS       []netip.Addr
	TunnelInterface string
	// PortalOnly limits forwarding to web and DNS (TCP 80/443, port 53),
	// for logging in to a captive portal while the VPN can't connect.
	PortalOnly bool
	// Guest adds the guest network: its devices may use the internet like
	// yours, but reach nothing else (not your devices, not RasBeacon's
	// settings, only its DHCP and DNS).
	Guest bool
	// PausedMACs are devices whose internet access the owner paused (or
	// that used up their data): the firewall refuses to forward anything
	// from them.
	PausedMACs []string
	// PausedIPv4 and PausedIPv6 are those devices' addresses on the
	// hotspot (from the DHCP leases and the neighbor table): nothing is
	// forwarded to them either.
	PausedIPv4 []netip.Addr
	PausedIPv6 []netip.Addr
	// Browser confines the login browser (see internal/browser) to the
	// uplinks. nil when there is no browser user.
	Browser *BrowserRule
	// RoutedIPv6 is the public IPv6 network the VPN server routes to
	// RasBeacon (see config.VPNConfig.RoutedIPv6Prefix). Devices' traffic
	// from it leaves through the tunnel without NAT. Needs TunnelInterface.
	RoutedIPv6 netip.Prefix
	// Plugins are the installed plugins (see internal/plugin): what each
	// may reach, and its ports on RasBeacon.
	Plugins []PluginRule
	// LANAddress is RasBeacon's address on the hotspot, where devices reach
	// plugins' ports. Needed with Plugins.
	LANAddress netip.Addr
}

// PluginRule is one plugin's place in the firewall.
type PluginRule struct {
	// Address is its address on the plugins' network (PluginSubnet).
	Address netip.Addr
	// Internet lets it reach the internet the way devices do (through the
	// VPN when that is in use); LANAccess lets it reach your devices.
	Internet  bool
	LANAccess bool
	Ports     []PluginPort
}

// PluginPort sends a port on RasBeacon (Host) to a plugin's port.
type PluginPort struct {
	Protocol  string
	Host      int
	Container int
}

// PluginBridge is the plugins' network's interface, and PluginGateway
// RasBeacon's address on it. Each plugin's container is plugged into the
// bridge by a port whose name starts with PluginPortPrefix (named by
// plugin.HostPortName; the agent's tests check the two packages agree).
const (
	PluginBridge     = "rbplug0"
	PluginGateway    = "10.89.0.1"
	PluginPortPrefix = "rbport"
)

type pluginPortData struct {
	Protocol  string
	Host      int
	Container int
	Address   string
}

// BrowserRule says which user runs the login browser and where it may go.
type BrowserRule struct {
	// UID is the browser user's number (the firewall matches on it).
	UID int
	// Uplinks are the only interfaces it may use (the hotel network).
	// Empty means it may reach nothing.
	Uplinks []string
}

type firewallData struct {
	PausedMACs      []string
	PausedIPv4      []string
	PausedIPv6      []string
	Browser         *BrowserRule
	RoutedIPv6      string
	PortalOnly      bool
	Guest           bool
	GuestBridge     string
	Bridge          string
	SSHEnabled      bool
	Egress          []string
	TunnelDNS4      []string
	TunnelDNS6      []string
	TunnelInterface string
	PluginBridge    string
	// PluginBridgePorts matches the names of the plugins' ports on their
	// bridge, such as "rbport*".
	PluginBridgePorts string
	PluginGateway     string
	PluginInternet    []string
	PluginLAN         []string
	PluginPorts       []pluginPortData
	LANAddress        string
}

// Firewall produces the nftables ruleset.
func Firewall(in FirewallInput) (string, error) {
	for _, name := range append(slices.Clone(in.Egress), in.TunnelInterface) {
		if name != "" && !isInterfaceName(name) {
			return "", fmt.Errorf("invalid interface name %q", name)
		}
	}
	data := firewallData{
		Bridge:            BridgeName,
		SSHEnabled:        in.SSHEnabled,
		Egress:            in.Egress,
		TunnelInterface:   in.TunnelInterface,
		PortalOnly:        in.PortalOnly,
		Guest:             in.Guest,
		GuestBridge:       GuestBridgeName,
		PluginBridge:      PluginBridge,
		PluginGateway:     PluginGateway,
		PluginBridgePorts: PluginPortPrefix + "*",
	}
	if len(in.Plugins) > 0 && !in.LANAddress.Is4() {
		return "", fmt.Errorf("plugins need RasBeacon's hotspot address")
	}
	data.LANAddress = in.LANAddress.String()
	usedPorts := map[string]bool{}
	for _, p := range in.Plugins {
		if !PluginSubnet.Contains(p.Address) || p.Address.String() == PluginGateway {
			return "", fmt.Errorf("plugin address %s is not on the plugins' network", p.Address)
		}
		if p.Internet {
			data.PluginInternet = append(data.PluginInternet, p.Address.String())
		}
		if p.LANAccess {
			data.PluginLAN = append(data.PluginLAN, p.Address.String())
		}
		for _, port := range p.Ports {
			key := fmt.Sprintf("%s/%d", port.Protocol, port.Host)
			if port.Protocol != "tcp" && port.Protocol != "udp" || port.Host < 1 || port.Host > 65535 ||
				port.Container < 1 || port.Container > 65535 || usedPorts[key] {
				return "", fmt.Errorf("invalid or repeated plugin port %s", key)
			}
			usedPorts[key] = true
			data.PluginPorts = append(data.PluginPorts, pluginPortData{
				Protocol: port.Protocol, Host: port.Host, Container: port.Container, Address: p.Address.String(),
			})
		}
	}
	if len(in.TunnelDNS) > 0 && in.TunnelInterface == "" {
		return "", fmt.Errorf("tunnel DNS servers need a tunnel interface")
	}
	for _, mac := range in.PausedMACs {
		hw, err := net.ParseMAC(mac)
		if err != nil || len(hw) != 6 {
			return "", fmt.Errorf("invalid MAC address %q", mac)
		}
		data.PausedMACs = append(data.PausedMACs, hw.String())
	}
	for _, addr := range in.PausedIPv4 {
		if !addr.Is4() {
			return "", fmt.Errorf("invalid IPv4 address %v", addr)
		}
		data.PausedIPv4 = append(data.PausedIPv4, addr.String())
	}
	for _, addr := range in.PausedIPv6 {
		if !addr.Is6() || addr.Is4In6() {
			return "", fmt.Errorf("invalid IPv6 address %v", addr)
		}
		data.PausedIPv6 = append(data.PausedIPv6, addr.String())
	}
	if b := in.Browser; b != nil {
		if b.UID <= 0 {
			return "", fmt.Errorf("the login browser can't run as root")
		}
		for _, name := range b.Uplinks {
			if !isInterfaceName(name) {
				return "", fmt.Errorf("invalid interface name %q", name)
			}
		}
		data.Browser = b
	}
	if in.RoutedIPv6.IsValid() {
		if in.TunnelInterface == "" {
			return "", fmt.Errorf("a routed IPv6 network needs a tunnel interface")
		}
		if err := config.ValidateRoutedIPv6Prefix(in.RoutedIPv6); err != nil {
			return "", fmt.Errorf("routed IPv6 network: %w", err)
		}
		data.RoutedIPv6 = in.RoutedIPv6.String()
	}
	for _, dns := range in.TunnelDNS {
		if dns.Is4() {
			data.TunnelDNS4 = append(data.TunnelDNS4, dns.String())
		} else {
			data.TunnelDNS6 = append(data.TunnelDNS6, dns.String())
		}
	}
	return execute("firewall.nft.tmpl", data)
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
