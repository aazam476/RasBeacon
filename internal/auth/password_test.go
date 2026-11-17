package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_VerifiesCorrectPasswordOnly(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=32768,t=3,p=2$") {
		t.Errorf("unexpected hash format: %s", hash)
	}

	ok, err := VerifyPassword("correct horse battery", hash)
	if err != nil || !ok {
		t.Errorf("correct password should verify, got ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong password", hash)
	if err != nil || ok {
		t.Errorf("wrong password must not verify, got ok=%v err=%v", ok, err)
	}
}

func TestHashPassword_SamePasswordGivesDifferentHashes(t *testing.T) {
	// A random salt makes every hash different, so two devices with the same
	// password can't be spotted by comparing hashes.
	a, _ := HashPassword("same password here")
	b, _ := HashPassword("same password here")
	if a == b {
		t.Error("hashes of the same password should differ (random salt)")
	}
}

func TestVerifyPassword_DamagedHashIsAnError(t *testing.T) {
	for _, stored := range []string{"", "plaintext", "$bcrypt$xyz", "$argon2id$v=19$m=1,t=1,p=1$!!!$!!!"} {
		if _, err := VerifyPassword("x", stored); err == nil {
			t.Errorf("expected an error for stored hash %q", stored)
		}
	}
}

// A stored hash with settings that argon2 can't run with (it panicked),
// that would take more memory than a Pi has, or with a salt or hash too
// short to be safe is damaged too: it is an error, and matches no
// password.
func TestVerifyPassword_UnusableSettingsMatchNothing(t *testing.T) {
	const salt, hash = "c2FsdHNhbHRzYWx0c2FsdA", "aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g"
	for name, stored := range map[string]string{
		"an empty hash":          "$argon2id$v=19$m=32768,t=3,p=2$" + salt + "$",
		"a hash of a few bytes":  "$argon2id$v=19$m=32768,t=3,p=2$" + salt + "$aGFzaA",
		"no salt":                "$argon2id$v=19$m=32768,t=3,p=2$$" + hash,
		"no passes":              "$argon2id$v=19$m=32768,t=0,p=2$" + salt + "$" + hash,
		"no threads":             "$argon2id$v=19$m=32768,t=3,p=0$" + salt + "$" + hash,
		"more memory than a Pi":  "$argon2id$v=19$m=4294967295,t=3,p=2$" + salt + "$" + hash,
		"a gigabyte, still more": "$argon2id$v=19$m=1048576,t=3,p=2$" + salt + "$" + hash,
	} {
		ok, err := VerifyPassword("any password", stored)
		if ok || err == nil {
			t.Errorf("%s: matched = %v, error = %v; want no match and an error", name, ok, err)
		}
	}
	// What HashPassword makes still verifies.
	stored, _ := HashPassword("correct horse battery staple")
	if ok, err := VerifyPassword("correct horse battery staple", stored); !ok || err != nil {
		t.Errorf("a real hash: %v, %v", ok, err)
	}
}

func TestCheckAdminPasswordStrength(t *testing.T) {
	tests := []struct {
		password string
		ok       bool
	}{
		{"short", false},
		{"123456789", false},         // 9 characters
		{"Password123", false},       // common, case-insensitive
		{"myRasBeaconPw!", false},    // contains the device name
		{"tangerine-orbit-42", true}, // long and uncommon
		{"ünïcödé-pässwörd", true},   // non-ASCII is fine for the admin password
	}
	for _, tc := range tests {
		err := CheckAdminPasswordStrength(tc.password)
		if (err == nil) != tc.ok {
			t.Errorf("CheckAdminPasswordStrength(%q) = %v, want ok=%v", tc.password, err, tc.ok)
		}
	}
}
