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

// tarEntry is one entry of a test tar: a plain file, or a link to target.
type tarEntry struct {
	name, content string
	// link is tar.TypeSymlink or tar.TypeLink for a link to target.
	link   byte
	target string
}

// writeTarEntries writes a tar holding entries in this order and returns
// its path.
func writeTarEntries(t *testing.T, entries ...tarEntry) string {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, e := range entries {
		header := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.content)), Typeflag: tar.TypeReg}
		if e.link != 0 {
			header.Typeflag, header.Linkname, header.Size = e.link, e.target, 0
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if e.link == 0 {
			w.Write([]byte(e.content))
		}
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

// Podman finds manifest.json and index.json by their cleaned path, takes
// the first one in a docker archive, and follows links. A tar that showed
// RasBeacon one manifest.json and Podman another would get another plugin's
// name past the check.
func TestImageNames_ReadsTheNamesPodmanWould(t *testing.T) {
	other := `[{"RepoTags":["` + LocalImage("samba", "4.20") + `"]}]`
	harmless := `[{"RepoTags":["my-plugin:1.0"]}]`
	own := LocalImage("my-plugin", "1.0")

	// Spelled another way, the file is still the one Podman reads: its
	// names are read, so the other plugin's name is found.
	for _, name := range []string{"./manifest.json", "././manifest.json", "x/../manifest.json", "/manifest.json"} {
		names, err := ImageNames(writeTarEntries(t, tarEntry{name: name, content: other}))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if CheckImageNames(names, own) == nil {
			t.Errorf("%s: another plugin's name got past the check (%q)", name, names)
		}
	}

	// Twice, under any two spellings: Podman would read one of them, and
	// nobody can tell which.
	for _, first := range []string{"././manifest.json", "x/../manifest.json", "manifest.json"} {
		file := writeTarEntries(t, tarEntry{name: first, content: other}, tarEntry{name: "manifest.json", content: harmless})
		if _, err := ImageNames(file); err == nil || !strings.Contains(err.Error(), "twice") {
			t.Errorf("%s and manifest.json: got %v", first, err)
		}
	}
	file := writeTarEntries(t, tarEntry{name: "./index.json", content: `{"manifests":[]}`}, tarEntry{name: "index.json", content: `{"manifests":[]}`})
	if _, err := ImageNames(file); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("index.json twice: got %v", err)
	}
}

func TestImageNames_RefusesLinksThatCouldChangeTheNames(t *testing.T) {
	other := `[{"RepoTags":["` + LocalImage("samba", "4.20") + `"]}]`
	harmless := `[{"RepoTags":["my-plugin:1.0"]}]`
	for name, file := range map[string]string{
		// Podman follows a link in manifest.json's place.
		"manifest.json is a symbolic link": writeTarEntries(t,
			tarEntry{name: "manifest.json", link: tar.TypeSymlink, target: "names.json"},
			tarEntry{name: "names.json", content: other}),
		"manifest.json is a hard link": writeTarEntries(t,
			tarEntry{name: "names.json", content: other},
			tarEntry{name: "manifest.json", link: tar.TypeLink, target: "names.json"}),
		"index.json is a symbolic link": writeTarEntries(t,
			tarEntry{name: "manifest.json", content: harmless},
			tarEntry{name: "index.json", link: tar.TypeSymlink, target: "names.json"}),
		// Unpacked, the file below the link lands on manifest.json.
		"a file inside a link to the top folder": writeTarEntries(t,
			tarEntry{name: "manifest.json", content: harmless},
			tarEntry{name: "x", link: tar.TypeSymlink, target: "."},
			tarEntry{name: "x/manifest.json", content: other}),
		"a file inside a link, listed before the link": writeTarEntries(t,
			tarEntry{name: "index.json", content: `{"manifests":[]}`},
			tarEntry{name: "a/b/index.json", content: `{"manifests":[]}`},
			tarEntry{name: "a/b", link: tar.TypeSymlink, target: "../.."}),
	} {
		if _, err := ImageNames(file); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// A link that nothing is written through is fine: `docker save` links
	// a layer that two images share.
	file := writeTarEntries(t,
		tarEntry{name: "manifest.json", content: harmless},
		tarEntry{name: "aaa/layer.tar", content: "…"},
		tarEntry{name: "bbb/layer.tar", link: tar.TypeSymlink, target: "../aaa/layer.tar"})
	if names, err := ImageNames(file); err != nil || strings.Join(names, " ") != "my-plugin:1.0" {
		t.Errorf("a shared layer's link: %q, %v", names, err)
	}
}
