package update

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// updateFile builds an update file by hand, so each check in Open can be
// given exactly the damage it looks for. entries are written in order; a
// nil data means "the manifest, signed with key".
type entry struct {
	name string
	data []byte
	size int64 // 0: len(data)
}

func updateFile(entries ...entry) []byte {
	var b bytes.Buffer
	t := tar.NewWriter(&b)
	for _, e := range entries {
		size := e.size
		if size == 0 {
			size = int64(len(e.data))
		}
		t.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: size})
		t.Write(e.data)
		// A short entry (size larger than the data) is padded by tar
		// when the next header is written: the size is what matters.
	}
	t.Flush()
	return b.Bytes()
}

// signed returns a manifest's JSON and its signature line.
func signed(key ed25519.PrivateKey, m any) (manifest, signature []byte) {
	manifest, _ = json.Marshal(m)
	return manifest, []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, manifest)) + "\n")
}

func TestOpen_RefusesDamagedFiles(t *testing.T) {
	public, key, _ := ed25519.GenerateKey(rand.Reader)
	keys := []ed25519.PublicKey{public}
	good := Manifest{Format: Format, Board: "rpi64", Version: "v1.0.0", BundleSHA256: strings.Repeat("a", 64), BundleSize: 4}
	withManifest := func(m any, rest ...entry) []byte {
		manifest, signature := signed(key, m)
		return updateFile(append([]entry{{name: "manifest.json", data: manifest}, {name: "manifest.sig", data: signature}}, rest...)...)
	}
	bundle := entry{name: "bundle.tar.zst", data: []byte("data")}

	broken := good
	broken.Format = "rbupdate-99"
	incomplete := good
	incomplete.BundleSHA256 = "short"

	for name, tc := range map[string]struct {
		file []byte
		want string
	}{
		"not a tar file":           {[]byte("hello"), "not a RasBeacon update file"},
		"wrong first entry":        {updateFile(entry{name: "readme.txt", data: []byte("hi")}), "expected manifest.json"},
		"signature not base64":     {updateFile(entry{name: "manifest.json", data: []byte("{}")}, entry{name: "manifest.sig", data: []byte("!!!")}), "not signed by a key"},
		"signed by someone else":   {updateFile(entry{name: "manifest.json", data: []byte("{}")}, entry{name: "manifest.sig", data: []byte(base64.StdEncoding.EncodeToString(make([]byte, 64)))}), "not signed by a key"},
		"manifest isn't JSON":      {withManifest("just a string", bundle), "reading the manifest"},
		"a newer format":           {withManifest(broken, bundle), `unknown update format "rbupdate-99"`},
		"incomplete manifest":      {withManifest(incomplete, bundle), "the manifest is incomplete"},
		"no bundle":                {withManifest(good), "has no bundle"},
		"bundle of the wrong size": {withManifest(good, entry{name: "bundle.tar.zst", data: []byte("longer data")}), "size doesn't match"},
	} {
		_, err := Open(bytes.NewReader(tc.file), keys, "rpi64")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}

	// An image without any trusted key can't check updates at all.
	if _, err := Open(bytes.NewReader(withManifest(good, bundle)), nil, "rpi64"); err == nil || !strings.Contains(err.Error(), "no update signing key") {
		t.Errorf("without keys: got %v", err)
	}
	// The right file opens.
	if _, err := Open(bytes.NewReader(withManifest(good, bundle)), keys, "rpi64"); err != nil {
		t.Errorf("the good file: %v", err)
	}
}

func TestPack_RefusesAMinimumThatIsntAVersion(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	err := Pack(&bytes.Buffer{}, bytes.NewReader([]byte("x")), "rpi64", "v1.0.0", "yesterday", key)
	if err == nil || !strings.Contains(err.Error(), "must be a version like") {
		t.Errorf("got %v", err)
	}
}

func TestParseKeys_RefuseWrongText(t *testing.T) {
	for _, text := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := ParsePublicKey(text); err == nil {
			t.Errorf("ParsePublicKey(%q) accepted", text)
		}
		if _, err := ParsePrivateKey(text); err == nil {
			t.Errorf("ParsePrivateKey(%q) accepted", text)
		}
	}
}
