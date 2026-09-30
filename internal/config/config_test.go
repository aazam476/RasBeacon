package config

import (
	"os"
	"path/filepath"
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
	if got != want {
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
