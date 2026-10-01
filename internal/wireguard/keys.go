// Package wireguard reads WireGuard settings: keys, and the ".conf" files
// that VPN providers and WireGuard servers hand out.
//
// RasBeacon doesn't run wg-quick. It reads the file itself, checks it, and
// configures the tunnel with the "wg" and "ip" tools (see internal/agent).
// That is why commands in the file (PostUp and friends) are refused: they
// would run as root on the device.
package wireguard

import (
	"crypto/rand"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/curve25519"
)

// Key is a WireGuard key: 32 bytes, written as 44 characters of base64.
type Key [32]byte

// ParseKey reads a base64 key such as "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=".
func ParseKey(text string) (Key, error) {
	var k Key
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil || len(data) != len(k) {
		return k, errors.New("Must be a WireGuard key: 44 characters ending in \"=\".")
	}
	copy(k[:], data)
	return k, nil
}

// String returns the key in base64.
func (k Key) String() string { return base64.StdEncoding.EncodeToString(k[:]) }

// GeneratePrivateKey makes a new random private key.
func GeneratePrivateKey() (Key, error) {
	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		return k, err
	}
	// "Clamping", required for Curve25519 private keys (same as `wg genkey`).
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	return k, nil
}

// PublicKey returns the public key that belongs to private key k. The
// public key is what the WireGuard server needs to know about RasBeacon.
func (k Key) PublicKey() Key {
	var public Key
	out, _ := curve25519.X25519(k[:], curve25519.Basepoint)
	copy(public[:], out)
	return public
}
