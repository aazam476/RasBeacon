// Package config defines RasBeacon's settings and checks that they are valid.
//
// Settings are split into three files so that secrets are easy to protect:
//
//   - Config (config.json): everything that is safe to show in the UI or put
//     in a diagnostics export, like the hotspot name.
//   - Secrets (secrets.json, readable by root only): passwords and keys.
//   - Identity (identity.json): values generated once on first boot that
//     never change afterwards, like the device ID.
//
// Reading and writing the files is done by store.go. This file only
// describes what the settings are and which values are allowed.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// CurrentVersion is the format version of config.json. It goes up when a
// release changes the file layout, so newer code can upgrade older files
// (see FillDefaults).
//
//	1: milestones 1 to 3
//	2: adds the VPN section, with the kill switch on by default
//	3: the VPN keeps several WireGuard profiles (see MoveVPNIntoProfile)
//	4: v0.2.0. The kill switch with the VPN off now blocks devices, so it
//	   is turned off where the VPN was off (devices keep their internet).
//	   The online update check's on/off becomes a schedule (CheckHours).
const CurrentVersion = 4

// Band is the Wi-Fi frequency band the hotspot uses.
type Band string

const (
	// Band2GHz is the default. It reaches further and every device supports
	// it (including all Apple Watch models), but it is slower and busier.
	Band2GHz Band = "2.4GHz"
	// Band5GHz is faster but has shorter range.
	Band5GHz Band = "5GHz"
)

// Security is the kind of password protection the hotspot uses.
type Security string

const (
	// SecurityWPA2WPA3 lets new devices use WPA3 while older ones use WPA2.
	// This is the default.
	SecurityWPA2WPA3 Security = "wpa2-wpa3"
	// SecurityWPA2 is a compatibility setting for old devices that fail to
	// join a WPA2/WPA3 network.
	SecurityWPA2 Security = "wpa2"
	// SecurityWPA3 allows WPA3 (SAE, with protected management frames)
	// only: the safest, but devices older than about 2019 can't join.
	SecurityWPA3 Security = "wpa3"
)

// Config holds all non-secret settings.
type Config struct {
	// Version is the file format version (see CurrentVersion).
	Version int `json:"version"`

	// Claimed becomes true once the owner has finished first-time setup by
	// choosing an admin password. Before that, the device runs the open (or
	// setup.txt-protected) setup network.
	Claimed bool `json:"claimed"`

	// Hostname is RasBeacon's name on the network (devices on the hotspot
	// reach it at <hostname>.lan). Empty means DefaultHostname.
	Hostname string `json:"hostname,omitempty"`

	Hotspot  HotspotConfig  `json:"hotspot"`
	Access   AccessConfig   `json:"access"`
	Guest    GuestConfig    `json:"guest"`
	SSH      SSHConfig      `json:"ssh"`
	Ethernet EthernetConfig `json:"ethernet"`
	Uplink   UplinkConfig   `json:"uplink"`
	VPN      VPNConfig      `json:"vpn"`
	HTTPS    HTTPSConfig    `json:"https"`
	Updates  UpdatesConfig  `json:"updates"`
	Clients  ClientsConfig  `json:"clients"`
	DNS      DNSConfig      `json:"dns"`
	Swap     SwapConfig     `json:"swap"`
	Plugins  PluginsConfig  `json:"plugins"`
}

// PluginsConfig is the installed plugins and where the catalog looks for
// more (see internal/plugin and agent/plugins.go).
type PluginsConfig struct {
	// Installed plugins, by ID.
	Installed map[string]InstalledPlugin `json:"installed,omitempty"`
	// Sources are extra GitHub repositories ("owner/repo") for the catalog,
	// besides the built-in one.
	Sources []string `json:"sources,omitempty"`
	// NoBuiltInSource is true when the owner removed the built-in source
	// (RasBeacon's own plugins) from the catalog.
	NoBuiltInSource bool `json:"noBuiltInSource,omitempty"`
}

// InstalledPlugin is one installed plugin's settings. Its manifest, icon and
// templates are kept in its folder (paths.PluginDir).
type InstalledPlugin struct {
	Version string `json:"version"`
	// Source is "file" (installed from a file) or the catalog's "owner/repo".
	Source string `json:"source"`
	// PublicKey is the signing key pinned at the first signed install
	// (base64); updates must be signed by it. Empty: unsigned so far.
	PublicKey string `json:"publicKey,omitempty"`
	// Image is the local image name it runs.
	Image string `json:"image"`
	// Address is its fixed address on the plugins' network.
	Address netip.Addr `json:"address"`
	// Ports maps "tcp/3000" (protocol/container port) to the port on
	// RasBeacon the owner chose.
	Ports map[string]int `json:"ports,omitempty"`
	// Settings are the non-secret settings (secrets are in Secrets.Plugins).
	Settings map[string]string `json:"settings,omitempty"`
	// Drive is the UUID of the USB drive it uses ("" = none).
	Drive string `json:"drive,omitempty"`
	// Running is whether it should run: kept across restarts.
	Running bool `json:"running"`
	// StoppedAfterCrashes is set when it crashed too often and was stopped;
	// it starts again only when the owner starts it.
	StoppedAfterCrashes bool `json:"stoppedAfterCrashes,omitempty"`
	// Fetch, after a restore from a backup without its image, says where
	// to download the image again; cleared once it is here.
	Fetch *PluginFetch `json:"fetch,omitempty"`
}

// PluginFetch is where a plugin's image can be downloaded again: its .rba
// file in a catalog's release, or a published image.
type PluginFetch struct {
	// URL is the .rba file (https). Its signature must match the plugin's
	// pinned key.
	URL string `json:"url,omitempty"`
	// Ref is a published image pinned by digest (name@sha256:...).
	Ref string `json:"ref,omitempty"`
}

// Clone returns a copy that can be changed without affecting p.
func (p InstalledPlugin) Clone() InstalledPlugin {
	p.Ports = maps.Clone(p.Ports)
	p.Settings = maps.Clone(p.Settings)
	if p.Fetch != nil {
		fetch := *p.Fetch
		p.Fetch = &fetch
	}
	return p
}

// SwapConfig is the size of the swap file on the data partition.
type SwapConfig struct {
	// SizeGB is in whole gigabytes; 0 turns swap off. Unset (nil) means the
	// default, DefaultSwapGB (less on a card too small for it).
	SizeGB *int `json:"sizeGb,omitempty"`
}

// DefaultSwapGB is the swap file's size until the owner changes it.
const DefaultSwapGB = 2

// DNSConfig chooses the DNS servers devices use while their traffic isn't
// going through the VPN (with the VPN in use, its own servers are used).
type DNSConfig struct {
	// Servers are used instead of the current network's; empty uses the
	// network's (the default). While a login page is in the way, the
	// network's are used anyway: many only work with them.
	Servers []netip.Addr `json:"servers,omitempty"`
	// DoH are DNS-over-HTTPS servers (https://host/path, see ParseDoH),
	// asked through RasBeacon's DoH forwarder (cmd/rbdoh) alongside Servers.
	DoH []string `json:"doh,omitempty"`
	// Order is every server above, plain and DoH, in the order the owner
	// listed them: devices' lookups go to the first, then the next if it
	// doesn't answer. Settings saved before v0.2.0 have none: their plain
	// servers came first, then the DoH ones (see Ordered).
	Order []string `json:"order,omitempty"`
	// WithVPN uses these servers while devices' traffic goes through the
	// VPN too (normally the VPN's own DNS servers are used then). They are
	// then reached through the tunnel, and the DoH forwarder's traffic goes
	// through it as well.
	WithVPN bool `json:"withVpn,omitempty"`
}

// Ordered returns the servers in the order the owner listed them.
func (d DNSConfig) Ordered() []string {
	if len(d.Order) > 0 {
		return d.Order
	}
	var out []string
	for _, s := range d.Servers {
		out = append(out, s.String())
	}
	return append(out, d.DoH...)
}

// MaxDNSServers keeps the list short; dnsmasq asks them in turn.
const MaxDNSServers = 4

// Validate checks the DNS servers: real addresses of other machines, and
// DoH addresses as ParseDoH writes them.
func (d DNSConfig) Validate() error {
	if len(d.Servers)+len(d.DoH) > MaxDNSServers {
		return &ValidationError{Field: "servers", Message: fmt.Sprintf("At most %d DNS servers.", MaxDNSServers)}
	}
	for _, u := range d.DoH {
		if parsed, err := ParseDoH(u); err != nil || parsed != u {
			return &ValidationError{Field: "servers", Message: fmt.Sprintf("%q is not a DNS-over-HTTPS address like https://dns.example/dns-query.", u)}
		}
	}
	for _, s := range d.Servers {
		if !s.IsValid() || s.IsUnspecified() || s.IsLoopback() || s.IsMulticast() || s.IsLinkLocalUnicast() {
			return &ValidationError{Field: "servers", Message: fmt.Sprintf("%s can't be a DNS server: use the address of a DNS server, like 1.1.1.1.", s)}
		}
	}
	return nil
}

// ClientsConfig holds per-device settings.
type ClientsConfig struct {
	// Paused lists the devices whose internet access the owner paused, by
	// MAC address (lower case), with when it was paused. They stay paused
	// until the owner allows them again, also after a restart.
	Paused map[string]time.Time `json:"paused,omitempty"`
	// Names are the owner's names for devices ("Ali's iPhone"), by MAC
	// address (lower case). They replace the name a device sends.
	Names map[string]string `json:"names,omitempty"`
	// UsageLimitMB pauses a device that has used this much data (down and
	// up) since the counters were last reset. 0 is no limit.
	UsageLimitMB int `json:"usageLimitMb,omitempty"`
	// FixedAddresses are the devices that always get the same address on
	// the hotspot ("Always use this address"), by MAC address (lower case):
	// "10.87.0.50".
	FixedAddresses map[string]string `json:"fixedAddresses,omitempty"`
}

// MaxDeviceNameLength keeps device names to one line on a phone.
const MaxDeviceNameLength = 40

// MaxUsageLimitMB is 1 TB, far above any real limit.
const MaxUsageLimitMB = 1_000_000

// UpdatesConfig holds the update settings.
type UpdatesConfig struct {
	// CheckHours is how often RasBeacon asks GitHub whether a newer
	// release exists: 0 (only when asked, the default; RasBeacon contacts
	// nobody unless told to), 1, 6, 24 or 168 hours. It only tells the
	// owner: installing stays manual.
	CheckHours int `json:"checkHours,omitempty"`
	// CheckOnline is the on/off setting before v0.2.0 (on meant every 6
	// hours). FillDefaults turns it into CheckHours.
	CheckOnline bool `json:"checkOnline,omitempty"`
	// NotifiedVersion is the newest version a notification was sent for,
	// so each version is announced once (see Secrets.UpdateNotifyURL).
	NotifiedVersion string `json:"notifiedVersion,omitempty"`
}

// UpdateCheckHours are the choices for UpdatesConfig.CheckHours.
var UpdateCheckHours = []int{0, 1, 6, 24, 168}

// HTTPSConfig is the HTTPS setting for the web UI. The certificate and key
// are files in the data directory (see paths.Paths.TLSDir).
type HTTPSConfig struct {
	Enabled bool `json:"enabled"`
	// Uploaded is true when the owner uploaded their own certificate
	// (otherwise RasBeacon made one).
	Uploaded bool `json:"uploaded"`
}

// VPNConfig holds the WireGuard VPN settings: the switches, and the
// profiles (one per WireGuard server or configuration file) the owner kept.
// One profile is in use at a time.
type VPNConfig struct {
	// Enabled sends devices' traffic through the WireGuard tunnel.
	Enabled bool `json:"enabled"`
	// KillSwitch on (the default) means devices' traffic never leaves
	// outside the tunnel: if the tunnel is down, or the VPN is turned off,
	// nothing gets through. Off means traffic goes directly through the
	// uplink while the tunnel is down or off (see internal/tunnel). It only
	// acts once a WireGuard profile exists.
	KillSwitch bool `json:"killSwitch"`
	// Active is the name of the profile in use; "" when there is none.
	Active   string       `json:"active"`
	Profiles []VPNProfile `json:"profiles"`
}

// MaxVPNProfiles keeps the VPN page short.
const MaxVPNProfiles = 20

// Profile returns the profile called name.
func (v VPNConfig) Profile(name string) (VPNProfile, bool) {
	for _, p := range v.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return VPNProfile{}, false
}

// VPNProfile is one WireGuard configuration. Its private key and optional
// preshared key are kept in Secrets.WireGuardKeys under the same name.
type VPNProfile struct {
	// Name is unique, usually the imported file's name ("home.conf").
	Name string `json:"name"`
	// Addresses are RasBeacon's addresses inside the tunnel.
	Addresses []netip.Prefix `json:"addresses"`
	// DNS servers reached through the tunnel.
	DNS []netip.Addr `json:"dns"`
	// MTU is 0 for the default (wireguard.DefaultMTU).
	MTU  int     `json:"mtu"`
	Peer VPNPeer `json:"peer"`
	// RoutedIPv6Prefix is an optional public IPv6 network (a /64) that the
	// WireGuard server routes to RasBeacon. Devices then get real IPv6
	// addresses from it and their IPv6 traffic goes through the tunnel
	// without NAT. Unset (IsValid() false) means devices use RasBeacon's
	// private IPv6 network with NAT, as without this setting.
	RoutedIPv6Prefix netip.Prefix `json:"routedIpv6Prefix"`
}

// ValidateProfileName checks a profile name: 1 to 64 characters of
// printable text.
func ValidateProfileName(name string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 64 || !utf8.ValidString(name) {
		return &ValidationError{Field: "name", Message: "A profile name must be 1 to 64 characters."}
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return &ValidationError{Field: "name", Message: "A profile name can't contain control characters."}
		}
	}
	return nil
}

// Clone returns a copy of p that shares no lists with p.
func (p VPNProfile) Clone() VPNProfile {
	p.Addresses = slices.Clone(p.Addresses)
	p.DNS = slices.Clone(p.DNS)
	p.Peer.AllowedIPs = slices.Clone(p.Peer.AllowedIPs)
	return p
}

// MoveVPNIntoProfile upgrades the VPN settings of a version 2 config.json
// (raw is the file's text): version 2 kept a single server's settings in
// the VPN section and its keys in secrets.json. They become the profile
// "wireguard.conf", in use. It does nothing if no server was set up.
func MoveVPNIntoProfile(raw []byte, c *Config, s *Secrets) error {
	var old struct {
		VPN struct {
			VPNProfile
		} `json:"vpn"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return err
	}
	if old.VPN.Peer.PublicKey == "" && s.WireGuardPrivateKey == "" {
		return nil
	}
	profile := old.VPN.VPNProfile
	profile.Name = "wireguard.conf"
	c.VPN.Profiles = []VPNProfile{profile}
	c.VPN.Active = profile.Name
	if s.WireGuardKeys == nil {
		s.WireGuardKeys = map[string]WireGuardKeys{}
	}
	s.WireGuardKeys[profile.Name] = WireGuardKeys{PrivateKey: s.WireGuardPrivateKey, PresharedKey: s.WireGuardPresharedKey}
	s.WireGuardPrivateKey, s.WireGuardPresharedKey = "", ""
	return nil
}

// ValidateRoutedIPv6Prefix checks VPNConfig.RoutedIPv6Prefix: a public
// IPv6 network of exactly 64 bits (the size devices need to pick their own
// addresses), written as the network, like 2001:db8:1234:5678::/64.
func ValidateRoutedIPv6Prefix(p netip.Prefix) error {
	if !p.IsValid() {
		return nil // not used
	}
	if !p.Addr().Is6() || p.Addr().Is4In6() {
		return errors.New("Must be an IPv6 network, like 2001:db8:1234:5678::/64.")
	}
	if p.Bits() != 64 {
		return errors.New("Must be a /64 network: devices pick their own addresses in it, which needs 64 bits.")
	}
	if p.Masked() != p {
		return errors.New("Write the network itself, ending in ::/64.")
	}
	if !p.Addr().IsGlobalUnicast() || p.Addr().IsPrivate() {
		return errors.New("Must be a public IPv6 network (starting with 2 or 3), not a private (fd..) or local one.")
	}
	return nil
}

// VPNPeer is the WireGuard server.
type VPNPeer struct {
	// PublicKey is the server's key, in base64. Empty means no VPN has been
	// set up yet.
	PublicKey           string         `json:"publicKey"`
	Endpoint            string         `json:"endpoint"`
	AllowedIPs          []netip.Prefix `json:"allowedIps"`
	PersistentKeepalive int            `json:"persistentKeepalive"`
}

// HotspotConfig describes the Wi-Fi network RasBeacon broadcasts for your
// devices. The password is stored separately in Secrets.
type HotspotConfig struct {
	SSID     string   `json:"ssid"`
	Band     Band     `json:"band"`
	Security Security `json:"security"`
	// Channel 0 means "pick the default channel for the band".
	Channel int `json:"channel"`
	// Country is the two-letter regulatory country code (for example "US").
	// It decides which channels and transmit powers are legal.
	Country string `json:"country"`
	// Metered tells devices that the internet connection is limited (a
	// phone's hotspot, a paid plan), so they hold back large downloads.
	// Only Android reads this hint automatically (see render.Dnsmasq).
	Metered bool `json:"metered"`
}

// AccessConfig limits which and how many devices may join the hotspot.
// It only applies to Wi-Fi: devices plugged into an Ethernet port always get
// in.
type AccessConfig struct {
	// AllowListOn lets only the devices in Allowed join your network (the
	// guest network stays open to anyone with its password).
	AllowListOn bool `json:"allowListOn,omitempty"`
	// Allowed are MAC addresses, lowercase, like "3a:1f:22:aa:bb:cc".
	Allowed []string `json:"allowed,omitempty"`
	// MaxDevices is the most devices on each Wi-Fi network at once; 0
	// means no limit other than the addresses DHCP hands out.
	MaxDevices int `json:"maxDevices,omitempty"`
}

// MaxDevicesLimit is the most MaxDevices can be: the number of addresses
// DHCP hands out on a network (see render.dhcpFirst and dhcpLast).
const MaxDevicesLimit = 231

// MaxAllowedDevices is the most addresses the allow list may hold.
const MaxAllowedDevices = 256

// NormalizeMAC returns a MAC address in lowercase with colons, or an error
// for anything that isn't a device's own (unicast) address.
func NormalizeMAC(text string) (string, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(text))
	if err != nil || len(hw) != 6 {
		return "", fmt.Errorf("%q is not a MAC address such as 3a:1f:22:aa:bb:cc.", strings.TrimSpace(text))
	}
	if hw[0]&0x01 != 0 {
		return "", fmt.Errorf("%s is a group address, not a device's.", hw)
	}
	return hw.String(), nil
}

// Validate checks the access settings. The addresses must already be
// normalized (see NormalizeMAC).
func (c AccessConfig) Validate() error {
	if c.MaxDevices < 0 || c.MaxDevices > MaxDevicesLimit {
		return &ValidationError{Field: "maxDevices", Message: fmt.Sprintf("Choose a number from 1 to %d, or no limit.", MaxDevicesLimit)}
	}
	if len(c.Allowed) > MaxAllowedDevices {
		return &ValidationError{Field: "allowed", Message: fmt.Sprintf("The list can hold up to %d devices.", MaxAllowedDevices)}
	}
	seen := map[string]bool{}
	for _, mac := range c.Allowed {
		normal, err := NormalizeMAC(mac)
		if err != nil || normal != mac {
			return &ValidationError{Field: "allowed", Message: fmt.Sprintf("%q is not a MAC address such as 3a:1f:22:aa:bb:cc.", mac)}
		}
		if seen[mac] {
			return &ValidationError{Field: "allowed", Message: mac + " is listed twice."}
		}
		seen[mac] = true
	}
	if c.AllowListOn && len(c.Allowed) == 0 {
		return &ValidationError{Field: "allowed", Message: "Add at least one device, or nobody could join over Wi-Fi."}
	}
	return nil
}

// GuestConfig describes the optional guest Wi-Fi network: a second network
// name on the same radio for visitors. Guests can use the internet (through
// the VPN, if it is on) but can't reach your devices, each other, or
// RasBeacon's settings. The password is stored in Secrets.GuestPassword.
type GuestConfig struct {
	Enabled bool   `json:"enabled"`
	SSID    string `json:"ssid"`
}

// Validate checks the guest network settings. password is the guest
// network's password (from Secrets), hotspotSSID the main network's name.
func (g GuestConfig) Validate(password, hotspotSSID string) error {
	if !g.Enabled {
		return nil
	}
	if err := ValidateSSID(g.SSID); err != nil {
		return &ValidationError{Field: "ssid", Message: err.Error()}
	}
	if g.SSID == hotspotSSID {
		return &ValidationError{Field: "ssid", Message: "Must be different from your own network's name."}
	}
	if err := ValidateWiFiPassword(password); err != nil {
		return &ValidationError{Field: "password", Message: err.Error()}
	}
	return nil
}

// SSHConfig controls remote login for advanced users. It is off by default.
// The allowed public keys are stored in their own file (see paths.Paths).
type SSHConfig struct {
	Enabled bool `json:"enabled"`
}

// EthernetRole is what the Ethernet port is used for.
type EthernetRole string

const (
	// EthernetUplink (the default): the port connects RasBeacon to the
	// internet, for example a hotel room's wall socket.
	EthernetUplink EthernetRole = "uplink"
	// EthernetClientPort: the port is for one of your own devices (a laptop
	// with a cable). It joins the hotspot's network as if it were on Wi-Fi.
	EthernetClientPort EthernetRole = "client-port"
)

// EthernetConfig holds the Ethernet port settings.
type EthernetConfig struct {
	Role EthernetRole `json:"role"`
}

// UplinkSecurity is how a saved upstream Wi-Fi network is protected.
type UplinkSecurity string

const (
	// UplinkOpen: no password (many hotels and cafés).
	UplinkOpen UplinkSecurity = "open"
	// UplinkPersonal: one shared password, WPA2-Personal or WPA3-Personal.
	UplinkPersonal UplinkSecurity = "personal"
	// UplinkEnterprise: a personal username and password (or certificate),
	// WPA2-Enterprise or WPA3-Enterprise. Used by eduroam, universities and
	// companies. See EnterpriseConfig.
	UplinkEnterprise UplinkSecurity = "enterprise"
)

// EAPMethod is how RasBeacon proves who you are to an Enterprise network.
// Your university or company tells you which one to use.
type EAPMethod string

const (
	// EAPTTLSPAP: username and password inside an encrypted tunnel
	// (EAP-TTLS with PAP). eduroam often uses this.
	EAPTTLSPAP EAPMethod = "ttls-pap"
	// EAPPEAP: username and password with PEAP and MSCHAPv2 (common in
	// Windows-based networks).
	EAPPEAP EAPMethod = "peap-mschapv2"
	// EAPTLS: a personal certificate and key instead of a password.
	EAPTLS EAPMethod = "tls"
)

// EnterpriseConfig holds the settings of a WPA-Enterprise network. The
// password (or the client key's passphrase, for EAP-TLS) is kept in
// Secrets.UplinkPasswords; the certificates are files in the certificates
// directory (see paths.Paths).
type EnterpriseConfig struct {
	Method EAPMethod `json:"method"`
	// Identity is your username, e.g. "user@example.com".
	Identity string `json:"identity"`
	// AnonymousIdentity is sent before the encrypted tunnel is set up, so
	// your real username is not visible to the hotspot, e.g.
	// "anonymous@example.com". Optional.
	AnonymousIdentity string `json:"anonymousIdentity"`
	// ServerName is the name in the login server's certificate, as your
	// company or university publishes it, e.g. "*.radius.example.com".
	// RasBeacon only talks to a server whose certificate matches it (see
	// ServerDomain). Optional.
	ServerName string `json:"serverName"`
	// ServerCertHash is the SHA-256 of the login server's certificate,
	// remembered at the first connection when neither a CA certificate nor
	// a server name was given ("trust on first connect", as phones do).
	// From then on only that certificate is accepted.
	ServerCertHash string `json:"serverCertHash,omitempty"`
	// AllowNoCertificateCheck turns off checking the login server's
	// certificate. Anyone could then pretend to be the network and collect
	// your password, so this is off by default and the UI warns about it.
	AllowNoCertificateCheck bool `json:"allowNoCertificateCheck"`
}

// ServerDomain turns the server name as networks publish it into the
// domain RasBeacon requires in the server's certificate:
// "*.radius.example.com" becomes "radius.example.com", which matches that
// name and every name ending in ".radius.example.com".
func (e EnterpriseConfig) ServerDomain() string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e.ServerName), "*."))
}

// UplinkConfig lists the upstream Wi-Fi networks RasBeacon may join.
type UplinkConfig struct {
	// Networks are in order of preference: when several are in range, the
	// first one in the list is joined.
	Networks []SavedNetwork `json:"networks"`
}

// SavedNetwork is one upstream Wi-Fi network. Its password (if any) is kept
// in Secrets.UplinkPasswords under the same ID.
type SavedNetwork struct {
	// ID is a random identifier that stays the same when the network is
	// renamed or reordered. It links the network to its password.
	ID       string         `json:"id"`
	SSID     string         `json:"ssid"`
	Security UplinkSecurity `json:"security"`
	// Hidden networks don't announce their name, so RasBeacon has to ask
	// for them by name.
	Hidden bool `json:"hidden"`
	// RandomMAC joins this network with a random MAC address instead of
	// the Pi's own, so the network can't recognize the Pi across visits.
	// wpa_supplicant 2.10 keeps it while RasBeacon runs and picks a new one
	// at the next start.
	RandomMAC bool `json:"randomMac,omitempty"`
	// Enterprise is set only when Security is UplinkEnterprise.
	Enterprise *EnterpriseConfig `json:"enterprise,omitempty"`
}

// NetworkCredentials says which secrets and certificates a saved network
// has. Validate needs it because they are stored outside config.json.
type NetworkCredentials struct {
	Password      string
	HasCACert     bool
	HasClientCert bool
	HasClientKey  bool
}

// MaxSavedNetworks keeps the list (and wpa_supplicant's scanning) small.
const MaxSavedNetworks = 20

// FillDefaults sets values for settings that were added in a later release
// and are therefore missing from an older config.json.
func (c *Config) FillDefaults() {
	if c.Ethernet.Role == "" {
		c.Ethernet.Role = EthernetUplink
	}
	if c.Guest.SSID == "" {
		// Settings from before the guest network existed.
		c.Guest.SSID = GuestSSIDFor(c.Hotspot.SSID)
	}
	if c.Version < 2 {
		// Version 2 added the VPN. Its kill switch starts on.
		c.VPN.KillSwitch = true
	}
	if c.Version < 4 && c.Updates.CheckOnline {
		// "Check online" checked every 6 hours.
		c.Updates.CheckHours, c.Updates.CheckOnline = 6, false
	}
	if c.Version < 4 && !c.VPN.Enabled {
		// Until v0.2.0 the kill switch did nothing while the VPN was off;
		// now it blocks devices then. Keep their internet as it was.
		c.VPN.KillSwitch = false
	}
	c.Version = CurrentVersion
}

// Validate checks the Ethernet settings.
func (e EthernetConfig) Validate() error {
	if e.Role != EthernetUplink && e.Role != EthernetClientPort {
		return &ValidationError{Field: "ethernetRole", Message: `Must be "uplink" or "client-port".`}
	}
	return nil
}

// Validate checks the saved networks. credentials maps network IDs to their
// passwords and certificates; every network must have what it needs.
func (u UplinkConfig) Validate(credentials map[string]NetworkCredentials) error {
	if len(u.Networks) > MaxSavedNetworks {
		return &ValidationError{Field: "networks", Message: fmt.Sprintf("At most %d networks can be saved.", MaxSavedNetworks)}
	}
	seenIDs := map[string]bool{}
	seenSSIDs := map[string]bool{}
	for i, n := range u.Networks {
		field := fmt.Sprintf("networks.%d", i)
		if n.ID == "" || seenIDs[n.ID] {
			return &ValidationError{Field: field + ".id", Message: "Each network needs its own ID."}
		}
		seenIDs[n.ID] = true
		if err := ValidateSSID(n.SSID); err != nil {
			return &ValidationError{Field: field + ".ssid", Message: err.Error()}
		}
		if seenSSIDs[n.SSID] {
			return &ValidationError{Field: field + ".ssid", Message: "This network is already in the list."}
		}
		seenSSIDs[n.SSID] = true
		switch n.Security {
		case UplinkOpen:
		case UplinkPersonal:
			if err := ValidateWiFiPassword(credentials[n.ID].Password); err != nil {
				return &ValidationError{Field: field + ".password", Message: err.Error()}
			}
		case UplinkEnterprise:
			if err := n.validateEnterprise(credentials[n.ID]); err != nil {
				return &ValidationError{Field: field + "." + err.Field, Message: err.Message}
			}
		default:
			return &ValidationError{Field: field + ".security", Message: `Must be "open", "personal" or "enterprise".`}
		}
		if n.Security != UplinkEnterprise && n.Enterprise != nil {
			return &ValidationError{Field: field + ".enterprise", Message: "Only Enterprise networks have these settings."}
		}
	}
	return nil
}

// Secrets holds everything that must never be shown in the UI, logs or
// diagnostics exports.
type Secrets struct {
	// HotspotPassword is the WPA2/WPA3 password for the hotspot.
	HotspotPassword string `json:"hotspotPassword"`
	// AdminPasswordHash is the argon2id hash of the web UI password (see
	// internal/auth). The password itself is never stored.
	AdminPasswordHash string `json:"adminPasswordHash"`
	// UplinkPasswords holds the passwords of saved upstream Wi-Fi networks,
	// keyed by SavedNetwork.ID.
	UplinkPasswords map[string]string `json:"uplinkPasswords,omitempty"`
	// GuestPassword is the WPA2/WPA3 password for the guest network.
	GuestPassword string `json:"guestPassword,omitempty"`
	// UpdateNotifyURL is where a new version is announced: an ntfy topic
	// (https://ntfy.sh/your-topic) or another web address that takes a
	// message (a Discord or Home Assistant webhook). It can hold a token,
	// so it is kept with the secrets.
	UpdateNotifyURL string `json:"updateNotifyUrl,omitempty"`
	// WireGuardKeys holds each VPN profile's keys, keyed by profile name.
	WireGuardKeys map[string]WireGuardKeys `json:"wireguardKeys,omitempty"`
	// WireGuardPrivateKey and WireGuardPresharedKey are where version 2
	// kept the single VPN server's keys. MoveVPNIntoProfile moves them into
	// WireGuardKeys.
	WireGuardPrivateKey   string `json:"wireguardPrivateKey,omitempty"`
	WireGuardPresharedKey string `json:"wireguardPresharedKey,omitempty"`
	// Plugins holds plugins' password settings: plugin ID → key → value.
	Plugins map[string]map[string]string `json:"plugins,omitempty"`
}

// WireGuardKeys are one VPN profile's secrets (base64).
type WireGuardKeys struct {
	// PrivateKey is RasBeacon's own key for this profile.
	PrivateKey string `json:"privateKey"`
	// PresharedKey is an optional extra secret shared with the server.
	PresharedKey string `json:"presharedKey,omitempty"`
}

// Identity holds values generated once, on first boot.
type Identity struct {
	// DeviceID is a random identifier, shown in the UI and used to make the
	// default network names unique (for example "RasBeacon-4F2A").
	DeviceID string `json:"deviceId"`
	// ShortID is the last 4 characters of DeviceID, used in network names.
	ShortID string `json:"shortId"`
	// ULAPrefix is a random private IPv6 prefix (RFC 4193) for the hotspot,
	// for example "fd12:3456:789a::/48".
	ULAPrefix string `json:"ulaPrefix"`
	// SuggestedHotspotPassword is offered in the setup wizard so the owner
	// does not have to invent one. It is only a suggestion.
	SuggestedHotspotPassword string `json:"suggestedHotspotPassword"`
}

// DefaultConfig returns the settings used before the owner changes anything.
// The hotspot name includes the device's short ID so two RasBeacons in the
// same room have different names.
func DefaultConfig(identity Identity) Config {
	return Config{
		Version: CurrentVersion,
		Claimed: false,
		Hotspot: HotspotConfig{
			SSID:     "RasBeacon-" + identity.ShortID,
			Band:     Band2GHz,
			Security: SecurityWPA2WPA3,
			Channel:  0,
			Country:  "US",
		},
		Guest:    GuestConfig{SSID: GuestSSIDFor("RasBeacon-" + identity.ShortID)},
		SSH:      SSHConfig{Enabled: false},
		Ethernet: EthernetConfig{Role: EthernetUplink},
		VPN:      VPNConfig{KillSwitch: true},
	}
}

// GuestSSIDFor suggests a guest network name: the hotspot's name plus
// "-Guest", shortened if needed to stay within the 32-byte limit.
func GuestSSIDFor(hotspotSSID string) string {
	name := hotspotSSID
	for len(name)+len("-Guest") > 32 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name + "-Guest"
}

// ValidationError explains which setting is wrong and why, in words that can
// be shown directly in the web UI.
type ValidationError struct {
	Field   string // for example "hotspot.ssid"
	Message string // for example "must be 1 to 32 bytes long"
}

// Error is "field: message", for logs; the web UI shows the message under
// the field.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validate checks every setting in the hotspot section. It returns the first
// problem found, or nil if everything is valid.
func (h HotspotConfig) Validate() error {
	if err := ValidateSSID(h.SSID); err != nil {
		return &ValidationError{Field: "hotspot.ssid", Message: err.Error()}
	}
	if h.Band != Band2GHz && h.Band != Band5GHz {
		return &ValidationError{Field: "hotspot.band", Message: `Must be "2.4GHz" or "5GHz".`}
	}
	if h.Security != SecurityWPA2WPA3 && h.Security != SecurityWPA2 && h.Security != SecurityWPA3 {
		return &ValidationError{Field: "hotspot.security", Message: `Must be "wpa2-wpa3", "wpa2" or "wpa3".`}
	}
	if h.Channel != 0 && !IsAllowedChannel(h.Band, h.Channel) {
		return &ValidationError{Field: "hotspot.channel", Message: "This channel is not offered for this band."}
	}
	if !isCountryCode(h.Country) {
		return &ValidationError{Field: "hotspot.country", Message: countryMessage(h.Country)}
	}
	return nil
}

// ValidateSSID checks a Wi-Fi network name. The Wi-Fi standard allows 1 to
// 32 bytes. We also reject control characters (like newlines) because they
// break config files and confuse phones.
func ValidateSSID(ssid string) error {
	if len(ssid) < 1 || len(ssid) > 32 {
		return errors.New("Must be 1 to 32 bytes long (emoji and accented letters count as several bytes).")
	}
	if !utf8.ValidString(ssid) {
		return errors.New("Must be valid text.")
	}
	for _, r := range ssid {
		if unicode.IsControl(r) {
			return errors.New("Must not contain control characters.")
		}
	}
	return nil
}

// ValidateWiFiPassword checks a WPA2/WPA3 password. The standard requires 8
// to 63 printable ASCII characters (letters, digits, punctuation, spaces).
func ValidateWiFiPassword(password string) error {
	if len(password) < 8 || len(password) > 63 {
		return errors.New("Must be 8 to 63 characters long.")
	}
	for _, r := range password {
		if r < 0x20 || r > 0x7e {
			return errors.New("May only contain printable ASCII characters (no emoji or accented letters).")
		}
	}
	return nil
}

// allowedChannels lists the channels RasBeacon offers per band. For 5 GHz we
// only offer channels that need no radar detection ("non-DFS"), because USB
// Wi-Fi adapters are not verified to handle radar detection correctly.
var allowedChannels = map[Band][]int{
	Band2GHz: {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	Band5GHz: {36, 40, 44, 48, 149, 153, 157, 161, 165},
}

// OfferedChannels lists the channels RasBeacon offers for band, before a
// country's rules narrow them down.
func OfferedChannels(band Band) []int { return slices.Clone(allowedChannels[band]) }

// IsAllowedChannel reports whether channel is offered for band.
func IsAllowedChannel(band Band, channel int) bool {
	for _, allowed := range allowedChannels[band] {
		if channel == allowed {
			return true
		}
	}
	return false
}

// DefaultChannel is used when HotspotConfig.Channel is 0.
func DefaultChannel(band Band) int {
	if band == Band5GHz {
		return 36
	}
	return 6
}

// Clone returns a copy of c that can be changed without affecting c. A plain
// assignment (copy := c) would share the list of saved networks.
func (c Config) Clone() Config {
	c.Uplink.Networks = append([]SavedNetwork(nil), c.Uplink.Networks...)
	for i, n := range c.Uplink.Networks {
		if n.Enterprise != nil {
			copied := *n.Enterprise
			c.Uplink.Networks[i].Enterprise = &copied
		}
	}
	c.VPN.Profiles = slices.Clone(c.VPN.Profiles)
	for i, p := range c.VPN.Profiles {
		c.VPN.Profiles[i] = p.Clone()
	}
	c.Clients.Paused = maps.Clone(c.Clients.Paused)
	c.Clients.Names = maps.Clone(c.Clients.Names)
	c.Clients.FixedAddresses = maps.Clone(c.Clients.FixedAddresses)
	c.DNS.Servers = slices.Clone(c.DNS.Servers)
	c.DNS.DoH = slices.Clone(c.DNS.DoH)
	c.Plugins.Sources = slices.Clone(c.Plugins.Sources)
	if c.Plugins.Installed != nil {
		installed := make(map[string]InstalledPlugin, len(c.Plugins.Installed))
		for id, p := range c.Plugins.Installed {
			installed[id] = p.Clone()
		}
		c.Plugins.Installed = installed
	}
	if c.Swap.SizeGB != nil {
		size := *c.Swap.SizeGB
		c.Swap.SizeGB = &size
	}
	return c
}

// Clone returns a copy of s that can be changed without affecting s.
func (s Secrets) Clone() Secrets {
	passwords := make(map[string]string, len(s.UplinkPasswords))
	for id, password := range s.UplinkPasswords {
		passwords[id] = password
	}
	s.UplinkPasswords = passwords
	s.WireGuardKeys = maps.Clone(s.WireGuardKeys)
	if s.Plugins != nil {
		plugins := make(map[string]map[string]string, len(s.Plugins))
		for id, values := range s.Plugins {
			plugins[id] = maps.Clone(values)
		}
		s.Plugins = plugins
	}
	return s
}

// validateEnterprise checks an Enterprise network's settings. The returned
// error's Field is relative to the network, e.g. "enterprise.identity".
func (n SavedNetwork) validateEnterprise(c NetworkCredentials) *ValidationError {
	e := n.Enterprise
	if e == nil {
		return &ValidationError{Field: "enterprise", Message: "Enterprise networks need a username and login method."}
	}
	switch e.Method {
	case EAPTTLSPAP, EAPPEAP:
		if c.Password == "" {
			return &ValidationError{Field: "password", Message: "Enter your password for this network."}
		}
	case EAPTLS:
		if !c.HasClientCert || !c.HasClientKey {
			return &ValidationError{Field: "enterprise.clientCert", Message: "EAP-TLS needs your personal certificate and its key."}
		}
	default:
		return &ValidationError{Field: "enterprise.method", Message: `Must be "ttls-pap", "peap-mschapv2" or "tls".`}
	}
	if err := checkEAPText(e.Identity, true); err != nil {
		return &ValidationError{Field: "enterprise.identity", Message: err.Error()}
	}
	if err := checkEAPText(e.AnonymousIdentity, false); err != nil {
		return &ValidationError{Field: "enterprise.anonymousIdentity", Message: err.Error()}
	}
	if err := checkEAPText(c.Password, false); err != nil {
		return &ValidationError{Field: "password", Message: err.Error()}
	}

	// The login server's certificate is always checked unless the owner
	// explicitly turned that off: without the check, anyone nearby could
	// set up a fake network with the same name and collect the password.
	// With a CA certificate and/or a server name, the certificate must
	// match them; with neither, RasBeacon trusts the certificate it sees
	// at the first connection and only that one afterwards.
	if e.AllowNoCertificateCheck || e.ServerName == "" {
		return nil
	}
	if !isDomainName(e.ServerDomain()) {
		return &ValidationError{Field: "enterprise.serverName", Message: "Must be a server name like radius.example.com or *.example.com."}
	}
	return nil
}

// checkEAPText checks a username or password for an Enterprise network:
// at most 253 bytes of valid text with no control characters (such as
// line breaks).
func checkEAPText(text string, required bool) error {
	if required && text == "" {
		return errors.New("This is required.")
	}
	if len(text) > 253 {
		return errors.New("Must be at most 253 bytes long.")
	}
	if !utf8.ValidString(text) {
		return errors.New("Must be valid text.")
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return errors.New("Must not contain control characters.")
		}
	}
	return nil
}

// isDomainName reports whether name looks like a DNS name such as
// "radius.example.com": dot-separated labels of lowercase letters, digits and
// hyphens (callers lowercase it first).
func isDomainName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

// ParseDoH reads a DNS-over-HTTPS server as people write it and returns
// it in full: "https://dns.google/dns-query", or just "dns.google", which
// means the usual path, /dns-query (RFC 8484). Only https is accepted, with
// a host name or address and no user name or password. Spaces and commas
// commas, quotes and backslashes can't appear (the list is passed on
// space-separated, in a systemd environment file).
func ParseDoH(text string) (string, error) {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "://") {
		text = "https://" + text
	}
	u, err := url.Parse(text)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" ||
		len(text) > 200 || strings.ContainsAny(text, " \t\r\n,\"'\\") {
		return "", errors.New("not a DNS-over-HTTPS address")
	}
	host := u.Hostname()
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() {
			return "", errors.New("not a DNS server's address")
		}
	} else if name := strings.ToLower(strings.TrimSuffix(host, ".")); !strings.Contains(name, ".") || !isDomainName(name) {
		// A server's name has at least one dot (dns.google).
		return "", errors.New("not a host name")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", errors.New("not a port")
		}
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/dns-query"
	}
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

// DefaultHostname is RasBeacon's name until the owner changes it.
const DefaultHostname = "rasbeacon"

// Name returns RasBeacon's host name.
func (c *Config) Name() string {
	if c.Hostname == "" {
		return DefaultHostname
	}
	return c.Hostname
}

// ValidateHostname checks a host name for RasBeacon: 1 to 63 lowercase
// letters, digits and hyphens, not starting or ending with a hyphen.
func ValidateHostname(name string) error {
	if name == "" || len(name) > 63 {
		return &ValidationError{Field: "hostname", Message: "The name must be 1 to 63 characters long."}
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return &ValidationError{Field: "hostname", Message: "Use only lowercase letters, digits and hyphens."}
		}
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return &ValidationError{Field: "hostname", Message: "The name can't start or end with a hyphen."}
	}
	return nil
}
