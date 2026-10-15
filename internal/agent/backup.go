package agent

// Backing up and restoring the settings (see internal/backup).
//
// A backup holds the settings (config.json, secrets.json, SSH keys, the
// HTTPS certificate, Enterprise certificates), and, if asked, the plugins:
// their files, images and data. It never holds the device's identity, so a
// backup can move the settings to another Raspberry Pi.
//
// Restoring checks the whole file first, then replaces the settings and
// restarts RasBeacon. A backup without plugins keeps the plugins installed
// now; one with plugins replaces them.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"rasbeacon/internal/api"
	"rasbeacon/internal/backup"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
	"rasbeacon/internal/version"
)

// backupDirs are the folders in DataDir a backup holds, besides config.json
// and secrets.json.
var backupDirs = []string{"ssh", "tls", "certs"}

// restoreUploadFile is where an uploaded backup waits for its password.
func (a *Agent) restoreUploadFile() string {
	return filepath.Join(a.paths.DataDir(), "restore-upload.rbbackup")
}

// Backup writes an encrypted backup to w. The admin password is asked again
// because the backup holds every password and key.
func (a *Agent) Backup(w io.Writer, req api.BackupRequest) error {
	if !a.VerifyAdminPassword(req.AdminPassword) {
		return ErrWrongPassword
	}
	if len(req.Password) < backup.MinPasswordLength {
		return &config.ValidationError{Field: "backupPassword", Message: fmt.Sprintf("Use at least %d characters.", backup.MinPasswordLength)}
	}
	a.mu.Lock()
	if a.mode != api.ModeNormal {
		a.mu.Unlock()
		return ErrWrongMode
	}
	cfg, secrets := a.cfg.Clone(), a.secrets.Clone()
	var plugins []string
	images := map[string]string{}
	running := map[string]bool{}
	if req.IncludePlugins {
		for _, id := range a.pluginIDs() {
			live := a.plugins.live[id]
			if live != nil && live.busy {
				a.mu.Unlock()
				return errPluginBusy
			}
			plugins = append(plugins, id)
			images[id] = cfg.Plugins.Installed[id].Image
			running[id] = live != nil && live.state == api.PluginRunning
		}
	} else {
		cfg.Plugins.Installed = nil
		secrets.Plugins = nil
	}
	board := a.pluginBoard()
	a.mu.Unlock()

	bw, err := backup.Create(w, req.Password, backup.Info{Created: a.now().UTC(), Version: version.Version, Board: board, Plugins: req.IncludePlugins})
	if err != nil {
		return err
	}
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	secretsJSON, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	if err := bw.AddBytes("config.json", configJSON, 0o644); err != nil {
		return err
	}
	if err := bw.AddBytes("secrets.json", secretsJSON, 0o600); err != nil {
		return err
	}
	for _, dir := range backupDirs {
		if err := bw.AddDir(dir, filepath.Join(a.paths.DataDir(), dir), nil); err != nil {
			return err
		}
	}
	for _, id := range plugins {
		if err := a.backupPlugin(bw, id, images[id], running[id]); err != nil {
			return fmt.Errorf("backing up plugin %s: %w", id, err)
		}
	}
	slog.Info("settings backed up", "plugins", len(plugins))
	return bw.Close()
}

// backupPlugin adds one plugin: its files, its image (`podman save`) and
// its data. A running plugin is paused while its data is copied, so the
// copy is whole.
func (a *Agent) backupPlugin(bw *backup.Writer, id, image string, running bool) error {
	dir := a.paths.PluginDir(id)
	if err := bw.AddDir("plugins/"+id, dir, func(relative string) bool { return relative == pluginDataImage }); err != nil {
		return err
	}
	if image != "" {
		tarFile := filepath.Join(a.paths.PluginsDir(), "backup-"+id+".tar")
		defer os.Remove(tarFile)
		if _, err := a.plugins.runner.Run("podman", "save", "--quiet", "--output", tarFile, image); err != nil {
			return fmt.Errorf("saving its image: %w", err)
		}
		if err := bw.AddFile("plugin-images/"+id+".tar", tarFile); err != nil {
			return err
		}
	}
	if running {
		if _, err := a.plugins.runner.Run("podman", "pause", plugin.ContainerName(id)); err == nil {
			defer a.plugins.runner.Run("podman", "unpause", plugin.ContainerName(id))
		}
		_, _ = a.plugins.runner.Run("sync")
	}
	return bw.AddFile("plugins/"+id+"/"+pluginDataImage, filepath.Join(dir, pluginDataImage))
}

// StageRestore keeps an uploaded backup until its password is given.
func (a *Agent) StageRestore(body io.Reader) error {
	a.mu.Lock()
	normal := a.mode == api.ModeNormal
	a.mu.Unlock()
	if !normal {
		return ErrWrongMode
	}
	file := a.restoreUploadFile()
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(file)
		return err
	}
	// A quick look: is it a backup at all?
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	magic := make([]byte, len(backup.Magic))
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != string(backup.Magic) {
		_ = os.Remove(file)
		return &config.ValidationError{Field: "file", Message: "This is not a RasBeacon backup (.rbbackup) file."}
	}
	return nil
}

// Restore opens the uploaded backup with its password, checks it, replaces
// the settings with it and restarts RasBeacon.
func (a *Agent) Restore(req api.RestoreRequest) (api.RestoreResult, error) {
	if !a.VerifyAdminPassword(req.AdminPassword) {
		return api.RestoreResult{}, ErrWrongPassword
	}
	upload := a.restoreUploadFile()
	f, err := os.Open(upload)
	if err != nil {
		return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: "Choose the backup file again."}
	}
	defer f.Close()
	staging := filepath.Join(a.paths.DataDir(), "restore-staging")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return api.RestoreResult{}, err
	}
	defer os.RemoveAll(staging)

	info, err := backup.Extract(f, req.Password, staging)
	switch {
	case errors.Is(err, backup.ErrWrongPassword):
		return api.RestoreResult{}, &config.ValidationError{Field: "backupPassword", Message: "The password is wrong, or the backup file is damaged."}
	case errors.Is(err, backup.ErrNotBackup):
		return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: "This is not a RasBeacon backup (.rbbackup) file."}
	case err != nil:
		return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: sentence(err.Error())}
	}

	// The settings must be ones this version understands.
	var cfg config.Config
	if err := config.ReadJSON(filepath.Join(staging, "config.json"), &cfg); err != nil {
		return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: "The backup's settings can't be read."}
	}
	if cfg.Version > config.CurrentVersion {
		return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: "This backup was made by a newer RasBeacon. Update RasBeacon first."}
	}
	var secrets config.Secrets
	if err := config.ReadJSON(filepath.Join(staging, "secrets.json"), &secrets); err != nil || secrets.AdminPasswordHash == "" || !cfg.Claimed {
		return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: "The backup's settings are incomplete."}
	}
	for id := range cfg.Plugins.Installed {
		if !info.Plugins {
			break
		}
		if _, err := os.Stat(filepath.Join(staging, "plugins", id, plugin.ManifestFile)); err != nil {
			return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: fmt.Sprintf("The backup's plugin %s is incomplete.", id)}
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return api.RestoreResult{}, ErrWrongMode
	}
	busy := slices.ContainsFunc(slices.Collect(maps.Values(a.plugins.live)), func(l *pluginLive) bool { return l.busy })
	if busy {
		return api.RestoreResult{}, errPluginBusy
	}
	if !info.Plugins {
		// The plugins installed now stay.
		cfg.Plugins.Installed = a.cfg.Plugins.Installed
		secrets.Plugins = a.secrets.Plugins
	} else if err := a.replacePlugins(staging, cfg); err != nil {
		return api.RestoreResult{}, fmt.Errorf("restoring the plugins: %w", err)
	}

	for _, dir := range backupDirs {
		target := filepath.Join(a.paths.DataDir(), dir)
		if err := os.RemoveAll(target); err != nil {
			return api.RestoreResult{}, err
		}
		if _, err := os.Stat(filepath.Join(staging, dir)); err == nil {
			if err := os.Rename(filepath.Join(staging, dir), target); err != nil {
				return api.RestoreResult{}, err
			}
		}
	}
	// Secrets before settings: a crash between them leaves the old
	// settings, which still start.
	if err := config.WriteJSON(a.paths.SecretsFile(), secrets, 0o600); err != nil {
		return api.RestoreResult{}, err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), cfg, 0o644); err != nil {
		return api.RestoreResult{}, err
	}
	_ = os.Remove(upload)
	slog.Warn("settings restored from a backup; restarting", "made", info.Created, "version", info.Version, "plugins", info.Plugins)
	a.afterFunc(switchDelay, func() { _, _ = a.runner.Run("systemctl", "reboot") })
	return api.RestoreResult{Version: info.Version, Created: info.Created.Format("2006-01-02T15:04:05Z07:00"), Plugins: info.Plugins}, nil
}

// replacePlugins swaps the installed plugins for the backup's: their
// files, data and images. The caller holds a.mu.
func (a *Agent) replacePlugins(staging string, cfg config.Config) error {
	a.stopAllPlugins()
	for id, p := range a.cfg.Plugins.Installed {
		if p.Image != "" {
			_, _ = a.plugins.runner.Run("podman", "rmi", "--ignore", "--force", p.Image)
		}
		if err := os.RemoveAll(a.paths.PluginDir(id)); err != nil {
			return err
		}
	}
	for id := range cfg.Plugins.Installed {
		if err := os.MkdirAll(filepath.Dir(a.paths.PluginDir(id)), 0o700); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(staging, "plugins", id), a.paths.PluginDir(id)); err != nil {
			return err
		}
		image := filepath.Join(staging, "plugin-images", id+".tar")
		if _, err := os.Stat(image); err == nil {
			if _, err := a.plugins.runner.Run("podman", "load", "--quiet", "--input", image); err != nil {
				return fmt.Errorf("loading %s's image: %w", id, err)
			}
		}
	}
	return nil
}

// backupFileName is the download's name: rasbeacon-backup-2026-10-15.rbbackup.
func (a *Agent) backupFileName() string {
	return "rasbeacon-backup-" + a.now().Format("2006-01-02") + ".rbbackup"
}
