package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
	"rasbeacon/internal/quotafs"
)

// sharesPlugin is the test plugin with shares (at /shares, with a
// read-only switch for each), allowed on USB drives.
func sharesPlugin(t *testing.T) []byte {
	manifest := testPluginManifest("1.0")
	manifest["permissions"] = []string{"internet", "usb-drive"}
	manifest["shares"] = map[string]any{"path": "/shares", "max": 3, "settings": []map[string]any{
		{"key": "readOnly", "label": "Read-only", "type": "switch", "default": "false"},
	}}
	return writeTestPlugin(t, manifest, nil)
}

// newSharesEnv installs the shares test plugin, with the USB stick
// plugged in and 10 GB free on both the SD card and the stick.
func newSharesEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newPluginEnv(t)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = usbLsblk
	e.agent.diskFree = func(string) (int64, error) { return 10 << 30, nil }
	if _, err := e.installTestPlugin(sharesPlugin(t), nil); err != nil {
		t.Fatal(err)
	}
	return e
}

// shares are the plugin's shares as its page shows them.
func shares(e *testEnv) []api.PluginShare {
	e.t.Helper()
	view := e.plugin("adguard-home").Shares
	if view == nil {
		e.t.Fatal("the plugin has no shares view")
	}
	return view.List
}

// addShare makes a share, failing the test on an error.
func addShare(e *testEnv, req api.PluginShareAddRequest) {
	e.t.Helper()
	req.ID = "adguard-home"
	if _, err := e.agent.AddPluginShare(req); err != nil {
		e.t.Fatal(err)
	}
	e.runBackground()
}

// reserveArgs are rbquotafs's arguments for a USB share's reservation,
// and the plugin's user namespace (with a space after them).
func reserveArgs(e *testEnv, shareID string) string {
	return "-reserve " + filepath.Join(e.paths.DriveMountDir("1234-ABCD"), quotafs.ReservedFolder) + " -name adguard-home-" + shareID + " " + userBaseArgs(e)
}

// userBaseArgs is rbquotafs's argument for the test plugin's user
// namespace (with a space after it).
func userBaseArgs(e *testEnv) string {
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	return "-user-base " + strconv.Itoa(plugin.UserBase(e.agent.cfg.Plugins.Installed["adguard-home"].Address.String())) + " "
}

// sharesEnvOf is RASBEACON_SHARES as the plugin last started with it.
func sharesEnvOf(t *testing.T, e *testEnv) []map[string]any {
	t.Helper()
	env := e.runEnvs[len(e.runEnvs)-1]
	for _, line := range strings.Split(env, "\n") {
		if value, ok := strings.CutPrefix(line, "RASBEACON_SHARES="); ok {
			var list []map[string]any
			if err := json.Unmarshal([]byte(value), &list); err != nil {
				t.Fatalf("RASBEACON_SHARES = %q: %v", value, err)
			}
			return list
		}
	}
	t.Fatalf("no RASBEACON_SHARES in %q", env)
	return nil
}

func TestShares_APluginStartsWithNone(t *testing.T) {
	e := newSharesEnv(t)
	view := e.plugin("adguard-home").Shares
	if len(view.List) != 0 || view.Max != 3 || !view.OnDrives || len(view.Settings) != 1 {
		t.Errorf("shares = %+v", view)
	}
	// 10 GB free, less the 2 GB RasBeacon keeps.
	if view.NewInternalMaxMB != 8*1024 {
		t.Errorf("new share max = %d MB, want 8192", view.NewInternalMaxMB)
	}
	if got := sharesEnvOf(t, e); len(got) != 0 {
		t.Errorf("RASBEACON_SHARES = %v, want none", got)
	}
}

func TestShares_OnInternalStorage(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 2048, Settings: map[string]string{"readOnly": "true"}})

	list := shares(e)
	if len(list) != 1 || list[0].ID != "s1" || list[0].Name != "Files" || list[0].QuotaMB != 2048 || list[0].Drive != "" {
		t.Fatalf("shares = %+v", list)
	}
	// Its own file the quota's size, mounted for the plugin at /shares/s1.
	image := e.paths.PluginShareImage("adguard-home", "s1")
	if !e.runner.ran("fallocate --length 2048M "+image) || !e.runner.ran("mkfs.ext4") {
		t.Error("the share's file should be made at its quota")
	}
	if !strings.Contains(strings.Join(e.runner.commands, "\n"), e.paths.PluginShareMount("adguard-home", "s1")+":/shares/s1") {
		t.Error("the plugin should get the share at /shares/s1")
	}
	got := sharesEnvOf(t, e)
	if len(got) != 1 || got[0]["name"] != "Files" || got[0]["path"] != "/shares/s1" || got[0]["quotaBytes"] != float64(2048<<20) {
		t.Errorf("RASBEACON_SHARES = %v", got)
	}
	if settings, _ := got[0]["settings"].(map[string]any); settings["readOnly"] != "true" {
		t.Errorf("settings = %v", got[0]["settings"])
	}

	// Renamed, and given more room: its file grows while the plugin is stopped.
	mustWrite(t, image, "")
	if err := os.Truncate(image, 2048<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := e.agent.UpdatePluginShare(api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s1", Name: "Travel files", QuotaMB: 4096}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if !e.runner.ran("fallocate --length 4096M "+image) || !e.runner.ran("resize2fs "+image) {
		t.Error("growing should grow its file")
	}
	if list := shares(e); list[0].Name != "Travel files" || list[0].QuotaMB != 4096 {
		t.Errorf("after the change: %+v", list[0])
	}

	// Removed: its file goes, with its files.
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(image); !errors.Is(err, os.ErrNotExist) {
		t.Error("the share's file should be deleted")
	}
	if len(shares(e)) != 0 {
		t.Error("the share should be gone")
	}
}

func TestShares_QuotaRulesOnInternalStorage(t *testing.T) {
	e := newSharesEnv(t)
	var invalid *config.ValidationError
	for quota, want := range map[int]string{
		100:      "at least 256 MB",
		9 * 1024: "room for at most 8 GB",
	} {
		_, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Files", QuotaMB: quota})
		if !errors.As(err, &invalid) || invalid.Field != "quotaMb" || !strings.Contains(invalid.Message, want) {
			t.Errorf("%d MB: %v, want %q", quota, err, want)
		}
	}
	// Two shares of 4 GB fit in 8 GB; once made, their files take the
	// room (the SD card's free space shrinks with them).
	addShare(e, api.PluginShareAddRequest{Name: "One", QuotaMB: 4096})
	e.agent.diskFree = func(string) (int64, error) { return 6 << 30, nil }
	addShare(e, api.PluginShareAddRequest{Name: "Two", QuotaMB: 4096})
	e.agent.diskFree = func(string) (int64, error) { return 2 << 30, nil }
	if _, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Three", QuotaMB: 256}); err == nil {
		t.Error("nothing is left once the 2 GB headroom is reached")
	}

	// Shrinking: not below what its files use (with room for ext4's own).
	image := e.paths.PluginShareImage("adguard-home", "s1")
	mustWrite(t, image, "")
	e.runner.outputs["dumpe2fs -h "+image] = dumpe2fsOutput(4096, 1500)
	_, err := e.agent.UpdatePluginShare(api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s1", Name: "One", QuotaMB: 1024})
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "Its files use 1.5 GB") {
		t.Errorf("shrinking below its files: %v", err)
	}
}

func TestShares_NamesAndLimits(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 256})
	var invalid *config.ValidationError
	for name, want := range map[string]string{
		"":                      "Give the share a name",
		"files":                 "Another share has this name",
		"Movies/Old":            "letters, digits",
		strings.Repeat("x", 41): "at most 40",
	} {
		_, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: name, QuotaMB: 256})
		if !errors.As(err, &invalid) || invalid.Field != "name" || !strings.Contains(invalid.Message, want) {
			t.Errorf("%q: %v, want %q", name, err, want)
		}
	}
	_, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Odd", QuotaMB: 256, Settings: map[string]string{"readOnly": "maybe"}})
	if !errors.As(err, &invalid) || invalid.Field != "settings.readOnly" {
		t.Errorf("a wrong setting: %v", err)
	}
	addShare(e, api.PluginShareAddRequest{Name: "Two", QuotaMB: 256})
	addShare(e, api.PluginShareAddRequest{Name: "Three", QuotaMB: 256})
	if _, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Four", QuotaMB: 256}); err == nil || !strings.Contains(err.Error(), "at most 3") {
		t.Errorf("a fourth share: %v", err)
	}
	if _, err := e.agent.UpdatePluginShare(api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s9", Name: "X", QuotaMB: 256}); err == nil {
		t.Error("an unknown share can't be changed")
	}
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s9"}); err == nil {
		t.Error("an unknown share can't be removed")
	}
}

func TestShares_OnAUSBDrive(t *testing.T) {
	e := newSharesEnv(t)
	drive := e.paths.DriveMountDir("1234-ABCD")
	// Files already in the folder become the share's.
	mustWrite(t, filepath.Join(drive, "Movies", "film.mkv"), strings.Repeat("x", 3<<20))
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "/Movies/", QuotaMB: 4096})

	list := shares(e)
	if len(list) != 1 || list[0].Folder != "Movies" || list[0].DriveLabel != "STICK" || list[0].UsedMB < 3 {
		t.Fatalf("shares = %+v", list)
	}
	// The plugin gets the folder held to its quota (rbquotafs) at /shares/s1.
	view := e.paths.ShareQuotaDir("adguard-home", "s1")
	if !strings.Contains(strings.Join(e.runner.commands, "\n"), view+":/shares/s1") {
		t.Error("the plugin should get the quota view at /shares/s1")
	}
	if !e.runner.ran("rbquotafs -limit 4294967296 " + reserveArgs(e, "s1") + filepath.Join(drive, "Movies") + " " + view) {
		t.Error("the folder should be held to its 4 GB quota")
	}
	var invalid *config.ValidationError
	// A folder inside another share's, or around it, can't be one.
	for _, folder := range []string{"Movies/Old", "movies", ""} {
		_, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Other", Drive: "1234-ABCD", Folder: folder, QuotaMB: 256})
		if !errors.As(err, &invalid) || invalid.Field != "folder" {
			t.Errorf("folder %q: %v", folder, err)
		}
	}
	for _, folder := range []string{"../etc", "a/../../b", encryptedFolder} {
		if _, err := cleanShareFolder(folder); err == nil {
			t.Errorf("folder %q should be refused", folder)
		}
	}
	// The drive's free space, less what Movies may still fill (4 GB less
	// its files): a second share gets about 6 GB at most.
	_, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Photos", Drive: "1234-ABCD", Folder: "Photos", QuotaMB: 7 * 1024})
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "drive has room for at most") {
		t.Errorf("too big for the drive: %v", err)
	}
	addShare(e, api.PluginShareAddRequest{Name: "Photos", Drive: "1234-ABCD", Folder: "Photos", QuotaMB: 1024})

	// Removed: its folder stays, unless asked; then it goes.
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(drive, "Movies", "film.mkv")); err != nil {
		t.Error("the folder should stay on the drive")
	}
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s2", DeleteFiles: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(drive, "Photos")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the folder should be deleted when asked")
	}
}

func TestShares_FilesAddedOnAComputerCount(t *testing.T) {
	e := newSharesEnv(t)
	clock := &fakeClock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return clock.now }
	drive := e.paths.DriveMountDir("1234-ABCD")
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 256})
	// 300 MB added while the drive was out: more than the quota.
	big := filepath.Join(drive, "Movies", "added.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(make([]byte, 300<<20)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	clock.advance(shareUsageEvery)
	shares(e) // asks for a new measurement
	e.runBackground()
	share := shares(e)[0]
	if share.UsedMB < 300 || share.QuotaMB != 256 || share.MinQuotaMB < 300 {
		t.Errorf("share = %+v, want 300 MB used of 256, and no quota below 300", share)
	}
	// Lowering it further is refused; raising it to fit is fine.
	if _, err := e.agent.UpdatePluginShare(api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s1", Name: "Movies", QuotaMB: 280}); err == nil {
		t.Error("a quota below the files can't be set")
	}
	if _, err := e.agent.UpdatePluginShare(api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s1", Name: "Movies", QuotaMB: 512}); err != nil {
		t.Errorf("raising it: %v", err)
	}
}

func TestShares_ADriveMissingIsLeftOut(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 256})
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 256})
	// The stick is pulled out, and the plugin starts again.
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = `{"blockdevices":[]}`
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.StartPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	got := sharesEnvOf(t, e)
	if len(got) != 1 || got[0]["name"] != "Files" {
		t.Errorf("RASBEACON_SHARES = %v, want only Files", got)
	}
	if share := shares(e)[1]; !share.DriveMissing {
		t.Errorf("Movies = %+v, want its drive missing", share)
	}
	// Its quota can't change without the drive, nor its files be deleted.
	if _, err := e.agent.UpdatePluginShare(api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s2", Name: "Movies", QuotaMB: 512}); err == nil {
		t.Error("a quota on a missing drive can't change")
	}
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s2", DeleteFiles: true}); err == nil {
		t.Error("files on a missing drive can't be deleted")
	}
	// Adding a share on it is refused too.
	if _, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Photos", Drive: "1234-ABCD", Folder: "Photos", QuotaMB: 256}); err == nil {
		t.Error("a share on a missing drive can't be made")
	}
}

func TestShares_ADriveBelongsToOnePlugin(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 256})
	if drives, _ := e.agent.PluginDrives(); drives.Drives[0].UsedBy != "AdGuard Home" {
		t.Errorf("used by = %q", drives.Drives[0].UsedBy)
	}
	e.agent.mu.Lock()
	err := e.agent.checkDrive("other", &plugin.Manifest{USBDrive: &plugin.USBDrive{Path: "/media"}}, "1234-ABCD")
	e.agent.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "already uses this drive") {
		t.Errorf("another plugin taking the drive: %v", err)
	}
}

func TestShares_KeptWithTheData(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 256})
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 256})
	mustWrite(t, filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage), "data")
	mustWrite(t, e.paths.PluginShareImage("adguard-home", "s1"), "files")

	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home", KeepData: true}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	e.agent.mu.Lock()
	kept := e.agent.keptData()
	e.agent.mu.Unlock()
	if len(kept) != 1 || kept[0].Bytes == 0 {
		t.Errorf("kept = %+v", kept)
	}
	// Unsigned, so its kept data comes back when the owner says so.
	preview, err := e.agent.StagePluginFile(strings.NewReader(string(sharesPlugin(t))))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.agent.InstallPlugin(api.PluginInstallRequest{StagingID: preview.StagingID, UseKeptData: true,
		Settings: map[string]string{"password": "s3cret-pass"}}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if list := shares(e); len(list) != 2 || list[0].Name != "Files" || list[1].Folder != "Movies" {
		t.Errorf("shares after reinstalling = %+v", list)
	}
	if got := e.readFile(e.paths.PluginShareImage("adguard-home", "s1")); got != "files" {
		t.Errorf("the share's file = %q", got)
	}
}

func TestShares_ForgettingKeptDataDeletesItsShares(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 256})
	mustWrite(t, filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage), "data")
	mustWrite(t, e.paths.PluginShareImage("adguard-home", "s1"), "files")
	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home", KeepData: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.ForgetKeptData("adguard-home"); err != nil {
		t.Fatal(err)
	}
	list, files := e.agent.keptShareFiles("adguard-home")
	for _, gone := range []string{list, files} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s should be gone", gone)
		}
	}
}

func TestShares_InSystemStorage(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 256})
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 256})
	if err := os.WriteFile(e.paths.PluginShareImage("adguard-home", "s1"), make([]byte, 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	plugins, total := e.agent.pluginStorageUse()
	e.agent.mu.Unlock()
	if len(plugins) != 1 || total < 2<<20 {
		t.Errorf("plugins = %+v, total %d: the share on internal storage counts", plugins, total)
	}
}

func TestShares_Routes(t *testing.T) {
	e := newSharesEnv(t)
	if status, body := e.call("POST", "/plugins/shares/add", api.PluginShareAddRequest{ID: "adguard-home", Name: "Files", QuotaMB: 256}); status != 200 {
		t.Errorf("add = %d %s", status, body)
	}
	e.runBackground()
	if status, body := e.call("PUT", "/plugins/shares", api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s1", Name: "Docs", QuotaMB: 256}); status != 200 {
		t.Errorf("update = %d %s", status, body)
	}
	if status, body := e.call("POST", "/plugins/shares/remove", api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s1"}); status != 200 {
		t.Errorf("remove = %d %s", status, body)
	}
	// A plugin without shares has none to add.
	if status, _ := e.call("POST", "/plugins/shares/add", api.PluginShareAddRequest{ID: "nope", Name: "Files", QuotaMB: 256}); status == 200 {
		t.Error("an unknown plugin can't get a share")
	}
}

func TestShares_Helpers(t *testing.T) {
	if got := nextShareID([]config.PluginShare{{ID: "s1"}, {ID: "s4"}}); got != "s5" {
		t.Errorf("next ID = %q", got)
	}
	if got, err := cleanShareFolder(` \Media\Movies/ `); err != nil || got != "Media/Movies" {
		t.Errorf("folder = %q, %v", got, err)
	}
	for a, b := range map[string]string{"Movies": "movies", "Media": "Media/Old"} {
		if !foldersOverlap(a, b) {
			t.Errorf("%q and %q overlap", a, b)
		}
	}
	if foldersOverlap("Movies", "MoviesOld") {
		t.Error("Movies and MoviesOld don't overlap")
	}
	if err := halfConverted([]config.PluginShare{{Name: "A", Encryption: encryptionDecrypting}}); err == nil {
		t.Error("a half decrypted share stops the plugin from starting")
	}
}

// A share on a USB drive is held to its quota by rbquotafs: mounted with
// its quota as it is when the plugin starts, again after the quota
// changes, and let go of when the plugin stops. Internal shares need none
// (their own filesystem is the quota).
func TestShares_AUSBShareIsHeldToItsQuota(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 256})
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 1024})
	view := e.paths.ShareQuotaDir("adguard-home", "s2")
	folder := filepath.Join(e.paths.DriveMountDir("1234-ABCD"), "Movies")
	if !e.runner.ran("rbquotafs -limit 1073741824 " + reserveArgs(e, "s2") + folder + " " + view) {
		t.Fatal("the USB share should be held to its quota")
	}
	if e.runner.ran("rbquotafs -limit 268435456") {
		t.Error("a share on internal storage needs no quota layer")
	}
	if _, err := e.agent.UpdatePluginShare(api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s2", Name: "Movies", QuotaMB: 2048}); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("rbquotafs -limit 2147483648 " + reserveArgs(e, "s2") + folder + " " + view) {
		t.Error("a new quota should be held to after the change")
	}
	umounts := e.runner.count("umount " + view)
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if e.runner.count("umount "+view) == umounts {
		t.Error("stopping should let go of the quota view")
	}
	// A quota layer that can't start stops the plugin from starting:
	// never the share without its quota.
	e.runner.failing = append(e.runner.failing, "rbquotafs")
	if err := e.agent.StartPlugin("adguard-home"); err == nil {
		e.runBackground()
	}
	if p := e.plugin("adguard-home"); !strings.Contains(p.Message, "held to its quota") {
		t.Errorf("state %s %q, want the quota problem", p.State, p.Message)
	}
}

func TestShares_AnInternalShareSetsItsQuotaAsideAtOnce(t *testing.T) {
	e := newSharesEnv(t)
	// Even while the plugin is stopped, its file is made when it is added,
	// the whole quota set aside (mkfs without discard keeps it so).
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 3072})
	image := e.paths.PluginShareImage("adguard-home", "s1")
	if !e.runner.ran("fallocate --length 3072M "+image) || !e.runner.ran("mkfs.ext4 -q -F -m 0 -E nodiscard -L plugin "+image) {
		t.Errorf("the share's file should be made, all set aside, at once: %v", e.runner.commands)
	}

	// A file made before (empty space given back) is set aside again.
	mustWrite(t, image, "")
	if err := os.Truncate(image, 3072<<20); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.StartPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("fallocate --length " + strconv.Itoa(3072<<20) + " " + image) {
		t.Error("an existing file should have its space set aside again")
	}

	// No room on the SD card: the share isn't made.
	e.runner.failing = append(e.runner.failing, "fallocate --length 1024M")
	_, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "More", QuotaMB: 1024})
	var invalid *config.ValidationError
	if !errors.As(err, &invalid) || invalid.Field != "quotaMb" {
		t.Errorf("no room: %v", err)
	}
	if len(shares(e)) != 1 {
		t.Errorf("shares = %+v, want only Files", shares(e))
	}
}

func TestShares_AUSBShareHoldsItsRoomOnTheDrive(t *testing.T) {
	e := newSharesEnv(t)
	movies := filepath.Join(e.paths.DriveMountDir("1234-ABCD"), "Movies")
	mustWrite(t, filepath.Join(movies, "a.mkv"), strings.Repeat("x", 3<<20)) // 3 MB
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	// While the plugin is stopped, rbd holds the room its files don't use.
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 1024})
	if got := e.held["adguard-home-s1"]; got != (1024-3)<<20 {
		t.Errorf("held %d MB, want the 1021 MB its files don't use", got>>20)
	}
	if _, err := e.agent.UpdatePluginShare(api.PluginShareUpdateRequest{ID: "adguard-home", Share: "s1", Name: "Movies", QuotaMB: 2048}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := e.held["adguard-home-s1"]; got != (2048-3)<<20 {
		t.Errorf("after the change, held %d MB, want 2045", got>>20)
	}

	// What a reservation holds is taken from the drive's free space already:
	// the 10 GB free count it once, not twice.
	reserved := filepath.Join(e.paths.DriveMountDir("1234-ABCD"), quotafs.ReservedFolder)
	mustWrite(t, filepath.Join(reserved, "adguard-home-s1-001.bin"), "")
	if err := os.Truncate(filepath.Join(reserved, "adguard-home-s1-001.bin"), 2045<<20); err != nil {
		t.Fatal(err)
	}
	e.agent.diskFree = func(string) (int64, error) { return (10 << 30) - (2045 << 20), nil }
	_, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Photos", Drive: "1234-ABCD", Folder: "Photos", QuotaMB: 8 * 1024})
	if err != nil {
		t.Errorf("8 GB is what is left: %v", err)
	}
	e.runBackground()
	_, err = e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "More", Drive: "1234-ABCD", Folder: "More", QuotaMB: 1024})
	var invalid *config.ValidationError
	if !errors.As(err, &invalid) || invalid.Field != "quotaMb" {
		t.Errorf("nothing is left for another share: %v", err)
	}

	// The reserved folder can't be a share's.
	_, err = e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Odd", Drive: "1234-ABCD", Folder: "rasbeacon reserved space/x", QuotaMB: 256})
	if !errors.As(err, &invalid) || invalid.Field != "folder" {
		t.Errorf("a share in the reserved folder: %v", err)
	}

	// Removed, its room is given back.
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(reserved, "adguard-home-s1-001.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Error("its reservation should be gone")
	}
}

// Stopping a plugin lets go of its shares' drive, so it can be pulled
// out: after every share's views are closed (a drive with two shares is
// busy until both are), and again while the programs behind them are
// still ending.
func TestShares_StoppingLetsGoOfTheDriveOnceItIsFree(t *testing.T) {
	e := newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 512})
	addShare(e, api.PluginShareAddRequest{Name: "Photos", Drive: "1234-ABCD", Folder: "Photos", QuotaMB: 512})
	drive := e.paths.DriveMountDir("1234-ABCD")
	// The drive is mounted, and busy the first two times it is unmounted.
	e.runner.outputs["mountpoint -q "+drive] = ""
	busy := 2
	effect := e.runner.effect
	e.runner.effect = func(args []string) {
		effect(args)
		if len(args) == 2 && args[0] == "umount" && args[1] == drive {
			e.runner.failing = slices.DeleteFunc(e.runner.failing, func(s string) bool { return s == "umount "+drive })
			if busy > 0 {
				busy--
				e.runner.failing = append(e.runner.failing, "umount "+drive)
			}
		}
	}
	before := len(e.runner.commands)
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	var umounts []string
	for _, command := range e.runner.commands[before:] {
		if dir, ok := strings.CutPrefix(command, "umount "); ok && (dir == drive || strings.Contains(dir, "/s1") || strings.Contains(dir, "/s2")) {
			umounts = append(umounts, strings.TrimPrefix(dir, filepath.Dir(drive)))
		}
	}
	// Two views for each share (its quota view and its opened view), then
	// the drive three times: busy twice.
	first := slices.Index(umounts, "/"+filepath.Base(drive))
	if len(umounts) != 7 || first != 4 || umounts[5] != umounts[4] || umounts[6] != umounts[4] {
		t.Errorf("unmounted %v, want both shares' views first, then the drive three times", umounts)
	}

	// A drive that isn't mounted is left alone after one try.
	delete(e.runner.outputs, "mountpoint -q "+drive)
	e.runner.failing = append(e.runner.failing, "umount "+drive)
	tries := e.runner.count("umount " + drive)
	e.agent.releaseDrive("1234-ABCD")
	if got := e.runner.count("umount "+drive) - tries; got != 1 {
		t.Errorf("an unmounted drive was unmounted %d times, want once", got)
	}
	// One that stays busy is given up on.
	e.runner.outputs["mountpoint -q "+drive] = ""
	busy = 1000
	tries = e.runner.count("umount " + drive)
	e.agent.releaseDrive("1234-ABCD")
	if got := e.runner.count("umount "+drive) - tries; got != releaseTries {
		t.Errorf("a busy drive was unmounted %d times, want %d", got, releaseTries)
	}
}
