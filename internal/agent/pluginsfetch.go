package agent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

// fetchPluginImage downloads a plugin's image again after a restore from a
// backup that didn't hold it (see InstalledPlugin.Fetch). It does nothing
// for other plugins. Called without a.mu, from runPlugin.
func (a *Agent) fetchPluginImage(id string) error {
	a.mu.Lock()
	p := a.cfg.Plugins.Installed[id]
	a.mu.Unlock()
	if p.Fetch == nil {
		return nil
	}
	// Still here from before the restore?
	if _, err := a.plugins.runner.Run("podman", "image", "exists", p.Image); err != nil {
		a.mu.Lock()
		a.setPluginState(id, api.PluginInstalling, "Downloading its app again…")
		a.mu.Unlock()
		if err := a.waitForPluginPath(id); err != nil {
			return err
		}
		if p.Fetch.Ref != "" {
			err = a.pullPluginImage(p.Fetch.Ref, p.Image)
		} else {
			err = a.downloadPluginImage(id, p)
		}
		if err != nil {
			return err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	slog.Info("plugin's image is back", "plugin", id)
	return a.savePluginConfig(id, func(p *config.InstalledPlugin) { p.Fetch = nil })
}

// pullPluginImage downloads a published image (through the VPN, when it is
// on) and names it image.
func (a *Agent) pullPluginImage(ref, image string) error {
	proxy, err := a.downloadProxy()
	if err != nil {
		return err
	}
	if _, err := a.plugins.runner.Run("env", "HTTPS_PROXY=http://"+proxy, "HTTP_PROXY=http://"+proxy,
		"podman", "pull", "--quiet", "--arch", "arm64", ref); err != nil {
		return fmt.Errorf("its image can't be downloaded: %w", err)
	}
	_, err = a.plugins.runner.Run("podman", "tag", ref, image)
	return err
}

// downloadPluginImage downloads the plugin's .rba file again and loads the
// image in it. The file must be this plugin, this version, signed with its
// pinned key.
func (a *Agent) downloadPluginImage(id string, p config.InstalledPlugin) error {
	file, err := a.newStagingFile()
	if err != nil {
		return err
	}
	defer os.Remove(file)
	if err := plugin.Download(a.plugins.client(), p.Fetch.URL, file); err != nil {
		return fmt.Errorf("its file can't be downloaded: %w", err)
	}
	archive, err := plugin.Open(file)
	if err != nil {
		return err
	}
	m := archive.Manifest
	switch {
	case m.ID != id || m.Version != p.Version:
		return fmt.Errorf("the download is %s %s, not %s %s", m.ID, m.Version, id, p.Version)
	case p.PublicKey == "" || base64.StdEncoding.EncodeToString(archive.PublicKey) != p.PublicKey:
		return errors.New("the download isn't signed by this plugin's author")
	case !archive.HasImage():
		return errors.New("the download holds no image")
	}
	tarFile := file + ".tar"
	defer os.Remove(tarFile)
	if err := archive.ExtractImage(tarFile); err != nil {
		return err
	}
	out, err := a.plugins.runner.Run("podman", "load", "--quiet", "--input", tarFile)
	if err != nil {
		return fmt.Errorf("its image can't be loaded: %w", err)
	}
	loaded := loadedImage(out)
	if loaded == "" {
		return errors.New("its image can't be loaded: podman named no image")
	}
	if _, err := a.plugins.runner.Run("podman", "tag", loaded, p.Image); err != nil {
		return err
	}
	if loaded != p.Image {
		_, _ = a.plugins.runner.Run("podman", "untag", loaded)
	}
	return nil
}
