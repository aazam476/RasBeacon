package agent

// Backing up and restoring the settings (see internal/backup).
//
// A backup holds the settings (config.json, secrets.json, SSH keys, the
// HTTPS certificate, Enterprise certificates) and the plugins' settings and
// files. The owner chooses whether it also holds the plugins' data, and
// their images. It never holds the device's identity, so a backup can move
// the settings to another Raspberry Pi.
//
// Making one takes two requests. The first (PrepareBackup) checks the
// request and answers with a ticket; the second (WriteBackup) is a plain
// download link the browser saves as the file arrives, instead of holding
// a large backup in memory first. Being logged in is enough: whoever is
// typed the admin password already.
//
// Restoring checks the whole file first, then replaces the settings and the
// plugins and restarts RasBeacon. A plugin whose image isn't in the backup
// downloads it again when it starts (fetchPluginImage).

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/backup"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
	"rasbeacon/internal/render"
	"rasbeacon/internal/version"
)

// backupDirs are the folders in DataDir a backup holds, besides config.json
// and secrets.json.
var backupDirs = []string{"ssh", "tls", "certs"}

// backupTicketLifetime is how long a backup ticket can be used: the
// browser starts the download right after asking.
const backupTicketLifetime = 2 * time.Minute

// backupTicket is a backup asked for (passwords checked) and not yet
// downloaded.
type backupTicket struct {
	req     api.BackupRequest
	expires time.Time
}

// restoreUploadFile is where an uploaded backup waits for its password.
func (a *Agent) restoreUploadFile() string {
	return filepath.Join(a.paths.DataDir(), "restore-upload.rbbackup")
}

// PrepareBackup checks a backup request and returns the ticket to download
// it with (POST /system/backup).
func (a *Agent) PrepareBackup(req api.BackupRequest) (api.BackupTicket, error) {
	if req.Plugins == "" {
		req.Plugins = backup.PluginSettings
	}
	switch req.Plugins {
	case backup.PluginSettings, backup.PluginData, backup.PluginEverything:
	default:
		return api.BackupTicket{}, &config.ValidationError{Field: "plugins", Message: `Must be "settings", "data" or "everything".`}
	}
	if len(req.Password) < backup.MinPasswordLength {
		return api.BackupTicket{}, &config.ValidationError{Field: "backupPassword", Message: fmt.Sprintf("Use at least %d characters.", backup.MinPasswordLength)}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return api.BackupTicket{}, ErrWrongMode
	}
	if a.anyPluginBusy() {
		return api.BackupTicket{}, errPluginBusy
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return api.BackupTicket{}, err
	}
	token := hex.EncodeToString(random)
	// Forget tickets nobody used.
	for t, ticket := range a.backupTickets {
		if a.now().After(ticket.expires) {
			delete(a.backupTickets, t)
		}
	}
	if a.backupTickets == nil {
		a.backupTickets = map[string]backupTicket{}
	}
	a.backupTickets[token] = backupTicket{req: req, expires: a.now().Add(backupTicketLifetime)}
	return api.BackupTicket{Token: token, FileName: a.backupFileName()}, nil
}

// WriteBackup makes the backup a ticket is for and writes it to w, as it is
// made (GET /system/backup/{token}). A ticket works once.
func (a *Agent) WriteBackup(w io.Writer, token string) error {
	a.mu.Lock()
	ticket, ok := a.backupTickets[token]
	delete(a.backupTickets, token)
	a.mu.Unlock()
	if !ok || a.now().After(ticket.expires) {
		return &config.ValidationError{Field: "token", Message: "This backup link has expired. Make the backup again."}
	}
	return a.writeBackup(w, ticket.req)
}

// anyPluginBusy reports whether a slow action runs on any plugin. The
// caller holds a.mu.
func (a *Agent) anyPluginBusy() bool {
	return slices.ContainsFunc(slices.Collect(maps.Values(a.plugins.live)), func(l *pluginLive) bool { return l.busy })
}

// writeBackup writes the encrypted backup to w.
func (a *Agent) writeBackup(w io.Writer, req api.BackupRequest) error {
	a.mu.Lock()
	if a.mode != api.ModeNormal {
		a.mu.Unlock()
		return ErrWrongMode
	}
	if a.anyPluginBusy() {
		a.mu.Unlock()
		return errPluginBusy
	}
	// Copies, so the settings can't change halfway through the backup.
	cfg, secrets := a.cfg.Clone(), a.secrets.Clone()
	var plugins []pluginInBackup
	for _, id := range a.pluginIDs() {
		live := a.plugins.live[id]
		p := pluginInBackup{
			id:       id,
			image:    cfg.Plugins.Installed[id].Image,
			running:  live != nil && live.state == api.PluginRunning,
			withData: req.Plugins != backup.PluginSettings,
		}
		if req.Plugins != backup.PluginEverything {
			p.fetch = a.pluginFetch(id)
		}
		plugins = append(plugins, p)
	}
	board := a.pluginBoard()
	a.mu.Unlock()

	// Large when it holds data or images: then compressed a little less.
	big := req.Plugins != backup.PluginSettings ||
		slices.ContainsFunc(plugins, func(p pluginInBackup) bool { return p.fetch == nil && p.image != "" })
	info := backup.Info{Created: a.now().UTC(), Version: version.Version, Board: board, Plugins: len(plugins) > 0, PluginContent: req.Plugins}
	bw, err := backup.Create(w, req.Password, info, big)
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
	for _, p := range plugins {
		if err := a.backupPlugin(bw, p); err != nil {
			return fmt.Errorf("backing up plugin %s: %w", p.id, err)
		}
	}
	slog.Info("settings backed up", "plugins", len(plugins), "content", req.Plugins)
	return bw.Close()
}

// pluginInBackup is what a backup takes of one plugin.
type pluginInBackup struct {
	id, image string
	running   bool
	withData  bool
	// fetch is where its image can be downloaded again; nil puts the
	// image in the backup.
	fetch *config.PluginFetch
}

// pluginFetch says where a plugin's image can be downloaded again: its
// published image, or its .rba file in the catalog if the catalog still has
// this version. nil when it can't be (installed from a file, or updated in
// the catalog since): its image then goes into the backup. The caller
// holds a.mu.
func (a *Agent) pluginFetch(id string) *config.PluginFetch {
	p := a.cfg.Plugins.Installed[id]
	if live := a.plugins.live[id]; live != nil && live.manifest != nil && live.manifest.Image != nil {
		return &config.PluginFetch{Ref: pinnedRef(*live.manifest.Image)}
	}
	if p.Source == "file" || p.PublicKey == "" {
		return nil
	}
	if entry, ok := a.plugins.catalog.find(p.Source, id); ok && entry.Version == p.Version && strings.HasPrefix(entry.DownloadURL, "https://") {
		return &config.PluginFetch{URL: entry.DownloadURL}
	}
	return nil
}

// pinnedRef is a published image's name pinned by its digest
// (ghcr.io/x/y@sha256:...); the digest pins it, not the tag.
func pinnedRef(image plugin.RegistryImage) string {
	ref := image.Ref
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref + "@" + image.Digest
}

// backupPlugin adds one plugin: its files, its image (`podman save`) or
// where to download it again, and its data if asked.
func (a *Agent) backupPlugin(bw *backup.Writer, p pluginInBackup) error {
	id, image, running := p.id, p.image, p.running
	dir := a.paths.PluginDir(id)
	// Its folder without the data file, which is added last (below).
	if err := bw.AddDir("plugins/"+id, dir, func(relative string) bool { return relative == pluginDataImage }); err != nil {
		return err
	}
	if p.fetch != nil {
		data, _ := json.Marshal(p.fetch)
		if err := bw.AddBytes("plugin-fetch/"+id+".json", data, 0o600); err != nil {
			return err
		}
	} else if image != "" {
		tarFile := filepath.Join(a.paths.PluginsDir(), "backup-"+id+".tar")
		defer os.Remove(tarFile)
		if _, err := a.plugins.runner.Run("podman", "save", "--quiet", "--output", tarFile, image); err != nil {
			return fmt.Errorf("saving its image: %w", err)
		}
		if !fileExists(tarFile) {
			return errors.New("saving its image: podman wrote no file")
		}
		if err := bw.AddFile("plugin-images/"+id+".tar", tarFile); err != nil {
			return err
		}
	}
	if !p.withData {
		return nil
	}
	data := filepath.Join(dir, pluginDataImage)
	if !running {
		return bw.AddFile("plugins/"+id+"/"+pluginDataImage, data)
	}
	// A running plugin could change its data while it is copied, leaving a
	// copy that is half old, half new. So it is paused (frozen, not
	// stopped) while its data file is copied on the SD card, which takes
	// seconds; the copy then goes into the backup at the network's pace.
	if _, err := a.plugins.runner.Run("podman", "pause", plugin.ContainerName(id)); err == nil {
		defer a.unpausePlugin(id) // in case the copy below fails
	}
	_, _ = a.plugins.runner.Run("sync")
	snapshot := filepath.Join(a.paths.PluginsDir(), "backup-"+id+".img")
	defer os.Remove(snapshot)
	if err := copySparse(data, snapshot); err != nil {
		// No room for a copy: send the data itself, paused all along.
		slog.Warn("could not copy the plugin's data; it stays paused during the backup", "plugin", id, "error", err)
		return bw.AddFile("plugins/"+id+"/"+pluginDataImage, data)
	}
	a.unpausePlugin(id)
	return bw.AddFile("plugins/"+id+"/"+pluginDataImage, snapshot)
}

// unpausePlugin lets a paused plugin run again (nothing if it isn't paused).
func (a *Agent) unpausePlugin(id string) {
	_, _ = a.plugins.runner.Run("podman", "unpause", plugin.ContainerName(id))
}

// copySparse copies a file, leaving out the parts that are only zeros
// (they read back as zeros anyway). A plugin's data file is mostly empty
// space, so the copy takes only the room its data uses.
func copySparse(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	block := make([]byte, 64<<10)
	zeros := make([]byte, len(block))
	var copyErr error
	for {
		n, err := io.ReadFull(in, block)
		if n > 0 {
			if bytes.Equal(block[:n], zeros[:n]) {
				// Skip ahead: the file gets a "hole" here.
				_, copyErr = out.Seek(int64(n), io.SeekCurrent)
			} else {
				_, copyErr = out.Write(block[:n])
			}
		}
		if copyErr != nil || err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			copyErr = err
			break
		}
	}
	// A hole at the very end needs the size set, or the copy is shorter.
	if copyErr == nil {
		copyErr = out.Truncate(info.Size())
	}
	if err := out.Close(); copyErr == nil {
		copyErr = err
	}
	return copyErr
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
// the settings and the plugins with the backup's and restarts RasBeacon.
func (a *Agent) Restore(req api.RestoreRequest) (api.RestoreResult, error) {
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
	if newer, ok := version.Compare(info.Version, version.Version); ok && newer > 0 {
		return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: fmt.Sprintf(
			"This backup was made by RasBeacon %s, which is newer than this one (%s). Update RasBeacon first.",
			strings.TrimPrefix(info.Version, "v"), strings.TrimPrefix(version.Version, "v"))}
	}
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
	// Backups made before RasBeacon 0.2 without plugins say nothing about
	// them: the installed ones stay. Every other backup brings its plugins
	// (perhaps none) in place of the installed ones.
	keepPlugins := !info.Plugins && info.PluginContent == ""
	if !keepPlugins {
		if err := checkRestoredPlugins(cfg, staging); err != nil {
			return api.RestoreResult{}, &config.ValidationError{Field: "file", Message: sentence(err.Error())}
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return api.RestoreResult{}, ErrWrongMode
	}
	if a.anyPluginBusy() {
		return api.RestoreResult{}, errPluginBusy
	}
	if keepPlugins {
		cfg.Plugins.Installed = a.cfg.Plugins.Installed
		secrets.Plugins = a.secrets.Plugins
	} else if err := a.replacePlugins(staging, &cfg); err != nil {
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
	// "Stay logged in" ends everywhere: the backup's admin password may
	// not be the one those browsers typed.
	if err := a.forgetAllLogins(); err != nil {
		slog.Warn("could not forget the remembered logins", "error", err)
	}
	slog.Warn("settings restored from a backup; restarting", "made", info.Created, "version", info.Version, "plugins", len(cfg.Plugins.Installed))
	a.afterFunc(switchDelay, func() { _, _ = a.runner.Run("systemctl", "reboot") })
	return api.RestoreResult{Version: info.Version, Created: info.Created.Format(time.RFC3339), Plugins: !keepPlugins}, nil
}

// checkRestoredPlugins checks a backup's plugins before they replace the
// installed ones. Their IDs become folder names and their drives mount
// points, their addresses and ports go into the firewall: a damaged or
// hand-made backup must not get that far.
func checkRestoredPlugins(cfg config.Config, staging string) error {
	gateway := netip.MustParseAddr(render.PluginGateway)
	addresses := map[netip.Addr]string{}
	ports := map[string]string{}
	for _, id := range slices.Sorted(maps.Keys(cfg.Plugins.Installed)) {
		p := cfg.Plugins.Installed[id]
		if !plugin.ValidID(id) {
			return fmt.Errorf("the backup has a plugin with an invalid name (%q)", id)
		}
		if _, err := os.Stat(filepath.Join(staging, "plugins", id, plugin.ManifestFile)); err != nil {
			return fmt.Errorf("the backup's plugin %s is incomplete", id)
		}
		// Its image keeps RasBeacon's name for this plugin, never another's.
		if p.Image != "" && p.Image != plugin.LocalImage(id, p.Version) {
			return fmt.Errorf("the backup's plugin %s has an invalid image name", id)
		}
		if p.Image != "" && !fileExists(filepath.Join(staging, "plugin-images", id+".tar")) {
			if _, err := readPluginFetch(staging, id); err != nil {
				return fmt.Errorf("the backup's plugin %s has no image and no place to download it", id)
			}
		}
		if !render.PluginSubnet.Contains(p.Address) || p.Address == gateway || addresses[p.Address] != "" {
			return fmt.Errorf("the backup's plugin %s has an invalid address", id)
		}
		addresses[p.Address] = id
		if p.Drive != "" && !plugin.ValidDriveUUID(p.Drive) {
			return fmt.Errorf("the backup's plugin %s has an invalid USB drive", id)
		}
		for key, host := range p.Ports {
			protocol, container, _ := strings.Cut(key, "/")
			n, err := strconv.Atoi(container)
			hostKey := protocol + "/" + strconv.Itoa(host)
			if (protocol != "tcp" && protocol != "udp") || err != nil || n < 1 || n > 65535 ||
				host < 1 || host > 65535 || plugin.Reserved(host) || ports[hostKey] != "" {
				return fmt.Errorf("the backup's plugin %s has an invalid port", id)
			}
			ports[hostKey] = id
		}
	}
	return nil
}

// readPluginFetch reads where a backup's plugin image can be downloaded
// again (plugin-fetch/<id>.json).
func readPluginFetch(staging, id string) (config.PluginFetch, error) {
	var fetch config.PluginFetch
	if err := config.ReadJSON(filepath.Join(staging, "plugin-fetch", id+".json"), &fetch); err != nil {
		return fetch, err
	}
	if (fetch.URL == "") == (fetch.Ref == "") || (fetch.URL != "" && !strings.HasPrefix(fetch.URL, "https://")) ||
		strings.ContainsAny(fetch.Ref, " \n") || (fetch.Ref != "" && !strings.Contains(fetch.Ref, "@sha256:")) {
		return fetch, errors.New("invalid download address")
	}
	return fetch, nil
}

// replacePlugins swaps the installed plugins for the backup's: their
// files, data and images. The backup's images are loaded first, so a
// problem with them leaves the installed plugins as they are; plugins
// whose image isn't in the backup get where to download it (cfg is
// changed). The caller holds a.mu.
func (a *Agent) replacePlugins(staging string, cfg *config.Config) error {
	newImages := map[string]bool{}
	for id, p := range cfg.Plugins.Installed {
		p.Fetch = nil
		image := filepath.Join(staging, "plugin-images", id+".tar")
		if _, err := os.Stat(image); err == nil {
			if err := a.loadPluginImage(image, p.Image); err != nil {
				return fmt.Errorf("loading %s's image: %w", id, err)
			}
			newImages[p.Image] = true
		} else if p.Image != "" {
			fetch, err := readPluginFetch(staging, id)
			if err != nil {
				return fmt.Errorf("%s's image isn't in the backup", id)
			}
			p.Fetch = &fetch
		}
		cfg.Plugins.Installed[id] = p
		// Kept if already here (the same version is installed now): then
		// it needn't be downloaded again.
		newImages[p.Image] = true
	}

	a.stopAllPlugins()
	for id, p := range a.cfg.Plugins.Installed {
		if p.Image != "" && !newImages[p.Image] {
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
	}
	return nil
}

// backupFileName is the download's name: rasbeacon-backup-2026-10-15.rbbackup.
func (a *Agent) backupFileName() string {
	return "rasbeacon-backup-" + a.now().Format("2006-01-02") + ".rbbackup"
}
