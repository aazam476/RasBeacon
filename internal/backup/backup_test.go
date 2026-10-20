package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeBackup(t *testing.T, password string, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	bw, err := Create(&buf, password, Info{Created: time.Unix(1, 0), Version: "v1.2.0", Plugins: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := bw.AddBytes(name, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBackup_RoundTripLargeFiles(t *testing.T) {
	big := make([]byte, 5*chunkSize+123) // several chunks, random (incompressible)
	rand.Read(big)
	files := map[string][]byte{"config.json": []byte(`{"claimed":true}`), "plugins/x/data.img": big, "empty": {}}
	data := makeBackup(t, "correct horse", files)

	dir := t.TempDir()
	info, err := Extract(bytes.NewReader(data), "correct horse", dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.2.0" || !info.Plugins {
		t.Errorf("info = %+v", info)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: %d bytes, %v; want %d", name, len(got), err, len(want))
		}
	}
	if bytes.Contains(data, []byte("claimed")) {
		t.Error("the contents must be encrypted")
	}
}

func TestBackup_WrongPasswordAndDamage(t *testing.T) {
	big := make([]byte, 3*chunkSize)
	rand.Read(big)
	data := makeBackup(t, "correct horse", map[string][]byte{"a": big})

	if _, err := Extract(bytes.NewReader(data), "wrong horse", t.TempDir()); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong password: %v", err)
	}
	// Cut off: the last chunk is missing.
	if _, err := Extract(bytes.NewReader(data[:len(data)-100]), "correct horse", t.TempDir()); err == nil {
		t.Error("a cut-off file must be refused")
	}
	// One byte changed in the middle.
	changed := bytes.Clone(data)
	changed[len(changed)/2] ^= 1
	if _, err := Extract(bytes.NewReader(changed), "correct horse", t.TempDir()); err == nil {
		t.Error("a changed file must be refused")
	}
	// Something added after the end.
	if _, err := Extract(bytes.NewReader(append(bytes.Clone(data), 0)), "correct horse", t.TempDir()); err == nil {
		t.Error("data after the last chunk must be refused")
	}
	if _, err := Extract(bytes.NewReader([]byte("PK\x03\x04 a zip file")), "correct horse", t.TempDir()); !errors.Is(err, ErrNotBackup) {
		t.Errorf("not a backup: %v", err)
	}
	if _, err := NewWriter(&bytes.Buffer{}, "short"); err == nil {
		t.Error("short passwords must be refused")
	}
}

func TestBackup_ExactChunkSizes(t *testing.T) {
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 2 * chunkSize} {
		var buf bytes.Buffer
		w, _ := NewWriter(&buf, "password1")
		payload := bytes.Repeat([]byte{7}, size)
		w.Write(payload)
		w.Close()
		r, err := NewReader(&buf, "password1")
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		var got bytes.Buffer
		if _, err := got.ReadFrom(r); err != nil || !bytes.Equal(got.Bytes(), payload) {
			t.Errorf("size %d: got %d bytes, %v", size, got.Len(), err)
		}
	}
}

func TestBackup_RefusesUnsafeNames(t *testing.T) {
	data := makeBackup(t, "correct horse", map[string][]byte{"../escape": []byte("x")})
	if _, err := Extract(bytes.NewReader(data), "correct horse", t.TempDir()); err == nil {
		t.Error("names leaving the folder must be refused")
	}
}

// Backups made before RasBeacon 0.2 are gzipped; they still restore.
func TestExtract_OldGzipBackup(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewWriter(&buf, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)
	for _, file := range [][2]string{{InfoFile, `{"version":"v0.1.0","plugins":false}`}, {"config.json", `{"claimed":true}`}} {
		_ = tw.WriteHeader(&tar.Header{Name: file[0], Mode: 0o600, Size: int64(len(file[1])), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(file[1]))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = gz.Close()
	_ = enc.Close()

	dir := t.TempDir()
	info, err := Extract(&buf, "correct horse", dir)
	if err != nil || info.Version != "v0.1.0" {
		t.Fatalf("info = %+v, %v", info, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(got) != `{"claimed":true}` {
		t.Errorf("config.json = %q", got)
	}
}
