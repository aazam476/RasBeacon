package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// image.tar comes out byte for byte, for `podman load`.
func TestExtractImage(t *testing.T) {
	m := validManifest()
	delete(m, "image") // the image is in the file instead
	path := writeRBA(t, map[string]string{
		ManifestFile: manifestJSON(m), IconFile: icon, ImageFile: "the image layers",
		"templates/AdGuardHome.yaml": "x",
	})
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !a.HasImage() || a.ImageSize() != int64(len("the image layers")) {
		t.Errorf("HasImage %v, ImageSize %d", a.HasImage(), a.ImageSize())
	}
	dst := filepath.Join(t.TempDir(), "image.tar")
	if err := a.ExtractImage(dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "the image layers" {
		t.Errorf("extracted %q", got)
	}

	// The file is gone by the time it's needed (a download that was
	// cleaned up): a clear error, not a crash.
	os.Remove(path)
	if err := a.ExtractImage(dst); err == nil {
		t.Error("extracting from a removed file should fail")
	}
	// A folder that can't be written.
	a2, _ := Open(writeRBA(t, map[string]string{
		ManifestFile: manifestJSON(m), IconFile: icon, ImageFile: "x", "templates/AdGuardHome.yaml": "x",
	}))
	if err := a2.ExtractImage(filepath.Join(t.TempDir(), "missing", "image.tar")); err == nil {
		t.Error("writing into a missing folder should fail")
	}
}

func TestExtractImage_WithoutOne(t *testing.T) {
	a, err := Open(writeRBA(t, map[string]string{
		ManifestFile: manifestJSON(validManifest()), IconFile: icon, "templates/AdGuardHome.yaml": "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if a.HasImage() || a.ImageSize() != 0 {
		t.Error("this plugin downloads its image; it has none in the file")
	}
	if err := a.ExtractImage(filepath.Join(t.TempDir(), "x")); err == nil || !strings.Contains(err.Error(), "no image.tar") {
		t.Errorf("got %v", err)
	}
}

func TestOpen_FilesThatArentPlugins(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(filepath.Join(dir, "missing.rba")); err == nil {
		t.Error("a missing file must be an error")
	}
	notZip := filepath.Join(dir, "photo.rba")
	os.WriteFile(notZip, []byte("JFIF not a zip"), 0o644)
	if _, err := Open(notZip); err == nil || !strings.Contains(err.Error(), "not a RasBeacon plugin") {
		t.Errorf("got %v", err)
	}
	// Folders other than templates/ aren't allowed.
	withFolder := writeRBA(t, map[string]string{ManifestFile: manifestJSON(validManifest()), IconFile: icon, "bin/": ""})
	if _, err := Open(withFolder); err == nil || !strings.Contains(err.Error(), "unexpected folder") {
		t.Errorf("got %v", err)
	}
	// A template with a path in its name.
	nested := writeRBA(t, map[string]string{ManifestFile: manifestJSON(validManifest()), IconFile: icon, "templates/a/b.yaml": "x"})
	if _, err := Open(nested); err == nil || !strings.Contains(err.Error(), "plain file names only") {
		t.Errorf("got %v", err)
	}
	// A manifest bigger than RasBeacon reads.
	big := writeRBA(t, map[string]string{ManifestFile: strings.Repeat(" ", maxManifestSize+1), IconFile: icon})
	if _, err := Open(big); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("got %v", err)
	}
}
