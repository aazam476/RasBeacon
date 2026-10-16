package wireguard

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// DefaultMTU is the tunnel's packet size. 1380 (rather than WireGuard's
// usual 1420) leaves room for hotel networks that wrap packets in extra
// layers (PPPoE, other tunnels), at a cost of about 3% speed.
const DefaultMTU = 1380

// DefaultKeepalive keeps the tunnel alive through hotel routers that forget
// idle connections after a minute or two, and lets RasBeacon notice quickly
// when the tunnel stops working.
const DefaultKeepalive = 25

// Config is a parsed WireGuard configuration with one server ("peer").
type Config struct {
	// PrivateKey is RasBeacon's own secret key.
	PrivateKey Key
	// Addresses are RasBeacon's addresses inside the tunnel, e.g.
	// 10.66.0.2/32 and fd00:66::2/128.
	Addresses []netip.Prefix
	// DNS servers to use through the tunnel.
	DNS []netip.Addr
	// MTU is 0 when the file doesn't set one (DefaultMTU is then used).
	MTU  int
	Peer Peer
}

// Peer is the WireGuard server.
type Peer struct {
	PublicKey Key
	// PresharedKey adds an extra secret shared with the server. Optional.
	PresharedKey    Key
	HasPresharedKey bool
	// Endpoint is the server's address, "host:port", e.g.
	// "vpn.example.com:51820".
	Endpoint string
	// AllowedIPs are the addresses that go through the tunnel. RasBeacon
	// sends everything through it, so this must include 0.0.0.0/0.
	AllowedIPs []netip.Prefix
	// PersistentKeepalive in seconds; 0 when the file doesn't set it.
	PersistentKeepalive int
}

// refusedKeys are wg-quick settings that run commands or change how the
// tunnel is routed. RasBeacon manages routing itself and never runs
// commands from a file.
var refusedKeys = map[string]bool{
	"preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true, "table": true, "fwmark": true,
}

// ParseConf reads a WireGuard .conf file (wg-quick format). Errors are
// written for the owner, e.g. "Line 7: ...".
func ParseConf(text string) (Config, error) {
	var c Config
	section := ""
	peers := 0
	seen := map[string]bool{}

	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		lineNumber := i + 1
		line := strings.TrimSpace(raw)
		if hash := strings.Index(line, "#"); hash >= 0 {
			line = strings.TrimSpace(line[:hash])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.Trim(line, "[] "))
			switch section {
			case "interface":
			case "peer":
				peers++
				if peers > 1 {
					return c, fmt.Errorf("Line %d: the file has more than one [Peer]. RasBeacon connects to one WireGuard server; remove the others.", lineNumber)
				}
			default:
				return c, fmt.Errorf("Line %d: unknown section %q.", lineNumber, line)
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return c, fmt.Errorf("Line %d: expected \"Name = value\".", lineNumber)
		}
		name := strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if refusedKeys[name] {
			return c, fmt.Errorf("Line %d: %s is not supported. RasBeacon never runs commands from a WireGuard file and sets up routing itself; remove this line.", lineNumber, strings.TrimSpace(key))
		}
		if section == "" {
			return c, fmt.Errorf("Line %d: setting outside [Interface] or [Peer].", lineNumber)
		}
		if seen[section+"."+name] && name != "address" && name != "dns" && name != "allowedips" {
			return c, fmt.Errorf("Line %d: %s appears twice.", lineNumber, strings.TrimSpace(key))
		}
		seen[section+"."+name] = true

		if err := c.set(section, name, value); err != nil {
			return c, fmt.Errorf("Line %d (%s): %v", lineNumber, strings.TrimSpace(key), err)
		}
	}
	if !seen["interface.privatekey"] {
		return c, fmt.Errorf("The [Interface] section needs a PrivateKey.")
	}
	if peers == 0 {
		return c, fmt.Errorf("The file needs a [Peer] section (the WireGuard server).")
	}
	return c, c.Validate()
}

// set applies one "name = value" line.
func (c *Config) set(section, name, value string) error {
	var err error
	switch section + "." + name {
	case "interface.privatekey":
		c.PrivateKey, err = ParseKey(value)
	case "interface.address":
		var prefixes []netip.Prefix
		prefixes, err = parsePrefixList(value, true)
		c.Addresses = append(c.Addresses, prefixes...)
	case "interface.dns":
		for _, item := range splitList(value) {
			addr, parseErr := netip.ParseAddr(item)
			if parseErr != nil {
				// wg-quick also allows search domains here; RasBeacon
				// doesn't use them.
				continue
			}
			c.DNS = append(c.DNS, addr)
		}
	case "interface.mtu":
		c.MTU, err = strconv.Atoi(value)
	case "interface.listenport":
		// Ignored: RasBeacon lets the kernel pick a port, which works
		// better behind hotel routers.
	case "peer.publickey":
		c.Peer.PublicKey, err = ParseKey(value)
	case "peer.presharedkey":
		c.Peer.PresharedKey, err = ParseKey(value)
		c.Peer.HasPresharedKey = err == nil
	case "peer.endpoint":
		c.Peer.Endpoint = value
	case "peer.allowedips":
		var prefixes []netip.Prefix
		prefixes, err = parsePrefixList(value, false)
		c.Peer.AllowedIPs = append(c.Peer.AllowedIPs, prefixes...)
	case "peer.persistentkeepalive":
		if value == "off" {
			c.Peer.PersistentKeepalive = 0
		} else {
			c.Peer.PersistentKeepalive, err = strconv.Atoi(value)
		}
	default:
		return fmt.Errorf("unknown setting")
	}
	return err
}

// Validate checks a configuration, whether it came from a file or was
// typed into the web UI.
func (c Config) Validate() error {
	if len(c.Addresses) == 0 {
		return fmt.Errorf("The [Interface] section needs an Address (RasBeacon's address inside the tunnel).")
	}
	if c.MTU != 0 && (c.MTU < 1280 || c.MTU > 1500) {
		return fmt.Errorf("MTU must be between 1280 and 1500.")
	}
	if err := ValidateEndpoint(c.Peer.Endpoint); err != nil {
		return err
	}
	if c.Peer.PersistentKeepalive < 0 || c.Peer.PersistentKeepalive > 3600 {
		return fmt.Errorf("PersistentKeepalive must be between 0 and 3600 seconds.")
	}
	if !c.SendsAllIPv4() {
		return fmt.Errorf("AllowedIPs must include 0.0.0.0/0. RasBeacon sends all of your devices' traffic through the tunnel; split tunnels are not supported.")
	}
	if len(c.DNS) == 0 {
		return fmt.Errorf("The [Interface] section needs a DNS server (reached through the tunnel), e.g. \"DNS = 10.66.0.1\" or your VPN provider's. Without one, name lookups would bypass the VPN.")
	}
	if c.Peer.PublicKey == (Key{}) {
		return fmt.Errorf("The [Peer] section needs the server's PublicKey.")
	}
	return nil
}

// SendsAllIPv4 reports whether AllowedIPs covers all IPv4 traffic.
func (c Config) SendsAllIPv4() bool {
	for _, p := range c.Peer.AllowedIPs {
		if p.Addr().Is4() && p.Bits() == 0 {
			return true
		}
	}
	return false
}

// CarriesIPv6 reports whether devices' IPv6 traffic can use the tunnel:
// RasBeacon needs an IPv6 address inside it and AllowedIPs must include ::/0.
func (c Config) CarriesIPv6() bool {
	hasAddress := false
	for _, a := range c.Addresses {
		hasAddress = hasAddress || a.Addr().Is6()
	}
	for _, p := range c.Peer.AllowedIPs {
		if p.Addr().Is6() && p.Bits() == 0 {
			return hasAddress
		}
	}
	return false
}

// EffectiveMTU is the MTU to use (DefaultMTU when none was set).
func (c Config) EffectiveMTU() int {
	if c.MTU == 0 {
		return DefaultMTU
	}
	return c.MTU
}

// EffectiveKeepalive is the keepalive to use (DefaultKeepalive when none
// was set, because RasBeacon relies on regular handshakes to know the
// tunnel works).
func (c Config) EffectiveKeepalive() int {
	if c.Peer.PersistentKeepalive == 0 {
		return DefaultKeepalive
	}
	return c.Peer.PersistentKeepalive
}

// ValidateEndpoint checks "host:port", where host is a name or an IP
// address (IPv6 in brackets).
func ValidateEndpoint(endpoint string) error {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" {
		return fmt.Errorf("Endpoint must look like vpn.example.com:51820 or 203.0.113.5:51820.")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("The Endpoint's port must be a number from 1 to 65535.")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil
	}
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return fmt.Errorf("The Endpoint's host name contains characters that are not allowed.")
		}
	}
	return nil
}

// parsePrefixList reads "10.0.0.2/32, fd00::2/128". A bare address counts
// as a single address (/32 or /128). keepHost keeps the host bits (for
// Address); otherwise the network is masked (for AllowedIPs).
func parsePrefixList(value string, keepHost bool) ([]netip.Prefix, error) {
	var result []netip.Prefix
	for _, item := range splitList(value) {
		var prefix netip.Prefix
		if strings.Contains(item, "/") {
			p, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, fmt.Errorf("%q is not an address with network size, like 10.66.0.2/32", item)
			}
			prefix = p
		} else {
			addr, err := netip.ParseAddr(item)
			if err != nil {
				return nil, fmt.Errorf("%q is not an IP address", item)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		if !keepHost {
			prefix = prefix.Masked()
		}
		result = append(result, prefix)
	}
	return result, nil
}

// splitList splits a comma-separated value, dropping empty items.
func splitList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
