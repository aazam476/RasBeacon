// Package api defines the JSON messages exchanged between the web UI, rbweb
// and rbd.
//
// These Go types are the single source of truth for the API. The web UI has
// matching TypeScript types in web/src/data/types.ts, and docs/api.md
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
	// Hostname is RasBeacon's name on the hotspot, e.g. rasbeacon.lan.
	Hostname string `json:"hostname,omitempty"`
	// Suggestions pre-fill the setup wizard. Only sent while unclaimed.
	SuggestedHotspotSSID     string `json:"suggestedHotspotSsid,omitempty"`
	SuggestedHotspotPassword string `json:"suggestedHotspotPassword,omitempty"`
	// SetupClosesInSeconds counts down the 30-minute setup window.
	SetupClosesInSeconds int `json:"setupClosesInSeconds,omitempty"`
	// PortalNotice is true while devices are shown the "this network needs
	// a login" notice (see PortalStatus). rbweb then sends phones'
	// connectivity checks to the notice page.
	PortalNotice bool `json:"portalNotice,omitempty"`
	// No5GHz is true when the hotspot's radio can't use 5 GHz, so setup
	// offers only 2.4 GHz.
	No5GHz bool `json:"no5GHz,omitempty"`
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
// that ask for it again (factory reset).
type PasswordRequest struct {
	Password string `json:"password"`
}

// AdminPasswordRequest changes the admin password (POST
// /system/admin-password): the current one, then the new one.
type AdminPasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
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
	Version       string `json:"version"`
	BoardModel    string `json:"boardModel"`
	DeviceID      string `json:"deviceId"`
	UptimeSeconds int    `json:"uptimeSeconds"`
	Mode          Mode   `json:"mode"`
	// Plugins are the installed plugins and their state.
	Plugins []PluginSummary    `json:"plugins"`
	Hotspot HotspotStatus      `json:"hotspot"`
	Uplink  UplinkStatus       `json:"uplink"`
	VPN     VPNStatus          `json:"vpn"`
	Portal  PortalStatus       `json:"portal"`
	Update  UpdateStatus       `json:"update"`
	Radios  []radio.Radio      `json:"radios"`
	Clients []stations.Station `json:"clients"`
	Guest   GuestStatus        `json:"guest"`
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
	// Running is true while a test runs, whoever started it, so every
	// page that shows the button says so until it finishes.
	Running bool `json:"running"`
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
	Running bool        `json:"running"`
	SSID    string      `json:"ssid"`
	Band    config.Band `json:"band"`
	// Channel is the saved channel (or the band's default when "auto").
	Channel int `json:"channel"`
	// ActualChannel is the one the radio uses right now, read from the
	// driver; 0 when it isn't known (the hotspot is off). With 80 MHz,
	// hostapd may move the hotspot within the same group of channels
	// (for example from 36 to 40) when a nearby network uses one of them.
	ActualChannel int             `json:"actualChannel,omitempty"`
	Security      config.Security `json:"security"`
	Interface     string          `json:"interface"`
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
	// Board is the image's board: "rpi64" (the Pi 4 and Pi 5 image; older
	// images said "pi4" or "pi5") or "rpi3" (the Pi Zero 2 W, 3B and 3B+
	// image). Update files must be for it.
	Board string `json:"board"`
	// CanInstall is false when the image has no update signing key, or
	// when SingleSystem is true.
	CanInstall bool `json:"canInstall"`
	// SingleSystem is true for the Pi Zero 2 W/3B/3B+ image: it has one copy of
	// the system (no A/B), so new versions are flashed, not installed.
	SingleSystem bool `json:"singleSystem,omitempty"`
	// Installing is true while an update is being written.
	Installing bool `json:"installing"`
	// Message describes the last update, e.g. "Updated to v1.2.0." or why
	// it failed.
	Message string `json:"message,omitempty"`
	// The optional online check (see UpdateSettings). CheckHours is how
	// often it runs (0: only with "Check now"); LastChecked is when it last
	// asked (RFC 3339, "" if never), and CheckError why that failed.
	CanCheckOnline bool   `json:"canCheckOnline"`
	CheckHours     int    `json:"checkHours"`
	LastChecked    string `json:"lastChecked,omitempty"`
	CheckError     string `json:"checkError,omitempty"`
	// LatestVersion is the newest release this RasBeacon can install, and
	// LatestURL its release page. NewerVersion is set when an even newer
	// release exists that can only be installed over LatestVersion (or a
	// later one): it is offered once LatestVersion runs.
	LatestVersion string `json:"latestVersion,omitempty"`
	LatestURL     string `json:"latestUrl,omitempty"`
	NewerVersion  string `json:"newerVersion,omitempty"`
	// Reflash is set when a newer version can't be installed over this one
	// (the online check found one, or its update file was refused): the
	// owner backs up the settings, flashes the new image and restores the
	// backup.
	Reflash *UpdateReflash `json:"reflash,omitempty"`
	// RestartPending is true when an update is installed but RasBeacon
	// hasn't restarted into it yet (the restart failed, or the power went
	// off first). POST /system/update/restart finishes it; Message says
	// why it waits.
	RestartPending bool `json:"restartPending,omitempty"`
}

// UpdateReflash describes a new version that needs a fresh install.
type UpdateReflash struct {
	Version string `json:"version"`
	// MinFromVersion is the oldest version that can install its update file.
	MinFromVersion string `json:"minFromVersion"`
	// URL is its release page, when known.
	URL string `json:"url,omitempty"`
}

// UpdateSettings is read and written by the System page
// (GET and PUT /settings/updates).
type UpdateSettings struct {
	// CheckHours is how often to ask GitHub for a new version: 0 (only
	// with "Check now"), 1, 6, 24 or 168.
	CheckHours int `json:"checkHours"`
	// NotifyURL is where to announce a new version: an ntfy topic or a
	// webhook ("" for none).
	NotifyURL string `json:"notifyUrl"`
}

// NotifyTestRequest sends a test message (POST /settings/updates/test) to
// URL, or to the saved address when URL is empty.
type NotifyTestRequest struct {
	URL string `json:"url"`
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
	// internal/tunnel), or "blocked". In "connecting" and "down" with the
	// kill switch on, devices have no internet; in "direct" they use the
	// uplink directly; "blocked" means the VPN is off but the kill switch is
	// on, so devices have no internet.
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

// VPNBlocked is VPNStatus.State while the kill switch blocks devices
// because the VPN is off.
const VPNBlocked = "blocked"

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

// HostnameRequest renames RasBeacon (POST /system/hostname). RasBeacon
// restarts afterwards.
type HostnameRequest struct {
	Hostname string `json:"hostname"`
}

// VPNRenameRequest renames a profile (POST /settings/vpn/profile/rename).
type VPNRenameRequest struct {
	Name    string `json:"name"`
	NewName string `json:"newName"`
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
	// RandomMAC joins with a random MAC address (new at each restart).
	RandomMAC bool `json:"randomMac"`
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
	// ServerCertHash is the SHA-256 of the login server's certificate,
	// trusted at the first connection (read only; empty until then, and
	// when a CA certificate or server name is given).
	ServerCertHash string `json:"serverCertHash,omitempty"`

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
	// No5GHz is true when the hotspot's radio can't use 5 GHz, so the page
	// offers only 2.4 GHz. Read-only: ignored when saving.
	No5GHz bool `json:"no5GHz,omitempty"`
	// NoWPA3 is true when the hotspot's radio can't run a WPA3-only
	// hotspot, so the page doesn't offer "WPA3 only".
	NoWPA3 bool `json:"noWpa3,omitempty"`
}

// HotspotChannelsRequest asks which channels a country allows (POST
// /settings/hotspot/channels).
type HotspotChannelsRequest struct {
	Country string `json:"country"`
}

// HotspotChannels lists, by band, the channels the Hotspot page may offer
// for a country: those RasBeacon offers that the country allows a hotspot
// on. A band the radio or the country doesn't allow has none.
type HotspotChannels struct {
	Channels map[config.Band][]int `json:"channels"`
}

// AccessSettings says which and how many devices may join the hotspot over
// Wi-Fi (GET and PUT /settings/access). Devices on Ethernet always can.
type AccessSettings struct {
	// AllowListOn lets only the devices in Allowed join your network. The
	// guest network isn't affected.
	AllowListOn bool `json:"allowListOn"`
	// Allowed are MAC addresses such as "3a:1f:22:aa:bb:cc".
	Allowed []string `json:"allowed"`
	// MaxDevices is the most devices on each network at once; 0 is no
	// limit (up to 231, the addresses DHCP hands out).
	MaxDevices int `json:"maxDevices"`
}

// RevealPasswordResponse returns the hotspot password. Being logged in is
// enough to ask for it.
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

// ClientNameRequest names a device (POST /clients/name); an empty name
// removes the owner's name.
type ClientNameRequest struct {
	MAC  string `json:"mac"`
	Name string `json:"name"`
}

// ClientFixedAddressRequest makes a device always get Address on the
// hotspot (POST /clients/fixed-address); an empty Address removes it.
type ClientFixedAddressRequest struct {
	MAC     string `json:"mac"`
	Address string `json:"address"`
}

// WiFiQRRequest asks for a QR code to join a network (POST /wifi-qr):
// "hotspot" or "guest". Being logged in is enough.
type WiFiQRRequest struct {
	Network string `json:"network"`
}

// WiFiQR is a QR code phones scan to join a network: an SVG image.
type WiFiQR struct {
	SSID string `json:"ssid"`
	SVG  string `json:"svg"`
}

// UsageSettings is the data limit (GET/PUT /settings/usage).
type UsageSettings struct {
	// LimitMB pauses a device after it used this much (down and up) since
	// Since, metered connection or not. 0: no limit.
	LimitMB int `json:"limitMb"`
	// Since is when counting started (RFC 3339); read-only.
	Since string `json:"since,omitempty"`
	// DevicesPaused is how many devices the limit pauses now; read-only.
	DevicesPaused int `json:"devicesPaused"`
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
	// WithVPN uses the servers while the VPN is on too.
	WithVPN bool `json:"withVpn"`
}

// SwapSettings is the System page's swap file (GET/PUT /settings/swap).
type SwapSettings struct {
	// SizeGB is the size in whole gigabytes; 0 is off.
	SizeGB int `json:"sizeGb"`
	// MaxGB is the largest size that fits: the free space on the SD card,
	// counting the swap file itself, minus 1 GB kept free (read-only).
	MaxGB int `json:"maxGb"`
}

// --- Plugins (see internal/plugin and docs/plugins.md) ---

// PluginPermission is a permission in plain words.
type PluginPermission struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// PluginPort is one of a plugin's ports: Key ("tcp/3000") names it in
// requests, Host is the port on RasBeacon.
type PluginPort struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Container int    `json:"container"`
	Host      int    `json:"host"`
	// URL is the address to open it at, for ports marked "open".
	URL string `json:"url,omitempty"`
}

// PluginOption is a choice of a select setting.
type PluginOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// PluginSetting is a field of a plugin's settings form, with its value.
// Passwords are never sent back: HasValue says one is saved.
type PluginSetting struct {
	Key      string         `json:"key"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	Help     string         `json:"help,omitempty"`
	Unit     string         `json:"unit,omitempty"`
	Required bool           `json:"required,omitempty"`
	Options  []PluginOption `json:"options,omitempty"`
	Min      *float64       `json:"min,omitempty"`
	Max      *float64       `json:"max,omitempty"`
	Value    string         `json:"value"`
	HasValue bool           `json:"hasValue,omitempty"`
}

// PluginMetric is a number on a plugin's page; Value is "" when it can't
// be read now.
type PluginMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Plugin states.
const (
	PluginInstalling = "installing" // getting its image, setting it up
	PluginWaiting    = "waiting"    // waiting for the VPN before downloading
	PluginStarting   = "starting"
	PluginRunning    = "running"
	PluginStopped    = "stopped"
	PluginCrashed    = "crashed" // stopped after crashing too often
	PluginFailed     = "failed"  // couldn't install or start; see Message
)

// Plugin is an installed plugin (GET /plugins).
type Plugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	// Icon is a data: URL.
	Icon   string `json:"icon"`
	Readme string `json:"readme,omitempty"`
	// Source is "file" or the catalog's "owner/repo".
	Source string `json:"source"`
	Signed bool   `json:"signed"`
	KeyID  string `json:"keyId,omitempty"`
	State  string `json:"state"`
	// Message explains the state (progress, or what went wrong).
	Message string `json:"message,omitempty"`
	// Healthy is the health check's last answer (nil: no check, or not yet).
	Healthy     *bool              `json:"healthy,omitempty"`
	Address     string             `json:"address"`
	Permissions []PluginPermission `json:"permissions"`
	Ports       []PluginPort       `json:"ports"`
	Settings    []PluginSetting    `json:"settings"`
	// UsesDrive: it can use a USB drive; Drive is the chosen one's UUID.
	UsesDrive     bool           `json:"usesDrive"`
	DriveOptional bool           `json:"driveOptional,omitempty"`
	Drive         string         `json:"drive,omitempty"`
	AfterInstall  string         `json:"afterInstall,omitempty"`
	Metrics       []PluginMetric `json:"metrics"`
	MemoryMB      int            `json:"memoryMb"`
	CPUs          float64        `json:"cpus"`
	StorageMB     int            `json:"storageMb"`
	// UpdateVersion is a newer version in the catalog ("" = none known).
	UpdateVersion string `json:"updateVersion,omitempty"`
	// CanUpdate: the update can be installed from the catalog (signed
	// plugins only; unsigned ones are updated from a file).
	CanUpdate bool `json:"canUpdate"`
	// Broken: its files (its manifest) can't be read, so all that is known
	// is its ID. Only removing it, or installing it again, helps.
	Broken bool `json:"broken,omitempty"`
}

// PluginSource is a catalog source.
type PluginSource struct {
	Source  string `json:"source"`
	BuiltIn bool   `json:"builtIn"`
}

// PluginsState is GET /plugins.
type PluginsState struct {
	Plugins []Plugin       `json:"plugins"`
	Sources []PluginSource `json:"sources"`
	// Board is this Pi (pi5, pi4, pi3, zero2w, or "").
	Board    string `json:"board"`
	MemoryMB int    `json:"memoryMb"`
	SwapMB   int    `json:"swapMb"`
	Metered  bool   `json:"metered"`
	// CheckedAt is when the catalog was last fetched ("" = never).
	CheckedAt  string `json:"checkedAt,omitempty"`
	CheckError string `json:"checkError,omitempty"`
}

// PluginSummary is a plugin on the status page.
type PluginSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	Icon  string `json:"icon"`
}

// PluginPreview is what the install sheet shows before installing (from
// POST /plugins/upload or /plugins/catalog/download).
type PluginPreview struct {
	StagingID   string `json:"stagingId"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Icon        string `json:"icon"`
	Source      string `json:"source"`
	Signed      bool   `json:"signed"`
	KeyID       string `json:"keyId,omitempty"`
	// Update is true when it replaces an installed version (FromVersion).
	Update      bool   `json:"update"`
	FromVersion string `json:"fromVersion,omitempty"`
	// Permissions it asks for; NewPermissions are those an update adds.
	Permissions    []PluginPermission `json:"permissions"`
	NewPermissions []string           `json:"newPermissions,omitempty"`
	Ports          []PluginPort       `json:"ports"`
	Settings       []PluginSetting    `json:"settings"`
	UsesDrive      bool               `json:"usesDrive"`
	DriveOptional  bool               `json:"driveOptional,omitempty"`
	Drive          string             `json:"drive,omitempty"`
	// DownloadSize is what installing still downloads (the image from its
	// registry; -1: unknown); Metered warns it counts against a limit.
	DownloadSize int64   `json:"downloadSize"`
	Metered      bool    `json:"metered"`
	MemoryMB     int     `json:"memoryMb"`
	CPUs         float64 `json:"cpus"`
	StorageMB    int     `json:"storageMb"`
	// Warnings are shown above the Install button (e.g. little memory).
	Warnings []string `json:"warnings,omitempty"`
	// KeptData is set when data kept from an earlier install with this ID
	// waits, signed by another key or not signed: the sheet asks whether
	// to use it (PluginInstallRequest.UseKeptData).
	KeptData *PluginKeptData `json:"keptData,omitempty"`
}

// PluginInstallRequest installs a previewed plugin (POST /plugins/install):
// ports by key ("tcp/3000"), settings by key (an empty password keeps the
// saved one on an update), and the USB drive's UUID.
type PluginInstallRequest struct {
	StagingID string            `json:"stagingId"`
	Ports     map[string]int    `json:"ports"`
	Settings  map[string]string `json:"settings"`
	Drive     string            `json:"drive"`
	// UseKeptData gives the new install the data kept from an earlier one
	// of the same ID that wasn't signed with the same key (the preview's
	// KeptData). Data of the same author comes back without it.
	UseKeptData bool `json:"useKeptData,omitempty"`
}

// PluginKeptData describes data kept from an earlier install with the same
// ID that doesn't come back by itself: the install sheet offers to use it.
type PluginKeptData struct {
	// Name is what the earlier plugin was called ("" if not known).
	Name string `json:"name,omitempty"`
	// KeyID is the earlier plugin's signing key ("" if it wasn't signed).
	KeyID string `json:"keyId,omitempty"`
}

// PluginSettingsRequest changes an installed plugin's settings (PUT
// /plugins/settings); it restarts.
type PluginSettingsRequest struct {
	ID       string            `json:"id"`
	Ports    map[string]int    `json:"ports"`
	Settings map[string]string `json:"settings"`
	Drive    string            `json:"drive"`
}

// PluginIDRequest names a plugin.
type PluginIDRequest struct {
	ID string `json:"id"`
}

// PluginRemoveRequest removes a plugin; KeepData keeps its data for a
// later install of the same plugin.
type PluginRemoveRequest struct {
	ID       string `json:"id"`
	KeepData bool   `json:"keepData"`
}

// PluginCatalogRequest asks for the catalog; Refresh asks GitHub again
// ("Check now") instead of using the saved copy.
type PluginCatalogRequest struct {
	Refresh bool `json:"refresh"`
}

// PluginCatalogEntry is a plugin a source offers.
type PluginCatalogEntry struct {
	Source       string   `json:"source"`
	File         string   `json:"file"`
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	Version      string   `json:"version,omitempty"`
	Description  string   `json:"description,omitempty"`
	Permissions  []string `json:"permissions,omitempty"`
	Size         int64    `json:"size"`
	DownloadSize int64    `json:"downloadSize,omitempty"`
	Icon         string   `json:"icon,omitempty"`
	// InstalledVersion is set when it is installed, and UpdateAvailable
	// when this version is newer than that one. RunsHere is false for
	// plugins made for other boards.
	InstalledVersion string `json:"installedVersion,omitempty"`
	UpdateAvailable  bool   `json:"updateAvailable,omitempty"`
	RunsHere         bool   `json:"runsHere"`
	// NeedsRasBeacon is set when this RasBeacon is too old for it: the
	// version it needs ("0.2.0").
	NeedsRasBeacon string `json:"needsRasBeacon,omitempty"`
}

// PluginCatalog is POST /plugins/catalog's answer.
type PluginCatalog struct {
	Entries   []PluginCatalogEntry `json:"entries"`
	CheckedAt string               `json:"checkedAt,omitempty"`
	// Errors by source (e.g. "no releases", GitHub asking to wait).
	Errors map[string]string `json:"errors,omitempty"`
}

// PluginDownloadRequest downloads a catalog plugin for the install sheet.
type PluginDownloadRequest struct {
	Source string `json:"source"`
	File   string `json:"file"`
}

// PluginLogs are a plugin's last lines of output.
type PluginLogs struct {
	Lines []PluginLogLine `json:"lines"`
}

// PluginLogLine is one line a plugin wrote: Time is when (RFC 3339, in UTC;
// the page shows it in the viewer's own time), left out for a line whose
// time isn't known.
type PluginLogLine struct {
	Time string `json:"time,omitempty"`
	Text string `json:"text"`
}

// PluginSourcesRequest sets the extra catalog sources.
type PluginSourcesRequest struct {
	Sources []string `json:"sources"`
}

// PluginDrive is a USB drive's partition; UsedBy names the plugin using it.
type PluginDrive struct {
	UUID   string `json:"uuid"`
	Label  string `json:"label"`
	FSType string `json:"fsType"`
	Size   int64  `json:"size"`
	UsedBy string `json:"usedBy,omitempty"`
}

// PluginDrives is GET /plugins/drives.
type PluginDrives struct {
	Drives []PluginDrive `json:"drives"`
}

// PluginPowerRequest is what a plugin with the power-button permission
// sends on its socket: shutdown or reboot.
type PluginPowerRequest struct {
	Action string `json:"action"`
}

// BackupRequest asks for a backup (POST /system/backup): the password that
// encrypts the file (being logged in is enough to ask), and how much of the
// plugins is in it.
type BackupRequest struct {
	// Password encrypts the backup; a restore needs it again.
	Password string `json:"password"`
	// Plugins is how much of the plugins goes in: "settings" (the
	// default), "data" (also their data) or "everything" (also their
	// images, so a restore works offline). Plugins' images left out are
	// downloaded again on restore.
	Plugins string `json:"plugins"`
}

// BackupTicket is the answer to a BackupRequest: the browser downloads the
// backup from GET /system/backup/{token} (once, within two minutes), as a
// file it saves while it arrives.
type BackupTicket struct {
	Token    string `json:"token"`
	FileName string `json:"fileName"`
}

// RestoreRequest restores the uploaded backup (POST /system/restore).
type RestoreRequest struct {
	Password string `json:"password"`
}

// RestoreResult describes the restored backup; RasBeacon restarts.
type RestoreResult struct {
	Version string `json:"version"`
	Created string `json:"created"`
	// Plugins is true when the backup's plugins replaced the installed
	// ones (always, except for backups made before RasBeacon 0.2 without
	// plugins).
	Plugins bool `json:"plugins"`
}
