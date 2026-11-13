package agent

// This file downloads a plugin's app (its container image) again when it
// isn't in Podman's storage: after a restore from a backup that didn't hold
// it, or when it went missing otherwise. The download is checked to be the
// same plugin, version and author.

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

// errPluginImageMissing means a plugin's image isn't in Podman's storage
// and RasBeacon has nowhere to download it from (it was installed from a
// file, or the catalog no longer has this version).
var errPluginImageMissing = errors.New("its app is missing from RasBeacon's storage")

// fetchPluginImage makes sure a plugin's image is in Podman's storage
// before it starts, downloading it again when it isn't: from where a
// restore said (InstalledPlugin.Fetch), or else from where a backup would
// (pluginFetch). Podman is never left to look for it in a registry. Called
// without a.mu, from runPlugin.
func (a *Agent) fetchPluginImage(id string) error {
	a.mu.Lock()
	p := a.cfg.Plugins.Installed[id]
	restored := p.Fetch != nil
	if p.Fetch == nil {
		p.Fetch = a.pluginFetch(id)
	}
	a.mu.Unlock()
	if p.Image == "" {
		return nil // not installed yet
	}
	// Still here (from before a restore, or as usual)?
	if _, err := a.plugins.runner.Run("podman", "image", "exists", p.Image); err != nil {
		if p.Fetch == nil {
			slog.Error("plugin's image is missing and can't be downloaded again", "plugin", id, "image", p.Image)
			return errPluginImageMissing
		}
		slog.Warn("plugin's image is missing; downloading it again", "plugin", id, "image", p.Image)
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
		slog.Info("plugin's image is back", "plugin", id)
	}
	if !restored {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
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
	return a.loadPluginImage(tarFile, p.Image)
}

// loadPluginImage loads an image file (`docker save` or `podman save`) and
// names it image, RasBeacon's name for the plugin's image. The names
// written in the file are checked first (see plugin.CheckImageNames), and
// removed again afterwards, so only RasBeacon's name is left.
func (a *Agent) loadPluginImage(tarFile, image string) error {
	names, err := plugin.ImageNames(tarFile)
	if err != nil {
		return fmt.Errorf("its image can't be loaded: %w", err)
	}
	if err := plugin.CheckImageNames(names, image); err != nil {
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
	if _, err := a.plugins.runner.Run("podman", "tag", loaded, image); err != nil {
		return err
	}
	for _, name := range names {
		if name != image {
			// `podman untag IMAGE NAME` takes NAME off IMAGE. With the
			// name alone (`podman untag NAME`), Podman takes every name
			// off the image, RasBeacon's too, and the plugin could no
			// longer start.
			_, _ = a.plugins.runner.Run("podman", "untag", name, name)
		}
	}
	return nil
}
