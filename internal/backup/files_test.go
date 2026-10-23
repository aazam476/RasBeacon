package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Adding files and folders from disk, as rbd does for /persistent, and
// archives that aren't RasBeacon backups.

func TestAddFileAndAddDir_KeepRegularFilesAndSkipWhatTheyAreTold(t *testing.T) {
	src := t.TempDir()
	write := func(name, text string) {
		p := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write("config.json", `{"claimed":true}`)
	write("plugins/notes/settings.json", `{"title":"Notes"}`)
	write("plugins/notes/cache/big.bin", "skipped")
	write("plugins/other/skip-me.txt", "skipped")
	if err := os.Symlink("config.json", filepath.Join(src, "plugins", "link")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	bw, err := Create(&buf, "correct horse", Info{Version: "v0.2.0"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := bw.AddFile("config.json", filepath.Join(src, "config.json")); err != nil {
		t.Fatal(err)
	}
	if err := bw.AddFile("missing.json", filepath.Join(src, "missing.json")); err != nil {
		t.Errorf("a missing file should be skipped, got %v", err)
	}
	skip := func(relative string) bool { return relative == "notes/cache" || relative == "other/skip-me.txt" }
	if err := bw.AddDir("plugins", filepath.Join(src, "plugins"), skip); err != nil {
		t.Fatal(err)
	}
	if err := bw.AddDir("gone", filepath.Join(src, "no-such-folder"), nil); err != nil {
		t.Errorf("a missing folder should be skipped, got %v", err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if _, err := Extract(&buf, "correct horse", dir); err != nil {
		t.Fatal(err)
	}
	var got []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			relative, _ := filepath.Rel(dir, p)
			got = append(got, filepath.ToSlash(relative))
		}
		return nil
	})
	want := []string{"config.json", "plugins/notes/settings.json"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("restored %v, want %v", got, want)
	}
	if text, _ := os.ReadFile(filepath.Join(dir, "plugins", "notes", "settings.json")); string(text) != `{"title":"Notes"}` {
		t.Errorf("settings.json = %q", text)
	}
}

func TestExtract_RefusesWhatIsNotARasBeaconBackup(t *testing.T) {
	t.Run("encrypted, but not a compressed archive", func(t *testing.T) {
		var buf bytes.Buffer
		enc, _ := NewWriter(&buf, "correct horse")
		_, _ = enc.Write([]byte("not an archive at all"))
		_ = enc.Close()
		if _, err := Extract(&buf, "correct horse", t.TempDir()); !errors.Is(err, ErrWrongPassword) {
			t.Errorf("Extract = %v, want ErrWrongPassword", err)
		}
	})

	t.Run("an archive that doesn't start with the backup's info", func(t *testing.T) {
		var buf bytes.Buffer
		enc, _ := NewWriter(&buf, "correct horse")
		gz := gzip.NewWriter(enc)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: "config.json", Mode: 0o600, Size: 2, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("{}"))
		_ = tw.Close()
		_ = gz.Close()
		_ = enc.Close()
		if _, err := Extract(&buf, "correct horse", t.TempDir()); !errors.Is(err, ErrNotBackup) {
			t.Errorf("Extract = %v, want ErrNotBackup", err)
		}
	})
}

func TestExtract_RefusesFoldersAndLinks(t *testing.T) {
	var buf bytes.Buffer
	bw, err := Create(&buf, "correct horse", Info{Created: time.Unix(1, 0)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := bw.tw.WriteHeader(&tar.Header{Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}); err != nil {
		t.Fatal(err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(&buf, "correct horse", t.TempDir()); err == nil || !strings.Contains(err.Error(), "unexpected entry") {
		t.Errorf("Extract = %v, want the link refused", err)
	}
}
