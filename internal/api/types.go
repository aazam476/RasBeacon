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
