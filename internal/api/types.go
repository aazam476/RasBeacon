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
	"time"
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
	Update        UpdateStatus       `json:"update"`
	Radios        []radio.Radio      `json:"radios"`
	Clients       []stations.Station `json:"clients"`
	Guest         GuestStatus        `json:"guest"`
	// Warnings are problems the owner should know about, in plain words.
	Warnings []string `json:"warnings"`

	Throughput Throughput  `json:"throughput"`
	System     SystemStats `json:"system"`
	Device     DeviceInfo  `json:"device"`
	Storage    StorageInfo `json:"storage"`
	LAN        LANInfo     `json:"lan"`
	Speedtest  Speedtest   `json:"speedtest"`
	// EthernetDiagnostics describes the Ethernet port while it carries the
	// internet (null otherwise).
	EthernetDiagnostics *EthernetDiagnostics `json:"ethernetDiagnostics"`
}

// Throughput is the traffic between RasBeacon and the devices on its
// networks (the hotspot and the guest network), in megabits per second.
// Download is towards the devices.
type Throughput struct {
	DownMbps     float64 `json:"downMbps"`
	DownPeakMbps float64 `json:"downPeakMbps"`
	UpMbps       float64 `json:"upMbps"`
	UpPeakMbps   float64 `json:"upPeakMbps"`
	// PeakMbps is the highest download plus upload rate since rbd started.
	PeakMbps float64 `json:"peakMbps"`
	// SessionBytes is everything the devices sent and received since the
	// router started.
	SessionBytes uint64 `json:"sessionBytes"`
}

// SystemStats are the hardware readings. Text fields are short phrases
// for the UI.
type SystemStats struct {
	CPUPercent   float64 `json:"cpuPercent"`
	CPUModel     string  `json:"cpuModel"`
	RAMUsedMB    float64 `json:"ramUsedMb"`
	RAMTotalMB   float64 `json:"ramTotalMb"`
	SwapUsedMB   float64 `json:"swapUsedMb"`
	SwapTotalMB  float64 `json:"swapTotalMb"`
	TemperatureC float64 `json:"temperatureC"`
	// ThermalState is "Normal", "Warm" or "Hot: slowing down".
	ThermalState string `json:"thermalState"`
	// Cooling is "Fan" or "No fan found".
	Cooling string `json:"cooling"`
	// SensorStatus is "OK" or "No sensor found".
	SensorStatus string `json:"sensorStatus"`
	// Throttling says whether the firmware slowed the processor down:
	// "No", "Yes, now", "Earlier, not now", possibly with "(power too weak)".
	Throttling string `json:"throttling"`
}

// DeviceInfo identifies the router on the System page.
type DeviceInfo struct {
	Hostname      string `json:"hostname"`
	ModelSpec     string `json:"modelSpec"`
	Kernel        string `json:"kernel"`
	KernelRelease string `json:"kernelRelease"`
	Appliance     string `json:"appliance"`
	BootDevice    string `json:"bootDevice"`
}

// StorageInfo describes the space RasBeacon uses on its card or drive, in
// gigabytes (10^9 bytes).
type StorageInfo struct {
	// TotalGB is RasBeacon's partitions together.
	TotalGB float64 `json:"totalGb"`
	// FirmwareGB is the boot and system partitions (both A/B copies).
	FirmwareGB float64 `json:"firmwareGb"`
	// UserGB is what settings and logs use on the data partition.
	UserGB     float64 `json:"userGb"`
	Medium     string  `json:"medium"`
	Filesystem string  `json:"filesystem"`
	// Health is "unknown": SD cards don't report it.
	Health string `json:"health"`
}

// LANInfo describes the hotspot's own network.
type LANInfo struct {
	// MaxClients is how many addresses DHCP can hand out on the hotspot.
	MaxClients int    `json:"maxClients"`
	Subnet     string `json:"subnet"`
	DHCPRange  string `json:"dhcpRange"`
}

// Speedtest is the last speed test (Cloudflare's speed test, run only when
// the owner asks; see internal/speedtest).
type Speedtest struct {
	Provider string `json:"provider"`
	// Last is null until a test has run since rbd started.
	Last *SpeedtestResult `json:"last"`
}

// SpeedtestResult is one finished speed test.
type SpeedtestResult struct {
	// Server is the Cloudflare location tested against, e.g.
	// "San Jose (SJC)", or "" when unknown.
	Server       string  `json:"server"`
	AgeSeconds   int     `json:"ageSeconds"`
	DownloadMbps float64 `json:"downloadMbps"`
	UploadMbps   float64 `json:"uploadMbps"`
	PingMs       float64 `json:"pingMs"`
}

// EthernetDiagnostics are details of the Ethernet port while it carries
// the internet, as short phrases.
type EthernetDiagnostics struct {
	CarrierState     string `json:"carrierState"`
	Controller       string `json:"controller"`
	MTU              string `json:"mtu"`
	PacketStatistics string `json:"packetStatistics"`
	DNSServers       string `json:"dnsServers"`
	DHCPLease        string `json:"dhcpLease"`
	PortDescription  string `json:"portDescription"`
	PollingInterval  string `json:"pollingInterval"`
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
	// Metered is true when devices are told the connection is limited.
	Metered bool `json:"metered"`
}

// GuestStatus describes the guest network on the status page.
type GuestStatus struct {
	Enabled bool `json:"enabled"`
	// Running is true when the guest network is being broadcast.
	Running bool   `json:"running"`
	SSID    string `json:"ssid"`
	// Clients are the guests connected right now.
	Clients []stations.Station `json:"clients"`
}

// GuestSettings is read and written by the guest network page. The
// password is never returned; Password in a PUT request is optional and an
// empty value keeps the saved one.
type GuestSettings struct {
	Enabled     bool   `json:"enabled"`
	SSID        string `json:"ssid"`
	Password    string `json:"password,omitempty"`
	HasPassword bool   `json:"hasPassword"`
	// Supported is false when the hotspot's Wi-Fi adapter can't broadcast
	// two networks at once (read-only).
	Supported bool `json:"supported"`
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
	// SubnetMask and InterfaceMAC describe RasBeacon's address on the
	// uplink in use ("" when not connected).
	SubnetMask   string `json:"subnetMask,omitempty"`
	InterfaceMAC string `json:"interfaceMac,omitempty"`
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
	// The access point while connected.
	BSSID        string `json:"bssid,omitempty"`
	Channel      int    `json:"channel,omitempty"`
	FrequencyMHz int    `json:"frequencyMhz,omitempty"`
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
	// LinkSpeed ("1 Gbps") and Duplex ("Full duplex") while the cable has
	// a link.
	LinkSpeed string `json:"linkSpeed,omitempty"`
	Duplex    string `json:"duplex,omitempty"`
}

// UpdateStatus describes software updates.
type UpdateStatus struct {
	// Board is the image's board, "rpi64" (the Pi 4 and Pi 5 image; older
	// images said "pi4" or "pi5"): update files must be for this board.
	Board string `json:"board"`
	// CanInstall is false when the image has no update signing key.
	CanInstall bool `json:"canInstall"`
	// Installing is true while an update is being written.
	Installing bool `json:"installing"`
	// Message describes the last update, e.g. "Updated to v1.2.0." or why
	// it failed.
	Message string `json:"message,omitempty"`
	// The optional online check (see UpdateSettings).
	CanCheckOnline bool   `json:"canCheckOnline"`
	CheckOnline    bool   `json:"checkOnline"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	LatestURL      string `json:"latestUrl,omitempty"`
}

// UpdateSettings is written by the System page.
type UpdateSettings struct {
	CheckOnline bool `json:"checkOnline"`
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
	// CanBrowse is true when the login page can be opened in RasBeacon's
	// own browser (zero-leak login, see PortalBrowserFrame): the browser is
	// installed and a login page is (or may be) in the way.
	CanBrowse bool `json:"canBrowse"`
	// Browsing is true while that browser is open.
	Browsing bool `json:"browsing"`
}

// PortalBrowserStart opens RasBeacon's own browser on the network's login
// page. Width and Height are the phone screen's size in CSS pixels; the
// page is laid out for that size.
type PortalBrowserStart struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// PortalBrowserFrame is a picture of the login page in RasBeacon's own
// browser.
type PortalBrowserFrame struct {
	// Running is false when the browser is closed (for example after the
	// login worked); the other fields are then empty.
	Running bool `json:"running"`
	// Seq goes up with every new picture: ask for the next one with
	// GET .../frame?after=<seq>.
	Seq int64 `json:"seq"`
	// Image is the picture, a base64-encoded JPEG, Width x Height.
	Image  string `json:"image"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

// PortalBrowserInput is something the owner did on the picture.
type PortalBrowserInput struct {
	// Action is "click" (X, Y), "scroll" (X, Y, DeltaY), "type" (Text),
	// "key" (Key: "Enter", "Backspace", "Tab" or "Escape"), "back",
	// "reload" or "navigate" (URL, http or https only).
	Action string  `json:"action"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	DeltaY float64 `json:"deltaY,omitempty"`
	Text   string  `json:"text,omitempty"`
	Key    string  `json:"key,omitempty"`
	URL    string  `json:"url,omitempty"`
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
	// RoutedIPv6Prefix is the public IPv6 network devices get addresses
	// from, while it is in use (see VPNSettings).
	RoutedIPv6Prefix string `json:"routedIpv6Prefix,omitempty"`
}

// VPNSettings is read by the VPN page: the switches and the WireGuard
// profiles (GET /settings/vpn). Each change has its own route, and each
// answers with the new VPNSettings.
type VPNSettings struct {
	Enabled    bool `json:"enabled"`
	KillSwitch bool `json:"killSwitch"`
	// ActiveProfile is the name of the profile in use; "" when there is
	// none.
	ActiveProfile string       `json:"activeProfile"`
	Profiles      []VPNProfile `json:"profiles"`
}

// VPNSwitches turns the VPN and its kill switch on or off
// (PUT /settings/vpn).
type VPNSwitches struct {
	Enabled    bool `json:"enabled"`
	KillSwitch bool `json:"killSwitch"`
}

// VPNProfile is one WireGuard configuration. Addresses are text (e.g.
// "10.66.0.2/32") so mistakes can be reported per field.
type VPNProfile struct {
	// Name is unique, usually the imported file's name.
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	DNS       []string `json:"dns"`
	// MTU 0 means the default (1380).
	MTU int `json:"mtu"`
	// Server (peer) settings.
	PeerPublicKey string   `json:"peerPublicKey"`
	Endpoint      string   `json:"endpoint"`
	AllowedIPs    []string `json:"allowedIps"`
	// PersistentKeepalive 0 means the default (25 seconds).
	PersistentKeepalive int `json:"persistentKeepalive"`
	// PublicKey is RasBeacon's own public key for this profile, to give to
	// the server (empty until a private key exists).
	PublicKey       string `json:"publicKey"`
	HasPresharedKey bool   `json:"hasPresharedKey"`
	// RoutedIPv6Prefix is optional: a public IPv6 /64 the WireGuard server
	// routes to RasBeacon (e.g. "2001:db8:1234:5678::/64"). Devices then
	// get public IPv6 addresses through the VPN, without NAT. Empty: off.
	RoutedIPv6Prefix string `json:"routedIpv6Prefix"`
}

// VPNProfileDetails saves the details typed into one profile's form
// (POST /settings/vpn/profile/save). Name picks the profile; it can't be
// changed. PublicKey and HasPresharedKey are ignored.
type VPNProfileDetails struct {
	VPNProfile
	// Write-only secrets: empty keeps the saved value.
	PrivateKey   string `json:"privateKey,omitempty"`
	PresharedKey string `json:"presharedKey,omitempty"`
	// RemovePresharedKey deletes the saved preshared key.
	RemovePresharedKey bool `json:"removePresharedKey,omitempty"`
}

// VPNProfileRequest names a profile (POST /settings/vpn/profile/activate,
// /remove and /generate-key).
type VPNProfileRequest struct {
	Name string `json:"name"`
}

// HTTPSSettings is read and written by the HTTPS page.
type HTTPSSettings struct {
	Enabled bool `json:"enabled"`
	// Read-only description of the certificate in use (empty if none).
	Uploaded    bool   `json:"uploaded"`
	Subject     string `json:"subject"`
	Fingerprint string `json:"fingerprint"`
	ValidUntil  string `json:"validUntil"`
	// Write-only actions: make a new certificate, or upload one (PEM).
	GenerateNew bool   `json:"generateNew,omitempty"`
	Certificate string `json:"certificate,omitempty"`
	PrivateKey  string `json:"privateKey,omitempty"`
}

// VPNImportRequest carries a WireGuard .conf file's text, which becomes a
// new profile (POST /settings/vpn/import). Name is the file's name, if it
// came from a file; RasBeacon makes it unique.
type VPNImportRequest struct {
	Conf string `json:"conf"`
	Name string `json:"name,omitempty"`
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
	// Metered tells devices the internet connection is limited (Android
	// reads it automatically; see docs/api.md).
	Metered  bool   `json:"metered"`
	Password string `json:"password,omitempty"`
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

// ClientRequest names a device on the hotspot by its MAC address
// (POST /clients/disconnect and /clients/flush-lease).
type ClientRequest struct {
	MAC string `json:"mac"`
}

// ClientInternetAccessRequest pauses (allowed false) or allows a device's
// internet access (POST /clients/internet-access).
type ClientInternetAccessRequest struct {
	MAC     string `json:"mac"`
	Allowed bool   `json:"allowed"`
}

// RememberedLogin is a "Stay logged in" token between rbweb and rbd. Expires
// is only set in rbd's answer when a token is made.
type RememberedLogin struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expires,omitzero"`
}

// LoginRequest is the login form (POST /api/v1/session).
type LoginRequest struct {
	Password     string `json:"password"`
	StayLoggedIn bool   `json:"stayLoggedIn"`
}

// DNSSettings is the Internet page's DNS servers (GET/PUT /settings/dns):
// used while devices' traffic isn't going through the VPN. Empty uses the
// current network's.
type DNSSettings struct {
	Servers []string `json:"servers"`
}

// SwapSettings is the System page's swap file (GET/PUT /settings/swap).
type SwapSettings struct {
	// SizeGB is the size in whole gigabytes; 0 is off.
	SizeGB int `json:"sizeGb"`
	// MaxGB is the largest size that fits: the free space on the SD card,
	// counting the swap file itself, minus 1 GB kept free (read-only).
	MaxGB int `json:"maxGb"`
}
