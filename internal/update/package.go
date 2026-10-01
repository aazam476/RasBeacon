// Package update installs RasBeacon updates.
//
// # The update file
//
// A release has one update file per board, e.g.
// rasbeacon-pi4-v1.2.0.rbupdate. It is a tar archive with three entries,
// in this order:
//
//	manifest.json   what this is: board, version, size and SHA-256 of the bundle
//	manifest.sig    an Ed25519 signature of manifest.json (base64)
//	bundle.tar.zst  rpi-image-gen's update bundle: zstd-compressed tar with
//	                the boot and system partitions as sparse images
//
// RasBeacon checks the signature against the public keys built into the
// image BEFORE writing anything, then streams the bundle into the
// partitions of the slot that isn't running, checking the SHA-256 along
// the way. Only if everything matches does it boot the new slot, once, on
// trial ("tryboot"; see internal/agent/update.go). Nobody without the
// release signing key can make an update a RasBeacon will install.
package update

import (
	"archive/tar"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"
)

// Format names this layout; a different layout would get a new name.
const Format = "rasbeacon-update-1"

// Manifest describes an update.
type Manifest struct {
	Format string `json:"format"`
	// Board is the image the update is for: "pi4" or "pi5".
	Board   string `json:"board"`
	Version string `json:"version"`
	// BundleSHA256 and BundleSize describe bundle.tar.zst.
	BundleSHA256 string `json:"bundleSha256"`
	BundleSize   int64  `json:"bundleSize"`
}

// Package is an update file being read.
type Package struct {
	Manifest Manifest
	tar      *tar.Reader
	hash     hash.Hash
	read     int64
}

// ErrBadSignature means the update was not signed by a trusted key.
var ErrBadSignature = errors.New("this update is not signed by a key this RasBeacon trusts")

// Open reads the manifest and signature from an update file and checks
// them. On success, Bundle returns the bundle to read next. board is this
// device's board; an update for another board is refused.
func Open(r io.Reader, trustedKeys []ed25519.PublicKey, board string) (*Package, error) {
	if len(trustedKeys) == 0 {
		return nil, errors.New("this RasBeacon image has no update signing key, so it can't check updates; flash the new image instead")
	}
	t := tar.NewReader(r)
	manifestJSON, err := readSmallEntry(t, "manifest.json")
	if err != nil {
		return nil, err
	}
	signatureText, err := readSmallEntry(t, "manifest.sig")
	if err != nil {
		return nil, err
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureText)))
	if err != nil {
		return nil, ErrBadSignature
	}
	trusted := false
	for _, key := range trustedKeys {
		if ed25519.Verify(key, manifestJSON, signature) {
			trusted = true
			break
		}
	}
	if !trusted {
		return nil, ErrBadSignature
	}

	// Only now, with a valid signature, is the manifest worth reading.
	var m Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("reading the manifest: %w", err)
	}
	if m.Format != Format {
		return nil, fmt.Errorf("unknown update format %q (this RasBeacon is too old for it?)", m.Format)
	}
	if m.Board != board {
		return nil, fmt.Errorf("this update is for %s, but this is a %s RasBeacon", m.Board, board)
	}
	if len(m.BundleSHA256) != 64 || m.BundleSize <= 0 {
		return nil, errors.New("the manifest is incomplete")
	}

	header, err := t.Next()
	if err != nil || header.Name != "bundle.tar.zst" {
		return nil, errors.New("the update file has no bundle")
	}
	if header.Size != m.BundleSize {
		return nil, errors.New("the bundle's size doesn't match the manifest")
	}
	return &Package{Manifest: m, tar: t, hash: sha256.New()}, nil
}

// Read reads the bundle (the compressed partitions), hashing it as it goes.
func (p *Package) Read(b []byte) (int, error) {
	n, err := p.tar.Read(b)
	p.hash.Write(b[:n])
	p.read += int64(n)
	return n, err
}

// Verify reads whatever is left of the bundle and checks its SHA-256
// against the signed manifest. Call it after the partitions were written
// and before using them.
func (p *Package) Verify() error {
	if _, err := io.Copy(io.Discard, p); err != nil {
		return err
	}
	sum := hex.EncodeToString(p.hash.Sum(nil))
	if p.read != p.Manifest.BundleSize || sum != p.Manifest.BundleSHA256 {
		return errors.New("the update was damaged or changed (its checksum doesn't match the signed one)")
	}
	return nil
}

// readSmallEntry reads the next tar entry, which must be called name and
// small.
func readSmallEntry(t *tar.Reader, name string) ([]byte, error) {
	header, err := t.Next()
	if err != nil {
		return nil, errors.New("this is not a RasBeacon update file")
	}
	if header.Name != name || header.Size > 64*1024 {
		return nil, fmt.Errorf("this is not a RasBeacon update file (expected %s)", name)
	}
	return io.ReadAll(t)
}

// --- Making update files (used by cmd/rbsign at release time) ---

// Pack writes an update file: manifest, signature and bundle. bundle must
// be readable twice (it is hashed first), e.g. an *os.File.
func Pack(out io.Writer, bundle io.ReadSeeker, board, version string, key ed25519.PrivateKey) error {
	h := sha256.New()
	size, err := io.Copy(h, bundle)
	if err != nil {
		return err
	}
	if _, err := bundle.Seek(0, io.SeekStart); err != nil {
		return err
	}
	manifestJSON, err := json.MarshalIndent(Manifest{
		Format: Format, Board: board, Version: version,
		BundleSHA256: hex.EncodeToString(h.Sum(nil)), BundleSize: size,
	}, "", "  ")
	if err != nil {
		return err
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(key, manifestJSON))

	t := tar.NewWriter(out)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.json", manifestJSON}, {"manifest.sig", []byte(signature + "\n")}} {
		if err := t.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.data))}); err != nil {
			return err
		}
		if _, err := t.Write(entry.data); err != nil {
			return err
		}
	}
	if err := t.WriteHeader(&tar.Header{Name: "bundle.tar.zst", Mode: 0o644, Size: size}); err != nil {
		return err
	}
	if _, err := io.Copy(t, bundle); err != nil {
		return err
	}
	return t.Close()
}

// ParsePublicKey reads a public key file's text (base64, 32 bytes).
func ParsePublicKey(text string) (ed25519.PublicKey, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(data) != ed25519.PublicKeySize {
		return nil, errors.New("not an update signing public key")
	}
	return ed25519.PublicKey(data), nil
}

// ParsePrivateKey reads a private key (base64 of the 32-byte seed).
func ParsePrivateKey(text string) (ed25519.PrivateKey, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(data) != ed25519.SeedSize {
		return nil, errors.New("not an update signing private key")
	}
	return ed25519.NewKeyFromSeed(data), nil
}
