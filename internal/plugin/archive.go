package plugin

// This file opens .rba files (zip archives), checks what is in them and
// their signature, and signs them (for rbsign).

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Size limits for the parts of a .rba. "<<" shifts bits: 1<<10 is 1024, so
// 64<<10 is 64 KB, 2<<30 is 2 GB.
const (
	MaxArchiveSize   = 2 << 30 // the whole file, image included
	maxImageSize     = 4 << 30 // image.tar once unpacked (zip may compress it)
	maxManifestSize  = 64 << 10
	maxIconSize      = 512 << 10
	maxReadmeSize    = 64 << 10
	maxTemplateSize  = 256 << 10
	maxTemplates     = 20
	maxSignatureSize = 4 << 10
	// The icon's smallest and largest sides, in pixels.
	minIconSide = 128
	maxIconSide = 1024
)

// File names inside a .rba.
const (
	ManifestFile  = "manifest.json"
	IconFile      = "icon.png"
	ReadmeFile    = "README.md"
	ImageFile     = "image.tar"
	SignatureFile = "signature"
	templatesDir  = "templates/"
)

// signaturePrefix starts the signed text, so a plugin signature can't be
// mistaken for any other signature made with the same key.
const signaturePrefix = "RasBeacon plugin signature v1\n"

// Archive is an opened .rba file.
type Archive struct {
	Manifest *Manifest
	// ManifestData is manifest.json as it is in the file.
	ManifestData []byte
	Icon         []byte
	Readme       string
	// Templates by file name.
	Templates map[string][]byte
	// PublicKey is the key that signed it (nil: not signed).
	PublicKey ed25519.PublicKey
	// Size is the .rba's size in bytes.
	Size int64

	// path and image are kept to unpack image.tar later (ExtractImage),
	// which is too large to hold in memory.
	path  string
	image *zip.File
}

// signatureFile is the signature file's JSON.
type signatureFile struct {
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
}

// Open reads and checks a .rba file: its contents, its manifest, and its
// signature if it has one. A signature that doesn't match is an error.
func Open(file string) (*Archive, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxArchiveSize {
		return nil, errors.New("the plugin file is larger than 2 GB")
	}
	z, err := zip.OpenReader(file)
	if err != nil {
		return nil, errors.New("this is not a RasBeacon plugin (.rba) file")
	}
	// defer runs z.Close() when Open returns, whichever return it is.
	defer z.Close()

	a := &Archive{Templates: map[string][]byte{}, Size: info.Size(), path: file}
	var manifestData, signatureData []byte
	seen := map[string]bool{}
	// Look at every entry in the zip. Only the names listed in the package
	// comment are allowed; anything else is refused rather than ignored.
	for _, f := range z.File {
		name := f.Name
		if f.FileInfo().IsDir() {
			if name != templatesDir {
				return nil, fmt.Errorf("unexpected folder %q in the plugin file", name)
			}
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("the plugin file has %s twice", name)
		}
		seen[name] = true
		switch {
		case name == ManifestFile:
			manifestData, err = readEntry(f, maxManifestSize)
		case name == IconFile:
			if a.Icon, err = readEntry(f, maxIconSize); err == nil {
				err = checkIcon(a.Icon)
			}
		case name == ReadmeFile:
			var data []byte
			data, err = readEntry(f, maxReadmeSize)
			a.Readme = string(data)
		case name == ImageFile:
			// The zip states each entry's unpacked size, and Go's zip reader
			// refuses data that doesn't match it, so this size is real.
			if f.UncompressedSize64 > maxImageSize {
				return nil, errors.New("its image (image.tar) is larger than 4 GB")
			}
			a.image = f
		case name == SignatureFile:
			signatureData, err = readEntry(f, maxSignatureSize)
		case strings.HasPrefix(name, templatesDir):
			base := strings.TrimPrefix(name, templatesDir)
			if !templateFileRe.MatchString(base) {
				return nil, fmt.Errorf("unexpected template %q (plain file names only)", name)
			}
			if len(a.Templates) >= maxTemplates {
				return nil, fmt.Errorf("the plugin file has more than %d templates", maxTemplates)
			}
			if a.Templates[base], err = readEntry(f, maxTemplateSize); err == nil {
				err = CheckTemplate(base, a.Templates[base])
			}
		default:
			return nil, fmt.Errorf("unexpected file %q in the plugin file", name)
		}
		if err != nil {
			return nil, err
		}
	}
	if manifestData == nil {
		return nil, errors.New("the plugin file has no manifest.json")
	}
	if a.Icon == nil {
		return nil, errors.New("the plugin file has no icon.png")
	}
	a.ManifestData = manifestData
	if a.Manifest, err = ParseManifest(manifestData, a.image != nil); err != nil {
		return nil, err
	}
	for _, t := range a.Manifest.Templates {
		if _, ok := a.Templates[t.File]; !ok {
			return nil, fmt.Errorf("the plugin file has no templates/%s", t.File)
		}
	}
	if signatureData != nil {
		digest, err := contentDigest(z.File)
		if err != nil {
			return nil, err
		}
		if a.PublicKey, err = checkSignature(signatureData, digest); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// checkIcon checks that icon.png is a square PNG of a sensible size. Only
// the image's header is read, not its pixels.
func checkIcon(data []byte) error {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return errors.New("its icon (icon.png) is not a PNG image")
	}
	if config.Width != config.Height || config.Width < minIconSide || config.Width > maxIconSide {
		return fmt.Errorf("its icon (icon.png) must be square, from %d×%d to %d×%d pixels (it is %d×%d)",
			minIconSide, minIconSide, maxIconSide, maxIconSide, config.Width, config.Height)
	}
	return nil
}

// readEntry reads one file of the zip, refusing more than limit bytes.
func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("the plugin file's %s is too large", f.Name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("the plugin file's %s can't be read: %w", f.Name, err)
	}
	defer r.Close()
	// LimitReader stops after limit+1 bytes, so a file that lies about its
	// size can't fill the memory; reading one byte more than allowed is how
	// "too large" is noticed.
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("the plugin file's %s can't be read: %w", f.Name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("the plugin file's %s is too large", f.Name)
	}
	return data, nil
}

// HasImage reports whether the .rba carries its image (image.tar).
func (a *Archive) HasImage() bool { return a.image != nil }

// ImageSize is image.tar's size (0 without one).
func (a *Archive) ImageSize() int64 {
	if a.image == nil {
		return 0
	}
	return int64(a.image.UncompressedSize64)
}

// ExtractImage writes image.tar to dst, for `podman load`.
func (a *Archive) ExtractImage(dst string) error {
	if a.image == nil {
		return errors.New("the plugin file has no image.tar")
	}
	// The zip was closed when Open returned, so open it again.
	z, err := zip.OpenReader(a.path)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != ImageFile {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		// The zip reader checks the size and checksum at the end, so a
		// damaged file fails here instead of reaching Podman.
		if _, err := io.Copy(out, io.LimitReader(r, maxImageSize+1)); err != nil {
			out.Close()
			return fmt.Errorf("unpacking image.tar: %w", err)
		}
		return out.Close()
	}
	return errors.New("the plugin file has no image.tar")
}

// contentDigest is the text a signature covers: a line with the SHA-256
// and name of every file but the signature, sorted by name.
func contentDigest(files []*zip.File) ([]byte, error) {
	var lines []string
	for _, f := range files {
		if f.FileInfo().IsDir() || f.Name == SignatureFile {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, io.LimitReader(r, maxImageSize+1))
		r.Close()
		if err != nil {
			return nil, fmt.Errorf("the plugin file's %s can't be read: %w", f.Name, err)
		}
		lines = append(lines, hex.EncodeToString(h.Sum(nil))+"  "+f.Name+"\n")
	}
	// Each line is 64 hex digits, two spaces, then the name: [66:] is the
	// name, which the lines are sorted by.
	sort.Slice(lines, func(i, j int) bool { return lines[i][66:] < lines[j][66:] })
	return []byte(signaturePrefix + strings.Join(lines, "")), nil
}

// checkSignature reads the signature file and checks it against digest. It
// returns the key that signed the file.
func checkSignature(data, digest []byte) (ed25519.PublicKey, error) {
	var s signatureFile
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, errors.New("the signature file can't be read")
	}
	key, err := base64.StdEncoding.DecodeString(s.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("the signature's public key can't be read")
	}
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil || !ed25519.Verify(key, digest, sig) {
		return nil, errors.New("the plugin's signature doesn't match its files: it was changed after signing, or damaged")
	}
	return key, nil
}

// Sign adds a signature to a .rba: it copies src to dst with a signature
// file made with key (replacing any old one). src and dst may be the same
// file: the signed copy is written beside dst first, then renamed over it.
func Sign(src, dst string, key ed25519.PrivateKey) error {
	z, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer z.Close()
	var files []*zip.File
	for _, f := range z.File {
		if f.Name != SignatureFile {
			files = append(files, f)
		}
	}
	digest, err := contentDigest(files)
	if err != nil {
		return err
	}
	sigJSON, err := json.Marshal(signatureFile{
		PublicKey: base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, digest)),
	})
	if err != nil {
		return err
	}

	out, err := os.CreateTemp(filepath.Dir(dst), ".signing-*.rba")
	if err != nil {
		return err
	}
	// Until the rename below succeeds, a failure removes the half-written
	// copy. After the rename the name is gone, and Remove does nothing.
	defer os.Remove(out.Name())
	w := zip.NewWriter(out)
	for _, f := range files {
		// Copy moves each entry over as it is, still compressed, so the
		// signed file holds exactly the bytes the digest covers.
		if err := w.Copy(f); err != nil {
			out.Close()
			return err
		}
	}
	sw, err := w.Create(SignatureFile)
	if err == nil {
		_, err = sw.Write(sigJSON)
	}
	if err == nil {
		err = w.Close()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(out.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(out.Name(), dst)
}

// KeyID is a short, readable form of a public key for the screens: the
// first 16 hex digits of its SHA-256, in groups of four.
func KeyID(key []byte) string {
	sum := sha256.Sum256(key)
	h := strings.ToUpper(hex.EncodeToString(sum[:8]))
	return strings.Join([]string{h[0:4], h[4:8], h[8:12], h[12:16]}, " ")
}
