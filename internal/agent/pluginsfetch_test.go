package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

// Downloading a plugin's app again after a restore from a backup that
// didn't hold it (pluginsfetch.go): the file must be the same plugin, the
// same version, signed by the same author.

// refetchURL is where the fake GitHub serves the plugin's file.
const refetchURL = "https://github.com/" + testSource + "/releases/download/v1/adguard-home-1.0.rba"

// refetchEnv is a RasBeacon whose source offers file as adguard-home 1.0,
// and the plugin as a restore leaves it: pinned to pinnedKey's public key.
func refetchEnv(t *testing.T, file []byte, pinnedKey ed25519.PrivateKey) (*testEnv, config.InstalledPlugin) {
	t.Helper()
	gh := &fakeGitHub{releases: map[string]map[string][]byte{testSource: {"adguard-home-1.0.rba": file}}}
	e := catalogEnv(t, gh)
	p := config.InstalledPlugin{
		Version:   "1.0",
		Image:     plugin.LocalImage("adguard-home", "1.0"),
		PublicKey: base64.StdEncoding.EncodeToString(pinnedKey.Public().(ed25519.PublicKey)),
		Fetch:     &config.PluginFetch{URL: refetchURL},
	}
	e.runner.commands = nil
	return e, p
}

func TestRefetch_AFileSignedByTheSameAuthorLoads(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	e, p := refetchEnv(t, writeTestPlugin(t, testPluginManifest("1.0"), key), key)
	if err := e.agent.downloadPluginImage("adguard-home", p); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("podman load --quiet --input") || !e.runner.ran("podman tag localhost/adguard:latest "+p.Image) {
		t.Errorf("the image should be loaded under RasBeacon's name; commands: %v", e.runner.commands)
	}
}

func TestRefetch_AFileSignedByAnotherKeyIsRefused(t *testing.T) {
	_, pinned, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	e, p := refetchEnv(t, writeTestPlugin(t, testPluginManifest("1.0"), other), pinned)
	err := e.agent.downloadPluginImage("adguard-home", p)
	if err == nil || !strings.Contains(err.Error(), "isn't signed by this plugin's author") {
		t.Fatalf("got %v, want the other author's file refused", err)
	}
	if e.runner.ran("podman load") {
		t.Error("nothing may be loaded from a file signed by someone else")
	}
}

func TestRefetch_AnUnsignedFileOrAnotherVersionIsRefused(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	e, p := refetchEnv(t, writeTestPlugin(t, testPluginManifest("1.0"), nil), key)
	if err := e.agent.downloadPluginImage("adguard-home", p); err == nil {
		t.Error("an unsigned file must be refused")
	}
	e, p = refetchEnv(t, writeTestPlugin(t, testPluginManifest("2.0"), key), key)
	if err := e.agent.downloadPluginImage("adguard-home", p); err == nil || !strings.Contains(err.Error(), "not adguard-home 1.0") {
		t.Errorf("another version must be refused, got %v", err)
	}
	if e.runner.ran("podman load") {
		t.Error("nothing may be loaded from a refused file")
	}
}

func TestRefetch_APublishedImageIsPulledThroughTheProxyAndRenamed(t *testing.T) {
	e := newPluginEnv(t)
	e.runner.commands = nil
	ref := "docker.io/adguard/adguardhome@sha256:" + strings.Repeat("a", 64)
	image := plugin.LocalImage("adguard-home", "1.0")
	if err := e.agent.pullPluginImage(ref, image); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("env HTTPS_PROXY=http://127.0.0.1:") || !e.runner.ran("podman tag "+ref+" "+image) {
		t.Errorf("the image should be pulled through the download proxy and renamed; commands: %v", e.runner.commands)
	}

	// A pull that fails says so, and names nothing.
	e.runner.commands = nil
	e.runner.failing = append(e.runner.failing, "env HTTPS_PROXY")
	if err := e.agent.pullPluginImage(ref, image); err == nil || !strings.Contains(err.Error(), "can't be downloaded") {
		t.Errorf("got %v", err)
	}
	if e.runner.ran("podman tag") {
		t.Error("a failed pull must not be renamed")
	}
}
