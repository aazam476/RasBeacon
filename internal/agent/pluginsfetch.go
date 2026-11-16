package agent

// This file downloads a plugin's app (its container image) again when it
// isn't in Podman's storage: after a restore from a backup that didn't hold
// it, or when it went missing otherwise. The download is checked to be the
// same plugin, version and author.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

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
//
// Podman has the last word on which names the file carried, so what it
// did is checked as well: the names it says it gave the image, and
// whether any other plugin's name leads to another image than before. If
// one does (RasBeacon and Podman read the file differently), everything
// is put back and the plugin is refused: no plugin may ever start another
// one's program.
func (a *Agent) loadPluginImage(tarFile, image string) error {
	names, err := plugin.ImageNames(tarFile)
	if err != nil {
		return fmt.Errorf("its image can't be loaded: %w", err)
	}
	if err := plugin.CheckImageNames(names, image); err != nil {
		return err
	}
	before := a.pluginImageIDs()
	out, err := a.plugins.runner.Run("podman", "load", "--quiet", "--input", tarFile)
	if err != nil {
		return fmt.Errorf("its image can't be loaded: %w", err)
	}
	loaded := loadedImages(out)
	if len(loaded) == 0 {
		return errors.New("its image can't be loaded: podman named no image")
	}
	if taken := takenImageNames(before, a.pluginImageIDs(), loaded, image); len(taken) > 0 {
		slog.Error("a plugin's image took another plugin's name; putting it back", "image", image, "names", taken)
		a.undoImageLoad(before, loaded, taken)
		return fmt.Errorf("its image carries another plugin's name, %s", taken[0])
	}
	if _, err := a.plugins.runner.Run("podman", "tag", loaded[0], image); err != nil {
		return err
	}
	for _, name := range slices.Compact(slices.Sorted(slices.Values(append(names, loaded...)))) {
		if name != image && !strings.HasPrefix(name, "sha256:") {
			// `podman untag IMAGE NAME` takes NAME off IMAGE. With the
			// name alone (`podman untag NAME`), Podman takes every name
			// off the image, RasBeacon's too, and the plugin could no
			// longer start.
			_, _ = a.plugins.runner.Run("podman", "untag", name, name)
		}
	}
	return nil
}

// loadedImages reads the names (or, for an image without one, the ID)
// from `podman load` output: a "Loaded image: localhost/x:1" line for
// each, or several on one line with commas between them.
func loadedImages(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		_, list, ok := strings.Cut(line, "Loaded image:")
		if !ok {
			_, list, ok = strings.Cut(line, "Loaded image(s):")
		}
		if !ok {
			continue
		}
		for _, name := range strings.Split(list, ",") {
			if name = strings.TrimSpace(name); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// pluginImageIDs lists what Podman has in the plugins' name space now:
// each name, and the ID of the image it leads to. nil when Podman can't
// be asked.
func (a *Agent) pluginImageIDs() map[string]string {
	out, err := a.plugins.runner.Run("podman", "images", "--format", "json")
	if err != nil {
		return nil
	}
	// The part of `podman images --format json` read here. An image's
	// names are in Names (RepoTags in some versions).
	var images []struct {
		ID       string   `json:"Id"`
		Names    []string `json:"Names"`
		RepoTags []string `json:"RepoTags"`
	}
	if err := json.Unmarshal([]byte(out), &images); err != nil {
		return nil
	}
	ids := map[string]string{}
	for _, image := range images {
		for _, name := range append(image.Names, image.RepoTags...) {
			if plugin.InNameSpace(name) {
				ids[name] = image.ID
			}
		}
	}
	return ids
}

// takenImageNames lists the names in the plugins' name space, other than
// own, that loading an image file gave its image: those Podman says it
// loaded, and those that lead to another image than before (or are new).
// before and after are pluginImageIDs before and after loading (nil when
// Podman couldn't be asked: then only what it says counts).
func takenImageNames(before, after map[string]string, loaded []string, own string) []string {
	var taken []string
	for _, name := range loaded {
		if name != own && plugin.InNameSpace(name) {
			taken = append(taken, name)
		}
	}
	if before != nil && after != nil {
		for _, name := range slices.Sorted(maps.Keys(after)) {
			if old, had := before[name]; name != own && (!had || old != after[name]) && !slices.Contains(taken, name) {
				taken = append(taken, name)
			}
		}
	}
	return taken
}

// undoImageLoad takes back what loading an image file did after it took
// other plugins' names (taken): each of those names goes back to the image
// it led to before, or goes away if it is new, and the loaded image loses
// its other names too, so nothing can start it.
func (a *Agent) undoImageLoad(before map[string]string, loaded, taken []string) {
	r := a.plugins.runner
	for _, name := range taken {
		if old := before[name]; old != "" {
			if _, err := r.Run("podman", "tag", old, name); err != nil {
				slog.Error("could not give a plugin's image its name back", "name", name, "error", err)
			}
			continue
		}
		_, _ = r.Run("podman", "untag", name, name)
	}
	for _, name := range loaded {
		if !slices.Contains(taken, name) && !strings.HasPrefix(name, "sha256:") {
			_, _ = r.Run("podman", "untag", name, name)
		}
	}
}
