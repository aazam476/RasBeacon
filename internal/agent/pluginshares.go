package agent

// Shares: folders the owner makes for a plugin to offer. Samba shares each
// one on the network. A plugin opts in with "shares" in its manifest, and
// starts with none.
//
// Each share has a name (which the owner can change), a place that never
// changes, and a quota:
//
//   - On RasBeacon's internal storage, a share is a file of its own on the
//     SD card holding an ext4 filesystem, the quota's size, set aside in
//     full. Its quota is a hard limit: when it is full, writing fails and
//     deleting still works. The quotas of all shares together can't take
//     the last 2 GB of the SD card (internalHeadroomMB).
//   - On a USB drive, a share is a folder on the drive. Nothing is set
//     aside: what it uses is measured from the files in its folder, also
//     those added on a computer while the drive was out. The plugin
//     reports the quota as the share's size (Samba: what Finder and File
//     Explorer see) and nothing free once it is used up, even when the
//     files are more than the quota ("22 of 20 GB").
//
// A quota can grow up to the space there is (on a drive: its free space,
// less what the other shares on it may still fill) and shrink down to what
// the files use, never below. Removing a share on internal storage deletes
// its files; on a drive, its folder stays unless the owner asks for it to
// be deleted too (or it is encrypted: then it always goes).

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

const (
	// internalHeadroomMB is what the SD card keeps free when a share or a
	// plugin's data is given space: room for settings, logs and updates.
	internalHeadroomMB = 2048
	// minShareQuotaMB is the smallest quota a share may have.
	minShareQuotaMB = 256
	// maxShareNameLength is how long a share's name may be.
	maxShareNameLength = 40
	// shareUsageEvery is how often a share's use is measured again for
	// the page (a big drive takes a while to add up).
	shareUsageEvery = time.Minute
)

// shareUsage is the last measurement of a share's files.
type shareUsage struct {
	mb        int
	measured  time.Time
	measuring bool
}

// shareRun is one share as the plugin's container gets it: where it is on
// RasBeacon, and the key that opens it when it is encrypted.
type shareRun struct {
	share config.PluginShare
	host  string // what is mounted in the container
	key   string // its recovery key, when encrypted
}

// rbquotafs is the program that holds a share on a USB drive to its quota
// (cmd/rbquotafs): nothing more can be added once its files reach it.
const rbquotafs = "rbquotafs"

// --- Where a share is ---

// shareStoreDir is where a share's files are kept: its storage file's
// mount on internal storage, or its folder on the drive. When encrypted,
// the encrypted folder is in there.
func (a *Agent) shareStoreDir(id string, share config.PluginShare) string {
	if share.Drive == "" {
		return a.paths.PluginShareMount(id, share.ID)
	}
	return filepath.Join(a.paths.DriveMountDir(share.Drive), filepath.FromSlash(share.Folder))
}

// shareFilesDir is where the plugin's view of a share's files is: the
// opened view when it is encrypted, else where its files are.
func (a *Agent) shareFilesDir(id string, share config.PluginShare) string {
	if share.Encryption == encryptionOn {
		return a.paths.SharePlainDir(id, share.ID)
	}
	return a.shareStoreDir(id, share)
}

// shareHostDir is what the plugin gets as the share: on a USB drive, its
// files held to the quota (rbquotafs), since nothing sets space aside
// there; on internal storage its files, whose own filesystem is the quota.
func (a *Agent) shareHostDir(id string, share config.PluginShare) string {
	if share.Drive != "" {
		return a.paths.ShareQuotaDir(id, share.ID)
	}
	return a.shareFilesDir(id, share)
}

// shareKeyName is where a share's recovery key is kept in
// Secrets.ShareKeys: by the folder on a drive (so a share made there again
// opens it), by plugin and share on internal storage.
func shareKeyName(id string, share config.PluginShare) string {
	if share.Drive == "" {
		return id + "/" + share.ID
	}
	return share.Drive + "/" + share.Folder
}

// findShare returns the index of a plugin's share, or -1.
func findShare(shares []config.PluginShare, shareID string) int {
	return slices.IndexFunc(shares, func(s config.PluginShare) bool { return s.ID == shareID })
}

// nextShareID is a new share's ID: one more than the highest so far.
func nextShareID(shares []config.PluginShare) string {
	highest := 0
	for _, s := range shares {
		if n, err := strconv.Atoi(strings.TrimPrefix(s.ID, "s")); err == nil && n > highest {
			highest = n
		}
	}
	return "s" + strconv.Itoa(highest+1)
}

// --- Checking what the owner typed ---

// checkShareName checks a share's name: shown on other computers, so plain
// characters only, and different from its plugin's other shares.
func checkShareName(name string, shares []config.PluginShare, except string) error {
	switch {
	case name == "":
		return &config.ValidationError{Field: "name", Message: "Give the share a name."}
	case utf8.RuneCountInString(name) > maxShareNameLength:
		return &config.ValidationError{Field: "name", Message: fmt.Sprintf("Use at most %d characters.", maxShareNameLength)}
	case strings.ContainsFunc(name, func(r rune) bool {
		return !(r == ' ' || r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}) || strings.HasPrefix(name, " ") || strings.HasSuffix(name, " "):
		return &config.ValidationError{Field: "name", Message: "Use letters, digits, spaces, - _ and . only."}
	}
	for _, s := range shares {
		if s.ID != except && strings.EqualFold(s.Name, name) {
			return &config.ValidationError{Field: "name", Message: "Another share has this name."}
		}
	}
	return nil
}

// cleanShareFolder checks a folder on a drive, typed like "Movies" or
// "Media/Movies", and returns it tidied up: no leading or trailing slashes,
// no "." or ".." (it must stay on the drive), and not RasBeacon's own.
func cleanShareFolder(folder string) (string, error) {
	invalid := &config.ValidationError{Field: "folder", Message: "Type a folder on the drive, like Movies or Media/Movies."}
	folder = strings.Trim(strings.ReplaceAll(strings.TrimSpace(folder), "\\", "/"), "/")
	if folder == "" || utf8.RuneCountInString(folder) > 200 || strings.ContainsFunc(folder, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", invalid
	}
	for _, part := range strings.Split(folder, "/") {
		if part == "" || part == "." || part == ".." || part == encryptedFolder || strings.TrimSpace(part) != part {
			return "", invalid
		}
	}
	return path.Clean(folder), nil
}

// foldersOverlap reports whether one folder is the other or inside it.
// "" is the whole drive (a share carried over from an older version), and
// holds every other folder.
func foldersOverlap(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// driveUser is the plugin that uses a drive, by its single drive or by a
// share on it ("" when none). The caller holds a.mu.
func (a *Agent) driveUser(uuid string) string {
	for id, p := range a.cfg.Plugins.Installed {
		if p.Drive == uuid || slices.ContainsFunc(p.Shares, func(s config.PluginShare) bool { return s.Drive == uuid }) {
			return id
		}
	}
	return ""
}

// checkShareSettings checks a share's values against the manifest's
// per-share settings, filling in defaults.
func checkShareSettings(m *plugin.Manifest, values map[string]string) (map[string]string, error) {
	checked := map[string]string{}
	for _, s := range m.Shares.Settings {
		value, ok := values[s.Key]
		if !ok {
			value = s.Default
		}
		if err := s.Check(value); err != nil {
			return nil, &config.ValidationError{Field: "settings." + s.Key, Message: s.Label + " " + err.Error() + "."}
		}
		checked[s.Key] = value
	}
	return checked, nil
}

// --- Quotas ---

// shareQuotaRange is the quotas a share may have now: down to what its
// files use (with room for ext4's own records on internal storage), up to
// the space there is. share.ID "" is a new share. usedMB is a fresh
// measurement (-1: not known). The caller holds a.mu.
func (a *Agent) shareQuotaRange(id string, share config.PluginShare, usedMB int) (minMB, maxMB int) {
	minMB = minShareQuotaMB
	current := 0
	if share.ID != "" {
		current = share.QuotaMB
	}
	if share.Drive == "" {
		if usedMB > 0 {
			minMB = max(minMB, usedMB+pluginStorageRoom(usedMB))
		}
		free, err := a.diskFree(a.paths.DataDir())
		if err != nil {
			return minMB, current
		}
		// The share's own file is already set aside: its quota can grow
		// by what is free, less the headroom. (The quota it has is always
		// allowed, even if the SD card filled up since.)
		maxMB = current + int(free>>20) - internalHeadroomMB
		return minMB, max(maxMB, current)
	}
	if usedMB > 0 {
		minMB = max(minMB, usedMB)
	}
	free, err := a.diskFree(a.paths.DriveMountDir(share.Drive))
	if err != nil {
		return minMB, current
	}
	// What is free on the drive, less what the other shares on it may
	// still fill (their quota less their files), plus this one's files.
	// A drive belongs to one plugin, so its shares are all this plugin's.
	room := int(free >> 20)
	for _, other := range a.cfg.Plugins.Installed[id].Shares {
		if other.Drive == share.Drive && other.ID != share.ID {
			room -= max(0, other.QuotaMB-max(a.cachedShareUsage(other), 0))
		}
	}
	maxMB = max(usedMB, 0) + room
	return minMB, max(maxMB, current)
}

// checkShareQuota checks a quota against shareQuotaRange. The size a share
// has now is always fine. The caller holds a.mu.
func (a *Agent) checkShareQuota(id string, share config.PluginShare, quotaMB, usedMB int) error {
	if share.ID != "" && quotaMB == share.QuotaMB {
		return nil
	}
	minMB, maxMB := a.shareQuotaRange(id, share, usedMB)
	switch {
	case quotaMB < minShareQuotaMB:
		return &config.ValidationError{Field: "quotaMb", Message: "Use at least " + sizeText(minShareQuotaMB) + "."}
	case quotaMB < minMB:
		return &config.ValidationError{Field: "quotaMb", Message: fmt.Sprintf(
			"Its files use %s, so its quota can't be less than %s.", sizeText(max(usedMB, 0)), sizeText(minMB))}
	case quotaMB > maxMB && share.Drive == "":
		return &config.ValidationError{Field: "quotaMb", Message: fmt.Sprintf(
			"The SD card has room for at most %s (RasBeacon keeps 2 GB free).", sizeText(max(maxMB, 0)))}
	case quotaMB > maxMB:
		return &config.ValidationError{Field: "quotaMb", Message: fmt.Sprintf(
			"The drive has room for at most %s, counting what its other shares may still fill.", sizeText(max(maxMB, 0)))}
	}
	return nil
}

// --- Measuring what a share uses ---

// measureShareMB adds up what a share's files use now: the filesystem's
// own counts on internal storage, the files in its folder on a drive
// (which must be mounted). -1 when it can't be measured. Called without
// a.mu: a big drive takes a while.
func (a *Agent) measureShareMB(id string, share config.PluginShare) int {
	if share.Drive == "" {
		mount := a.paths.PluginShareMount(id, share.ID)
		if _, err := a.plugins.runner.Run("mountpoint", "-q", mount); err == nil {
			var st syscall.Statfs_t
			if syscall.Statfs(mount, &st) == nil {
				return int(((st.Blocks - st.Bfree) * uint64(st.Bsize)) >> 20)
			}
		}
		if _, err := os.Stat(a.paths.PluginShareImage(id, share.ID)); err != nil {
			return 0 // not made yet: it is made empty when the plugin starts
		}
		return a.imageUsedMB(a.paths.PluginShareImage(id, share.ID))
	}
	if _, err := a.plugins.runner.Run("mountpoint", "-q", a.paths.DriveMountDir(share.Drive)); err != nil && a.mountDrive(share.Drive) != nil {
		return -1
	}
	return a.measureFolderMB(a.shareStoreDir(id, share))
}

// cachedShareUsage is the last measurement of a share (-1: none yet). The
// caller holds a.mu.
func (a *Agent) cachedShareUsage(share config.PluginShare) int {
	if u, ok := a.plugins.shareUsage[share.Drive+"|"+share.Folder+"|"+share.ID]; ok && !u.measured.IsZero() {
		return u.mb
	}
	return -1
}

// shareUsageFor is a share's use for the page: the last measurement, and
// a new one in the background when it is older than shareUsageEvery. The
// caller holds a.mu.
func (a *Agent) shareUsageFor(id string, share config.PluginShare) int {
	key := share.Drive + "|" + share.Folder + "|" + share.ID
	if a.plugins.shareUsage == nil {
		a.plugins.shareUsage = map[string]shareUsage{}
	}
	u := a.plugins.shareUsage[key]
	if !u.measuring && a.now().Sub(u.measured) >= shareUsageEvery {
		u.measuring = true
		a.plugins.shareUsage[key] = u
		a.background(func() {
			mb := a.measureShareMB(id, share)
			a.mu.Lock()
			defer a.mu.Unlock()
			a.plugins.shareUsage[key] = shareUsage{mb: mb, measured: a.now()}
		})
	}
	if u.measured.IsZero() {
		return -1
	}
	return u.mb
}

// rememberShareUsage stores a fresh measurement. The caller holds a.mu.
func (a *Agent) rememberShareUsage(share config.PluginShare, mb int) {
	if a.plugins.shareUsage == nil {
		a.plugins.shareUsage = map[string]shareUsage{}
	}
	a.plugins.shareUsage[share.Drive+"|"+share.Folder+"|"+share.ID] = shareUsage{mb: mb, measured: a.now()}
}

// --- Making, changing and removing shares ---

// AddPluginShare makes a share. On a drive, its folder may already hold
// files: they become the share's (and count against its quota). The
// plugin restarts with it if it runs.
func (a *Agent) AddPluginShare(req api.PluginShareAddRequest) (api.Plugin, error) {
	req.Name = strings.TrimSpace(req.Name)
	// Where it goes first, then its files measured without the lock (a
	// folder on a big drive takes a while to add up).
	share := config.PluginShare{Name: req.Name, Drive: req.Drive}
	if req.Drive != "" {
		if !plugin.ValidDriveUUID(req.Drive) {
			return api.Plugin{}, &config.ValidationError{Field: "drive", Message: "Choose a USB drive from the list."}
		}
		folder, err := cleanShareFolder(req.Folder)
		if err != nil {
			return api.Plugin{}, err
		}
		share.Folder = folder
		if err := a.mountDrive(req.Drive); err != nil {
			return api.Plugin{}, &config.ValidationError{Field: "drive", Message: "This drive isn't plugged in."}
		}
	}
	used := 0
	if share.Drive != "" {
		used = a.measureShareMB(req.ID, share)
	}

	a.mu.Lock()
	live, err := a.beginPluginWork(req.ID)
	if err != nil {
		a.mu.Unlock()
		return api.Plugin{}, err
	}
	fail := func(err error) (api.Plugin, error) {
		live.busy = false
		a.mu.Unlock()
		return api.Plugin{}, err
	}
	m := live.manifest
	if m == nil || m.Shares == nil {
		return fail(&config.ValidationError{Field: "id", Message: "This plugin has no shares."})
	}
	p := a.cfg.Plugins.Installed[req.ID]
	if len(p.Shares) >= m.Shares.MaxShareCount() {
		return fail(&config.ValidationError{Field: "name", Message: fmt.Sprintf("%s can have at most %d shares.", m.Name, m.Shares.MaxShareCount())})
	}
	if err := checkShareName(req.Name, p.Shares, ""); err != nil {
		return fail(err)
	}
	if share.Drive != "" {
		if !m.Has("usb-drive") {
			return fail(&config.ValidationError{Field: "drive", Message: "This plugin's shares can only be on RasBeacon's internal storage."})
		}
		if user := a.driveUser(share.Drive); user != "" && user != req.ID {
			return fail(&config.ValidationError{Field: "drive", Message: a.pluginName(user) + " already uses this drive."})
		}
		for _, other := range p.Shares {
			if other.Drive == share.Drive && foldersOverlap(other.Folder, share.Folder) {
				return fail(&config.ValidationError{Field: "folder", Message: "The share " + other.Name + " is in this folder, or this folder is in it."})
			}
		}
	}
	settings, err := checkShareSettings(m, req.Settings)
	if err != nil {
		return fail(err)
	}
	if err := a.checkShareQuota(req.ID, share, req.QuotaMB, used); err != nil {
		return fail(err)
	}
	share.ID, share.QuotaMB, share.Settings = nextShareID(p.Shares), req.QuotaMB, settings
	// A folder RasBeacon encrypted for an earlier share opens again.
	if share.Drive != "" && a.secrets.ShareKeys[shareKeyName(req.ID, share)] != "" {
		if _, err := os.Stat(filepath.Join(a.shareStoreDir(req.ID, share), encryptedFolder, "gocryptfs.conf")); err == nil {
			share.Encryption, share.RecoveryKeySaved = encryptionOn, true
		}
	}
	if err := a.savePluginConfig(req.ID, func(p *config.InstalledPlugin) { p.Shares = append(p.Shares, share) }); err != nil {
		return fail(err)
	}
	a.rememberShareUsage(share, used)
	slog.Info("share added", "plugin", req.ID, "share", share.ID, "onDrive", share.Drive != "", "encrypt", req.Encrypt)
	if req.Encrypt && share.Encryption != encryptionOn {
		// Encrypted from the start: the move (of nothing, or of what the
		// drive's folder already holds) restarts the plugin when done.
		if err := a.startConversion(live, req.ID, share, encryptionEncrypting); err != nil {
			return fail(err)
		}
		defer a.mu.Unlock()
		return a.pluginView(req.ID), nil
	}
	return a.restartAfterShareChange(req.ID, nil)
}

// UpdatePluginShare changes a share's name, quota and settings. A smaller
// quota on internal storage shrinks its file (while the plugin is
// stopped); if the files don't fit after all, the old quota stays.
func (a *Agent) UpdatePluginShare(req api.PluginShareUpdateRequest) (api.Plugin, error) {
	req.Name = strings.TrimSpace(req.Name)
	a.mu.Lock()
	p, ok := a.cfg.Plugins.Installed[req.ID]
	index := findShare(p.Shares, req.Share)
	if !ok || index < 0 {
		a.mu.Unlock()
		return api.Plugin{}, &config.ValidationError{Field: "share", Message: "No such share."}
	}
	share := p.Shares[index]
	a.mu.Unlock()
	used := -1
	if req.QuotaMB != share.QuotaMB {
		used = a.measureShareMB(req.ID, share)
	}

	a.mu.Lock()
	live, err := a.beginPluginWork(req.ID)
	if err != nil {
		a.mu.Unlock()
		return api.Plugin{}, err
	}
	fail := func(err error) (api.Plugin, error) {
		live.busy = false
		a.mu.Unlock()
		return api.Plugin{}, err
	}
	m := live.manifest
	if m == nil || m.Shares == nil {
		return fail(&config.ValidationError{Field: "id", Message: "This plugin has no shares."})
	}
	if err := checkShareName(req.Name, a.cfg.Plugins.Installed[req.ID].Shares, req.Share); err != nil {
		return fail(err)
	}
	settings, err := checkShareSettings(m, req.Settings)
	if err != nil {
		return fail(err)
	}
	if used < 0 && req.QuotaMB != share.QuotaMB {
		return fail(&config.ValidationError{Field: "quotaMb", Message: "Plug in the share's drive to change its quota."})
	}
	if err := a.checkShareQuota(req.ID, share, req.QuotaMB, used); err != nil {
		return fail(err)
	}
	if err := a.savePluginConfig(req.ID, func(p *config.InstalledPlugin) {
		if i := findShare(p.Shares, req.Share); i >= 0 {
			p.Shares[i].Name, p.Shares[i].QuotaMB, p.Shares[i].Settings = req.Name, req.QuotaMB, settings
		}
	}); err != nil {
		return fail(err)
	}
	if used >= 0 {
		a.rememberShareUsage(share, used)
	}
	var resize func() error
	if share.Drive == "" && req.QuotaMB != share.QuotaMB {
		resize = func() error {
			return a.resizeImage(a.paths.PluginShareImage(req.ID, share.ID), a.paths.PluginShareMount(req.ID, share.ID), req.QuotaMB)
		}
	}
	plugin, err := a.restartAfterShareChange(req.ID, resize)
	if err != nil {
		// The files don't fit after all: the quota it had stays.
		a.mu.Lock()
		_ = a.savePluginConfig(req.ID, func(p *config.InstalledPlugin) {
			if i := findShare(p.Shares, req.Share); i >= 0 {
				p.Shares[i].QuotaMB = share.QuotaMB
			}
		})
		a.mu.Unlock()
		return api.Plugin{}, &config.ValidationError{Field: "quotaMb", Message: "The quota couldn't be changed: " + err.Error() + "."}
	}
	slog.Info("share changed", "plugin", req.ID, "share", share.ID)
	return plugin, nil
}

// RemovePluginShare removes a share. On internal storage its file goes,
// and its files with it. On a drive its folder stays, unless DeleteFiles
// asks for it to go too, or it is encrypted (its files can't be read
// without the share anyway, so they go).
func (a *Agent) RemovePluginShare(req api.PluginShareRemoveRequest) (api.Plugin, error) {
	a.mu.Lock()
	live, err := a.beginPluginWork(req.ID)
	if err != nil {
		a.mu.Unlock()
		return api.Plugin{}, err
	}
	p := a.cfg.Plugins.Installed[req.ID]
	index := findShare(p.Shares, req.Share)
	if index < 0 {
		live.busy = false
		a.mu.Unlock()
		return api.Plugin{}, &config.ValidationError{Field: "share", Message: "No such share."}
	}
	share := p.Shares[index]
	deleteFiles := req.DeleteFiles || share.Drive == "" || share.Encryption != encryptionOff
	// A share of a whole drive (carried over from an older version) never
	// deletes the drive's files; encrypted, they stay readable with its
	// recovery key, which is kept.
	if share.Drive != "" && share.Folder == "" {
		deleteFiles = false
	}
	a.mu.Unlock()

	// Stopped first: nothing may write to the files while they go.
	_, _ = a.plugins.runner.Run("podman", "rm", "--force", "--ignore", "--time", "10", plugin.ContainerName(req.ID))
	_, _ = a.plugins.runner.Run("umount", a.paths.ShareQuotaDir(req.ID, share.ID))
	_, _ = a.plugins.runner.Run("umount", a.paths.SharePlainDir(req.ID, share.ID))
	if deleteFiles {
		if err := a.deleteShareFiles(req.ID, share); err != nil {
			a.endPluginWork(req.ID)
			return api.Plugin{}, err
		}
	}

	a.mu.Lock()
	if err := a.savePluginConfig(req.ID, func(p *config.InstalledPlugin) {
		if i := findShare(p.Shares, req.Share); i >= 0 {
			p.Shares = slices.Delete(p.Shares, i, i+1)
		}
	}); err != nil {
		live.busy = false
		a.mu.Unlock()
		return api.Plugin{}, err
	}
	if deleteFiles {
		a.forgetShareKey(shareKeyName(req.ID, share))
	}
	slog.Info("share removed", "plugin", req.ID, "share", share.ID, "filesDeleted", deleteFiles)
	return a.restartAfterShareChange(req.ID, nil)
}

// deleteShareFiles deletes a share's files: its storage file on internal
// storage, its folder on a drive (which must be plugged in). Called
// without a.mu, with the plugin stopped.
func (a *Agent) deleteShareFiles(id string, share config.PluginShare) error {
	if share.Drive == "" {
		_, _ = a.plugins.runner.Run("umount", a.paths.PluginShareMount(id, share.ID))
		if err := os.Remove(a.paths.PluginShareImage(id, share.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := a.mountDrive(share.Drive); err != nil {
		return &config.ValidationError{Field: "deleteFiles", Message: "Plug in the share's drive to delete its files, or keep them."}
	}
	if share.Folder == "" {
		return &config.ValidationError{Field: "deleteFiles", Message: "This share is the whole drive: its files stay on it."}
	}
	folder := a.shareStoreDir(id, share)
	// Never above the drive's top: the folder was checked when the share
	// was made, and is checked again here.
	if rel, err := filepath.Rel(a.paths.DriveMountDir(share.Drive), folder); err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("the share's folder %q isn't on its drive", share.Folder)
	}
	return os.RemoveAll(folder)
}

// forgetShareKey deletes a share's recovery key. The caller holds a.mu.
func (a *Agent) forgetShareKey(name string) {
	if a.secrets.ShareKeys[name] == "" {
		return
	}
	newSecrets := a.secrets.Clone()
	delete(newSecrets.ShareKeys, name)
	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		slog.Warn("could not delete a share's recovery key", "error", err)
		return
	}
	a.secrets = newSecrets
}

// restartAfterShareChange runs change (if any) while the plugin is
// stopped, then starts it again if it runs, and answers with its view.
// It is called holding a.mu (the change is saved) and the plugin busy;
// it unlocks.
func (a *Agent) restartAfterShareChange(id string, change func() error) (api.Plugin, error) {
	running := a.cfg.Plugins.Installed[id].Running
	a.mu.Unlock()
	var err error
	if change != nil {
		_, _ = a.plugins.runner.Run("podman", "rm", "--force", "--ignore", "--time", "10", plugin.ContainerName(id))
		err = change()
	}
	if running {
		_ = a.runPlugin(id) // its state shows any problem
	}
	a.endPluginWork(id)
	if err != nil {
		return api.Plugin{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pluginView(id), nil
}

// --- Carrying an older version's folder over ---

// carryOverToShares turns the one folder an older version of a plugin had
// into its first share, when an update moves it to shares (Samba 1.0 to
// 1.1), so the owner's files stay where the new version looks:
//
//   - With a USB drive, the share is the whole drive, as the older version
//     shared it (folder ""): nothing on the drive moves.
//   - Without one, its files were in its own data, in the volume a drive
//     would have replaced. They move into a share on internal storage as
//     big as its data was (or as small as they fit, when the SD card has
//     no room for both), and its data shrinks to what the new version
//     asks for.
//
// The share is named after the older version's "share" setting (Samba
// 1.0's share name), else "Files". Called without a.mu, after the update's
// files are in place and before it starts. A problem is logged, and the
// files stay where they were.
func (a *Agent) carryOverToShares(id string, old, updated *plugin.Manifest, record config.InstalledPlugin) {
	if old == nil || old.USBDrive == nil || old.Shares != nil || updated.Shares == nil {
		return
	}
	a.mu.Lock()
	already := len(a.cfg.Plugins.Installed[id].Shares) > 0
	a.mu.Unlock()
	if already {
		return
	}
	name := record.Settings["share"]
	if checkShareName(name, nil, "") != nil {
		name = "Files"
	}
	settings, _ := checkShareSettings(updated, nil)
	share := config.PluginShare{ID: "s1", Name: name, Settings: settings}
	if record.Drive != "" {
		share.Drive = record.Drive
		share.QuotaMB = a.driveSizeMB(record.Drive)
	} else {
		quota, err := a.moveDataIntoShare(id, old, updated, record, share)
		if err != nil {
			slog.Error("could not carry the plugin's files over to a share; they stay in its data", "plugin", id, "error", err)
			return
		}
		share.QuotaMB = quota
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.savePluginConfig(id, func(p *config.InstalledPlugin) {
		p.Shares, p.Drive = []config.PluginShare{share}, ""
	}); err != nil {
		slog.Error("could not save the plugin's carried-over share", "plugin", id, "error", err)
		return
	}
	slog.Info("plugin's folder carried over to a share", "plugin", id, "onDrive", share.Drive != "")
}

// driveSizeMB is a drive's size, for a share that is the whole drive
// (256 MB when it can't be read: the owner can raise it).
func (a *Agent) driveSizeMB(uuid string) int {
	out, err := a.runner.Run("lsblk", plugin.LsblkArgs...)
	if err == nil {
		drives, _ := plugin.ParseDrives(out)
		for _, d := range drives {
			if d.UUID == uuid && d.Size > 0 {
				return int(d.Size >> 20)
			}
		}
	}
	return minShareQuotaMB
}

// moveDataIntoShare moves the files of the volume an older version's drive
// replaced, from its data into a new share on internal storage, and
// shrinks its data to the new version's size. It returns the share's
// quota. Called without a.mu, with the plugin stopped.
func (a *Agent) moveDataIntoShare(id string, old, updated *plugin.Manifest, record config.InstalledPlugin, share config.PluginShare) (int, error) {
	index := slices.IndexFunc(old.Volumes, func(v plugin.Volume) bool { return v.Path == old.USBDrive.Path })
	if index < 0 {
		return 0, errors.New("the older version kept no folder in its data")
	}
	oldSize := pluginStorageMB(old, record)
	if err := a.mountPluginData(id, oldSize); err != nil {
		return 0, err
	}
	from := filepath.Join(a.pluginDataMount(id), old.Volumes[index].Name)
	used := a.measureFolderMB(from)
	// As big as its data was, if the SD card has room for both a moment;
	// else just big enough for the files.
	quota := oldSize
	if free, err := a.diskFree(a.paths.DataDir()); err == nil && int(free>>20)-internalHeadroomMB < quota {
		quota = max(minShareQuotaMB, used+pluginStorageRoom(used))
	}
	to := a.paths.PluginShareMount(id, share.ID)
	if err := a.mountImage(a.paths.PluginShareImage(id, share.ID), to, quota); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(from, 0o755); err != nil {
		return 0, err
	}
	if err := a.moveFiles(from, to, func(rel string) bool { return rel == "lost+found" }, func(int, int) error { return nil }); err != nil {
		return 0, err
	}
	// Its data only holds the new version's own files now.
	if err := a.resizeImage(a.pluginDataFile(id), a.pluginDataMount(id), pluginStorageMB(updated, record)); err != nil {
		slog.Warn("could not shrink the plugin's data after moving its files", "plugin", id, "error", err)
	}
	return quota, nil
}

// measureFolderMB adds up the files in a folder, rounded up.
func (a *Agent) measureFolderMB(dir string) int {
	var bytes int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				bytes += spaceUsed(info)
			}
		}
		return nil
	})
	return int((bytes + 1<<20 - 1) >> 20)
}

// --- Running the plugin with its shares ---

// planShares adds a plugin's shares to its container: each at
// <shares path>/<ID>, and all of them in RASBEACON_SHARES. The caller
// holds a.mu.
func (a *Agent) planShares(plan *pluginRunPlan, spec *plugin.RunSpec, p config.InstalledPlugin) {
	for _, share := range p.Shares {
		run := shareRun{share: share, host: a.shareHostDir(plan.id, share)}
		if share.Encryption == encryptionOn {
			run.key = a.secrets.ShareKeys[shareKeyName(plan.id, share)]
		}
		plan.shares = append(plan.shares, run)
		spec.Mounts = append(spec.Mounts, plugin.Mount{Host: run.host, Container: path.Join(plan.manifest.Shares.Path, share.ID)})
	}
	spec.Env["RASBEACON_SHARES"] = sharesEnv(plan.manifest.Shares.Path, plan.shares)
}

// sharesEnv describes the shares for the plugin, one line of JSON:
// [{"id":"s1","name":"Movies","path":"/shares/s1","quotaBytes":…,
// "settings":{…}}].
func sharesEnv(base string, shares []shareRun) string {
	type described struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Path       string            `json:"path"`
		QuotaBytes int64             `json:"quotaBytes"`
		Settings   map[string]string `json:"settings"`
	}
	list := []described{}
	for _, run := range shares {
		settings := run.share.Settings
		if settings == nil {
			settings = map[string]string{}
		}
		list = append(list, described{
			ID: run.share.ID, Name: run.share.Name, Path: path.Join(base, run.share.ID),
			QuotaBytes: int64(run.share.QuotaMB) << 20, Settings: settings,
		})
	}
	data, _ := json.Marshal(list)
	return string(data)
}

// prepareShares mounts each share before the plugin starts: its storage
// file on internal storage, its drive and folder otherwise, then opens it
// if encrypted. A share whose drive isn't plugged in is left out until it
// is. Called without a.mu.
func (a *Agent) prepareShares(plan *pluginRunPlan) error {
	var ready []shareRun
	for _, run := range plan.shares {
		share := run.share
		store := a.shareStoreDir(plan.id, share)
		if share.Drive == "" {
			if err := a.mountImage(a.paths.PluginShareImage(plan.id, share.ID), store, share.QuotaMB); err != nil {
				return fmt.Errorf("the share %s can't be opened: %w", share.Name, err)
			}
		} else {
			if err := a.mountDrive(share.Drive); err != nil {
				slog.Warn("a share's drive isn't there; starting without it", "plugin", plan.id, "share", share.ID, "error", err)
				plan.spec.Mounts = removeMount(plan.spec.Mounts, run.host)
				continue
			}
			if err := os.MkdirAll(store, 0o755); err != nil {
				return fmt.Errorf("the share %s's folder can't be made: %w", share.Name, err)
			}
		}
		if run.key != "" {
			// Never the encrypted files themselves: if they can't be
			// opened, the plugin doesn't start.
			if err := a.openEncryptedFolder(store, a.paths.SharePlainDir(plan.id, share.ID), run.key); err != nil {
				return fmt.Errorf("the share %s: %w", share.Name, err)
			}
		}
		if share.Drive != "" {
			if err := a.holdToQuota(plan.id, share); err != nil {
				return fmt.Errorf("the share %s can't be held to its quota: %w", share.Name, err)
			}
		}
		ready = append(ready, run)
	}
	if plan.manifest.Shares != nil {
		plan.spec.Env["RASBEACON_SHARES"] = sharesEnv(plan.manifest.Shares.Path, ready)
	}
	return nil
}

// holdToQuota mounts a USB share's quota view (rbquotafs) over its files,
// counted afresh and with its quota as it is now: one already mounted
// (from before a change of quota) is unmounted first. Called without
// a.mu, with the plugin stopped.
func (a *Agent) holdToQuota(id string, share config.PluginShare) error {
	view := a.paths.ShareQuotaDir(id, share.ID)
	_, _ = a.plugins.runner.Run("umount", view)
	if err := os.MkdirAll(view, 0o755); err != nil {
		return err
	}
	limit := strconv.FormatInt(int64(share.QuotaMB)<<20, 10)
	_, err := a.plugins.runner.Run(rbquotafs, "-limit", limit, a.shareFilesDir(id, share), view)
	return err
}

// releaseShares closes a plugin's encrypted shares and unmounts its
// shares' drives, so they can be pulled out. Called without a.mu.
func (a *Agent) releaseShares(id string) {
	a.mu.Lock()
	shares := a.cfg.Plugins.Installed[id].Shares
	a.mu.Unlock()
	a.releaseSharesOf(id, shares)
}

// releaseSharesOf is releaseShares for shares already read.
func (a *Agent) releaseSharesOf(id string, shares []config.PluginShare) {
	released := map[string]bool{}
	for _, share := range shares {
		// Top down: the quota view, the opened view, then the drive.
		if share.Drive != "" {
			_, _ = a.plugins.runner.Run("umount", a.paths.ShareQuotaDir(id, share.ID))
		}
		_, _ = a.plugins.runner.Run("umount", a.paths.SharePlainDir(id, share.ID))
		if share.Drive != "" && !released[share.Drive] {
			released[share.Drive] = true
			a.releaseDrive(share.Drive)
		}
	}
}

// halfConverted refuses to start a plugin with a share whose files are
// only partly encrypted or decrypted: it would see some of them only.
func halfConverted(shares []config.PluginShare) error {
	for _, share := range shares {
		switch share.Encryption {
		case encryptionEncrypting:
			return fmt.Errorf("the share %s is only partly encrypted; turn its encryption on again to finish", share.Name)
		case encryptionDecrypting:
			return fmt.Errorf("the share %s is only partly decrypted; turn its encryption off again to finish", share.Name)
		}
	}
	return nil
}

// --- The page ---

// sharesView is a plugin's shares for its page. The caller holds a.mu.
func (a *Agent) sharesView(id string, m *plugin.Manifest, p config.InstalledPlugin) *api.PluginShares {
	view := &api.PluginShares{
		Max: m.Shares.MaxShareCount(), OnDrives: m.Has("usb-drive"),
		Settings: a.settingViews(&plugin.Manifest{Settings: m.Shares.Settings}, nil, nil),
		List:     []api.PluginShare{},
	}
	_, view.NewInternalMaxMB = a.shareQuotaRange(id, config.PluginShare{}, 0)
	labels := a.driveLabels(p.Shares)
	live := a.plugins.live[id]
	for _, share := range p.Shares {
		used := a.shareUsageFor(id, share)
		item := api.PluginShare{
			ID: share.ID, Name: share.Name, Drive: share.Drive, Folder: share.Folder, QuotaMB: share.QuotaMB, UsedMB: used,
			Settings:   a.settingViews(&plugin.Manifest{Settings: m.Shares.Settings}, share.Settings, nil),
			Encryption: api.PluginEncryption{State: share.Encryption, RecoveryKeySaved: share.RecoveryKeySaved},
		}
		if share.Encryption == encryptionOff {
			item.Encryption.State = "off"
		}
		if share.Drive != "" {
			label, present := labels[share.Drive]
			item.DriveLabel, item.DriveMissing = label, !present
		}
		if live != nil && live.convertShare == share.ID {
			item.Encryption.Done, item.Encryption.Total = live.convertDone, live.convertTotal
		}
		item.MinQuotaMB, item.MaxQuotaMB = a.shareQuotaRange(id, share, used)
		view.List = append(view.List, item)
	}
	return view
}

// driveLabels is the name of each plugged-in drive the shares are on
// (missing: not plugged in). The caller holds a.mu.
func (a *Agent) driveLabels(shares []config.PluginShare) map[string]string {
	labels := map[string]string{}
	if !slices.ContainsFunc(shares, func(s config.PluginShare) bool { return s.Drive != "" }) {
		return labels
	}
	out, err := a.runner.Run("lsblk", plugin.LsblkArgs...)
	if err != nil {
		return labels
	}
	drives, _ := plugin.ParseDrives(out)
	for _, d := range drives {
		label := d.Label
		if label == "" {
			label = "Untitled"
		}
		labels[d.UUID] = label
	}
	return labels
}

// shareStorageBytes is what a plugin's shares on internal storage take on
// the SD card, for System → Storage. The caller holds a.mu.
func (a *Agent) shareStorageBytes(id string) uint64 {
	var total uint64
	for _, share := range a.cfg.Plugins.Installed[id].Shares {
		if share.Drive != "" {
			continue
		}
		var st syscall.Stat_t
		if syscall.Stat(a.paths.PluginShareImage(id, share.ID), &st) == nil {
			total += uint64(st.Blocks) * 512
		}
	}
	return total
}
