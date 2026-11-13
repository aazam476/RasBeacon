package agent

// A plugin's image must keep RasBeacon's name for it, and a plugin whose
// image went missing must not make Podman look for it in a registry: it is
// downloaded again when RasBeacon can, and the page says what to do when
// it can't.

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/plugin"
)

// `podman untag NAME` alone takes every name off the image, RasBeacon's
// too; only the author's name may go.
func TestPlugins_RemovingTheAuthorsImageNameKeepsRasBeaconsName(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	untagged := false
	for _, command := range e.runner.commands {
		if !strings.HasPrefix(command, "podman untag ") {
			continue
		}
		untagged = true
		if command != "podman untag localhost/adguard:latest localhost/adguard:latest" {
			t.Errorf("untag must name the image and the one name to take off it: %q", command)
		}
	}
	if !untagged {
		t.Error("the author's name should be taken off the image")
	}
	if !e.runner.ran("--pull never") {
		t.Error("podman run must never look for the image in a registry")
	}
}

func TestPlugins_AMissingImageFromAFileSaysToInstallItAgain(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	runs := e.runner.count("podman run")
	e.runner.failing = append(e.runner.failing, "podman image exists")

	if err := e.agent.StartPlugin("adguard-home"); err == nil {
		t.Error("starting without its image should fail")
	}
	p := e.plugin("adguard-home")
	if p.State != api.PluginFailed || !strings.Contains(p.Message, "missing from RasBeacon's storage") ||
		!strings.Contains(p.Message, "Install the plugin again") {
		t.Errorf("state %s, message %q", p.State, p.Message)
	}
	if e.runner.count("podman run") != runs {
		t.Error("Podman must not be asked to run an image that isn't there")
	}
}

func TestPlugins_AMissingImageFromTheCatalogIsDownloadedAgain(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	e, p := refetchEnv(t, writeTestPlugin(t, testPluginManifest("1.0"), key), key)
	e.agent.PluginCatalog(api.PluginCatalogRequest{Refresh: true})
	// Installed from the catalog as usual: nothing left over from a restore.
	p.Source, p.Fetch = testSource, nil
	installRefetched(e, p)
	e.runner.failing = append(e.runner.failing, "podman image exists")

	if err := e.agent.fetchPluginImage("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("podman load --quiet --input") ||
		!e.runner.ran("podman tag localhost/adguard:latest "+plugin.LocalImage("adguard-home", "1.0")) {
		t.Errorf("the image should be downloaded again and named; commands: %v", e.runner.commands)
	}
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	if e.agent.cfg.Plugins.Installed["adguard-home"].Fetch != nil {
		t.Error("nothing should be recorded for later: the image is back")
	}
}
