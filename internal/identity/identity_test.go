package identity

import (
	"regexp"
	"testing"

	"rasbeacon/internal/config"
)

func TestNew_ProducesWellFormedValues(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !regexp.MustCompile(`^[0-9A-F]{16}$`).MatchString(id.DeviceID) {
		t.Errorf("DeviceID %q should be 16 uppercase hex characters", id.DeviceID)
	}
	if id.ShortID != id.DeviceID[12:] {
		t.Errorf("ShortID %q should be the last 4 characters of DeviceID %q", id.ShortID, id.DeviceID)
	}
	if !regexp.MustCompile(`^fd[0-9a-f]{2}:[0-9a-f]{4}:[0-9a-f]{4}::/48$`).MatchString(id.ULAPrefix) {
		t.Errorf("ULAPrefix %q is not a fdXX:XXXX:XXXX::/48 prefix", id.ULAPrefix)
	}
	if err := config.ValidateWiFiPassword(id.SuggestedHotspotPassword); err != nil {
		t.Errorf("suggested password %q is not a valid Wi-Fi password: %v", id.SuggestedHotspotPassword, err)
	}
}

func TestNewReadablePassword_FormatAndNoConfusingCharacters(t *testing.T) {
	format := regexp.MustCompile(`^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$`)
	confusing := regexp.MustCompile(`[01oli]`)

	for i := 0; i < 200; i++ {
		p, err := NewReadablePassword()
		if err != nil {
			t.Fatal(err)
		}
		if !format.MatchString(p) {
			t.Fatalf("password %q does not match xxxx-xxxx-xxxx-xxxx", p)
		}
		if confusing.MatchString(p) {
			t.Fatalf("password %q contains a look-alike character", p)
		}
	}
}

func TestNew_TwoDevicesDiffer(t *testing.T) {
	a, _ := New()
	b, _ := New()
	if a.DeviceID == b.DeviceID || a.ULAPrefix == b.ULAPrefix {
		t.Error("two generated identities should not be equal")
	}
}
