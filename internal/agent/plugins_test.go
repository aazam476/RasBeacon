package agent

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
	"rasbeacon/internal/version"
)

// testPluginManifest is a plugin like AdGuard Home, with its image in the file.
func testPluginManifest(version string) map[string]any {
	return map[string]any{
		"id": "adguard-home", "name": "AdGuard Home", "version": version, "description": "Blocks ads.",
		"permissions": []string{"internet"},
		"ports":       []map[string]any{{"name": "Web interface", "container": 3000, "open": true}, {"name": "DNS", "container": 53, "protocol": "udp", "default": 5353}},
		"volumes":     []map[string]any{{"name": "conf", "path": "/opt/adguardhome/conf"}, {"name": "work", "path": "/opt/adguardhome/work"}},
		"settings": []map[string]any{
			{"key": "password", "label": "Admin password", "type": "password", "required": true},
			{"key": "upstream", "label": "Upstream", "type": "select", "default": "quad9", "env": "UPSTREAM",
				"options": []map[string]any{{"value": "quad9", "label": "Quad9"}, {"value": "cloudflare", "label": "Cloudflare"}}},
		},
		"templates":    []map[string]any{{"file": "AdGuardHome.yaml", "path": "/opt/adguardhome/conf/AdGuardHome.yaml", "once": true}},
		"afterInstall": "Set the DNS server to {address}. Open port {port:3000}.",
	}
}

// testIcon is a real 128×128 PNG (plugin icons are checked).
func testIcon() string {
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, 128, 128)))
	return b.String()
}

// imageTar is a small image file as `docker save` writes it, with these
// names in its manifest.json.
func imageTar(names ...string) string {
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	manifest, _ := json.Marshal([]map[string]any{{"Config": "config.json", "RepoTags": names}})
	w.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o644, Size: int64(len(manifest))})
	w.Write(manifest)
	w.Close()
	return b.String()
}

// writeTestPlugin makes a .rba (signed with key unless nil) and returns its bytes.
func writeTestPlugin(t *testing.T, manifest map[string]any, key ed25519.PrivateKey) []byte {
	t.Helper()
	return writeTestPluginWithImage(t, manifest, key, imageTar("localhost/adguard:latest"))
}

// writeTestPluginWithImage is writeTestPlugin with this image.tar (none
// if imageFile is "", for a plugin whose manifest names a published image).
func writeTestPluginWithImage(t *testing.T, manifest map[string]any, key ed25519.PrivateKey, imageFile string) []byte {
	t.Helper()
	dir := t.TempDir()
	unsigned := filepath.Join(dir, "p.rba")
	f, err := os.Create(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	m, _ := json.Marshal(manifest)
	add := func(name, content string) {
		fw, _ := w.Create(name)
		fw.Write([]byte(content))
	}
	add(plugin.ManifestFile, string(m))
	add(plugin.IconFile, testIcon())
	if imageFile != "" {
		add(plugin.ImageFile, imageFile)
	}
	// Every template the manifest lists: AdGuardHome.yaml as AdGuard Home's,
	// any other a line with a setting.
	templates, _ := manifest["templates"].([]map[string]any)
	for _, t := range templates {
		file := t["file"].(string)
		content := "upstream: {{.Settings.upstream}}\n"
		if file == "AdGuardHome.yaml" {
			content = "password: {{bcrypt .Settings.password}}\nupstream: {{.Settings.upstream}}\n"
		}
		add("templates/"+file, content)
	}
	w.Close()
	f.Close()
	path := unsigned
	if key != nil {
		path = filepath.Join(dir, "signed.rba")
		if err := plugin.Sign(unsigned, path, key); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// newPluginEnv is a claimed RasBeacon whose Podman and mounts are fake.
func newPluginEnv(t *testing.T) *testEnv {
	e := newClaimedEnv(t, false)
	e.runner.prefixOutputs = map[string]string{"podman load": "Loaded image: localhost/adguard:latest\n", "podman ps": "[]"}
	e.runner.failing = []string{"mountpoint"} // nothing is mounted yet
	e.runner.effect = func(args []string) {
		// podman save writes the image (the last argument) to the file
		// after --output.
		if len(args) > 2 && args[0] == "podman" && args[1] == "save" {
			for i, arg := range args[:len(args)-1] {
				if arg == "--output" {
					_ = os.WriteFile(args[i+1], []byte(imageTar(args[len(args)-1])), 0o600)
				}
			}
		}
		// podman run reads the environment from the file after --env-file.
		if len(args) > 2 && args[0] == "podman" && args[1] == "run" {
			for i, arg := range args[:len(args)-1] {
				if arg == "--env-file" {
					env, _ := os.ReadFile(args[i+1])
					e.runEnvs = append(e.runEnvs, string(env))
				}
			}
		}
	}
	e.runBackground()
	return e
}

// installTestPlugin uploads and installs a plugin with a password.
func (e *testEnv) installTestPlugin(data []byte, ports map[string]int) (api.Plugin, error) {
	e.t.Helper()
	preview, err := e.agent.StagePluginFile(bytes.NewReader(data))
	if err != nil {
		return api.Plugin{}, err
	}
	p, err := e.agent.InstallPlugin(api.PluginInstallRequest{
		StagingID: preview.StagingID, Ports: ports, Settings: map[string]string{"password": "s3cret-pass"},
	})
	if err != nil {
		return p, err
	}
	e.runBackground()
	return p, nil
}

func (e *testEnv) plugin(id string) api.Plugin {
	for _, p := range e.agent.PluginsState().Plugins {
		if p.ID == id {
			return p
		}
	}
	e.t.Fatalf("plugin %s is not installed", id)
	return api.Plugin{}
}

func TestPlugins_InstallFromAFileStartsItWithItsOwnAddressPortsAndData(t *testing.T) {
	e := newPluginEnv(t)
	data := writeTestPlugin(t, testPluginManifest("1.0"), nil)

	preview, err := e.agent.StagePluginFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Name != "AdGuard Home" || preview.Signed || preview.Update || len(preview.Permissions) != 1 ||
		preview.Permissions[0].Title != "Use the internet" {
		t.Errorf("preview = %+v", preview)
	}
	if preview.Ports[0].Host != 3000 || preview.Ports[1].Host != 5353 {
		t.Errorf("suggested ports = %+v", preview.Ports)
	}
	// The password is required.
	_, err = e.agent.InstallPlugin(api.PluginInstallRequest{StagingID: preview.StagingID})
	var verr *config.ValidationError
	if !errors.As(err, &verr) || verr.Field != "settings.password" {
		t.Fatalf("a missing password should be refused, got %v", err)
	}
	p, err := e.agent.InstallPlugin(api.PluginInstallRequest{
		StagingID: preview.StagingID, Ports: map[string]int{"tcp/3000": 8080}, Settings: map[string]string{"password": "s3cret-pass"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.State != api.PluginInstalling {
		t.Errorf("state right after = %s", p.State)
	}
	e.runBackground()

	p = e.plugin("adguard-home")
	if p.State != api.PluginRunning || p.Address != "10.89.0.2" {
		t.Fatalf("after installing: %+v", p)
	}
	if p.AfterInstall != "Set the DNS server to 10.89.0.2. Open port 8080." {
		t.Errorf("afterInstall = %q", p.AfterInstall)
	}
	if p.Ports[0].URL != "http://10.87.0.1:8080/" {
		t.Errorf("open URL = %q", p.Ports[0].URL)
	}
	for _, want := range []string{
		"podman load --quiet --input", "podman tag localhost/adguard:latest localhost/rasbeacon-plugin/adguard-home:1.0",
		"podman untag localhost/adguard:latest", // only RasBeacon's name stays
		"fallocate --length 256M", "mkfs.ext4", "mount -o loop,nosuid,nodev",
		"podman run --detach --replace --name rasbeacon-adguard-home",
		"--network rasbeacon-plugins:ip=10.89.0.2,host_interface_name=rbport2 --dns 10.89.0.1",
		"--security-opt mask=/sys/class/thermal", "--env-file " + e.paths.PluginEnvFile("adguard-home"),
	} {
		if !e.runner.ran(want) {
			t.Errorf("never ran %q", want)
		}
	}
	// The settings reach the container through a file only root can read,
	// gone once podman has read it, never on the command line.
	if len(e.runEnvs) != 1 || !strings.Contains(e.runEnvs[0], "UPSTREAM=quad9\n") {
		t.Errorf("environment files = %q", e.runEnvs)
	}
	if e.runner.ran("--env ") {
		t.Error("environment variables must not be on podman's command line")
	}
	// No command rbd ran holds the password: a command line can end up in
	// an error message, the journal and so the debug log on the SD card.
	for _, command := range e.runner.commands {
		if strings.Contains(command, "s3cret-pass") {
			t.Errorf("a command line holds the plugin's password: %s", command)
		}
	}
	if _, err := os.Stat(e.paths.PluginEnvFile("adguard-home")); !os.IsNotExist(err) {
		t.Errorf("the environment file should be deleted after the start: %v", err)
	}
	// The password is a secret, not a setting; the template got its hash.
	if strings.Contains(e.readFile(e.paths.ConfigFile()), "s3cret-pass") {
		t.Error("the password must not be in config.json")
	}
	if !strings.Contains(e.readFile(e.paths.SecretsFile()), "s3cret-pass") {
		t.Error("the password belongs in secrets.json")
	}
	yaml := e.readFile(filepath.Join(e.agent.pluginDataMount("adguard-home"), "conf", "AdGuardHome.yaml"))
	if !strings.Contains(yaml, "password: $2a$10$") || !strings.Contains(yaml, "upstream: quad9") {
		t.Errorf("template:\n%s", yaml)
	}
	// Devices on the hotspot reach it; it may use the internet.
	rules := e.readFile(e.paths.FirewallRules())
	for _, want := range []string{
		`iifname "br-lan" ip daddr 10.87.0.1 tcp dport 8080 dnat ip to 10.89.0.2:3000`,
		`iifname "br-lan" ip daddr 10.87.0.1 udp dport 5353 dnat ip to 10.89.0.2:53`,
		`iifname "rbplug0" ip saddr { 10.89.0.2 } oifname @egress accept`,
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("firewall is missing %q", want)
		}
	}
	if s := e.agent.Status(); len(s.Plugins) != 1 || s.Plugins[0].State != api.PluginRunning {
		t.Errorf("status plugins = %+v", s.Plugins)
	}
	if !strings.Contains(e.agent.Diagnostics(), "adguard-home 1.0 (file, unsigned): running") {
		t.Error("diagnostics should list the plugin and its state")
	}
	if strings.Contains(e.agent.Diagnostics(), "s3cret-pass") {
		t.Error("diagnostics must not show plugin settings")
	}
}

// A plugin's image.tar can name images itself, and podman load would take
// those names from the images that have them. One naming another plugin's
// image would make that plugin run this one's program.
func TestPlugins_AnImageCarryingAnotherPluginsNameIsRefused(t *testing.T) {
	e := newPluginEnv(t)
	tar := imageTar("localhost/adguard:latest", "localhost/rasbeacon-plugin/samba:4.20")
	_, err := e.installTestPlugin(writeTestPluginWithImage(t, testPluginManifest("1.0"), nil, tar), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := e.plugin("adguard-home")
	if p.State == api.PluginRunning || !strings.Contains(p.Message, "another plugin's name, localhost/rasbeacon-plugin/samba:4.20") {
		t.Errorf("after installing: %+v", p)
	}
	if e.runner.ran("podman load") {
		t.Error("the image must not be loaded")
	}

	// An image file podman would read but RasBeacon can't (compressed) is
	// refused too: its names can't be checked.
	e = newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPluginWithImage(t, testPluginManifest("1.0"), nil, "\x1f\x8b gzip"), nil); err != nil {
		t.Fatal(err)
	}
	if p := e.plugin("adguard-home"); p.State == api.PluginRunning || e.runner.ran("podman load") {
		t.Errorf("a compressed image: %+v", p)
	}
}

func TestPlugins_PortsAreChecked(t *testing.T) {
	e := newPluginEnv(t)
	data := writeTestPlugin(t, testPluginManifest("1.0"), nil)
	cases := []struct {
		ports map[string]int
		field string
	}{
		{map[string]int{"tcp/3000": 80}, "ports.tcp/3000"},
		{map[string]int{"tcp/3000": 0}, "ports.tcp/3000"},
		{map[string]int{"tcp/3000": 70000}, "ports.tcp/3000"},
		{map[string]int{"udp/53": 53}, "ports.udp/53"},
		{map[string]int{"udp/53": 22}, ""}, // 22 is reserved for both
	}
	for _, c := range cases {
		_, err := e.installTestPlugin(data, c.ports)
		var verr *config.ValidationError
		if !errors.As(err, &verr) || (c.field != "" && verr.Field != c.field) {
			t.Errorf("ports %v: got %v", c.ports, err)
		}
	}

	// A second plugin can't take a port the first one uses.
	if _, err := e.installTestPlugin(data, map[string]int{"tcp/3000": 3000}); err != nil {
		t.Fatal(err)
	}
	other := testPluginManifest("1.0")
	other["id"], other["name"] = "other", "Other"
	preview, err := e.agent.StagePluginFile(bytes.NewReader(writeTestPlugin(t, other, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Ports[0].Host == 3000 || preview.Ports[1].Host == 5353 {
		t.Errorf("suggested ports should skip the taken ones: %+v", preview.Ports)
	}
	_, err = e.agent.InstallPlugin(api.PluginInstallRequest{StagingID: preview.StagingID,
		Ports: map[string]int{"tcp/3000": 3000}, Settings: map[string]string{"password": "x"}})
	if err == nil || !strings.Contains(err.Error(), "AdGuard Home already uses port 3000") {
		t.Errorf("got %v", err)
	}
}

func TestPlugins_SigningKeyIsPinnedOnFirstSignedInstall(t *testing.T) {
	e := newPluginEnv(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)

	// Unsigned first: updates from files are fine, signed or not.
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.1"), nil), nil); err != nil {
		t.Fatalf("unsigned update of an unsigned plugin: %v", err)
	}
	// A signed update pins its key from then on.
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.2"), key), nil); err != nil {
		t.Fatal(err)
	}
	p := e.plugin("adguard-home")
	if !p.Signed || p.Version != "1.2" || p.KeyID == "" {
		t.Fatalf("after a signed update: %+v", p)
	}
	for name, data := range map[string][]byte{
		"unsigned":      writeTestPlugin(t, testPluginManifest("1.3"), nil),
		"different key": writeTestPlugin(t, testPluginManifest("1.3"), otherKey),
	} {
		if _, err := e.agent.StagePluginFile(bytes.NewReader(data)); err == nil {
			t.Errorf("%s update was accepted", name)
		}
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.3"), key), nil); err != nil {
		t.Errorf("same key: %v", err)
	}
	// The update kept the password, and the old image went.
	if !strings.Contains(e.readFile(e.paths.SecretsFile()), "s3cret-pass") || !e.runner.ran("podman rmi --ignore localhost/rasbeacon-plugin/adguard-home:1.2") {
		t.Error("an update keeps the settings and removes the old image")
	}

	// Remove it with its data kept.
	dataFile := filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage)
	mustWrite(t, dataFile, "data") // fallocate is fake here
	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home", KeepData: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.paths.PluginKeptDir(), "adguard-home.img")); err != nil {
		t.Errorf("the data should be kept: %v", err)
	}

	// Another author's plugin with the same ID: the sheet asks, naming the
	// earlier plugin and its key, and the data stays aside unless chosen.
	preview, err := e.agent.StagePluginFile(bytes.NewReader(writeTestPlugin(t, testPluginManifest("2.0"), otherKey)))
	if err != nil {
		t.Fatal(err)
	}
	if k := preview.KeptData; k == nil || k.Name != "AdGuard Home" || k.KeyID != p.KeyID {
		t.Fatalf("the sheet should ask about the data kept from the earlier AdGuard Home (key %s): %+v", p.KeyID, k)
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("2.0"), otherKey), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataFile); err == nil {
		t.Error("a plugin signed with another key must not get the kept data without being asked")
	}

	// The same author's plugin gets it back by itself, without a question.
	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home"}); err != nil {
		t.Fatal(err)
	}
	preview, err = e.agent.StagePluginFile(bytes.NewReader(writeTestPlugin(t, testPluginManifest("2.0"), key)))
	if err != nil || preview.KeptData != nil {
		t.Errorf("the same key: %+v, %v", preview.KeptData, err)
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("2.0"), key), nil); err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(dataFile); got != "data" {
		t.Errorf("the kept data should come back to the same author's plugin: %q", got)
	}
	if _, err := os.Stat(filepath.Join(e.paths.PluginKeptDir(), "adguard-home.key.json")); !os.IsNotExist(err) {
		t.Errorf("the note of whose data it was should go with it: %v", err)
	}
}

// When nothing proves two installs are the same plugin (here both are
// unsigned), kept data comes back only if the owner chooses it.
func TestPlugins_UnsignedKeptDataComesBackOnlyWhenChosen(t *testing.T) {
	for _, chosen := range []bool{false, true} {
		e := newPluginEnv(t)
		if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
			t.Fatal(err)
		}
		dataFile := filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage)
		mustWrite(t, dataFile, "data")
		if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home", KeepData: true}); err != nil {
			t.Fatal(err)
		}
		preview, err := e.agent.StagePluginFile(bytes.NewReader(writeTestPlugin(t, testPluginManifest("1.0"), nil)))
		if err != nil {
			t.Fatal(err)
		}
		if k := preview.KeptData; k == nil || k.Name != "AdGuard Home" || k.KeyID != "" {
			t.Fatalf("the sheet should ask, saying the earlier one wasn't signed: %+v", k)
		}
		_, err = e.agent.InstallPlugin(api.PluginInstallRequest{
			StagingID: preview.StagingID, Settings: map[string]string{"password": "s3cret-pass"}, UseKeptData: chosen,
		})
		if err != nil {
			t.Fatal(err)
		}
		e.runBackground()
		_, statErr := os.Stat(dataFile)
		if back := statErr == nil; back != chosen {
			t.Errorf("chosen %v: the data came back %v", chosen, back)
		}
	}
}

// Data kept by RasBeacon 0.1 has no note of whose it was: the sheet asks,
// without a name or key, and it stays aside unless chosen.
func TestPlugins_KeptDataWithoutAnOwnerStaysAside(t *testing.T) {
	e := newPluginEnv(t)
	mustWrite(t, filepath.Join(e.paths.PluginKeptDir(), "adguard-home.img"), "old data")
	preview, err := e.agent.StagePluginFile(bytes.NewReader(writeTestPlugin(t, testPluginManifest("1.0"), nil)))
	if err != nil || preview.KeptData == nil || *preview.KeptData != (api.PluginKeptData{}) {
		t.Fatalf("preview = %+v, %v", preview.KeptData, err)
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage)); err == nil {
		t.Error("data of unknown origin must not be given to a new install unasked")
	}
}

func TestPlugins_CrashingFiveTimesStopsIt(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	// Its container isn't running ("podman ps" lists none).
	for i := 1; i < maxCrashes; i++ {
		e.agent.checkPluginContainers()
		if s := e.plugin("adguard-home").State; s != api.PluginRunning {
			t.Fatalf("after crash %d it should be started again, state %s", i, s)
		}
	}
	e.agent.checkPluginContainers()
	p := e.plugin("adguard-home")
	if p.State != api.PluginCrashed {
		t.Fatalf("after %d crashes: %s", maxCrashes, p.State)
	}
	runs := e.runner.count("podman run")
	e.agent.checkPluginContainers()
	if e.runner.count("podman run") != runs {
		t.Error("a crashed plugin must not be started again by itself")
	}
	// It stays stopped across a restart, until the owner starts it.
	e.agent.mu.Lock()
	e.agent.loadPlugins()
	e.agent.mu.Unlock()
	if e.plugin("adguard-home").State != api.PluginCrashed {
		t.Error("crashed state should survive a restart")
	}
	if err := e.agent.StartPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if e.plugin("adguard-home").State != api.PluginRunning {
		t.Error("starting it by hand should work")
	}
}

func TestPlugins_StoppedStaysStoppedAndRunningStartsAtBoot(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if e.plugin("adguard-home").State != api.PluginStopped || !e.runner.ran("podman rm --force --ignore --time 10 rasbeacon-adguard-home") {
		t.Fatal("stop")
	}
	var cfg config.Config
	config.ReadJSON(e.paths.ConfigFile(), &cfg)
	if cfg.Plugins.Installed["adguard-home"].Running {
		t.Error("stopped is remembered")
	}
	runs := e.runner.count("podman run")
	e.agent.startPlugins()
	if e.runner.count("podman run") != runs {
		t.Error("a stopped plugin must not start at boot")
	}
	if err := e.agent.StartPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	e.agent.startPlugins()
	if e.runner.count("podman run") != runs+2 {
		t.Error("a running plugin starts again at boot")
	}
}

func TestPlugins_SettingsChangeRestartsIt(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	runs := e.runner.count("podman run")
	p, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home",
		Ports: map[string]int{"tcp/3000": 3001}, Settings: map[string]string{"upstream": "cloudflare"}})
	if err != nil {
		t.Fatal(err)
	}
	if e.runner.count("podman run") != runs+1 || !strings.Contains(e.runEnvs[len(e.runEnvs)-1], "UPSTREAM=cloudflare\n") {
		t.Error("changing settings restarts it with them")
	}
	if p.Ports[0].Host != 3001 || !strings.Contains(e.readFile(e.paths.FirewallRules()), "tcp dport 3001 dnat ip to 10.89.0.2:3000") {
		t.Error("the new port should be in the firewall")
	}
	// The empty password kept the saved one.
	if !strings.Contains(e.readFile(e.paths.SecretsFile()), "s3cret-pass") {
		t.Error("an empty password keeps the saved one")
	}
	if _, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", Settings: map[string]string{"upstream": "evil"}}); err == nil {
		t.Error("a value that isn't an option must be refused")
	}
}

func TestPlugins_WaitsForTheVPNBeforeDownloading(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	e.runner.failing = []string{"mountpoint"}
	e.runBackground()
	// A plugin the catalog offers, so a refusal below can only come from
	// the VPN check.
	e.agent.mu.Lock()
	e.agent.plugins.catalog.loaded = true
	e.agent.plugins.catalog.Entries = []catalogRecord{{
		Entry:       plugin.Entry{Source: plugin.BuiltInSource, File: "x.rba", ID: "adguard-home", Name: "AdGuard Home", Version: "1.0"},
		DownloadURL: "https://github.com/" + plugin.BuiltInSource + "/releases/download/v1/x.rba",
	}}
	e.agent.mu.Unlock()
	// The tunnel isn't up yet (the kill switch keeps it closed): catalog
	// downloads fail at once, saying why.
	_, err := e.agent.StagePluginFromCatalog(api.PluginDownloadRequest{Source: plugin.BuiltInSource, File: "x.rba"})
	if err == nil || !strings.Contains(err.Error(), "kill switch") {
		t.Fatalf("got %v, want the download refused until the VPN is up", err)
	}
	if err := e.agent.waitForPluginPath(""); err == nil || !strings.Contains(err.Error(), "kill switch") {
		t.Errorf("got %v", err)
	}
}

func TestPlugins_LogLines(t *testing.T) {
	text := "2026-10-14T10:00:00.000000000+00:00 stdout F started\n2026-10-14T12:00:01.5+02:00 stderr F oops\nraw line\n"
	lines := lastLogLines(text, 2)
	// Times are sent in UTC, for the page to show in the viewer's time.
	want := []api.PluginLogLine{{Time: "2026-10-14T10:00:01Z", Text: "oops"}, {Text: "raw line"}}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %+v, want %+v", lines, want)
	}
}

func TestPlugins_PowerSocketShutsDown(t *testing.T) {
	e := newPluginEnv(t)
	m := testPluginManifest("1.0")
	m["permissions"] = []string{"power-button", "i2c", "gpio", "temperature"}
	mustWrite(t, filepath.Join(e.paths.DevDir(), "i2c-1"), "")
	mustWrite(t, filepath.Join(e.paths.DevDir(), "gpiochip0"), "")
	if _, err := e.installTestPlugin(writeTestPlugin(t, m, nil), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--device " + filepath.Join(e.paths.DevDir(), "i2c-1"), "--device " + filepath.Join(e.paths.DevDir(), "gpiochip0")} {
		if !e.runner.ran(want) {
			t.Errorf("missing %q", want)
		}
	}
	if len(e.runEnvs) == 0 {
		t.Fatal("the plugin never ran")
	}
	if !strings.Contains(e.runEnvs[len(e.runEnvs)-1], "RASBEACON_SOCKET=/run/rasbeacon/rbd.sock\n") {
		t.Errorf("the plugin should be told where the socket is: %q", e.runEnvs)
	}
	if e.runner.ran("mask=/sys/class/thermal") && strings.Contains(e.runner.commands[len(e.runner.commands)-1], "mask=") {
		t.Error("with the temperature permission the sensors aren't hidden")
	}
	socket := filepath.Join(e.agent.pluginSocketDir("adguard-home"), "rbd.sock")
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("the power socket should exist: %v", err)
	}
	defer e.agent.closePowerSocket("adguard-home")

	// The plugin asks over the socket, as an HTTP request.
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}
	ask := func(body string) int {
		t.Helper()
		resp, err := client.Post("http://rbd/power", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, bad := range []string{`{"action":"explode"}`, `not json`} {
		if code := ask(bad); code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", bad, code)
		}
	}
	e.runner.commands = nil
	if code := ask(`{"action":"shutdown"}`); code != http.StatusNoContent {
		t.Fatalf("shutdown: got %d, want 204", code)
	}
	// It waits a second (a timer), so the plugin gets the answer first.
	e.timers.runAll()
	if !e.runner.ran("systemctl poweroff") {
		t.Errorf("the plugin's request should shut RasBeacon down; commands: %v", e.runner.commands)
	}
}

func TestPlugins_TemplatesAreNeverWrittenThroughLinks(t *testing.T) {
	e := newPluginEnv(t)
	m := testPluginManifest("1.0")
	m["templates"] = []map[string]any{
		{"file": "AdGuardHome.yaml", "path": "/opt/adguardhome/conf/AdGuardHome.yaml", "once": true},
		{"file": "app.yaml", "path": "/opt/adguardhome/conf/sub/app.yaml"},
		{"file": "top.yaml", "path": "/opt/adguardhome/conf/top.yaml"},
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, m, nil), nil); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(e.agent.pluginDataMount("adguard-home"), "conf")
	if !strings.Contains(e.readFile(filepath.Join(conf, "sub", "app.yaml")), "upstream: quad9") {
		t.Fatal("the template should be written at first")
	}

	// The plugin, while it ran, put links where RasBeacon writes: its
	// folder "sub" now leads outside, and so does top.yaml (and the old
	// fixed temporary name next to it).
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "victim"), "untouched")
	os.RemoveAll(filepath.Join(conf, "sub"))
	os.Remove(filepath.Join(conf, "top.yaml"))
	for link, target := range map[string]string{
		"sub":          outside,
		"top.yaml":     filepath.Join(outside, "victim"),
		"top.yaml.tmp": filepath.Join(outside, "victim"),
	} {
		if err := os.Symlink(target, filepath.Join(conf, link)); err != nil {
			t.Fatal(err)
		}
	}
	err := e.agent.StartPlugin("adguard-home")
	if err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("a linked folder must stop the start, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "app.yaml")); err == nil {
		t.Error("RasBeacon wrote through the plugin's link")
	}
	if e.readFile(filepath.Join(outside, "victim")) != "untouched" {
		t.Error("a file outside the volume was changed")
	}

	// Without the folder link, the file link is replaced, not written through.
	os.Remove(filepath.Join(conf, "sub"))
	if err := e.agent.StartPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if e.readFile(filepath.Join(outside, "victim")) != "untouched" {
		t.Error("a file outside the volume was changed")
	}
	if info, err := os.Lstat(filepath.Join(conf, "top.yaml")); err != nil || !info.Mode().IsRegular() {
		t.Error("top.yaml should now be a plain file")
	}
}

func TestPlugins_FailedUpdatePutsTheOldVersionBack(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), map[string]int{"tcp/3000": 3000}); err != nil {
		t.Fatal(err)
	}
	e.runner.failing = append(e.runner.failing, "podman load")
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("2.0"), nil), map[string]int{"tcp/3000": 3001}); err != nil {
		t.Fatal(err)
	}
	p := e.plugin("adguard-home")
	if p.Version != "1.0" || p.Ports[0].Host != 3000 || p.State != api.PluginRunning {
		t.Errorf("the old version should be back, running as before: %+v", p)
	}
	if !strings.Contains(p.Message, "Updating to 2.0 failed") {
		t.Errorf("message = %q", p.Message)
	}
	if !strings.Contains(e.readFile(e.paths.SecretsFile()), "s3cret-pass") {
		t.Error("its password should be kept")
	}
}

func TestPlugins_GivingUpAfterCrashesLetsGoOfTheContainer(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	for range maxCrashes {
		e.agent.checkPluginContainers()
	}
	if e.plugin("adguard-home").State != api.PluginCrashed {
		t.Fatal("it should have been given up on")
	}
	last := e.runner.commands[len(e.runner.commands)-1]
	if !e.runner.ran("podman rm --force --ignore --time 10 rasbeacon-adguard-home") || strings.HasPrefix(last, "podman run") {
		t.Error("the dead container should be removed, not started again")
	}
	if e.agent.plugins.live["adguard-home"].busy {
		t.Error("it must not stay busy")
	}
}

func TestPlugins_CheckURLsOnlyReachThePlugin(t *testing.T) {
	values := plugin.Values{Address: "10.89.0.2", Settings: map[string]string{"path": "stats"}}
	for text, ok := range map[string]bool{
		"http://{address}:3000/{setting:path}": true,
		"http://{address}/":                    true,
		"http://{address}@192.168.1.1/":        false,
		"http://{address}.example.com/":        false,
		"https://{address}/":                   false,
		"http://192.168.1.1/":                  false,
	} {
		if _, err := pluginURL(text, values); (err == nil) != ok {
			t.Errorf("%s: err = %v", text, err)
		}
	}
}

func TestPlugins_PasswordsKeepTheirSpaces(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home",
		Settings: map[string]string{"password": " spaced pass "}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.readFile(e.paths.SecretsFile()), `" spaced pass "`) {
		t.Error("a password is kept exactly as typed")
	}
	_, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", Drive: "../../etc"})
	if err == nil {
		t.Error("a plugin without a drive must refuse one")
	}
}

func TestPlugins_LongLogLinesAreJoined(t *testing.T) {
	text := "2026-10-14T10:00:00Z stdout P part one, \n2026-10-14T10:00:00Z stdout F part two\n"
	lines := lastLogLines(text, 10)
	if len(lines) != 1 || lines[0].Text != "part one, part two" {
		t.Errorf("lines = %+v", lines)
	}
}

func TestPlugins_DownloadProxyTunnelsHTTPS(t *testing.T) {
	// An HTTPS server stands for a registry; the proxy's dial goes to it
	// whatever the client asked for.
	registry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("image layers"))
	}))
	defer registry.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectTunnel(w, r, func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", registry.Listener.Addr().String())
		})
	}))
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // the test server's own certificate
	}}
	resp, err := client.Get("https://registry.example/v2/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "image layers" {
		t.Errorf("through the proxy: %q", body)
	}
}

func TestPlugins_UnreadableFilesMarkItBroken(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(e.paths.PluginDir("adguard-home"), plugin.ManifestFile))
	e.agent.mu.Lock()
	e.agent.loadPlugins()
	e.agent.mu.Unlock()
	p := e.plugin("adguard-home")
	if !p.Broken || p.State != api.PluginFailed || p.Name != "adguard-home" {
		t.Errorf("a plugin whose files can't be read: %+v", p)
	}
	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home"}); err != nil {
		t.Errorf("it can still be removed: %v", err)
	}
}

func TestPlugins_TooNewForThisRasBeaconIsRefused(t *testing.T) {
	e := newPluginEnv(t)
	old := version.Version
	version.Version = "v0.1.0"
	defer func() { version.Version = old }()
	m := testPluginManifest("1.0")
	m["minRasBeacon"] = "0.2.0"
	_, err := e.agent.StagePluginFile(bytes.NewReader(writeTestPlugin(t, m, nil)))
	if err == nil || !strings.Contains(err.Error(), "needs RasBeacon 0.2.0 or newer") {
		t.Errorf("got %v", err)
	}
	m["minRasBeacon"] = "0.1.0"
	if _, err := e.agent.StagePluginFile(bytes.NewReader(writeTestPlugin(t, m, nil))); err != nil {
		t.Errorf("a plugin for this version: %v", err)
	}
}

func TestPlugins_TheBuiltInSourceCanBeRemovedAndAddedBack(t *testing.T) {
	e := newClaimedEnv(t, false)
	state, err := e.agent.SetPluginSources(api.PluginSourcesRequest{Sources: []string{"someone/more-plugins"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Sources) != 1 || state.Sources[0].Source != "someone/more-plugins" || state.Sources[0].BuiltIn {
		t.Errorf("without the built-in source listed, it is removed: %+v", state.Sources)
	}
	state, err = e.agent.SetPluginSources(api.PluginSourcesRequest{Sources: []string{"someone/more-plugins", "AAZAM476/rasbeacon-plugins"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Sources) != 2 || !state.Sources[0].BuiltIn {
		t.Errorf("typing its name brings it back: %+v", state.Sources)
	}
}

// A plugin's password never reaches the diagnostics report, even when an
// error message carries it.
func TestDiagnostics_HidesPluginPasswords(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	e.agent.setPluginState("adguard-home", api.PluginFailed, "It couldn't start: bad password s3cret-pass")
	e.agent.mu.Unlock()
	if report := e.agent.Diagnostics(); strings.Contains(report, "s3cret-pass") {
		t.Errorf("the plugin's password is in the report:\n%s", report)
	}
}

// Plugins start with RasBeacon, and a change to the hotspot or the guest
// network leaves them running: restarting them would cut AdGuard Home's DNS
// or a Samba transfer for nothing.
func TestPlugins_HotspotAndGuestChangesLeaveThemRunning(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	runs := e.runner.count("podman run")

	settings := e.agent.HotspotSettings()
	settings.SSID = "New Name"
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.UpdateGuestSettings(api.GuestSettings{Enabled: false, SSID: "Guests"}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := e.runner.count("podman run"); got != runs {
		t.Errorf("the plugin was started again %d times", got-runs)
	}
}

// When the hotspot network moves (the upstream network uses 10.87.0.0/24),
// the plugins' ports follow RasBeacon to its new address.
func TestPlugins_PortsFollowTheHotspotNetworkWhenItMoves(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	e.fakeUplink("wlan0", "10.87.0.50", "24", false)
	e.check()
	rules := e.readFile(e.paths.FirewallRules())
	if !strings.Contains(rules, "ip daddr 10.88.0.1 tcp dport") || strings.Contains(rules, "ip daddr 10.87.0.1 tcp dport") {
		t.Errorf("the plugin's port should be forwarded from 10.88.0.1:\n%s", rules)
	}
}
