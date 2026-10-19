package config

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultConfig_UsesShortIDInHotspotName(t *testing.T) {
	cfg := DefaultConfig(Identity{ShortID: "4F2A"})

	if cfg.Hotspot.SSID != "RasBeacon-4F2A" {
		t.Errorf("SSID = %q, want RasBeacon-4F2A", cfg.Hotspot.SSID)
	}
	if cfg.Hotspot.Band != Band2GHz {
		t.Errorf("default band = %q, want 2.4GHz", cfg.Hotspot.Band)
	}
	if cfg.SSH.Enabled {
		t.Error("SSH must be off by default")
	}
	if err := cfg.Hotspot.Validate(); err != nil {
		t.Errorf("default hotspot settings should be valid, got: %v", err)
	}
}

func TestValidateSSID(t *testing.T) {
	tests := []struct {
		name    string
		ssid    string
		wantErr bool
	}{
		{"normal name", "My Hotspot", false},
		{"exactly 32 bytes", strings.Repeat("a", 32), false},
		{"empty", "", true},
		{"33 bytes is too long", strings.Repeat("a", 33), true},
		{"newline would break config files", "evil\nname", true},
		{"emoji is allowed", "Café ☕", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSSID(tc.ssid)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateSSID(%q) error = %v, wantErr %v", tc.ssid, err, tc.wantErr)
			}
		})
	}
}

func TestValidateWiFiPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"8 characters is the minimum", "12345678", false},
		{"7 characters is too short", "1234567", true},
		{"63 characters is the maximum", strings.Repeat("x", 63), false},
		{"64 characters is too long", strings.Repeat("x", 64), true},
		{"spaces and punctuation are fine", "correct horse, battery!", false},
		{"accented letters are not allowed by WPA", "pässwörd123", true},
		{"newline is not allowed", "password\n123", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWiFiPassword(tc.password)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateWiFiPassword(%q) error = %v, wantErr %v", tc.password, err, tc.wantErr)
			}
		})
	}
}

func TestHotspotValidate_RejectsDFSChannelOn5GHz(t *testing.T) {
	h := DefaultConfig(Identity{ShortID: "AAAA"}).Hotspot
	h.Band = Band5GHz
	h.Channel = 52 // a radar-detection (DFS) channel we don't offer

	err := h.Validate()
	if err == nil {
		t.Fatal("expected an error for channel 52 on 5 GHz")
	}
	if !strings.Contains(err.Error(), "hotspot.channel") {
		t.Errorf("error should name the field, got: %v", err)
	}
}

func TestHotspotValidate_RejectsLowercaseCountry(t *testing.T) {
	h := DefaultConfig(Identity{ShortID: "AAAA"}).Hotspot
	h.Country = "us"
	if h.Validate() == nil {
		t.Fatal("expected an error for lowercase country code")
	}
}

func TestWriteJSON_ThenReadJSON_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	want := DefaultConfig(Identity{ShortID: "BEEF"})

	if err := WriteJSON(path, want, 0o644); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var got Config
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}

	// No temporary file should be left behind.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file still exists after write")
	}
}

func TestWriteJSON_SecretsFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := WriteJSON(path, Secrets{HotspotPassword: "hunter2hunter2"}, 0o600); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("secrets file permissions = %o, want 600", perm)
	}
}

func TestReadJSON_MissingFileReturnsErrNotFound(t *testing.T) {
	var cfg Config
	err := ReadJSON(filepath.Join(t.TempDir(), "nope.json"), &cfg)
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestFillDefaults_OldConfigGetsEthernetUplink(t *testing.T) {
	// A config.json written by milestone 1 has no "ethernet" section.
	var cfg Config
	cfg.FillDefaults()
	if cfg.Ethernet.Role != EthernetUplink {
		t.Errorf("Ethernet role = %q, want %q", cfg.Ethernet.Role, EthernetUplink)
	}
}

func TestEthernetConfig_Validate(t *testing.T) {
	for _, role := range []EthernetRole{EthernetUplink, EthernetClientPort} {
		if err := (EthernetConfig{Role: role}).Validate(); err != nil {
			t.Errorf("role %q rejected: %v", role, err)
		}
	}
	if err := (EthernetConfig{Role: "bridge"}).Validate(); err == nil {
		t.Error("unknown role accepted")
	}
}

func TestUplinkConfig_Validate(t *testing.T) {
	open := SavedNetwork{ID: "a", SSID: "Hotel Guest", Security: UplinkOpen}
	home := SavedNetwork{ID: "b", SSID: "Home", Security: UplinkPersonal}
	goodPasswords := map[string]NetworkCredentials{"b": {Password: "correct-horse"}}

	tests := []struct {
		name      string
		networks  []SavedNetwork
		passwords map[string]NetworkCredentials
		wantField string // "" means valid
	}{
		{"empty list", nil, nil, ""},
		{"open and personal", []SavedNetwork{open, home}, goodPasswords, ""},
		{"personal without password", []SavedNetwork{home}, nil, "networks.0.password"},
		{"short password", []SavedNetwork{home}, map[string]NetworkCredentials{"b": {Password: "short"}}, "networks.0.password"},
		{"same ID twice", []SavedNetwork{open, {ID: "a", SSID: "Other", Security: UplinkOpen}}, nil, "networks.1.id"},
		{"missing ID", []SavedNetwork{{SSID: "x", Security: UplinkOpen}}, nil, "networks.0.id"},
		{"same name twice", []SavedNetwork{open, {ID: "c", SSID: "Hotel Guest", Security: UplinkOpen}}, nil, "networks.1.ssid"},
		{"empty name", []SavedNetwork{{ID: "a", Security: UplinkOpen}}, nil, "networks.0.ssid"},
		{"unknown security", []SavedNetwork{{ID: "a", SSID: "x", Security: "wep"}}, nil, "networks.0.security"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UplinkConfig{Networks: tt.networks}.Validate(tt.passwords)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Field != tt.wantField {
				t.Fatalf("got %v, want an error for field %s", err, tt.wantField)
			}
		})
	}
}

func TestUplinkConfig_ValidateEnterprise(t *testing.T) {
	eduroam := func(change func(*EnterpriseConfig)) SavedNetwork {
		e := &EnterpriseConfig{
			Method: EAPTTLSPAP, Identity: "NetID@rutgers.edu", AnonymousIdentity: "anonymous@rutgers.edu",
			ServerName: "*.ruw.rutgers.edu",
		}
		if change != nil {
			change(e)
		}
		return SavedNetwork{ID: "e", SSID: "eduroam", Security: UplinkEnterprise, Enterprise: e}
	}
	withCA := NetworkCredentials{Password: "pw", HasCACert: true}

	tests := []struct {
		name      string
		network   SavedNetwork
		creds     NetworkCredentials
		wantField string
	}{
		{"complete TTLS", eduroam(nil), withCA, ""},
		// Without a CA certificate, the server name is checked against the
		// system's CAs; with neither, the first connection's certificate is
		// trusted from then on.
		{"no CA certificate", eduroam(nil), NetworkCredentials{Password: "pw"}, ""},
		{"no server name", eduroam(func(e *EnterpriseConfig) { e.ServerName = "" }), withCA, ""},
		{"neither", eduroam(func(e *EnterpriseConfig) { e.ServerName = "" }), NetworkCredentials{Password: "pw"}, ""},
		{"bad server name", eduroam(func(e *EnterpriseConfig) { e.ServerName = "ruw rutgers" }), withCA, "networks.0.enterprise.serverName"},
		{"check turned off on purpose", eduroam(func(e *EnterpriseConfig) { e.AllowNoCertificateCheck = true; e.ServerName = "" }),
			NetworkCredentials{Password: "pw"}, ""},
		{"no password", eduroam(nil), NetworkCredentials{HasCACert: true}, "networks.0.password"},
		{"no username", eduroam(func(e *EnterpriseConfig) { e.Identity = "" }), withCA, "networks.0.enterprise.identity"},
		{"newline in username", eduroam(func(e *EnterpriseConfig) { e.Identity = "a\nb" }), withCA, "networks.0.enterprise.identity"},
		{"TLS without key", eduroam(func(e *EnterpriseConfig) { e.Method = EAPTLS }),
			NetworkCredentials{HasCACert: true, HasClientCert: true}, "networks.0.enterprise.clientCert"},
		{"TLS complete", eduroam(func(e *EnterpriseConfig) { e.Method = EAPTLS }),
			NetworkCredentials{HasCACert: true, HasClientCert: true, HasClientKey: true}, ""},
		{"unknown method", eduroam(func(e *EnterpriseConfig) { e.Method = "leap" }), withCA, "networks.0.enterprise.method"},
		{"missing settings", SavedNetwork{ID: "e", SSID: "x", Security: UplinkEnterprise}, withCA, "networks.0.enterprise"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UplinkConfig{Networks: []SavedNetwork{tt.network}}.Validate(map[string]NetworkCredentials{"e": tt.creds})
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Field != tt.wantField {
				t.Fatalf("got %v, want an error for %s", err, tt.wantField)
			}
		})
	}
}

func TestServerDomain(t *testing.T) {
	for in, want := range map[string]string{
		"*.ruw.rutgers.edu":    "ruw.rutgers.edu",
		"radius.example.edu":   "radius.example.edu",
		" RADIUS.Example.EDU ": "radius.example.edu",
	} {
		if got := (EnterpriseConfig{ServerName: in}).ServerDomain(); got != want {
			t.Errorf("ServerDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClone_CopiesEnterpriseSettings(t *testing.T) {
	c := Config{Uplink: UplinkConfig{Networks: []SavedNetwork{{Enterprise: &EnterpriseConfig{Identity: "a"}}}}}
	copied := c.Clone()
	copied.Uplink.Networks[0].Enterprise.Identity = "b"
	if c.Uplink.Networks[0].Enterprise.Identity != "a" {
		t.Error("changing the copy changed the original")
	}
}

func TestFillDefaults_KillSwitchOffWhereTheVPNWasOff(t *testing.T) {
	// Before v0.2.0 the kill switch did nothing with the VPN off; now it
	// would block devices, so it is turned off there.
	cfg := Config{Version: 3, VPN: VPNConfig{KillSwitch: true}}
	cfg.FillDefaults()
	if cfg.VPN.KillSwitch {
		t.Error("devices with the VPN off must keep their internet after the upgrade")
	}
	on := Config{Version: 3, VPN: VPNConfig{Enabled: true, KillSwitch: true}}
	on.FillDefaults()
	if !on.VPN.KillSwitch {
		t.Error("with the VPN on, the kill switch stays on")
	}
}

func TestFillDefaults_OldConfigGetsKillSwitchOn(t *testing.T) {
	// A version 1 config with the VPN on gets the kill switch on.
	cfg := Config{Version: 1, VPN: VPNConfig{Enabled: true}}
	cfg.FillDefaults()
	if !cfg.VPN.KillSwitch || cfg.Version != CurrentVersion {
		t.Errorf("got %+v", cfg)
	}
	// A current config keeps the owner's choice.
	cfg.VPN.KillSwitch = false
	cfg.FillDefaults()
	if cfg.VPN.KillSwitch {
		t.Error("the owner's kill switch choice was overwritten")
	}
}

func TestValidateRoutedIPv6Prefix(t *testing.T) {
	good := []string{"2001:db8:1234:5678::/64", "2a01:4f8:1:2::/64"}
	bad := []string{"10.0.0.0/24", "2001:db8::/48", "2001:db8:1234:5678::1/64", "fd12:3456:789a:1::/64", "fe80::/64", "::ffff:10.0.0.0/120"}
	for _, text := range good {
		if err := ValidateRoutedIPv6Prefix(netip.MustParsePrefix(text)); err != nil {
			t.Errorf("%s: %v", text, err)
		}
	}
	for _, text := range bad {
		if err := ValidateRoutedIPv6Prefix(netip.MustParsePrefix(text)); err == nil {
			t.Errorf("%s should be refused", text)
		}
	}
	if err := ValidateRoutedIPv6Prefix(netip.Prefix{}); err != nil {
		t.Errorf("no prefix is fine: %v", err)
	}
}

func TestParseDoH(t *testing.T) {
	for in, want := range map[string]string{
		"dns.google":                           "https://dns.google/dns-query",
		" https://Cloudflare-DNS.com/ ":        "https://cloudflare-dns.com/dns-query",
		"https://dns.quad9.net:5053/dns-query": "https://dns.quad9.net:5053/dns-query",
		"https://1.1.1.1/dns-query":            "https://1.1.1.1/dns-query",
		"https://[2606:4700::1111]/dns-query":  "https://[2606:4700::1111]/dns-query",
		"https://doh.example/q?ct=dns":         "https://doh.example/q?ct=dns",
	} {
		if got, err := ParseDoH(in); err != nil || got != want {
			t.Errorf("ParseDoH(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", "http://dns.google/dns-query", "tls://dns.google", "https://", "localhost", "https://127.0.0.1/dns-query",
		"https://u:p@dns.google/", "https://dns.google/a b", "https://dns.google/a,b", "https://dns.google/q?x='y'",
		"https://-bad-.example/", "https://dns.google:99999/", "https://dns.google/#frag",
	} {
		if got, err := ParseDoH(bad); err == nil {
			t.Errorf("ParseDoH(%q) = %q, want an error", bad, got)
		}
	}
}

func TestDNSConfig_CountsDoHServersTowardTheLimit(t *testing.T) {
	d := DNSConfig{
		Servers: []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("9.9.9.9")},
		DoH:     []string{"https://dns.google/dns-query", "https://cloudflare-dns.com/dns-query"},
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	d.DoH = append(d.DoH, "https://dns.quad9.net/dns-query")
	if err := d.Validate(); err == nil {
		t.Error("five servers should be refused")
	}
	if err := (DNSConfig{DoH: []string{"dns.google"}}).Validate(); err == nil {
		t.Error("DoH servers are saved in full")
	}
}

func TestFillDefaults_CheckOnlineBecomesASchedule(t *testing.T) {
	cfg := Config{Version: 3, Updates: UpdatesConfig{CheckOnline: true}}
	cfg.FillDefaults()
	if cfg.Updates.CheckHours != 6 || cfg.Updates.CheckOnline {
		t.Errorf("updates = %+v, want every 6 hours as before", cfg.Updates)
	}
}
