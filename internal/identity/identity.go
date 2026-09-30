// Package identity creates the values that make each RasBeacon unique.
//
// Release images are identical for everyone and contain no passwords or
// keys. Anything that must differ between devices is generated here, on the
// device, the first time it boots, and then saved to identity.json.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"rasbeacon/internal/config"
)

// New generates a fresh identity using the operating system's secure random
// number generator.
func New() (config.Identity, error) {
	deviceBytes, err := randomBytes(8)
	if err != nil {
		return config.Identity{}, err
	}
	deviceID := strings.ToUpper(hex.EncodeToString(deviceBytes)) // 16 hex characters

	ula, err := newULAPrefix()
	if err != nil {
		return config.Identity{}, err
	}

	password, err := NewReadablePassword()
	if err != nil {
		return config.Identity{}, err
	}

	return config.Identity{
		DeviceID:                 deviceID,
		ShortID:                  deviceID[len(deviceID)-4:],
		ULAPrefix:                ula,
		SuggestedHotspotPassword: password,
	}, nil
}

// passwordAlphabet leaves out characters people confuse when reading a
// password off a screen: 0/o, 1/l/i. 32 symbols means each one carries 5 bits
// of randomness.
const passwordAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// NewReadablePassword returns a random Wi-Fi password like
// "kq7m-2xvd-h8pt-rn4w": 16 random symbols (about 79 bits of randomness, far
// beyond what can be guessed) in groups of 4 so it is easy to type on a phone.
func NewReadablePassword() (string, error) {
	var groups []string
	for g := 0; g < 4; g++ {
		var group strings.Builder
		for i := 0; i < 4; i++ {
			index, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordAlphabet))))
			if err != nil {
				return "", fmt.Errorf("generating password: %w", err)
			}
			group.WriteByte(passwordAlphabet[index.Int64()])
		}
		groups = append(groups, group.String())
	}
	return strings.Join(groups, "-"), nil
}

// newULAPrefix creates a random IPv6 "unique local address" prefix, as
// described in RFC 4193: "fd" followed by 40 random bits, giving a /48 such as
// fd12:3456:789a::/48. Because it is random, it is very unlikely to clash
// with any other private network the device joins.
func newULAPrefix() (string, error) {
	b, err := randomBytes(5)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("fd%02x:%02x%02x:%02x%02x::/48", b[0], b[1], b[2], b[3], b[4]), nil
}

// randomBytes returns n bytes from the secure random number generator.
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("reading random bytes: %w", err)
	}
	return b, nil
}
