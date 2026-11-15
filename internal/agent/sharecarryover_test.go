package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/plugin"
)

// oneFolderPlugin is an older version like Samba 1.0: one folder, on a USB
// drive if one is chosen (optional), else in its own 2 GB of data, named
// by its "share" setting.
func oneFolderPlugin(t *testing.T) []byte {
	manifest := testPluginManifest("1.0")
	manifest["permissions"] = []string{"internet", "usb-drive"}
	manifest["storageMb"] = 2048
	manifest["volumes"] = []map[string]any{{"name": "share", "path": "/share"}}
	manifest["usbDrive"] = map[string]any{"path": "/share", "optional": true}
	manifest["templates"] = nil
	manifest["settings"] = append(manifest["settings"].([]map[string]any),
		map[string]any{"key": "share", "label": "Share name", "type": "text", "default": "Files"})
	return writeTestPlugin(t, manifest, nil)
}

// sharesVersion is the update to shares (like Samba 1.1), 16 MB of data.
func sharesVersion(t *testing.T) []byte {
	manifest := testPluginManifest("1.1")
	manifest["permissions"] = []string{"internet", "usb-drive"}
	manifest["storageMb"] = 16
	manifest["volumes"] = nil
	manifest["templates"] = nil
	manifest["shares"] = map[string]any{"path": "/shares", "settings": []map[string]any{
		{"key": "readOnly", "label": "Read-only", "type": "switch", "default": "false"},
	}}
	return writeTestPlugin(t, manifest, nil)
}

// installVersion installs (or updates to) a plugin file with these choices.
func installVersion(e *testEnv, data []byte, drive string, settings map[string]string) {
	e.t.Helper()
	preview, err := e.agent.StagePluginFile(strings.NewReader(string(data)))
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.agent.InstallPlugin(api.PluginInstallRequest{StagingID: preview.StagingID, Drive: drive, Settings: settings}); err != nil {
		e.t.Fatal(err)
	}
	e.runBackground()
}

func TestCarryOver_FilesInItsDataMoveIntoAShare(t *testing.T) {
	e := newPluginEnv(t)
	installVersion(e, oneFolderPlugin(t), "", map[string]string{"password": "s3cret-pass", "share": "Travel Docs"})
	data := filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage)
	mustWrite(t, data, "")
	if err := os.Truncate(data, 2048<<20); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(e.agent.pluginDataMount("adguard-home"), "share", "trips", "visa.pdf"), "stamped")

	installVersion(e, sharesVersion(t), "", map[string]string{})

	list := shares(e)
	if len(list) != 1 || list[0].Name != "Travel Docs" || list[0].Drive != "" || list[0].QuotaMB != 2048 {
		t.Fatalf("shares = %+v, want Travel Docs on internal storage, 2 GB", list)
	}
	if got := e.readFile(filepath.Join(e.paths.PluginShareMount("adguard-home", "s1"), "trips", "visa.pdf")); got != "stamped" {
		t.Errorf("visa.pdf = %q", got)
	}
	if _, err := os.Stat(filepath.Join(e.agent.pluginDataMount("adguard-home"), "share", "trips", "visa.pdf")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the file should have moved out of its data")
	}
	// Its data shrinks to the new version's 16 MB.
	if !e.runner.ran("resize2fs " + data + " 16M") {
		t.Error("its data should shrink")
	}
	if got := sharesEnvOf(t, e); len(got) != 1 || got[0]["name"] != "Travel Docs" {
		t.Errorf("RASBEACON_SHARES = %v", got)
	}
}

func TestCarryOver_WithLittleRoomTheShareIsJustBigEnough(t *testing.T) {
	e := newPluginEnv(t)
	installVersion(e, oneFolderPlugin(t), "", map[string]string{"password": "s3cret-pass"})
	mustWrite(t, filepath.Join(e.agent.pluginDataMount("adguard-home"), "share", "a.txt"), "a")
	e.agent.diskFree = func(string) (int64, error) { return 3 << 30, nil } // 1 GB above the headroom
	installVersion(e, sharesVersion(t), "", map[string]string{})
	if list := shares(e); len(list) != 1 || list[0].Name != "Files" || list[0].QuotaMB != minShareQuotaMB {
		t.Errorf("shares = %+v, want Files at the smallest quota", list)
	}
}

func TestCarryOver_ADriveBecomesAShareOfTheWholeDrive(t *testing.T) {
	e := newPluginEnv(t)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = usbLsblk
	installVersion(e, oneFolderPlugin(t), "1234-ABCD", map[string]string{"password": "s3cret-pass"})
	drive := e.paths.DriveMountDir("1234-ABCD")
	mustWrite(t, filepath.Join(drive, "film.mkv"), "film")

	installVersion(e, sharesVersion(t), "", map[string]string{})

	list := shares(e)
	if len(list) != 1 || list[0].Drive != "1234-ABCD" || list[0].Folder != "" || list[0].QuotaMB != int(int64(64000000000)>>20) {
		t.Fatalf("shares = %+v, want the whole drive at its size", list)
	}
	if e.agent.cfg.Plugins.Installed["adguard-home"].Drive != "" {
		t.Error("the single drive gives way to the share")
	}
	view := e.paths.ShareQuotaDir("adguard-home", "s1")
	if !strings.Contains(strings.Join(e.runner.commands, "\n"), view+":/shares/s1") || !e.runner.ran("rbquotafs -limit") ||
		!strings.Contains(strings.Join(e.runner.commands, "\n"), " "+drive+" "+view) {
		t.Error("the plugin should get the whole drive, held to its quota, as its share")
	}
	// No other share fits on it, and removing it keeps the drive's files.
	if _, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 256}); err == nil {
		t.Error("a folder on a drive shared whole can't be another share")
	}
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s1", DeleteFiles: true}); err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(filepath.Join(drive, "film.mkv")); got != "film" {
		t.Error("the drive's files stay")
	}
}

func TestCarryOver_OnlyFromOneFolderToShares(t *testing.T) {
	e := newPluginEnv(t)
	// A plugin that already had shares, or never had a folder, is left as it is.
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	installVersion(e, sharesVersion(t), "", map[string]string{})
	if list := shares(e); len(list) != 0 {
		t.Errorf("shares = %+v, want none", list)
	}
}
