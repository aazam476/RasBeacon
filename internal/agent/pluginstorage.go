package agent

// The size of a plugin's data on the SD card.
//
// A plugin's data lives in one file in its folder (data.img), which holds
// an ext4 filesystem. The whole size is reserved on the SD card when the
// file is made, whether the plugin fills it or not, so System → Storage
// shows it as taken ("Plugins").
//
// Most plugins get the size their manifest asks for. A plugin whose
// manifest says storageAdjustable (such as Samba, which keeps the shared
// files there when no USB drive is chosen) lets the owner choose: more
// room for files, or less to keep the SD card free. The size can grow up
// to what the SD card has room for, keeping some free for RasBeacon, and
// shrink down to what the files use, with some room to spare.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

// pluginStorageMB is the size of a plugin's data: the owner's choice when
// the manifest allows one, else the manifest's.
func pluginStorageMB(m *plugin.Manifest, p config.InstalledPlugin) int {
	if m.StorageAdjustable && p.StorageMB > 0 {
		return p.StorageMB
	}
	return m.Storage()
}

// pluginDataFile is the file that holds a plugin's data.
func (a *Agent) pluginDataFile(id string) string {
	return filepath.Join(a.paths.PluginDir(id), pluginDataImage)
}

// pluginDataFileMB is the size of a plugin's data file (0: none yet).
func (a *Agent) pluginDataFileMB(id string) int {
	info, err := os.Stat(a.pluginDataFile(id))
	if err != nil {
		return 0
	}
	return int(info.Size() >> 20)
}

// pluginDataUsedMB is what a plugin's files use in its data file, read
// from the filesystem's own counts (dumpe2fs -h), which works whether it
// is mounted or not. 0: no data file yet, or the counts can't be read.
func (a *Agent) pluginDataUsedMB(id string) int {
	return a.imageUsedMB(a.pluginDataFile(id))
}

// imageUsedMB is what the files use in a storage file (a plugin's data or
// a share), from the filesystem's own counts (dumpe2fs -h), which works
// whether it is mounted or not. 0: no file, or the counts can't be read.
func (a *Agent) imageUsedMB(file string) int {
	if _, err := os.Stat(file); err != nil {
		return 0
	}
	out, err := a.plugins.runner.Run("dumpe2fs", "-h", file)
	if err != nil {
		return 0
	}
	return parseFilesystemUsedMB(out)
}

// parseFilesystemUsedMB reads the space in use from dumpe2fs -h: the
// blocks that aren't free, times the block size. 0 when a count is missing.
func parseFilesystemUsedMB(out string) int {
	counts := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch name = strings.TrimSpace(name); name {
		case "Block count", "Free blocks", "Block size":
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err == nil {
				counts[name] = n
			}
		}
	}
	blocks, free, size := counts["Block count"], counts["Free blocks"], counts["Block size"]
	if blocks == 0 || size == 0 || free > blocks {
		return 0
	}
	return int(((blocks - free) * size) >> 20)
}

// pluginStorageRoom is how much room to leave above what the files use
// when shrinking: a tenth, and at least 64 MB, so the plugin can still
// write (and ext4 has room for its own records).
func pluginStorageRoom(usedMB int) int {
	return max(64, usedMB/10)
}

// pluginStorageRange is the sizes the owner may choose for a plugin's
// data now. The caller holds a.mu.
func (a *Agent) pluginStorageRange(id string) api.PluginStorage {
	used := a.pluginDataUsedMB(id)
	current := a.pluginDataFileMB(id)
	storage := api.PluginStorage{StorageUsedMB: used, StorageMinMB: plugin.MinStorageMB}
	if used > 0 {
		storage.StorageMinMB = max(plugin.MinStorageMB, used+pluginStorageRoom(used))
	}
	// The data file can grow by what the SD card has free, less what is
	// kept free for RasBeacon.
	largest := plugin.MaxStorageMB
	if free, err := a.diskFree(a.paths.DataDir()); err == nil {
		largest = current + int(free>>20) - internalHeadroomMB
	}
	storage.StorageMaxMB = min(plugin.MaxStorageMB, max(largest, current, storage.StorageMinMB))
	return storage
}

// checkPluginStorage checks the size the owner chose for a plugin's data
// and returns the size to save (InstalledPlugin.StorageMB): 0 for "keep
// what it has". The caller holds a.mu.
func (a *Agent) checkPluginStorage(id string, m *plugin.Manifest, requestedMB int) (int, error) {
	saved := a.cfg.Plugins.Installed[id].StorageMB
	if requestedMB == 0 {
		return saved, nil
	}
	if !m.StorageAdjustable {
		return 0, &config.ValidationError{Field: "storageMb", Message: "This plugin's storage size can't be changed."}
	}
	// The size it has now is always fine, even if the SD card filled up
	// since.
	if requestedMB == pluginStorageMB(m, a.cfg.Plugins.Installed[id]) {
		return requestedMB, nil
	}
	limits := a.pluginStorageRange(id)
	switch {
	case requestedMB < plugin.MinStorageMB:
		return 0, &config.ValidationError{Field: "storageMb", Message: "Use at least " + sizeText(plugin.MinStorageMB) + "."}
	case requestedMB < limits.StorageMinMB:
		return 0, &config.ValidationError{Field: "storageMb", Message: fmt.Sprintf(
			"Its files use %s, so it needs at least %s.", sizeText(limits.StorageUsedMB), sizeText(limits.StorageMinMB))}
	case requestedMB > limits.StorageMaxMB:
		return 0, &config.ValidationError{Field: "storageMb", Message: fmt.Sprintf(
			"The SD card has room for at most %s.", sizeText(limits.StorageMaxMB))}
	}
	return requestedMB, nil
}

// sizeText is a size for the owner: "512 MB" below a gigabyte, else
// "2 GB" or "1.5 GB" (1 GB = 1024 MB, as the pages count).
func sizeText(mb int) string {
	if mb < 1024 {
		return fmt.Sprintf("%d MB", mb)
	}
	gb := strconv.FormatFloat(float64(mb)/1024, 'f', 1, 64)
	return strings.TrimSuffix(gb, ".0") + " GB"
}

// resizePluginData makes a plugin's data file storageMB big: its container
// is stopped and its data unmounted first (the caller starts it again).
// Growing adds space to the file and the filesystem; shrinking makes the
// filesystem smaller first (resize2fs refuses if the files don't fit, and
// then nothing changed), then gives the end of the file back to the SD
// card. Without a data file yet, there is nothing to do: it is made at
// this size when the plugin starts. Called without a.mu, while the plugin
// is busy (beginPluginWork).
func (a *Agent) resizePluginData(id string, storageMB int) error {
	_, _ = a.plugins.runner.Run("podman", "rm", "--force", "--ignore", "--time", "10", plugin.ContainerName(id))
	return a.resizeImage(a.pluginDataFile(id), a.pluginDataMount(id), storageMB)
}

// resizeImage makes a storage file storageMB big, unmounting it first
// (what used it must be stopped). Growing adds space to the file and the
// filesystem; shrinking makes the filesystem smaller first (resize2fs
// refuses if the files don't fit, and then nothing changed), then gives
// the end of the file back to the SD card. A file not made yet is left:
// it is made at the new size when first mounted.
func (a *Agent) resizeImage(file, mount string, storageMB int) error {
	info, err := os.Stat(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	want := int64(storageMB) << 20
	if info.Size() == want {
		return nil
	}
	r := a.plugins.runner
	_, _ = r.Run("umount", mount)
	// resize2fs works only on a filesystem just checked. -p fixes what is
	// safe to fix; its exit code says whether it fixed anything, so it
	// isn't an error here.
	_, _ = r.Run("e2fsck", "-f", "-p", file)
	size := strconv.Itoa(storageMB) + "M"
	if info.Size() < want {
		if _, err := r.Run("fallocate", "--length", size, file); err != nil {
			return fmt.Errorf("the SD card has no room for %s: %w", sizeText(storageMB), err)
		}
		_, err := r.Run("resize2fs", file)
		return err
	}
	if _, err := r.Run("resize2fs", file, size); err != nil {
		return fmt.Errorf("its files don't fit in %s: %w", sizeText(storageMB), err)
	}
	return os.Truncate(file, want)
}

// pluginStorageUse is the space each plugin's data takes on the SD card,
// for System → Storage, and all of it together in bytes. The caller holds
// a.mu.
func (a *Agent) pluginStorageUse() ([]api.StoragePlugin, uint64) {
	const gb = 1e9
	plugins := []api.StoragePlugin{}
	var total uint64
	for _, id := range a.pluginIDs() {
		// What its files really take on the card (their blocks), which
		// for a reserved file is its size: its data, and its shares on
		// internal storage.
		bytes := a.shareStorageBytes(id)
		var st syscall.Stat_t
		if err := syscall.Stat(a.pluginDataFile(id), &st); err == nil {
			bytes += uint64(st.Blocks) * 512
		}
		if bytes == 0 {
			continue
		}
		total += bytes
		plugins = append(plugins, api.StoragePlugin{Name: a.pluginName(id), GB: float64(bytes) / gb})
	}
	return plugins, total
}
