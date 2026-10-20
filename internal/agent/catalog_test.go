package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

// The plugin catalog in rbd, against a fake GitHub: reading the sources,
// remembering what they offered, GitHub's limits, and installing and
// updating plugins from it.

// fakeGitHub answers like api.github.com and github.com: one release per
// source, each with the .rba files in files and a catalog file listing them.
type fakeGitHub struct {
	mu sync.Mutex
	// releases maps "owner/repo" to the .rba files of its newest release.
	releases map[string]map[string][]byte
	// answer, if set, replaces every answer of the API (GitHub refusing).
	answer func(w http.ResponseWriter)
	asked  int
}

const testSource = "example/rasbeacon-plugins"

func (f *fakeGitHub) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := httptest.NewRecorder()
	url := req.URL.String()
	switch {
	case strings.HasPrefix(url, "https://api.github.com/repos/"):
		f.asked++
		source := strings.TrimSuffix(strings.TrimPrefix(url, "https://api.github.com/repos/"), "/releases/latest")
		files, ok := f.releases[source]
		switch {
		case f.answer != nil:
			f.answer(rec)
		case !ok:
			rec.WriteHeader(http.StatusNotFound)
		default:
			_ = json.NewEncoder(rec).Encode(f.release(source, files))
		}
	case strings.HasPrefix(url, "https://github.com/"):
		parts := strings.Split(strings.TrimPrefix(url, "https://github.com/"), "/")
		source, name := parts[0]+"/"+parts[1], parts[len(parts)-1]
		if name == plugin.CatalogFile {
			_ = json.NewEncoder(rec).Encode(f.catalog(f.releases[source]))
		} else if data, ok := f.releases[source][name]; ok {
			_, _ = rec.Write(data)
		} else {
			rec.WriteHeader(http.StatusNotFound)
		}
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

// release is GitHub's answer for a source's newest release.
func (f *fakeGitHub) release(source string, files map[string][]byte) map[string]any {
	asset := func(name string, size int) map[string]any {
		return map[string]any{"name": name, "size": size, "browser_download_url": "https://github.com/" + source + "/releases/download/v1/" + name}
	}
	assets := []map[string]any{asset(plugin.CatalogFile, 100)}
	for name, data := range files {
		assets = append(assets, asset(name, len(data)))
	}
	return map[string]any{"tag_name": "v1", "assets": assets}
}

// catalog describes every file as the test plugin (adguard-home), at the
// version in its name ("adguard-home-2.0.rba").
func (f *fakeGitHub) catalog(files map[string][]byte) map[string]any {
	var plugins []map[string]any
	for name := range files {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "adguard-home-"), ".rba")
		plugins = append(plugins, map[string]any{"file": name, "id": "adguard-home", "name": "AdGuard Home", "version": version})
	}
	return map[string]any{"plugins": plugins}
}

// catalogEnv is a RasBeacon with a test source, reaching GitHub through gh.
func catalogEnv(t *testing.T, gh *fakeGitHub) *testEnv {
	t.Helper()
	e := newPluginEnv(t)
	e.agent.mu.Lock()
	e.agent.plugins.client = func() *http.Client { return &http.Client{Transport: gh} }
	e.agent.cfg.Plugins.Sources = []string{testSource}
	e.agent.cfg.Plugins.NoBuiltInSource = true
	e.agent.mu.Unlock()
	return e
}

func TestCatalog_ListsWhatTheSourcesOfferAndAsksGitHubOnlyWhenTold(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	gh := &fakeGitHub{releases: map[string]map[string][]byte{
		testSource: {"adguard-home-1.0.rba": writeTestPlugin(t, testPluginManifest("1.0"), key)},
	}}
	e := catalogEnv(t, gh)

	catalog := e.agent.PluginCatalog(api.PluginCatalogRequest{})
	if len(catalog.Entries) != 1 || catalog.Entries[0].Name != "AdGuard Home" || catalog.Entries[0].Version != "1.0" {
		t.Fatalf("catalog = %+v", catalog)
	}
	if catalog.CheckedAt == "" || !catalog.Entries[0].RunsHere {
		t.Errorf("catalog = %+v", catalog)
	}

	// Opening it again uses what was saved; Check now asks again.
	asked := gh.asked
	e.agent.PluginCatalog(api.PluginCatalogRequest{})
	if gh.asked != asked {
		t.Error("GitHub was asked again without Check now")
	}
	e.agent.PluginCatalog(api.PluginCatalogRequest{Refresh: true})
	if gh.asked != asked+1 {
		t.Error("Check now should ask GitHub")
	}
}

func TestCatalog_ASourceThatFailsKeepsWhatItOfferedAndSaysWhy(t *testing.T) {
	gh := &fakeGitHub{releases: map[string]map[string][]byte{
		testSource: {"adguard-home-1.0.rba": writeTestPlugin(t, testPluginManifest("1.0"), nil)},
	}}
	e := catalogEnv(t, gh)
	e.agent.PluginCatalog(api.PluginCatalogRequest{})

	gh.answer = func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) }
	catalog := e.agent.PluginCatalog(api.PluginCatalogRequest{Refresh: true})
	if len(catalog.Entries) != 1 {
		t.Errorf("entries after a failed check = %+v, want the last ones kept", catalog.Entries)
	}
	if msg := catalog.Errors[testSource]; !strings.Contains(msg, "GitHub answered 502") {
		t.Errorf("errors = %v", catalog.Errors)
	}
}

func TestCatalog_WaitsAsLongAsGitHubAsks(t *testing.T) {
	gh := &fakeGitHub{releases: map[string]map[string][]byte{testSource: {}}}
	reset := time.Now().Add(time.Hour).Unix()
	gh.answer = func(w http.ResponseWriter) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(http.StatusForbidden)
	}
	e := catalogEnv(t, gh)
	catalog := e.agent.PluginCatalog(api.PluginCatalogRequest{Refresh: true})
	if !strings.Contains(catalog.Errors[testSource], "GitHub asks to wait") {
		t.Errorf("errors = %v", catalog.Errors)
	}
	asked := gh.asked
	e.agent.PluginCatalog(api.PluginCatalogRequest{Refresh: true})
	if gh.asked != asked {
		t.Error("GitHub was asked again before the time it gave")
	}
}

func TestCatalog_NoDownloadsOutsideTheVPN(t *testing.T) {
	gh := &fakeGitHub{releases: map[string]map[string][]byte{testSource: {}}}
	// The VPN is in use with the kill switch on, but the tunnel isn't up.
	e, _ := newVPNEnv(t, true)
	e.runner.failing = []string{"mountpoint"}
	e.runBackground()
	e.agent.mu.Lock()
	e.agent.plugins.client = func() *http.Client { return &http.Client{Transport: gh} }
	e.agent.cfg.Plugins.Sources = []string{testSource}
	e.agent.cfg.Plugins.NoBuiltInSource = true
	e.agent.mu.Unlock()
	catalog := e.agent.PluginCatalog(api.PluginCatalogRequest{Refresh: true})
	if !strings.Contains(catalog.Errors[testSource], "kill switch") || gh.asked != 0 {
		t.Errorf("errors = %v, GitHub asked %d times; want no question while the VPN is down", catalog.Errors, gh.asked)
	}
}

func TestCatalog_InstallAndUpdateFromIt(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	gh := &fakeGitHub{releases: map[string]map[string][]byte{
		testSource: {"adguard-home-1.0.rba": writeTestPlugin(t, testPluginManifest("1.0"), key)},
	}}
	e := catalogEnv(t, gh)
	e.agent.PluginCatalog(api.PluginCatalogRequest{})

	// No update while the catalog has nothing newer to say.
	if _, err := e.agent.UpdatePluginFromCatalog("adguard-home"); err == nil {
		t.Error("updating a plugin that isn't installed should fail")
	}

	preview, err := e.agent.StagePluginFromCatalog(api.PluginDownloadRequest{Source: testSource, File: "adguard-home-1.0.rba"})
	if err != nil || preview.Version != "1.0" {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if _, err := e.agent.InstallPlugin(api.PluginInstallRequest{StagingID: preview.StagingID, Settings: map[string]string{"password": "s3cret-pass"}}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if p := e.plugin("adguard-home"); p.Source != testSource || !p.Signed {
		t.Fatalf("installed = %+v, want it from the catalog, signed", p)
	}

	// Version 2.0 appears in the catalog.
	gh.mu.Lock()
	gh.releases[testSource] = map[string][]byte{"adguard-home-2.0.rba": writeTestPlugin(t, testPluginManifest("2.0"), key)}
	gh.mu.Unlock()
	e.agent.PluginCatalog(api.PluginCatalogRequest{Refresh: true})
	update, err := e.agent.UpdatePluginFromCatalog("adguard-home")
	if err != nil || update.Version != "2.0" {
		t.Fatalf("update = %+v, %v", update, err)
	}

	// A file that left the catalog can't be downloaded.
	if _, err := e.agent.StagePluginFromCatalog(api.PluginDownloadRequest{Source: testSource, File: "adguard-home-1.0.rba"}); err == nil {
		t.Error("a file no longer in the catalog should be refused")
	}
}

func TestCatalog_UnsignedPluginsUpdateFromFilesOnly(t *testing.T) {
	gh := &fakeGitHub{releases: map[string]map[string][]byte{testSource: {}}}
	e := catalogEnv(t, gh)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	_, err := e.agent.UpdatePluginFromCatalog("adguard-home")
	var verr *config.ValidationError
	if !errors.As(err, &verr) || !strings.Contains(verr.Message, "isn't signed") {
		t.Errorf("err = %v, want it refused as unsigned", err)
	}
}
