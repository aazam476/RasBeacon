// Package auth hashes and checks the web UI admin password.
//
// # Why hash?
//
// We never store the admin password itself. We store a "hash": the output of
// a one-way function. To check a login, we hash what was typed and compare.
// If someone copies the SD card, they get the hash, not the password.
//
// # Why argon2id?
//
// Ordinary hash functions (like SHA-256) are designed to be fast, which also
// makes guessing millions of passwords fast. argon2id is designed to be slow
// and to use a lot of memory, which makes guessing expensive. It is the
// algorithm recommended by the OWASP password storage guidance.
//
// # Stored format
//
// The hash is stored as one string in the widely used "PHC" format:
//
//	$argon2id$v=19$m=32768,t=3,p=2$<salt in base64>$<hash in base64>
//
// It records the settings used, so we can raise them in a later release and
// still verify passwords that were hashed with the old settings.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Settings for new hashes. 32 MiB of memory and 3 passes take roughly a third
// of a second on a Raspberry Pi Zero 2 W: slow for an attacker guessing
// passwords, fine for a person logging in. The Zero 2 W has only 512 MB of
// RAM, which is why memory is not set higher.
const (
	argonMemoryKiB  = 32 * 1024
	argonIterations = 3
	argonThreads    = 2
	argonKeyLength  = 32
	saltLength      = 16
)

// MinAdminPasswordLength is the shortest admin password we accept.
const MinAdminPasswordLength = 10

// HashPassword returns the stored form of password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonThreads, argonKeyLength)

	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonIterations, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches a hash made by
// HashPassword. It returns an error only if the stored hash is damaged.
func VerifyPassword(password, stored string) (bool, error) {
	// Expected parts after splitting on "$":
	//   ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("stored password hash is not in argon2id format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("stored password hash has an unsupported version")
	}

	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, fmt.Errorf("stored password hash settings are unreadable: %w", err)
	}

	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("stored password salt is unreadable: %w", err)
	}
	wantKey, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("stored password hash is unreadable: %w", err)
	}

	gotKey := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(wantKey)))

	// ConstantTimeCompare takes the same time whether the first byte or the
	// last byte differs, so an attacker cannot learn anything from timing.
	return subtle.ConstantTimeCompare(gotKey, wantKey) == 1, nil
}

// commonPasswordsText is a short list of the most common passwords (one per
// line), built into the program. See common-passwords.txt.
//
//go:embed common-passwords.txt
var commonPasswordsText string

// commonPasswords is commonPasswordsText as a set, built once at startup.
var commonPasswords = func() map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Split(commonPasswordsText, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			set[strings.ToLower(line)] = true
		}
	}
	return set
}()

// CheckAdminPasswordStrength returns a friendly error if password is too
// weak to protect the admin UI, or nil if it is acceptable.
func CheckAdminPasswordStrength(password string) error {
	if len([]rune(password)) < MinAdminPasswordLength {
		return fmt.Errorf("must be at least %d characters long", MinAdminPasswordLength)
	}
	if commonPasswords[strings.ToLower(password)] {
		return errors.New("is one of the most commonly used passwords; please choose another")
	}
	if strings.Contains(strings.ToLower(password), "rasbeacon") {
		return errors.New("must not contain the device name")
	}
	return nil
}
