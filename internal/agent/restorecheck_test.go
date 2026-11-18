package agent

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/config"
)

// The checks of a backup's sections, one by one: each refuses what its
// settings page would, and takes what it would.
func TestRestoreCheck_Sections(t *testing.T) {
	const mac = "aa:bb:cc:dd:ee:01"
	good := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: %v", what, err)
		}
	}
	bad := func(err error, want, what string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error naming %q", what, err, want)
		}
	}

	// Devices.
	good(checkRestoredClients(config.ClientsConfig{
		Paused: map[string]time.Time{mac: time.Now()}, Names: map[string]string{mac: "Laptop"},
		FixedAddresses: map[string]string{mac: "10.87.0.5"},
	}), "sound devices")
	bad(checkRestoredClients(config.ClientsConfig{Names: map[string]string{"x": "Laptop"}}), "named device", "a name for no MAC")
	bad(checkRestoredClients(config.ClientsConfig{FixedAddresses: map[string]string{"x": "10.87.0.5"}}), "fixed address", "a fixed address for no MAC")
	bad(checkRestoredClients(config.ClientsConfig{FixedAddresses: map[string]string{mac: "010.087.000.005"}}), "fixed address", "an address written oddly")
	bad(checkRestoredClients(config.ClientsConfig{UsageLimitMB: -1}), "data limit", "a data limit below 0")
	bad(checkDeviceName(strings.Repeat("x", 41)), "device name", "a name too long")

	// Priority devices and speeds.
	good(checkRestoredQoS(config.QoSConfig{Priority: []string{mac}, UploadKbps: 5_000}), "a priority device")
	bad(checkRestoredQoS(config.QoSConfig{Priority: []string{"x"}}), "priority device", "a priority device that isn't a MAC")
	many := make([]string, config.MaxPriorityDevices+1)
	for i := range many {
		many[i] = mac
	}
	bad(checkRestoredQoS(config.QoSConfig{Priority: many}), "priority", "too many priority devices")
	if err := checkRestoredQoS(config.QoSConfig{DownloadKbps: 1}); err == nil {
		t.Error("a speed below the least must be refused")
	}

	// Radios.
	good(checkRestoredRadios(config.RadiosConfig{Roles: map[string]string{mac: "uplink"}, Names: map[string]string{mac: "Blue stick"}}), "a radio's job and name")
	bad(checkRestoredRadios(config.RadiosConfig{Roles: map[string]string{"x": "uplink"}}), "radio's address", "a job for no MAC")
	bad(checkRestoredRadios(config.RadiosConfig{Names: map[string]string{"x": "Blue"}}), "radio's address", "a name for no MAC")
	bad(checkRestoredRadios(config.RadiosConfig{Names: map[string]string{mac: "a\nb"}}), "device name", "a radio name with a line break")

	// VPN profiles.
	profile := config.VPNProfile{Name: "home.conf", Addresses: []netip.Prefix{netip.MustParsePrefix("10.8.0.2/32")}}
	good(checkRestoredVPN(config.VPNConfig{Profiles: []config.VPNProfile{profile}, Active: "home.conf"}, nil), "a profile still being filled in")
	bad(checkRestoredVPN(config.VPNConfig{Profiles: []config.VPNProfile{profile, profile}}, nil), "two VPN profiles", "two profiles of one name")
	bad(checkRestoredVPN(config.VPNConfig{Profiles: []config.VPNProfile{{Name: ""}}}, nil), "profile's name", "a profile without a name")
	broken := profile
	broken.RoutedIPv6Prefix = netip.MustParsePrefix("fd00::/48")
	bad(checkRestoredVPN(config.VPNConfig{Profiles: []config.VPNProfile{broken}}, nil), "home.conf", "a routed prefix that isn't a /64")

	// The address networks see.
	bad(checkRestoredMAC(config.MACChoice{Mode: config.MACCloned, Address: "not a MAC"}), "address shown to networks", "a cloned address that isn't one")
}

// Enterprise networks' certificates are looked for in the backup, where
// they are under the data folder.
func TestRestoreCheck_CredentialsComeFromTheBackup(t *testing.T) {
	e := newClaimedEnv(t, false)
	staging := t.TempDir()
	rel, err := filepath.Rel(e.paths.DataDir(), e.paths.CACertFile("n1"))
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(staging, rel), "-----BEGIN CERTIFICATE-----")
	// A link in its place is no certificate.
	relKey, _ := filepath.Rel(e.paths.DataDir(), e.paths.ClientKeyFile("n1"))
	if err := os.Symlink("/etc/passwd", filepath.Join(staging, relKey)); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Uplink: config.UplinkConfig{Networks: []config.SavedNetwork{{ID: "n1", SSID: "eduroam"}}}}
	got := restoredCredentials(e.agent, cfg, config.Secrets{UplinkPasswords: map[string]string{"n1": "pw"}}, staging)["n1"]
	if got.Password != "pw" || !got.HasCACert || got.HasClientCert || got.HasClientKey {
		t.Errorf("credentials = %+v, want the password and the CA certificate only", got)
	}
}
