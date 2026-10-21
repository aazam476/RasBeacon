package agent

// Data kept when a plugin is removed ("Keep its data") waits in
// <plugins>/kept/<id>.img for a later install of the same plugin. A plugin's
// ID is chosen by its author, so another author's plugin could use the
// same one, such as "adguard-home", to collect the old configuration,
// passwords and files. So the removed plugin's signing key is kept next to
// its data (<id>.key.json), and the data only goes back to an install
// signed with that same key (or, for a plugin that wasn't signed, to one
// that isn't either). Otherwise it stays aside, and the install sheet
// says so.

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"rasbeacon/internal/config"
)

// keptDataOwner is who the kept data belonged to.
type keptDataOwner struct {
	// PublicKey is the removed plugin's signing key (base64), "" when it
	// wasn't signed.
	PublicKey string `json:"publicKey"`
}

// keptDataFiles are where a removed plugin's data and its owner are kept.
func (a *Agent) keptDataFiles(id string) (data, owner string) {
	dir := a.paths.PluginKeptDir()
	return filepath.Join(dir, id+".img"), filepath.Join(dir, id+".key.json")
}

// keepPluginData moves a plugin's data aside when it is removed, with the
// key it was signed with.
func (a *Agent) keepPluginData(id, publicKey string) {
	data, owner := a.keptDataFiles(id)
	if err := os.MkdirAll(filepath.Dir(data), 0o700); err != nil {
		slog.Warn("could not keep the plugin's data", "plugin", id, "error", err)
		return
	}
	if err := os.Rename(filepath.Join(a.paths.PluginDir(id), pluginDataImage), data); err != nil {
		slog.Warn("could not keep the plugin's data", "plugin", id, "error", err)
		return
	}
	if err := config.WriteJSON(owner, keptDataOwner{PublicKey: publicKey}, 0o600); err != nil {
		slog.Warn("could not note whose data was kept", "plugin", id, "error", err)
	}
}

// keptDataFor says whether data kept for id is waiting, and whether an
// install signed with publicKey ("" for unsigned) may have it. Data kept
// before the key was noted (by RasBeacon 0.1) has no owner, so it stays
// aside too.
func (a *Agent) keptDataFor(id, publicKey string) (waiting, matches bool) {
	data, owner := a.keptDataFiles(id)
	if _, err := os.Stat(data); err != nil {
		return false, false
	}
	var kept keptDataOwner
	if err := config.ReadJSON(owner, &kept); err != nil {
		return true, false
	}
	return true, kept.PublicKey == publicKey
}

// restoreKeptData gives kept data back to a new install of id signed with
// publicKey, if it may have it (keptDataFor).
func (a *Agent) restoreKeptData(id, publicKey string) {
	target := filepath.Join(a.paths.PluginDir(id), pluginDataImage)
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		return // it has data of its own already
	}
	if waiting, matches := a.keptDataFor(id, publicKey); !waiting || !matches {
		if waiting {
			slog.Warn("kept data stays aside: the plugin is signed by another key", "plugin", id)
		}
		return
	}
	data, owner := a.keptDataFiles(id)
	if err := os.Rename(data, target); err != nil {
		slog.Warn("could not restore the plugin's kept data", "plugin", id, "error", err)
		return
	}
	_ = os.Remove(owner)
	slog.Info("plugin's kept data restored", "plugin", id)
}
