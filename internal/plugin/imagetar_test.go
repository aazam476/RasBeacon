package plugin

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTar writes a tar holding files (name → content) and returns its path.
func writeTar(t *testing.T, files map[string]string) string {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for name, content := range files {
		w.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))})
		w.Write([]byte(content))
	}
	w.Close()
	path := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImageNames_ReadsDockerAndOCITars(t *testing.T) {
	docker := writeTar(t, map[string]string{
		"manifest.json": `[{"RepoTags":["my-plugin:1.0","ghcr.io/me/x:2"]},{"RepoTags":null}]`,
		"layer.tar":     "…",
	})
	if got, err := ImageNames(docker); err != nil || strings.Join(got, " ") != "my-plugin:1.0 ghcr.io/me/x:2" {
		t.Errorf("docker tar: %q, %v", got, err)
	}
	oci := writeTar(t, map[string]string{
		"./index.json": `{"manifests":[{"annotations":{"org.opencontainers.image.ref.name":"my-plugin:1.0"}},` +
			`{"annotations":{"io.containerd.image.name":"docker.io/library/other:1"}},{}]}`,
	})
	if got, err := ImageNames(oci); err != nil || strings.Join(got, " ") != "my-plugin:1.0 docker.io/library/other:1" {
		t.Errorf("OCI tar: %q, %v", got, err)
	}
	// No names at all is fine: podman then names nothing.
	if got, err := ImageNames(writeTar(t, map[string]string{"manifest.json": `[{}]`})); err != nil || len(got) != 0 {
		t.Errorf("no names: %q, %v", got, err)
	}
}

func TestImageNames_RefusesWhatItCantRead(t *testing.T) {
	notTar := filepath.Join(t.TempDir(), "image.tar.gz")
	os.WriteFile(notTar, []byte("\x1f\x8b compressed, or anything else"), 0o600)
	for name, path := range map[string]string{
		"not a tar (compressed)": notTar,
		"no manifest":            writeTar(t, map[string]string{"layer.tar": "x"}),
		"broken manifest.json":   writeTar(t, map[string]string{"manifest.json": "[{"}),
		"broken index.json":      writeTar(t, map[string]string{"index.json": "{"}),
		"missing file":           filepath.Join(t.TempDir(), "none.tar"),
	} {
		if _, err := ImageNames(path); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCheckImageNames_RefusesAnotherPluginsName(t *testing.T) {
	own := LocalImage("my-plugin", "1.0")
	for _, names := range [][]string{nil, {"my-plugin:1.0"}, {own}, {"ghcr.io/me/my-plugin:1.0", own}} {
		if err := CheckImageNames(names, own); err != nil {
			t.Errorf("%q: %v", names, err)
		}
	}
	for _, name := range []string{
		LocalImage("adguard-home", "0.107.0"),
		"rasbeacon-plugin/adguard-home:0.107.0", // podman adds localhost/
		"LOCALHOST/RasBeacon-Plugin/adguard-home:0.107.0",
		LocalImage("my-plugin", "0.9"), // its own older version: rolling back would run this one
	} {
		if err := CheckImageNames([]string{"my-plugin:1.0", name}, own); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
