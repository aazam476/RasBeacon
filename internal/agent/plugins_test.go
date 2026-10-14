package agent

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
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

// writeTestPlugin makes a .rba (signed with key unless nil) and returns its bytes.
func writeTestPlugin(t *testing.T, manifest map[string]any, key ed25519.PrivateKey) []byte {
	t.Helper()
	dir := t.TempDir()
	unsigned := filepath.Join(dir, "p.rba")
	f, err := os.Create(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	m, _ := json.Marshal(manifest)
	files := map[string]string{
		plugin.ManifestFile: string(m), plugin.IconFile: "\x89PNG\r\n\x1a\nicon", plugin.ImageFile: "image tar",
		"templates/AdGuardHome.yaml": "password: {{bcrypt .Settings.password}}\nupstream: {{.Settings.upstream}}\n",
	}
	for _, name := range []string{plugin.ManifestFile, plugin.IconFile, plugin.ImageFile, "templates/AdGuardHome.yaml"} {
		fw, _ := w.Create(name)
		fw.Write([]byte(files[name]))
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
		"fallocate --length 256M", "mkfs.ext4", "mount -o loop,nosuid,nodev",
		"podman run --detach --replace --name rasbeacon-adguard-home",
		"--network rasbeacon-plugins --ip 10.89.0.2 --dns 10.89.0.1",
		"--security-opt mask=/sys/class/thermal", "--env UPSTREAM=quad9",
	} {
		if !e.runner.ran(want) {
			t.Errorf("never ran %q", want)
		}
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

	// Remove with its data kept, then a different key may install it again.
	mustWrite(t, filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage), "data") // fallocate is fake here
	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home", KeepData: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.paths.PluginKeptDir(), "adguard-home.img")); err != nil {
		t.Errorf("the data should be kept: %v", err)
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("2.0"), otherKey), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage)); err != nil {
		t.Error("the kept data should come back")
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
	if e.runner.count("podman run") != runs+1 || !e.runner.ran("--env UPSTREAM=cloudflare") {
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
	// The tunnel isn't up yet (the kill switch keeps it closed): catalog
	// downloads fail at once.
	_, err := e.agent.StagePluginFromCatalog(api.PluginDownloadRequest{Source: plugin.BuiltInSource, File: "x.rba"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if err := e.agent.waitForPluginPath(""); err == nil || !strings.Contains(err.Error(), "kill switch") {
		t.Errorf("got %v", err)
	}
}

func TestPlugins_LogLines(t *testing.T) {
	text := "2026-10-14T10:00:00.000000000+00:00 stdout F started\n2026-10-14T10:00:01.5+00:00 stderr F oops\nraw line\n"
	lines := lastLogLines(text, 2)
	if len(lines) != 2 || !strings.HasSuffix(lines[0], " oops") || lines[1] != "raw line" {
		t.Errorf("lines = %q", lines)
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
	for _, want := range []string{"--device " + filepath.Join(e.paths.DevDir(), "i2c-1"), "--device " + filepath.Join(e.paths.DevDir(), "gpiochip0"), "RASBEACON_SOCKET=/run/rasbeacon/rbd.sock"} {
		if !e.runner.ran(want) {
			t.Errorf("missing %q", want)
		}
	}
	if e.runner.ran("mask=/sys/class/thermal") && strings.Contains(e.runner.commands[len(e.runner.commands)-1], "mask=") {
		t.Error("with the temperature permission the sensors aren't hidden")
	}
	socket := filepath.Join(e.agent.pluginSocketDir("adguard-home"), "rbd.sock")
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("the power socket should exist: %v", err)
	}
	e.agent.closePowerSocket("adguard-home")
}
