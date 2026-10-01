package config

import (
	"errors"
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
		{"no CA certificate", eduroam(nil), NetworkCredentials{Password: "pw"}, "networks.0.enterprise.caCert"},
		{"no server name", eduroam(func(e *EnterpriseConfig) { e.ServerName = "" }), withCA, "networks.0.enterprise.serverName"},
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
