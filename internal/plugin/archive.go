package plugin

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
	"io"
	"os"
	"sort"
	"strings"
)

// Size limits for the parts of a .rba.
const (
	MaxArchiveSize   = 2 << 30 // the whole file, image included
	maxManifestSize  = 64 << 10
	maxIconSize      = 512 << 10
	maxReadmeSize    = 64 << 10
	maxTemplateSize  = 256 << 10
	maxTemplates     = 20
	maxSignatureSize = 4 << 10
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
	defer z.Close()

	a := &Archive{Templates: map[string][]byte{}, Size: info.Size(), path: file}
	var manifestData, signatureData []byte
	seen := map[string]bool{}
	for _, f := range z.File {
		name := f.Name
		if f.FileInfo().IsDir() {
			if name != templatesDir {
				return nil, fmt.Errorf("unexpected folder %q in the plugin file", name)
			}
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("%s is in the plugin file twice", name)
		}
		seen[name] = true
		switch {
		case name == ManifestFile:
			manifestData, err = readEntry(f, maxManifestSize)
		case name == IconFile:
			a.Icon, err = readEntry(f, maxIconSize)
			if err == nil && !bytes.HasPrefix(a.Icon, []byte("\x89PNG\r\n\x1a\n")) {
				err = errors.New("icon.png is not a PNG image")
			}
		case name == ReadmeFile:
			var data []byte
			data, err = readEntry(f, maxReadmeSize)
			a.Readme = string(data)
		case name == ImageFile:
			a.image = f
		case name == SignatureFile:
			signatureData, err = readEntry(f, maxSignatureSize)
		case strings.HasPrefix(name, templatesDir):
			base := strings.TrimPrefix(name, templatesDir)
			if !templateFileRe.MatchString(base) || len(a.Templates) >= maxTemplates {
				return nil, fmt.Errorf("unexpected template %q", name)
			}
			a.Templates[base], err = readEntry(f, maxTemplateSize)
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
			return nil, fmt.Errorf("templates/%s is missing", t.File)
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

// readEntry reads one file of the zip, refusing more than limit bytes.
func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%s is too large", f.Name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is too large", f.Name)
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
		// The zip reader checks the CRC at the end, so a damaged file fails.
		if _, err := io.Copy(out, io.LimitReader(r, MaxArchiveSize)); err != nil {
			out.Close()
			return fmt.Errorf("image.tar: %w", err)
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
		_, err = io.Copy(h, io.LimitReader(r, MaxArchiveSize))
		r.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		lines = append(lines, hex.EncodeToString(h.Sum(nil))+"  "+f.Name+"\n")
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i][66:] < lines[j][66:] })
	return []byte(signaturePrefix + strings.Join(lines, "")), nil
}

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
// file made with key (replacing any old one).
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
	sigJSON, _ := json.Marshal(signatureFile{
		PublicKey: base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, digest)),
	})

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	w := zip.NewWriter(out)
	for _, f := range files {
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
	if err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// KeyID is a short, readable form of a public key for the screens: the
// first 16 hex digits of its SHA-256, in groups of four.
func KeyID(key []byte) string {
	sum := sha256.Sum256(key)
	h := strings.ToUpper(hex.EncodeToString(sum[:8]))
	return strings.Join([]string{h[0:4], h[4:8], h[8:12], h[12:16]}, " ")
}
