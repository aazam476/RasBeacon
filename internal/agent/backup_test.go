package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

func TestBackup_RestoreBringsSettingsAndPluginsBack(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage), "plugin data")
	mustWrite(t, e.paths.SSHAuthorizedKeysFile(), "ssh-ed25519 AAAA owner@laptop\n")

	// The admin password is asked again, and the backup's must be long enough.
	if err := e.agent.Backup(&bytes.Buffer{}, api.BackupRequest{AdminPassword: "wrong", Password: "backup-pass-1"}); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong admin password: %v", err)
	}
	if err := e.agent.Backup(&bytes.Buffer{}, api.BackupRequest{AdminPassword: validClaim.AdminPassword, Password: "short"}); err == nil {
		t.Error("a short backup password must be refused")
	}
	var file bytes.Buffer
	if err := e.agent.Backup(&file, api.BackupRequest{AdminPassword: validClaim.AdminPassword, Password: "backup-pass-1", IncludePlugins: true}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(file.Bytes(), []byte("s3cret-pass")) || bytes.Contains(file.Bytes(), []byte(validClaim.HotspotPassword)) {
		t.Fatal("the backup must be encrypted")
	}
	if !e.runner.ran("podman save --quiet --output") || !e.runner.ran("podman pause rasbeacon-adguard-home") {
		t.Error("the plugin's image is saved, and it is paused while its data is copied")
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

	if err := e.agent.StageRestore(bytes.NewReader(file.Bytes())); err != nil {
		t.Fatal(err)
	}
	_, err := e.agent.Restore(api.RestoreRequest{AdminPassword: validClaim.AdminPassword, Password: "not-the-password"})
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
	var file bytes.Buffer
	if err := e.agent.Backup(&file, api.BackupRequest{AdminPassword: validClaim.AdminPassword, Password: "backup-pass-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.StageRestore(bytes.NewReader(file.Bytes())); err != nil {
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
