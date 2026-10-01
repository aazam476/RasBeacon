// Package api defines the JSON messages exchanged between the web UI, rbweb
// and rbd.
//
// These Go types are the single source of truth for the API. The web UI has
// matching TypeScript types in web/src/api/types.ts, and docs/api.md
// describes every endpoint. When you change a type here, update both.
//
// Field names use camelCase in JSON (the `json:"..."` tags) because that is
// the convention in JavaScript/TypeScript.
package api

import (
	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/stations"
	"rasbeacon/internal/uplink"
)

// Mode is what the device is doing right now.
type Mode string

const (
	// ModeSetup: first boot, the setup network is up and waiting for the
	// owner to choose an admin password.
	ModeSetup Mode = "setup"
	// ModeSetupClosed: nobody finished setup within 30 minutes, so the setup
	// network was switched off. Power-cycle the device to reopen it.
	ModeSetupClosed Mode = "setup-closed"
	// ModeSetupError: setup.txt exists but can't be used. The setup network
	// stays off and the LED blinks an error code.
	ModeSetupError Mode = "setup-error"
	// ModeNormal: setup is done and the configured hotspot is running.
	ModeNormal Mode = "normal"
	// ModeNoRadio: no Wi-Fi radio that can run a hotspot was found.
	ModeNoRadio Mode = "no-radio"
)

// ErrorResponse is returned with any 4xx/5xx status. Field names the setting
// that was rejected (for example "hotspot.ssid"), when there is one, so the
// UI can show the message next to the right input.
type ErrorResponse struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

// SetupState is public (no login needed). The UI uses it to decide whether
// to show the setup wizard or the login page.
type SetupState struct {
	Mode    Mode `json:"mode"`
	Claimed bool `json:"claimed"`
	// SetupNetworkSecured is true when setup.txt protected the setup network.
	SetupNetworkSecured bool   `json:"setupNetworkSecured"`
	DeviceShortID       string `json:"deviceShortId"`
	// LANAddress is RasBeacon's address on the hotspot, e.g. 10.87.0.1.
	LANAddress string `json:"lanAddress"`
	// Suggestions pre-fill the setup wizard. Only sent while unclaimed.
	SuggestedHotspotSSID     string `json:"suggestedHotspotSsid,omitempty"`
	SuggestedHotspotPassword string `json:"suggestedHotspotPassword,omitempty"`
	// SetupClosesInSeconds counts down the 30-minute setup window.
	SetupClosesInSeconds int `json:"setupClosesInSeconds,omitempty"`
	// PortalNotice is true while devices are shown the "this network needs
	// a login" notice (see PortalStatus). rbweb then sends phones'
	// connectivity checks to the notice page.
	PortalNotice bool `json:"portalNotice,omitempty"`
}

// ClaimRequest finishes first-time setup.
type ClaimRequest struct {
	AdminPassword   string      `json:"adminPassword"`
	HotspotSSID     string      `json:"hotspotSsid"`
	HotspotPassword string      `json:"hotspotPassword"`
	HotspotBand     config.Band `json:"hotspotBand"`
}

// ClaimResponse tells the UI what to show before the setup network
// disappears: the phone must join the new hotspot to continue.
type ClaimResponse struct {
	HotspotSSID string `json:"hotspotSsid"`
	// SwitchInSeconds is how long until the setup network is replaced.
	SwitchInSeconds int `json:"switchInSeconds"`
	// AdminURL is where to log in after joining the new hotspot.
	AdminURL string `json:"adminUrl"`
}

// PasswordRequest carries the admin password, for login and for actions
// that ask for it again (reveal hotspot password, factory reset).
type PasswordRequest struct {
	Password string `json:"password"`
}

// VerifyPasswordResponse is rbd's answer to a password check.
type VerifyPasswordResponse struct {
	OK bool `json:"ok"`
}

// SessionInfo is returned after login and by GET /api/v1/session.
type SessionInfo struct {
	LoggedIn bool `json:"loggedIn"`
	// CSRFToken must be sent back in the X-CSRF-Token header on every
	// request that changes something (POST, PUT, DELETE).
	CSRFToken string `json:"csrfToken,omitempty"`
}

// Status is the overview shown on the status page.
type Status struct {
	Version       string             `json:"version"`
	BoardModel    string             `json:"boardModel"`
	DeviceID      string             `json:"deviceId"`
	UptimeSeconds int                `json:"uptimeSeconds"`
	Mode          Mode               `json:"mode"`
	Hotspot       HotspotStatus      `json:"hotspot"`
	Uplink        UplinkStatus       `json:"uplink"`
	VPN           VPNStatus          `json:"vpn"`
	Portal        PortalStatus       `json:"portal"`
	Radios        []radio.Radio      `json:"radios"`
	Clients       []stations.Station `json:"clients"`
	// Warnings are problems the owner should know about, in plain words.
	Warnings []string `json:"warnings"`
}

// HotspotStatus summarises the running hotspot.
type HotspotStatus struct {
	Running   bool            `json:"running"`
	SSID      string          `json:"ssid"`
	Band      config.Band     `json:"band"`
	Channel   int             `json:"channel"`
	Security  config.Security `json:"security"`
	Interface string          `json:"interface"`
	// LANAddress is RasBeacon's address on the hotspot, e.g. 10.87.0.1.
	LANAddress string `json:"lanAddress"`
}

// UplinkStatus describes RasBeacon's connection to the internet.
type UplinkStatus struct {
	// Connected is true when RasBeacon has a route to the internet.
	Connected bool `json:"connected"`
	// Kind is "ethernet" or "wifi" for the uplink in use, or "" if none.
	Kind      string `json:"kind"`
	Interface string `json:"interface"`
	// Address is RasBeacon's IPv4 address on the upstream network.
	Address string `json:"address"`
	Gateway string `json:"gateway"`
	// IPv6 is true when devices get IPv6 internet access.
	IPv6     bool                 `json:"ipv6"`
	WiFi     WiFiUplinkStatus     `json:"wifi"`
	Ethernet EthernetUplinkStatus `json:"ethernet"`
}

// WiFiUplinkStatus describes the Wi-Fi uplink radio.
type WiFiUplinkStatus struct {
	// Available is false when there is no second radio for joining Wi-Fi
	// (for example when the built-in radio has to run the hotspot).
	Available bool   `json:"available"`
	Interface string `json:"interface"`
	// State is "connected", "connecting", "searching", "login-failed"
	// (an Enterprise network rejected the login), "not-configured" (no
	// saved networks) or "off".
	State     string `json:"state"`
	SSID      string `json:"ssid"`
	Band      string `json:"band"`
	SignalDBM int    `json:"signalDbm"`
}

// EthernetUplinkStatus describes the Ethernet port.
type EthernetUplinkStatus struct {
	Present bool                `json:"present"`
	Role    config.EthernetRole `json:"role"`
	// CablePlugged is true when the port has a link.
	CablePlugged bool `json:"cablePlugged"`
	// ForeignDHCPServer is set in client-port mode when a DHCP server
	// answered on the port: it is probably plugged into a network rather
	// than a laptop, so it was not connected to the hotspot network.
	ForeignDHCPServer string `json:"foreignDhcpServer,omitempty"`
}

// PortalStatus describes captive portals (login pages of hotel networks).
// It is public: the notice page shows it before anyone logs in.
type PortalStatus struct {
	// Detected is true when the uplink shows a login page.
	Detected bool `json:"detected"`
	// LoginURL is where the network sends visitors, when known.
	LoginURL string `json:"loginUrl,omitempty"`
	// CanStart is true when login mode can be started (a login page is in
	// the way and the VPN's kill switch keeps devices offline).
	CanStart bool `json:"canStart"`
	// LoginMode is true while devices may reach the login page directly.
	LoginMode   bool `json:"loginMode"`
	SecondsLeft int  `json:"secondsLeft"`
}

// VPNStatus describes the WireGuard tunnel.
type VPNStatus struct {
	Enabled    bool `json:"enabled"`
	KillSwitch bool `json:"killSwitch"`
	// State is "off", "connecting", "up", "down" or "direct" (see
	// internal/tunnel). In "connecting" and "down" with the kill switch on,
	// devices have no internet; in "direct" they use the uplink directly.
	State string `json:"state"`
	// HandshakeAgeSeconds is the time since the last handshake with the
	// server, or -1 if there has been none.
	HandshakeAgeSeconds int    `json:"handshakeAgeSeconds"`
	Endpoint            string `json:"endpoint"`
	ReceivedBytes       int64  `json:"receivedBytes"`
	SentBytes           int64  `json:"sentBytes"`
}

// VPNSettings is read and written by the VPN page. Addresses are text
// (e.g. "10.66.0.2/32") so mistakes can be reported per field.
type VPNSettings struct {
	Enabled    bool `json:"enabled"`
	KillSwitch bool `json:"killSwitch"`
	// Configured is true once a server has been set up (read-only).
	Configured bool     `json:"configured"`
	Addresses  []string `json:"addresses"`
	DNS        []string `json:"dns"`
	MTU        int      `json:"mtu"`
	// Server (peer) settings.
	PeerPublicKey       string   `json:"peerPublicKey"`
	Endpoint            string   `json:"endpoint"`
	AllowedIPs          []string `json:"allowedIps"`
	PersistentKeepalive int      `json:"persistentKeepalive"`
	// PublicKey is RasBeacon's own public key, to give to the server
	// (read-only; empty until a private key exists).
	PublicKey string `json:"publicKey"`
	// Write-only secrets: empty keeps the saved value.
	PrivateKey      string `json:"privateKey,omitempty"`
	PresharedKey    string `json:"presharedKey,omitempty"`
	HasPresharedKey bool   `json:"hasPresharedKey"`
	// RemovePresharedKey deletes the saved preshared key.
	RemovePresharedKey bool `json:"removePresharedKey,omitempty"`
}

// VPNImportRequest carries a WireGuard .conf file's text.
type VPNImportRequest struct {
	Conf string `json:"conf"`
}

// UplinkSettings is read and written by the Internet (uplink) settings page.
type UplinkSettings struct {
	EthernetRole config.EthernetRole `json:"ethernetRole"`
	// Networks are upstream Wi-Fi networks, most preferred first.
	Networks []SavedNetwork `json:"networks"`
	// Read-only hints for the UI.
	EthernetPresent     bool `json:"ethernetPresent"`
	WiFiUplinkAvailable bool `json:"wifiUplinkAvailable"`
}

// SavedNetwork is one upstream Wi-Fi network in UplinkSettings.
type SavedNetwork struct {
	// ID is empty for a network being added; rbd assigns one.
	ID       string                `json:"id"`
	SSID     string                `json:"ssid"`
	Security config.UplinkSecurity `json:"security"`
	Hidden   bool                  `json:"hidden"`
	// HasPassword is true when a password is saved (it is never returned).
	HasPassword bool `json:"hasPassword"`
	// Password is write-only: set it to add or change the password, leave it
	// empty to keep the saved one. For Enterprise networks it is the user's
	// password (TTLS, PEAP) or the client key's passphrase (TLS).
	Password string `json:"password,omitempty"`
	// Enterprise is set for Enterprise networks only.
	Enterprise *EnterpriseSettings `json:"enterprise,omitempty"`
}

// EnterpriseSettings are the login settings of a WPA-Enterprise network.
// Certificates are uploaded as PEM text or base64-encoded DER; they are
// write-only (an empty value keeps the saved file) and described by the
// read-only summaries.
type EnterpriseSettings struct {
	Method                  config.EAPMethod `json:"method"`
	Identity                string           `json:"identity"`
	AnonymousIdentity       string           `json:"anonymousIdentity"`
	ServerName              string           `json:"serverName"`
	AllowNoCertificateCheck bool             `json:"allowNoCertificateCheck"`

	CACert            string `json:"caCert,omitempty"`
	CACertSummary     string `json:"caCertSummary"`
	ClientCert        string `json:"clientCert,omitempty"`
	ClientCertSummary string `json:"clientCertSummary"`
	ClientKey         string `json:"clientKey,omitempty"`
	HasClientKey      bool   `json:"hasClientKey"`
}

// ScanResponse lists the Wi-Fi networks RasBeacon can see.
type ScanResponse struct {
	Networks []uplink.NearbyNetwork `json:"networks"`
}

// HotspotSettings is read and written by the hotspot settings page. The
// password is never returned; Password in a PUT request is optional and an
// empty value means "keep the current password".
type HotspotSettings struct {
	SSID     string          `json:"ssid"`
	Band     config.Band     `json:"band"`
	Channel  int             `json:"channel"`
	Security config.Security `json:"security"`
	Country  string          `json:"country"`
	Password string          `json:"password,omitempty"`
}

// RevealPasswordResponse returns the hotspot password after the admin
// password was entered again.
type RevealPasswordResponse struct {
	HotspotPassword string `json:"hotspotPassword"`
}

// SSHSettings is read and written by the SSH settings page.
type SSHSettings struct {
	Enabled bool `json:"enabled"`
	// AuthorizedKeys holds OpenSSH public keys, one per line.
	AuthorizedKeys string `json:"authorizedKeys"`
}

// CaptivePortalInfo is the RFC 8908 captive portal API response. Phones
// that support RFC 8910 fetch it to learn whether they must visit a page
// before the network works (true during first-boot setup).
type CaptivePortalInfo struct {
	Captive       bool   `json:"captive"`
	UserPortalURL string `json:"user-portal-url,omitempty"`
}
