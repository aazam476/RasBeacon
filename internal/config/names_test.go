package config

import (
	"errors"
	"strings"
	"testing"
)

// The names and addresses the owner types: RasBeacon's name, VPN profile
// names, MAC addresses, and the allow list and guest network built on them.

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantErr  string
	}{
		{"the default", "rasbeacon", ""},
		{"digits and hyphens", "travel-pi-2", ""},
		{"63 characters", strings.Repeat("a", 63), ""},
		{"empty", "", "1 to 63"},
		{"64 characters", strings.Repeat("a", 64), "1 to 63"},
		{"capitals", "MyPi", "lowercase"},
		{"a space", "my pi", "lowercase"},
		{"a dot", "pi.lan", "lowercase"},
		{"starts with a hyphen", "-pi", "hyphen"},
		{"ends with a hyphen", "pi-", "hyphen"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateHostname(tc.hostname)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateHostname(%q) = %v, want no error", tc.hostname, err)
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Field != "hostname" || !strings.Contains(verr.Message, tc.wantErr) {
				t.Errorf("ValidateHostname(%q) = %v, want a hostname error mentioning %q", tc.hostname, err, tc.wantErr)
			}
		})
	}
}

func TestName_IsTheDefaultUntilChanged(t *testing.T) {
	var c Config
	if c.Name() != DefaultHostname {
		t.Errorf("Name() = %q, want %q", c.Name(), DefaultHostname)
	}
	c.Hostname = "travel-pi"
	if c.Name() != "travel-pi" {
		t.Errorf("Name() = %q, want travel-pi", c.Name())
	}
}

func TestValidateProfileName(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		wantErr bool
	}{
		{"a file name", "home.conf", false},
		{"spaces and accents", "Café VPN", false},
		{"64 characters", strings.Repeat("é", 64), false},
		{"empty", "", true},
		{"only spaces", "   ", true},
		{"65 characters", strings.Repeat("a", 65), true},
		{"a newline", "home\nwork", true},
		{"not UTF-8", "home\xff", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateProfileName(tc.profile); (err != nil) != tc.wantErr {
				t.Errorf("ValidateProfileName(%q) = %v, wantErr %v", tc.profile, err, tc.wantErr)
			}
		})
	}
}

func TestNormalizeMAC(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "3a:1f:22:aa:bb:cc", want: "3a:1f:22:aa:bb:cc"},
		{in: " 3A-1F-22-AA-BB-CC ", want: "3a:1f:22:aa:bb:cc"},
		{in: "3a1f.22aa.bbcc", want: "3a:1f:22:aa:bb:cc"},
		{in: "not a mac", wantErr: true},
		{in: "3a:1f:22:aa:bb", wantErr: true},
		// 20-byte InfiniBand addresses parse, but aren't Wi-Fi devices.
		{in: "00:00:00:00:fe:80:00:00:00:00:00:00:02:00:5e:10:00:00:00:01", wantErr: true},
		// The lowest bit of the first byte set: a group (multicast) address.
		{in: "01:00:5e:00:00:01", wantErr: true},
	}
	for _, tc := range tests {
		got, err := NormalizeMAC(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("NormalizeMAC(%q) = %q, %v; want %q, error %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestAccessConfig_Validate(t *testing.T) {
	many := make([]string, MaxAllowedDevices+1)
	for i := range many {
		many[i] = "3a:1f:22:aa:" + string("0123456789abcdef"[i/16%16]) + string("0123456789abcdef"[i%16]) + ":cc"
	}
	tests := []struct {
		name      string
		access    AccessConfig
		wantField string
	}{
		{"nothing set", AccessConfig{}, ""},
		{"a list and a limit", AccessConfig{AllowListOn: true, Allowed: []string{"3a:1f:22:aa:bb:cc"}, MaxDevices: 8}, ""},
		{"the largest limit", AccessConfig{MaxDevices: MaxDevicesLimit}, ""},
		{"a limit too large", AccessConfig{MaxDevices: MaxDevicesLimit + 1}, "maxDevices"},
		{"a negative limit", AccessConfig{MaxDevices: -1}, "maxDevices"},
		{"too many devices", AccessConfig{Allowed: many}, "allowed"},
		{"an address not normalized", AccessConfig{Allowed: []string{"3A:1F:22:AA:BB:CC"}}, "allowed"},
		{"an address twice", AccessConfig{Allowed: []string{"3a:1f:22:aa:bb:cc", "3a:1f:22:aa:bb:cc"}}, "allowed"},
		{"on with nobody listed", AccessConfig{AllowListOn: true}, "allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.access.Validate()
			if tc.wantField == "" {
				if err != nil {
					t.Errorf("Validate() = %v, want no error", err)
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Field != tc.wantField {
				t.Errorf("Validate() = %v, want an error on %q", err, tc.wantField)
			}
		})
	}
}

func TestGuestConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		guest     GuestConfig
		password  string
		wantField string
	}{
		{"off needs nothing", GuestConfig{}, "", ""},
		{"on with a name and password", GuestConfig{Enabled: true, SSID: "Trip-Guests"}, "guest-pass-1", ""},
		{"on without a name", GuestConfig{Enabled: true}, "guest-pass-1", "ssid"},
		{"the hotspot's own name", GuestConfig{Enabled: true, SSID: "Trip"}, "guest-pass-1", "ssid"},
		{"a short password", GuestConfig{Enabled: true, SSID: "Trip-Guests"}, "short", "password"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.guest.Validate(tc.password, "Trip")
			if tc.wantField == "" {
				if err != nil {
					t.Errorf("Validate() = %v, want no error", err)
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Field != tc.wantField {
				t.Errorf("Validate() = %v, want an error on %q", err, tc.wantField)
			}
		})
	}
}

func TestDefaultChannel(t *testing.T) {
	if got := DefaultChannel(Band5GHz); got != 36 {
		t.Errorf("DefaultChannel(5 GHz) = %d, want 36", got)
	}
	if got := DefaultChannel(Band2GHz); got != 6 {
		t.Errorf("DefaultChannel(2.4 GHz) = %d, want 6", got)
	}
}
