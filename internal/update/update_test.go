package update

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// memDevice is a partition in memory.
type memDevice struct{ data []byte }

func (d *memDevice) WriteAt(p []byte, off int64) (int, error) {
	copy(d.data[off:], p)
	return len(p), nil
}

// makeSparse builds a sparse image with block size 16: 2 raw blocks, 1
// fill block of 0xAB, 1 don't-care block and a CRC chunk.
func makeSparse() []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&b, le, sparseHeader{Magic: sparseMagic, MajorVersion: 1, FileHeaderSize: 28,
		ChunkHeadSize: 12, BlockSize: 16, TotalBlocks: 4, TotalChunks: 4})
	binary.Write(&b, le, chunkHeader{Type: chunkRaw, Blocks: 2, TotalSize: 12 + 32})
	b.WriteString("0123456789abcdef0123456789ABCDEF")
	binary.Write(&b, le, chunkHeader{Type: chunkFill, Blocks: 1, TotalSize: 16})
	b.Write([]byte{0xAB, 0xAB, 0xAB, 0xAB})
	binary.Write(&b, le, chunkHeader{Type: chunkDontCare, Blocks: 1, TotalSize: 12})
	binary.Write(&b, le, chunkHeader{Type: chunkCRC32, TotalSize: 16})
	b.Write([]byte{0, 0, 0, 0})
	return b.Bytes()
}

func TestWriteSparse(t *testing.T) {
	dev := &memDevice{data: bytes.Repeat([]byte{'.'}, 64)}
	size, err := WriteSparse(dev, bytes.NewReader(makeSparse()), 64)
	if err != nil || size != 64 {
		t.Fatalf("size %d, err %v", size, err)
	}
	want := "0123456789abcdef0123456789ABCDEF" + strings.Repeat("\xAB", 16) + strings.Repeat(".", 16)
	if string(dev.data) != want {
		t.Errorf("got %q", dev.data)
	}
}

func TestWriteSparse_RefusesTooBigOrBroken(t *testing.T) {
	if _, err := WriteSparse(&memDevice{data: make([]byte, 32)}, bytes.NewReader(makeSparse()), 32); err == nil {
		t.Error("an image bigger than the partition must be refused")
	}
	if _, err := WriteSparse(&memDevice{data: make([]byte, 64)}, strings.NewReader("not sparse at all......."), 64); err == nil {
		t.Error("junk must be refused")
	}
}

// makeBundle builds an uncompressed bundle (tar of sparse images).
func makeBundle(names ...string) []byte {
	var b bytes.Buffer
	t := tar.NewWriter(&b)
	for _, name := range names {
		image := makeSparse()
		t.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(image))})
		t.Write(image)
	}
	t.Close()
	return b.Bytes()
}

func TestWriteBundle(t *testing.T) {
	boot, system := &memDevice{data: make([]byte, 64)}, &memDevice{data: make([]byte, 64)}
	targets := map[string]Target{"boot": {boot, 64}, "system": {system, 64}}
	if err := WriteBundle(bytes.NewReader(makeBundle("boot", "system")), targets); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(system.data, []byte("0123")) {
		t.Error("system partition not written")
	}
	if err := WriteBundle(bytes.NewReader(makeBundle("boot")), targets); err == nil {
		t.Error("a bundle without the system partition must be refused")
	}
	if err := WriteBundle(bytes.NewReader(makeBundle("boot", "system", "evil")), targets); err == nil {
		t.Error("unexpected entries must be refused")
	}
}

// packed makes an update file for pi4 signed with key.
func packed(t *testing.T, key ed25519.PrivateKey, bundle []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Pack(&out, bytes.NewReader(bundle), "pi4", "v1.2.0", "v1.0.0", key); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestOpen_VerifiesSignatureBoardAndChecksum(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	otherPublic, otherPrivate, _ := ed25519.GenerateKey(rand.Reader)
	bundle := []byte("pretend this is a zstd bundle")
	file := packed(t, private, bundle)

	// Good file: opens, the bundle reads back, the checksum matches.
	p, err := Open(bytes.NewReader(file), []ed25519.PublicKey{otherPublic, public}, "pi4")
	if err != nil {
		t.Fatal(err)
	}
	if p.Manifest.Version != "v1.2.0" || p.Manifest.MinFromVersion != "v1.0.0" {
		t.Errorf("manifest = %+v", p.Manifest)
	}
	if err := p.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}

	// Signed by someone else.
	_, err = Open(bytes.NewReader(packed(t, otherPrivate, bundle)), []ed25519.PublicKey{public}, "pi4")
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("foreign signature: got %v", err)
	}
	// No keys in the image.
	if _, err := Open(bytes.NewReader(file), nil, "pi4"); err == nil {
		t.Error("an image without keys must refuse updates")
	}
	// Wrong board.
	if _, err := Open(bytes.NewReader(file), []ed25519.PublicKey{public}, "pi5"); err == nil {
		t.Error("an update for another board must be refused")
	}

	// The bundle was changed after signing (same length).
	tampered := bytes.Replace(file, []byte("pretend this"), []byte("PRETEND THIS"), 1)
	p, err = Open(bytes.NewReader(tampered), []ed25519.PublicKey{public}, "pi4")
	if err != nil {
		t.Fatalf("the manifest is still valid, so Open should succeed: %v", err)
	}
	if err := p.Verify(); err == nil {
		t.Error("a changed bundle must fail Verify")
	}
}

func TestPack_RefusesAMinimumNewerThanTheVersion(t *testing.T) {
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	for _, minFrom := range []string{"v1.3.0", "dev"} {
		var out bytes.Buffer
		if err := Pack(&out, strings.NewReader("bundle"), "pi4", "v1.2.0", minFrom, private); err == nil {
			t.Errorf("min-from %q: an update no version can install must be refused", minFrom)
		}
	}
}

func TestCheckVersion(t *testing.T) {
	cases := []struct {
		version, minFrom, running string
		ok, reflash               bool
	}{
		{"v1.2.0", "", "v1.1.0", true, false},
		{"v1.2.0", "", "v1.2.0", true, false},              // the same again, to repair
		{"v1.2.0", "", "v1.10.0", false, false},            // older: refused
		{"v1.2.0-beta.1", "", "v1.2.0", true, false},       // only the numbers count
		{"v2.0.0", "v1.5.0", "v1.5.0", true, false},        // new enough
		{"v2.0.0", "v1.5.0", "v1.4.9", false, true},        // too old: flash instead
		{"v2.0.0", "v1.5.0", "v1.5.0-beta.2", true, false}, // a beta of 1.5.0 counts as 1.5.0
		{"v0.1.0", "v9.0.0", "dev", true, false},           // development builds install anything
	}
	for _, c := range cases {
		err := CheckVersion(Manifest{Version: c.version, MinFromVersion: c.minFrom}, c.running)
		var needs *NeedsReflashError
		if (err == nil) != c.ok || errors.As(err, &needs) != c.reflash {
			t.Errorf("%s (min %q) on %s: got %v", c.version, c.minFrom, c.running, err)
		}
	}
}

func TestKeysRoundTrip(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	pub, err := ParsePublicKey(encode(public))
	if err != nil || !pub.Equal(public) {
		t.Errorf("public key: %v", err)
	}
	priv, err := ParsePrivateKey(encode(private.Seed()))
	if err != nil || !priv.Equal(private) {
		t.Errorf("private key: %v", err)
	}
}

func encode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
