package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/plugin"
	"rasbeacon/internal/update"
)

// rbsign runs one command and returns its exit code and output. env holds
// the environment variables the command sees.
func rbsign(t *testing.T, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, func(name string) string { return env[name] }, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// keys reads the private and public keys from keygen's output: the lines
// after "Private key" and "Public key".
func keys(t *testing.T, output string) (private, public string) {
	t.Helper()
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "Private key"):
			private = lines[i+1]
		case strings.HasPrefix(line, "Public key"):
			public = lines[i+1]
		}
	}
	if private == "" || public == "" {
		t.Fatalf("no keys in:\n%s", output)
	}
	return private, public
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"nonsense"},
		{"pack"},                    // nothing given
		{"pack", "-unknown-flag"},   // a flag rbsign doesn't know
		{"plugin-sign", "-in", "x"}, // no -out
		{"plugin-check"},            // no files
	} {
		code, _, stderr := rbsign(t, nil, args...)
		if code != 2 || !strings.Contains(stderr, "usage: rbsign") {
			t.Errorf("rbsign %v: exit %d, %q; want 2 and the usage", args, code, stderr)
		}
	}
}

// The update key keygen prints signs update files that RasBeacon accepts
// with the printed public key.
func TestKeygenAndPack(t *testing.T) {
	code, out, _ := rbsign(t, nil, "keygen")
	if code != 0 {
		t.Fatal("keygen failed")
	}
	private, public := keys(t, out)

	dir := t.TempDir()
	bundle := filepath.Join(dir, "x.update.tar.zst")
	os.WriteFile(bundle, []byte("the system image"), 0o644)
	rbupdate, info := filepath.Join(dir, "x.rbupdate"), filepath.Join(dir, "x.update.json")
	env := map[string]string{"RASBEACON_UPDATE_SIGNING_KEY": private}
	code, _, stderr := rbsign(t, env, "pack", "-board", "rpi64", "-version", "v1.2.0", "-min-from", "v1.0.0",
		"-bundle", bundle, "-out", rbupdate, "-info", info)
	if code != 0 {
		t.Fatalf("pack: exit %d: %s", code, stderr)
	}

	publicKey, _ := base64.StdEncoding.DecodeString(public)
	f, _ := os.Open(rbupdate)
	defer f.Close()
	pkg, err := update.Open(f, []ed25519.PublicKey{publicKey}, "rpi64")
	if err != nil {
		t.Fatalf("RasBeacon refuses the packed update: %v", err)
	}
	if pkg.Manifest.Version != "v1.2.0" {
		t.Errorf("version = %q", pkg.Manifest.Version)
	}
	var written update.Info
	data, _ := os.ReadFile(info)
	if json.Unmarshal(data, &written) != nil || written.Version != "v1.2.0" || written.MinFromVersion != "v1.0.0" {
		t.Errorf("info file = %s", data)
	}
}

// With -trusted, pack says which of the image's keys signs, and refuses a
// key the image doesn't hold before writing anything.
func TestPackChecksTheTrustedKeys(t *testing.T) {
	_, out, _ := rbsign(t, nil, "keygen")
	private, public := keys(t, out)
	_, out, _ = rbsign(t, nil, "keygen")
	_, other := keys(t, out)

	dir := t.TempDir()
	trusted := filepath.Join(dir, "update-keys")
	os.Mkdir(trusted, 0o755)
	os.WriteFile(filepath.Join(trusted, "README"), []byte("not a key"), 0o644)
	os.WriteFile(filepath.Join(trusted, "release.pub"), []byte(other+"\n"), 0o644)
	bundle := filepath.Join(dir, "x.update.tar.zst")
	os.WriteFile(bundle, []byte("the system image"), 0o644)
	rbupdate := filepath.Join(dir, "x.rbupdate")
	env := map[string]string{"RASBEACON_UPDATE_SIGNING_KEY": private}
	args := []string{"pack", "-board", "rpi64", "-version", "v1.2.0", "-bundle", bundle, "-out", rbupdate, "-trusted", trusted}

	code, _, stderr := rbsign(t, env, args...)
	if code != 1 || !strings.Contains(stderr, "is none of the keys") || !strings.Contains(stderr, public) {
		t.Errorf("a key the image doesn't trust: exit %d, %q", code, stderr)
	}
	if _, err := os.Stat(rbupdate); err == nil {
		t.Error("nothing should be written for a key the image doesn't trust")
	}

	os.WriteFile(filepath.Join(trusted, "release-2.pub"), []byte(public+"\n"), 0o644)
	code, stdout, stderr := rbsign(t, env, args...)
	if code != 0 || !strings.Contains(stdout, "Signing with the key of release-2.pub") {
		t.Errorf("a trusted key: exit %d, %q, %q", code, stdout, stderr)
	}

	// A folder that isn't there.
	args[len(args)-1] = filepath.Join(dir, "missing")
	if code, _, _ := rbsign(t, env, args...); code != 1 {
		t.Errorf("a missing folder: exit %d", code)
	}
}

func TestPackFailures(t *testing.T) {
	dir := t.TempDir()
	args := []string{"pack", "-board", "rpi64", "-version", "v1", "-bundle", filepath.Join(dir, "missing"), "-out", filepath.Join(dir, "x")}
	// No key in the environment: refused before reading anything.
	code, _, stderr := rbsign(t, nil, args...)
	if code != 1 || !strings.Contains(stderr, "RASBEACON_UPDATE_SIGNING_KEY") {
		t.Errorf("without a key: exit %d, %q", code, stderr)
	}
	// A key, but no bundle file.
	_, out, _ := rbsign(t, nil, "keygen")
	private, _ := keys(t, out)
	code, _, stderr = rbsign(t, map[string]string{"RASBEACON_UPDATE_SIGNING_KEY": private}, args...)
	if code != 1 || !strings.Contains(stderr, "missing") {
		t.Errorf("without the bundle: exit %d, %q", code, stderr)
	}
}

// testPlugin writes a small unsigned plugin file and returns its path.
func testPlugin(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trip-notes.rba")
	f, _ := os.Create(path)
	w := zip.NewWriter(f)
	add := func(name string, content []byte) {
		fw, _ := w.Create(name)
		fw.Write(content)
	}
	manifest, _ := json.Marshal(map[string]any{"id": "trip-notes", "name": "Trip Notes", "version": "1.0", "description": "Notes."})
	add(plugin.ManifestFile, manifest)
	var icon bytes.Buffer
	png.Encode(&icon, image.NewGray(image.Rect(0, 0, 128, 128)))
	add(plugin.IconFile, icon.Bytes())
	// image.tar as `docker save` writes it.
	var image bytes.Buffer
	tw := tar.NewWriter(&image)
	names, _ := json.Marshal([]map[string]any{{"Config": "config.json", "RepoTags": []string{"localhost/trip-notes:1.0"}}})
	tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o644, Size: int64(len(names))})
	tw.Write(names)
	tw.Close()
	add(plugin.ImageFile, image.Bytes())
	w.Close()
	f.Close()
	return path
}

// A plugin signed with plugin-keygen's key opens with that key's ID, and
// plugin-check describes it.
func TestPluginKeygenSignAndCheck(t *testing.T) {
	code, out, _ := rbsign(t, nil, "plugin-keygen")
	if code != 0 {
		t.Fatal("plugin-keygen failed")
	}
	private, public := keys(t, out)
	publicKey, _ := base64.StdEncoding.DecodeString(public)
	keyID := plugin.KeyID(publicKey)
	if !strings.Contains(out, "Key ID: "+keyID) {
		t.Errorf("the key ID is missing:\n%s", out)
	}

	unsigned := testPlugin(t)
	code, _, stderr := rbsign(t, nil, "plugin-check", unsigned)
	if code != 0 {
		t.Fatalf("plugin-check of the unsigned file: %s", stderr)
	}
	signed := filepath.Join(t.TempDir(), "signed.rba")
	code, out, stderr = rbsign(t, map[string]string{"RASBEACON_PLUGIN_SIGNING_KEY": private}, "plugin-sign", "-in", unsigned, "-out", signed)
	if code != 0 || !strings.Contains(out, "Signed trip-notes 1.0 (key ID "+keyID+")") {
		t.Fatalf("plugin-sign: exit %d, %q %q", code, out, stderr)
	}
	code, out, _ = rbsign(t, nil, "plugin-check", signed)
	if code != 0 || !strings.Contains(out, "trip-notes 1.0 (Trip Notes), signed, key ID "+keyID) {
		t.Errorf("plugin-check: exit %d, %q", code, out)
	}
}

func TestPluginSignAndCheckFailures(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.rba")
	code, _, stderr := rbsign(t, nil, "plugin-sign", "-in", missing, "-out", missing+".signed")
	// Named as a signing key, not an update's: the plugin key is a separate one.
	if code != 1 || !strings.Contains(stderr, "RASBEACON_PLUGIN_SIGNING_KEY") || strings.Contains(stderr, "update") {
		t.Errorf("without a key: exit %d, %q", code, stderr)
	}
	_, out, _ := rbsign(t, nil, "plugin-keygen")
	private, _ := keys(t, out)
	code, _, _ = rbsign(t, map[string]string{"RASBEACON_PLUGIN_SIGNING_KEY": private}, "plugin-sign", "-in", missing, "-out", missing+".signed")
	if code != 1 {
		t.Errorf("signing a missing file: exit %d, want 1", code)
	}
	code, _, stderr = rbsign(t, nil, "plugin-check", missing)
	if code != 1 || !strings.Contains(stderr, "missing.rba") {
		t.Errorf("checking a missing file: exit %d, %q", code, stderr)
	}
}
