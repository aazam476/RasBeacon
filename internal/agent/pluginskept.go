package agent

// Data kept when a plugin is removed ("Keep its data") waits in
// <plugins>/kept/<id>.img for a later install of the same plugin. A plugin's
// ID is chosen by its author, so another author's plugin could use the
// same one, such as "adguard-home", to collect the old configuration,
// passwords and files. So the removed plugin's name and signing key are
// kept next to its data (<id>.key.json):
//
//   - an install signed with that same key gets the data back by itself;
//   - any other install (another key, or either of them unsigned, so
//     nothing proves they are the same plugin) only gets it if the owner
//     turns on "Use the data kept from the earlier … install" in the
//     install sheet, which names the earlier plugin and its key.

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

// keptDataOwner is whose the kept data was.
type keptDataOwner struct {
	// Name is the removed plugin's name, for the install sheet.
	Name string `json:"name"`
	// PublicKey is its signing key (base64), "" when it wasn't signed.
	PublicKey string `json:"publicKey"`
}

// keptDataFiles are where a removed plugin's data and its owner are kept.
func (a *Agent) keptDataFiles(id string) (data, owner string) {
	dir := a.paths.PluginKeptDir()
	return filepath.Join(dir, id+".img"), filepath.Join(dir, id+".key.json")
}

// keptShareFiles are where a removed plugin's shares are kept: the list
// (all of them, those on drives too), and the files of those on internal
// storage.
func (a *Agent) keptShareFiles(id string) (list, files string) {
	dir := a.paths.PluginKeptDir()
	return filepath.Join(dir, id+".shares.json"), filepath.Join(dir, id+".shares")
}

// keepPluginShares moves a removed plugin's shares aside with its data.
func (a *Agent) keepPluginShares(id string, shares []config.PluginShare) {
	if len(shares) == 0 {
		return
	}
	list, files := a.keptShareFiles(id)
	if err := config.WriteJSON(list, shares, 0o600); err != nil {
		slog.Warn("could not keep the plugin's shares", "plugin", id, "error", err)
		return
	}
	_ = os.RemoveAll(files)
	if err := os.Rename(filepath.Join(a.paths.PluginDir(id), "shares"), files); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("could not keep the plugin's shares' files", "plugin", id, "error", err)
	}
}

// restoreKeptShares gives a new install its kept shares back (with its
// kept data, by the same rules): their files, and the list it returns.
func (a *Agent) restoreKeptShares(id string) []config.PluginShare {
	list, files := a.keptShareFiles(id)
	var shares []config.PluginShare
	if err := config.ReadJSON(list, &shares); err != nil {
		return nil
	}
	if err := os.Rename(files, filepath.Join(a.paths.PluginDir(id), "shares")); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("could not restore the plugin's shares' files", "plugin", id, "error", err)
		return nil
	}
	_ = os.Remove(list)
	return shares
}

// keepPluginData moves a plugin's data aside when it is removed, with its
// name and the key it was signed with.
func (a *Agent) keepPluginData(id string, owner keptDataOwner) {
	data, ownerFile := a.keptDataFiles(id)
	if err := os.MkdirAll(filepath.Dir(data), 0o700); err != nil {
		slog.Warn("could not keep the plugin's data", "plugin", id, "error", err)
		return
	}
	if err := os.Rename(filepath.Join(a.paths.PluginDir(id), pluginDataImage), data); err != nil {
		slog.Warn("could not keep the plugin's data", "plugin", id, "error", err)
		return
	}
	if err := config.WriteJSON(ownerFile, owner, 0o600); err != nil {
		slog.Warn("could not note whose data was kept", "plugin", id, "error", err)
	}
}

// keptDataFor says whether data kept for id is waiting, and whether an
// install signed with publicKey ("" for unsigned) gets it back by itself:
// only when both are signed with the same key. owner is whose it was (empty
// for data RasBeacon 0.1 kept, which didn't note it).
func (a *Agent) keptDataFor(id, publicKey string) (waiting, sameAuthor bool, owner keptDataOwner) {
	data, ownerFile := a.keptDataFiles(id)
	if _, err := os.Stat(data); err != nil {
		return false, false, owner
	}
	if err := config.ReadJSON(ownerFile, &owner); err != nil {
		return true, false, keptDataOwner{}
	}
	return true, owner.PublicKey != "" && owner.PublicKey == publicKey, owner
}

// keptDataQuestion is what the install sheet asks about kept data that
// doesn't come back by itself (nil when there's nothing to ask).
func (a *Agent) keptDataQuestion(id, publicKey string) *api.PluginKeptData {
	waiting, sameAuthor, owner := a.keptDataFor(id, publicKey)
	if !waiting || sameAuthor {
		return nil
	}
	question := &api.PluginKeptData{Name: owner.Name}
	if key, err := base64.StdEncoding.DecodeString(owner.PublicKey); err == nil && len(key) > 0 {
		question.KeyID = plugin.KeyID(key)
	}
	return question
}

// restoreKeptData gives kept data back to a new install of id signed with
// publicKey: by itself for the same author, or because the owner chose to
// (useKeptData).
func (a *Agent) restoreKeptData(id, publicKey string, useKeptData bool) {
	target := filepath.Join(a.paths.PluginDir(id), pluginDataImage)
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		return // it has data of its own already
	}
	waiting, sameAuthor, _ := a.keptDataFor(id, publicKey)
	if !waiting {
		return
	}
	if !sameAuthor && !useKeptData {
		slog.Info("kept data stays aside: not the same signing key, and the owner didn't choose it", "plugin", id)
		return
	}
	data, ownerFile := a.keptDataFiles(id)
	if err := os.Rename(data, target); err != nil {
		slog.Warn("could not restore the plugin's kept data", "plugin", id, "error", err)
		return
	}
	_ = os.Remove(ownerFile)
	if shares := a.restoreKeptShares(id); len(shares) > 0 {
		a.mu.Lock()
		err := a.savePluginConfig(id, func(p *config.InstalledPlugin) {
			if len(p.Shares) == 0 {
				p.Shares = shares
			}
		})
		a.mu.Unlock()
		if err != nil {
			slog.Warn("could not restore the plugin's shares", "plugin", id, "error", err)
		}
	}
	slog.Info("plugin's kept data restored", "plugin", id, "chosenByOwner", !sameAuthor)
}

// keptData lists the data kept from removed plugins, for the Plugins page,
// which can delete it (ForgetKeptData). The caller holds a.mu.
func (a *Agent) keptData() []api.PluginKept {
	kept := []api.PluginKept{}
	entries, _ := os.ReadDir(a.paths.PluginKeptDir())
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".img")
		info, err := entry.Info()
		if !ok || err != nil || !info.Mode().IsRegular() {
			continue
		}
		_, _, owner := a.keptDataFor(id, "")
		name := owner.Name
		if name == "" {
			name = id
		}
		bytes := spaceUsed(info)
		_, files := a.keptShareFiles(id)
		_ = filepath.WalkDir(files, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if share, err := d.Info(); err == nil {
					bytes += spaceUsed(share)
				}
			}
			return nil
		})
		kept = append(kept, api.PluginKept{ID: id, Name: name, Bytes: bytes})
	}
	return kept
}

// spaceUsed is the space a file takes on the card. A plugin's data is a
// sparse file: its size is how large it may grow, not what it holds.
func spaceUsed(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}

// ForgetKeptData deletes the data kept from a removed plugin, and whose it
// was, for good.
func (a *Agent) ForgetKeptData(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Only an ID from the list: an ID is a file name here.
	if !slices.ContainsFunc(a.keptData(), func(k api.PluginKept) bool { return k.ID == id }) {
		return &config.ValidationError{Field: "id", Message: "No data is kept for this plugin."}
	}
	data, owner := a.keptDataFiles(id)
	if err := os.Remove(data); err != nil {
		return err
	}
	_ = os.Remove(owner)
	// Its shares go too: those on internal storage with their files;
	// those on drives keep their folders.
	list, files := a.keptShareFiles(id)
	_ = os.Remove(list)
	_ = os.RemoveAll(files)
	slog.Info("kept plugin data deleted", "plugin", id)
	return nil
}
