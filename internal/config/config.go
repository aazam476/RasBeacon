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
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CurrentVersion is the format version of config.json. It goes up when a
// release changes the file layout, so newer code can upgrade older files.
const CurrentVersion = 1

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
)

// Config holds all non-secret settings.
type Config struct {
	// Version is the file format version (see CurrentVersion).
	Version int `json:"version"`

	// Claimed becomes true once the owner has finished first-time setup by
	// choosing an admin password. Before that, the device runs the open (or
	// setup.txt-protected) setup network.
	Claimed bool `json:"claimed"`

	Hotspot  HotspotConfig  `json:"hotspot"`
	SSH      SSHConfig      `json:"ssh"`
	Ethernet EthernetConfig `json:"ethernet"`
	Uplink   UplinkConfig   `json:"uplink"`
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
)

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
}

// MaxSavedNetworks keeps the list (and wpa_supplicant's scanning) small.
const MaxSavedNetworks = 20

// FillDefaults sets values for settings that were added in a later release
// and are therefore missing from an older config.json.
func (c *Config) FillDefaults() {
	if c.Ethernet.Role == "" {
		c.Ethernet.Role = EthernetUplink
	}
}

// Validate checks the Ethernet settings.
func (e EthernetConfig) Validate() error {
	if e.Role != EthernetUplink && e.Role != EthernetClientPort {
		return &ValidationError{Field: "ethernetRole", Message: `Must be "uplink" or "client-port".`}
	}
	return nil
}

// Validate checks the saved networks. passwords maps network IDs to their
// passwords; every network that needs a password must have a valid one.
func (u UplinkConfig) Validate(passwords map[string]string) error {
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
			if err := ValidateWiFiPassword(passwords[n.ID]); err != nil {
				return &ValidationError{Field: field + ".password", Message: err.Error()}
			}
		default:
			return &ValidationError{Field: field + ".security", Message: `Must be "open" or "personal".`}
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
		SSH:      SSHConfig{Enabled: false},
		Ethernet: EthernetConfig{Role: EthernetUplink},
	}
}

// ValidationError explains which setting is wrong and why, in words that can
// be shown directly in the web UI.
type ValidationError struct {
	Field   string // for example "hotspot.ssid"
	Message string // for example "must be 1 to 32 bytes long"
}

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
	if h.Security != SecurityWPA2WPA3 && h.Security != SecurityWPA2 {
		return &ValidationError{Field: "hotspot.security", Message: `Must be "wpa2-wpa3" or "wpa2".`}
	}
	if h.Channel != 0 && !IsAllowedChannel(h.Band, h.Channel) {
		return &ValidationError{Field: "hotspot.channel", Message: "This channel is not offered for this band."}
	}
	if !isCountryCode(h.Country) {
		return &ValidationError{Field: "hotspot.country", Message: "Must be a two-letter country code such as US."}
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

// isCountryCode reports whether code looks like an ISO 3166 two-letter code.
func isCountryCode(code string) bool {
	if len(code) != 2 {
		return false
	}
	return strings.ToUpper(code) == code && unicode.IsLetter(rune(code[0])) && unicode.IsLetter(rune(code[1]))
}

// Clone returns a copy of c that can be changed without affecting c. A plain
// assignment (copy := c) would share the list of saved networks.
func (c Config) Clone() Config {
	c.Uplink.Networks = append([]SavedNetwork(nil), c.Uplink.Networks...)
	return c
}

// Clone returns a copy of s that can be changed without affecting s.
func (s Secrets) Clone() Secrets {
	passwords := make(map[string]string, len(s.UplinkPasswords))
	for id, password := range s.UplinkPasswords {
		passwords[id] = password
	}
	s.UplinkPasswords = passwords
	return s
}
