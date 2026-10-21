package agent

// Previewing and installing (or updating) a plugin. See plugins.go.
//
// A plugin file (uploaded, or downloaded from the catalog) is first kept in
// the staging folder and checked; the install sheet shows what it asks for
// (PluginPreview). Installing then runs in the background: it waits for the
// VPN if the kill switch blocks, gets the image (from the file, or from its
// registry through the VPN), saves the plugin's files and starts it. An
// update that fails puts the old version back as it was.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
	"rasbeacon/internal/tunnel"
	"rasbeacon/internal/version"
)

// stagedPlugin is a plugin file waiting in the install sheet.
type stagedPlugin struct {
	file    string
	archive *plugin.Archive
	// source is "file" or the catalog's "owner/repo".
	source  string
	created time.Time
	// downloadSize is what installing still downloads (-1: unknown).
	downloadSize int64
}

// stagingLifetime is how long a file waits in the install sheet.
const stagingLifetime = time.Hour

// vpnWaitEvery is how often an install waiting for the VPN looks again.
const vpnWaitEvery = 10 * time.Second

// StagePluginFile keeps an uploaded .rba and answers what installing it
// means (POST /plugins/upload).
func (a *Agent) StagePluginFile(body io.Reader) (api.PluginPreview, error) {
	file, err := a.newStagingFile()
	if err != nil {
		return api.PluginPreview{}, err
	}
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return api.PluginPreview{}, err
	}
	// Copy at most one byte more than allowed: that byte tells the file is
	// too large, without reading all of it.
	n, err := io.Copy(out, io.LimitReader(body, plugin.MaxArchiveSize+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > plugin.MaxArchiveSize {
		err = errors.New("the plugin file is larger than 2 GB")
	}
	if err != nil {
		_ = os.Remove(file)
		return api.PluginPreview{}, &config.ValidationError{Field: "file", Message: sentence(err.Error())}
	}
	return a.stage(file, "file")
}

// StagePluginFromCatalog downloads a catalog plugin (through the VPN) and
// answers what installing it means.
func (a *Agent) StagePluginFromCatalog(req api.PluginDownloadRequest) (api.PluginPreview, error) {
	a.mu.Lock()
	entry, ok := a.plugins.catalog.entry(req.Source, req.File)
	a.mu.Unlock()
	if !ok {
		return api.PluginPreview{}, &config.ValidationError{Field: "file", Message: "This plugin isn't in the catalog any more. Check the catalog again."}
	}
	if err := a.waitForPluginPath(""); err != nil {
		return api.PluginPreview{}, err
	}
	file, err := a.newStagingFile()
	if err != nil {
		return api.PluginPreview{}, err
	}
	if err := plugin.Download(a.plugins.client(), entry.DownloadURL, file); err != nil {
		_ = os.Remove(file)
		return api.PluginPreview{}, fmt.Errorf("downloading %s: %w", entry.Name, err)
	}
	return a.stage(file, req.Source)
}

// UpdatePluginFromCatalog downloads the newer version of an installed
// plugin for the install sheet (signed plugins only).
func (a *Agent) UpdatePluginFromCatalog(id string) (api.PluginPreview, error) {
	a.mu.Lock()
	p, installed := a.cfg.Plugins.Installed[id]
	entry, found := a.plugins.catalog.find(p.Source, id)
	a.mu.Unlock()
	switch {
	case !installed:
		return api.PluginPreview{}, errNoPlugin
	case p.PublicKey == "":
		return api.PluginPreview{}, &config.ValidationError{Field: "id", Message: "This plugin isn't signed, so its updates are installed from a file."}
	case !found:
		return api.PluginPreview{}, &config.ValidationError{Field: "id", Message: "No update for this plugin is in the catalog."}
	}
	return a.StagePluginFromCatalog(api.PluginDownloadRequest{Source: entry.Source, File: entry.File})
}

// CancelPluginInstall forgets a staged file (the sheet was closed).
func (a *Agent) CancelPluginInstall(stagingID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.plugins.staged[stagingID]; s != nil {
		_ = os.Remove(s.file)
		delete(a.plugins.staged, stagingID)
	}
}

// newStagingFile makes a name for a file in the staging folder, and
// forgets files left there for too long.
func (a *Agent) newStagingFile() (string, error) {
	dir := a.paths.PluginStagingDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	a.mu.Lock()
	for id, s := range a.plugins.staged {
		if a.now().Sub(s.created) > stagingLifetime {
			_ = os.Remove(s.file)
			delete(a.plugins.staged, id)
		}
	}
	a.mu.Unlock()
	// A random name, which is also the staging ID the browser sends back:
	// nobody can guess another sheet's file.
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return filepath.Join(dir, hex.EncodeToString(random)+".rba"), nil
}

// stage checks a plugin file and makes its preview.
func (a *Agent) stage(file, source string) (api.PluginPreview, error) {
	archive, err := plugin.Open(file)
	if err != nil {
		_ = os.Remove(file)
		return api.PluginPreview{}, &config.ValidationError{Field: "file", Message: sentence(err.Error())}
	}
	s := &stagedPlugin{file: file, archive: archive, source: source, created: a.now(), downloadSize: 0}
	if archive.Manifest.Image != nil {
		// Asked without the lock: the registry may take a few seconds.
		if size, err := plugin.DownloadSize(a.plugins.client(), *archive.Manifest.Image); err == nil {
			s.downloadSize = size
		} else {
			slog.Warn("could not ask the image's size", "error", err)
			s.downloadSize = -1
		}
	}
	stagingID := strings.TrimSuffix(filepath.Base(file), ".rba")

	a.mu.Lock()
	defer a.mu.Unlock()
	preview, err := a.preview(s)
	if err != nil {
		_ = os.Remove(file)
		return api.PluginPreview{}, err
	}
	if a.plugins.staged == nil {
		a.plugins.staged = map[string]*stagedPlugin{}
	}
	a.plugins.staged[stagingID] = s
	preview.StagingID = stagingID
	return preview, nil
}

// sentence starts a message with a capital and ends it with a full stop.
func sentence(text string) string {
	if text == "" {
		return text
	}
	text = strings.ToUpper(text[:1]) + text[1:]
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	return text
}

// preview checks whether a staged plugin can be installed here and
// describes it. The caller holds a.mu.
func (a *Agent) preview(s *stagedPlugin) (api.PluginPreview, error) {
	m := s.archive.Manifest
	refuse := func(message string) (api.PluginPreview, error) {
		return api.PluginPreview{}, &config.ValidationError{Field: "file", Message: message}
	}
	board := a.pluginBoard()
	if !m.RunsOn(board) {
		return refuse(fmt.Sprintf("%s doesn't run on this Raspberry Pi.", m.Name))
	}
	if !plugin.SupportedBy(m.MinRasBeacon, version.Version) {
		return refuse(fmt.Sprintf("%s needs RasBeacon %s or newer. Update RasBeacon first (System → Software).", m.Name, m.MinRasBeacon))
	}
	memory, swap := a.memoryMB()
	if m.MinMemoryMB > 0 && memory > 0 && memory < m.MinMemoryMB {
		return refuse(fmt.Sprintf("%s needs a Raspberry Pi with at least %d MB of memory.", m.Name, m.MinMemoryMB))
	}
	if m.MinSwapMB > 0 && swap < m.MinSwapMB {
		return refuse(fmt.Sprintf("%s needs at least %d MB of swap. Make the swap file larger in System → Swap file, then try again.", m.Name, m.MinSwapMB))
	}

	// An update must come from the same author: once a version was signed,
	// every later one must be signed with the same key (like Android apps).
	installed, update := a.cfg.Plugins.Installed[m.ID]
	var key string
	if s.archive.PublicKey != nil {
		key = base64.StdEncoding.EncodeToString(s.archive.PublicKey)
	}
	if update {
		switch {
		case installed.PublicKey != "" && key == "":
			return refuse(fmt.Sprintf("This version of %s isn't signed, but the installed one is. It may not come from the same author.", m.Name))
		case installed.PublicKey != "" && key != installed.PublicKey:
			return refuse(fmt.Sprintf("This version of %s is signed by a different key than the installed one. If you trust it, remove %s (you can keep its data) and install this one.", m.Name, m.Name))
		case installed.PublicKey == "" && s.source != "file":
			return refuse(fmt.Sprintf("The installed %s isn't signed, so its updates are installed from a file.", m.Name))
		}
		if live := a.plugins.live[m.ID]; live != nil && live.busy {
			return api.PluginPreview{}, errPluginBusy
		}
	} else if len(a.cfg.Plugins.Installed) >= maxPlugins {
		return refuse(fmt.Sprintf("At most %d plugins can be installed.", maxPlugins))
	}

	preview := api.PluginPreview{
		ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description, Author: m.Author, Homepage: m.Homepage,
		Icon:   "data:image/png;base64," + base64.StdEncoding.EncodeToString(s.archive.Icon),
		Source: s.source, Signed: key != "", Update: update,
		Permissions: permissionViews(m.Permissions), UsesDrive: m.USBDrive != nil,
		DriveOptional: m.USBDrive != nil && m.USBDrive.Optional,
		DownloadSize:  s.downloadSize, Metered: a.cfg.Hotspot.Metered,
		MemoryMB: m.Memory(), CPUs: m.CPULimit(), StorageMB: m.Storage(),
	}
	if key != "" {
		preview.KeyID = plugin.KeyID(s.archive.PublicKey)
	}
	ports := a.suggestPorts(m.ID, m, installed.Ports)
	preview.Ports = a.portViews(m, nil)
	for i := range preview.Ports {
		preview.Ports[i].Host = ports[preview.Ports[i].Key]
	}
	preview.Settings = a.settingViews(m, installed.Settings, a.secrets.Plugins[m.ID])
	if update {
		preview.FromVersion = installed.Version
		preview.Drive = installed.Drive
		if live := a.plugins.live[m.ID]; live != nil && live.manifest != nil {
			for _, p := range m.Permissions {
				if !live.manifest.Has(p) {
					preview.NewPermissions = append(preview.NewPermissions, p)
				}
			}
		}
	}
	if board == "zero2w" || board == "pi3" || (memory > 0 && memory < 1024) {
		preview.Warnings = append(preview.Warnings, fmt.Sprintf(
			"This Pi has little memory (%d MB). %s may use up to %d MB, which can slow RasBeacon down.", memory, m.Name, m.Memory()))
	}
	// Room on the SD card: its data, the image (unpacked, about twice the
	// download), and 512 MB to spare for everything else.
	if free, err := a.diskFree(a.paths.DataDir()); err == nil {
		need := int64(m.Storage())<<20 + max(s.downloadSize, 0)*2 + s.archive.ImageSize()
		if free < need+(512<<20) {
			return refuse(fmt.Sprintf("The SD card doesn't have room for %s (it needs about %d MB).", m.Name, (need>>20)+512))
		}
	}
	return preview, nil
}

// previousVersion is the installed version an update replaces, kept to put
// back if the update fails.
type previousVersion struct {
	manifest *plugin.Manifest
	icon     string
	readme   string
	record   config.InstalledPlugin
	secrets  map[string]string
}

// InstallPlugin installs a previewed plugin with the owner's choices. It
// answers at once; the plugin's state shows the progress.
func (a *Agent) InstallPlugin(req api.PluginInstallRequest) (api.Plugin, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return api.Plugin{}, ErrWrongMode
	}
	s := a.plugins.staged[req.StagingID]
	if s == nil {
		return api.Plugin{}, &config.ValidationError{Field: "stagingId", Message: "This plugin file is gone. Choose it again."}
	}
	preview, err := a.preview(s) // things may have changed since
	if err != nil {
		return api.Plugin{}, err
	}
	m := s.archive.Manifest
	ports, err := a.checkPorts(m.ID, m, req.Ports)
	if err != nil {
		return api.Plugin{}, err
	}
	values, secrets, err := a.checkSettings(m.ID, m, req.Settings)
	if err != nil {
		return api.Plugin{}, err
	}
	if err := a.checkDrive(m.ID, m, req.Drive); err != nil {
		return api.Plugin{}, err
	}

	old, update := a.cfg.Plugins.Installed[m.ID]
	live := a.plugins.live[m.ID]
	if live == nil {
		live = &pluginLive{}
		a.plugins.live[m.ID] = live
	}
	var previous *previousVersion // nil for a new install
	if update {
		previous = &previousVersion{
			manifest: live.manifest, icon: live.icon, readme: live.readme,
			record: old.Clone(), secrets: maps.Clone(a.secrets.Plugins[m.ID]),
		}
	}

	// The new record keeps the old version's address and image until the
	// new image is in place (installPluginSteps).
	record := config.InstalledPlugin{
		Version: m.Version, Source: s.source, PublicKey: old.PublicKey, Image: old.Image, Address: old.Address,
		Ports: ports, Settings: values, Drive: req.Drive, Running: true,
	}
	if preview.Signed {
		record.PublicKey = base64.StdEncoding.EncodeToString(s.archive.PublicKey)
	}
	if !update {
		if record.Address, err = a.nextPluginAddress(); err != nil {
			return api.Plugin{}, err
		}
	}
	if err := a.savePluginSecrets(m.ID, secrets); err != nil {
		return api.Plugin{}, err
	}
	newCfg := a.cfg.Clone()
	if newCfg.Plugins.Installed == nil {
		newCfg.Plugins.Installed = map[string]config.InstalledPlugin{}
	}
	newCfg.Plugins.Installed[m.ID] = record
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return api.Plugin{}, err
	}
	a.cfg = newCfg
	delete(a.plugins.staged, req.StagingID)

	// Until it is installed, its page shows the new version's details.
	live.manifest, live.busy, live.crashes = m, true, 0
	live.icon = preview.Icon
	live.readme = s.archive.Readme
	a.setPluginState(m.ID, api.PluginInstalling, "Getting ready…")
	slog.Info("installing plugin", "plugin", m.ID, "version", m.Version, "source", s.source, "update", update)
	a.background(func() { a.installPlugin(m.ID, s, previous) })
	return a.pluginView(m.ID), nil
}

// installPlugin does the slow part of installing, in the background.
// previous is the version an update replaces (nil for a new install).
func (a *Agent) installPlugin(id string, s *stagedPlugin, previous *previousVersion) {
	defer os.Remove(s.file)
	oldImage := ""
	if previous != nil {
		oldImage = previous.record.Image
	}
	image, err := a.installPluginSteps(id, s, oldImage)
	if err != nil {
		a.installFailed(id, s.archive.Manifest, image, previous, err)
		return
	}
	a.mu.Lock()
	if err := a.applyFirewall(); err != nil {
		slog.Error("could not apply the plugin's firewall rules", "error", err)
	}
	a.mu.Unlock()

	_ = a.runPlugin(id)
	a.endPluginWork(id)
}

// installFailed cleans up after a failed install. A new plugin stays listed
// as failed (its page says why; the owner removes it). An update puts the
// old version back as it was: its settings, its passwords, and running if
// it ran.
func (a *Agent) installFailed(id string, m *plugin.Manifest, image string, previous *previousVersion, cause error) {
	slog.Error("plugin install failed", "plugin", id, "error", cause)
	// The new version's image, if it got that far, isn't used.
	if image != "" && (previous == nil || image != previous.record.Image) {
		_, _ = a.plugins.runner.Run("podman", "rmi", "--ignore", image)
	}
	a.mu.Lock()
	live := a.plugins.live[id]
	if previous == nil || live == nil {
		_ = a.savePluginConfig(id, func(p *config.InstalledPlugin) { p.Running = false })
		a.setPluginState(id, api.PluginFailed, "Installing failed: "+cause.Error())
		if live != nil {
			live.busy = false
		}
		a.mu.Unlock()
		return
	}
	live.manifest, live.icon, live.readme = previous.manifest, previous.icon, previous.readme
	_ = a.savePluginConfig(id, func(p *config.InstalledPlugin) { *p = previous.record })
	_ = a.savePluginSecrets(id, previous.secrets)
	a.setPluginState(id, api.PluginStopped, "")
	a.mu.Unlock()

	if previous.record.Running {
		_ = a.runPlugin(id)
	}
	a.mu.Lock()
	if live := a.plugins.live[id]; live != nil {
		live.message = fmt.Sprintf("Updating to %s failed: %s", m.Version, cause.Error())
	}
	a.mu.Unlock()
	a.endPluginWork(id)
}

// installPluginSteps gets the image, saves the files and the data. It
// returns the image's local name once it is there. Called without a.mu.
func (a *Agent) installPluginSteps(id string, s *stagedPlugin, oldImage string) (string, error) {
	m := s.archive.Manifest
	image := plugin.LocalImage(id, m.Version)
	progress := func(message string) {
		a.mu.Lock()
		a.setPluginState(id, api.PluginInstalling, message)
		a.mu.Unlock()
	}

	if s.archive.HasImage() {
		progress("Unpacking its image…")
		tarFile := s.file + ".tar"
		defer os.Remove(tarFile)
		if err := s.archive.ExtractImage(tarFile); err != nil {
			return "", err
		}
		if err := a.loadPluginImage(tarFile, image); err != nil {
			return "", err
		}
	} else {
		if err := a.waitForPluginPath(id); err != nil {
			return "", err
		}
		progress("Downloading its image…")
		if err := a.pullPluginImage(pinnedRef(*m.Image), image); err != nil {
			return "", err
		}
	}

	progress("Saving its files…")
	if err := a.savePluginFiles(id, s); err != nil {
		return image, err
	}
	a.mu.Lock()
	err := a.savePluginConfig(id, func(p *config.InstalledPlugin) { p.Image = image })
	a.mu.Unlock()
	if err != nil {
		return image, err
	}

	// An update: stop the old version, then remove its image.
	a.stopPluginContainer(id)
	a.unmountPluginData(id)
	if oldImage != "" && oldImage != image {
		_, _ = a.plugins.runner.Run("podman", "rmi", "--ignore", oldImage)
	}
	// Data kept from an earlier install comes back.
	kept := filepath.Join(a.paths.PluginKeptDir(), id+".img")
	data := filepath.Join(a.paths.PluginDir(id), pluginDataImage)
	if _, err := os.Stat(data); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(kept); err == nil {
			if err := os.Rename(kept, data); err == nil {
				slog.Info("plugin's kept data restored", "plugin", id)
			}
		}
	}
	return image, nil
}

// loadedImage reads the image name or ID from `podman load` output
// ("Loaded image: localhost/x:1", or several such lines).
func loadedImage(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if _, name, ok := strings.Cut(line, "Loaded image:"); ok {
			if name = strings.TrimSpace(name); name != "" {
				first, _, _ := strings.Cut(name, ",")
				return strings.TrimSpace(first)
			}
		}
	}
	return ""
}

// savePluginFiles writes the plugin's manifest, icon, readme and
// templates into its folder (replacing an older version's).
func (a *Agent) savePluginFiles(id string, s *stagedPlugin) error {
	dir := a.paths.PluginDir(id)
	templates := filepath.Join(dir, "templates")
	if err := os.RemoveAll(templates); err != nil {
		return err
	}
	if err := os.MkdirAll(templates, 0o700); err != nil {
		return err
	}
	files := map[string][]byte{plugin.ManifestFile: s.archive.ManifestData, plugin.IconFile: s.archive.Icon, plugin.ReadmeFile: []byte(s.archive.Readme)}
	for name, data := range s.archive.Templates {
		files[filepath.Join("templates", name)] = data
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := config.WriteFileAtomic(filepath.Join(dir, name), files[name], 0o600); err != nil {
			return err
		}
	}
	return nil
}

// waitForPluginPath waits while plugin downloads can't go out the way they
// must (through the VPN, when it is in use): the tunnel isn't connected and
// the kill switch blocks, or the tunnel is set aside for a hotel's login
// page (downloads would leave outside the VPN). id names the plugin whose
// state shows the wait ("" = none: then it doesn't wait, it fails).
func (a *Agent) waitForPluginPath(id string) error {
	for {
		a.mu.Lock()
		state := a.vpn.machine.State()
		tunnelDown := state != tunnel.Up && state != tunnel.Direct
		blocked := a.vpnActive() && (a.portal.loginMode || tunnelDown)
		_, stillInstalled := a.cfg.Plugins.Installed[id]
		if blocked && id != "" {
			a.setPluginState(id, api.PluginWaiting, "Waiting for the VPN to connect before downloading.")
		}
		a.mu.Unlock()
		switch {
		case !blocked:
			return nil
		case id == "":
			return errors.New("the VPN isn't connected, and the kill switch keeps RasBeacon from downloading without it. Try again once it connects")
		case !stillInstalled:
			return errors.New("it was removed")
		}
		a.sleep(vpnWaitEvery)
	}
}
