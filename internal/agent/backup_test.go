package agent

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// backUp asks for a backup and downloads it, as the browser does.
func (e *testEnv) backUp(req api.BackupRequest) ([]byte, error) {
	ticket, err := e.agent.PrepareBackup(req)
	if err != nil {
		return nil, err
	}
	var file bytes.Buffer
	err = e.agent.WriteBackup(&file, ticket.Token)
	return file.Bytes(), err
}

func TestBackup_RestoreBringsSettingsAndPluginsBack(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage), "plugin data")
	mustWrite(t, e.paths.SSHAuthorizedKeysFile(), "ssh-ed25519 AAAA owner@laptop\n")

	// The admin password is asked again, and the backup's must be long enough.
	if _, err := e.backUp(api.BackupRequest{AdminPassword: "wrong", Password: "backup-pass-1"}); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong admin password: %v", err)
	}
	if _, err := e.backUp(api.BackupRequest{AdminPassword: validClaim.AdminPassword, Password: "short"}); err == nil {
		t.Error("a short backup password must be refused")
	}
	file, err := e.backUp(api.BackupRequest{AdminPassword: validClaim.AdminPassword, Password: "backup-pass-1", IncludePlugins: true})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(file, []byte("s3cret-pass")) || bytes.Contains(file, []byte(validClaim.HotspotPassword)) {
		t.Fatal("the backup must be encrypted")
	}
	if !e.runner.ran("podman save --quiet --output") || !e.runner.ran("podman pause rasbeacon-adguard-home") ||
		!e.runner.ran("podman unpause rasbeacon-adguard-home") {
		t.Error("the plugin's image is saved, and it is paused while its data is copied")
	}
	if _, err := os.Stat(filepath.Join(e.paths.PluginsDir(), "backup-adguard-home.img")); err == nil {
		t.Error("the copy of its data should be gone")
	}

	// Things change after the backup: a new hotspot name, the plugin removed.
	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home"}); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	e.agent.cfg.Hotspot.SSID = "Changed"
	config.WriteJSON(e.paths.ConfigFile(), e.agent.cfg, 0o644)
	e.agent.mu.Unlock()
	os.Remove(e.paths.SSHAuthorizedKeysFile())

	if err := e.agent.StageRestore(bytes.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	_, err = e.agent.Restore(api.RestoreRequest{AdminPassword: validClaim.AdminPassword, Password: "not-the-password"})
	var verr *config.ValidationError
	if !errors.As(err, &verr) || verr.Field != "backupPassword" {
		t.Fatalf("wrong backup password: %v", err)
	}
	result, err := e.agent.Restore(api.RestoreRequest{AdminPassword: validClaim.AdminPassword, Password: "backup-pass-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Plugins {
		t.Error("the result should say plugins were restored")
	}
	var cfg config.Config
	config.ReadJSON(e.paths.ConfigFile(), &cfg)
	if cfg.Hotspot.SSID != validClaim.HotspotSSID {
		t.Errorf("hotspot name = %q, want the backup's", cfg.Hotspot.SSID)
	}
	if _, ok := cfg.Plugins.Installed["adguard-home"]; !ok {
		t.Error("the plugin should be back in the settings")
	}
	if e.readFile(filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage)) != "plugin data" {
		t.Error("the plugin's data should be back")
	}
	if !strings.Contains(e.readFile(e.paths.SSHAuthorizedKeysFile()), "owner@laptop") {
		t.Error("the SSH keys should be back")
	}
	if !strings.Contains(e.readFile(e.paths.SecretsFile()), "s3cret-pass") {
		t.Error("the plugin's password should be back")
	}
	e.timers.runAll()
	if !e.runner.ran("systemctl reboot") {
		t.Error("RasBeacon restarts after restoring")
	}
}

func TestBackup_WithoutPluginsKeepsTheInstalledOnes(t *testing.T) {
	e := newPluginEnv(t)
	file, err := e.backUp(api.BackupRequest{AdminPassword: validClaim.AdminPassword, Password: "backup-pass-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.StageRestore(bytes.NewReader(file)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.agent.Restore(api.RestoreRequest{AdminPassword: validClaim.AdminPassword, Password: "backup-pass-1"}); err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	config.ReadJSON(e.paths.ConfigFile(), &cfg)
	if _, ok := cfg.Plugins.Installed["adguard-home"]; !ok {
		t.Error("a backup without plugins must keep the installed plugins")
	}
}

func TestBackup_UploadThatIsNotABackupIsRefused(t *testing.T) {
	e := newPluginEnv(t)
	err := e.agent.StageRestore(strings.NewReader("PK zip file"))
	var verr *config.ValidationError
	if !errors.As(err, &verr) || verr.Field != "file" {
		t.Errorf("got %v", err)
	}
}

func TestBackup_TicketsWorkOnceAndExpire(t *testing.T) {
	e := newPluginEnv(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	e.agent.now = func() time.Time { return now }
	ticket, err := e.agent.PrepareBackup(api.BackupRequest{AdminPassword: validClaim.AdminPassword, Password: "backup-pass-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ticket.FileName, "rasbeacon-backup-") || len(ticket.Token) != 32 {
		t.Errorf("ticket = %+v", ticket)
	}
	if err := e.agent.WriteBackup(io.Discard, ticket.Token); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.WriteBackup(io.Discard, ticket.Token); err == nil {
		t.Error("a ticket works once")
	}
	ticket, _ = e.agent.PrepareBackup(api.BackupRequest{AdminPassword: validClaim.AdminPassword, Password: "backup-pass-1"})
	now = now.Add(backupTicketLifetime + time.Second)
	if err := e.agent.WriteBackup(io.Discard, ticket.Token); err == nil {
		t.Error("an old ticket must not work")
	}
}

func TestBackup_RestoreRefusesUnsafePluginSettings(t *testing.T) {
	staging := t.TempDir()
	mustWrite(t, filepath.Join(staging, "plugins", "good", "manifest.json"), "{}")
	good := config.InstalledPlugin{Address: netip.MustParseAddr("10.89.0.2"), Ports: map[string]int{"tcp/3000": 3000}}
	cases := map[string]map[string]config.InstalledPlugin{
		"path in the name":     {"../../etc": good},
		"address elsewhere":    {"good": {Address: netip.MustParseAddr("192.168.1.1")}},
		"drive as a path":      {"good": {Address: good.Address, Drive: "../../boot"}},
		"RasBeacon's own port": {"good": {Address: good.Address, Ports: map[string]int{"tcp/3000": 443}}},
		"missing files":        {"other": good},
	}
	for name, installed := range cases {
		cfg := config.Config{Plugins: config.PluginsConfig{Installed: installed}}
		if err := checkRestoredPlugins(cfg, staging); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	cfg := config.Config{Plugins: config.PluginsConfig{Installed: map[string]config.InstalledPlugin{"good": good}}}
	if err := checkRestoredPlugins(cfg, staging); err != nil {
		t.Errorf("a sound plugin was refused: %v", err)
	}
}

func TestBackup_CopySparseKeepsTheData(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "data.img"), filepath.Join(dir, "copy.img")
	data := make([]byte, 300<<10)
	copy(data[100<<10:], "the middle")
	copy(data[len(data)-5:], "end!!")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copySparse(src, dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, data) {
		t.Error("the copy differs")
	}
}
