package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// dumpe2fsOutput is what dumpe2fs -h prints, cut down to the lines that
// count: usedMB in use out of 4 KiB blocks.
func dumpe2fsOutput(totalMB, usedMB int) string {
	blocks := totalMB * 256
	return "Filesystem volume name:   plugin\n" +
		"Block count:              " + strconv.Itoa(blocks) + "\n" +
		"Free blocks:              " + strconv.Itoa(blocks-usedMB*256) + "\n" +
		"Block size:               4096\n"
}

// newStoragePluginEnv installs the test plugin with an adjustable data
// size (2 GB suggested) on an SD card with freeMB free.
func newStoragePluginEnv(t *testing.T, freeMB int) *testEnv {
	t.Helper()
	e := newPluginEnv(t)
	e.agent.diskFree = func(string) (int64, error) { return int64(freeMB) << 20, nil }
	manifest := testPluginManifest("1.0")
	manifest["storageMb"] = 2048
	manifest["storageAdjustable"] = true
	if _, err := e.installTestPlugin(writeTestPlugin(t, manifest, nil), nil); err != nil {
		t.Fatal(err)
	}
	return e
}

// makeDataFile stands in for the data file fallocate would have made.
func makeDataFile(t *testing.T, e *testEnv, sizeMB int) string {
	t.Helper()
	file := filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage)
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(file, int64(sizeMB)<<20); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestPluginStorage_TheOwnersSizeIsUsedAndShown(t *testing.T) {
	e := newStoragePluginEnv(t, 10*1024)
	p := e.plugin("adguard-home")
	if !p.StorageAdjustable || p.StorageMB != 2048 {
		t.Errorf("plugin = adjustable %v, %d MB; want adjustable, the manifest's 2048", p.StorageAdjustable, p.StorageMB)
	}
	// Nothing used yet: from 256 MB to the free space less 2 GB.
	if p.StorageMinMB != 256 || p.StorageMaxMB != 8*1024 {
		t.Errorf("range = %d to %d MB, want 256 to 8192", p.StorageMinMB, p.StorageMaxMB)
	}

	// Growing: the data file grows while it is stopped, then it starts again.
	file := makeDataFile(t, e, 2048)
	e.runner.outputs["dumpe2fs -h "+file] = dumpe2fsOutput(2048, 100)
	runs := e.runner.count("podman run")
	p, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", StorageMB: 4096})
	if err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if p.StorageMB != 4096 {
		t.Errorf("size = %d MB, want 4096", p.StorageMB)
	}
	for _, command := range []string{"e2fsck -f -p " + file, "fallocate --length 4096M " + file, "resize2fs " + file} {
		if !e.runner.ran(command) {
			t.Errorf("growing should run %q", command)
		}
	}
	if e.runner.count("podman run") == runs {
		t.Error("it should start again after growing")
	}
	var saved config.Config
	if err := config.ReadJSON(e.paths.ConfigFile(), &saved); err != nil || saved.Plugins.Installed["adguard-home"].StorageMB != 4096 {
		t.Errorf("the size should be saved: %v", err)
	}

	// A request without a size keeps it.
	if p, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home"}); err != nil || p.StorageMB != 4096 {
		t.Errorf("without a size: %d MB, %v; want it kept at 4096", p.StorageMB, err)
	}
}

func TestPluginStorage_ShrinkingGivesSpaceBack(t *testing.T) {
	e := newStoragePluginEnv(t, 10*1024)
	file := makeDataFile(t, e, 2048)
	e.runner.outputs["dumpe2fs -h "+file] = dumpe2fsOutput(2048, 300)

	if _, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", StorageMB: 512}); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("resize2fs " + file + " 512M") {
		t.Error("the filesystem should shrink first")
	}
	if info, err := os.Stat(file); err != nil || info.Size() != 512<<20 {
		t.Errorf("the file should end at 512 MB: %v %v", info.Size(), err)
	}
}

func TestPluginStorage_Rules(t *testing.T) {
	e := newStoragePluginEnv(t, 3*1024)
	file := makeDataFile(t, e, 2048)
	e.runner.outputs["dumpe2fs -h "+file] = dumpe2fsOutput(2048, 1000)
	var invalid *config.ValidationError
	for _, c := range []struct {
		mb   int
		want string
	}{
		{100, "at least 256 MB"},
		{1024, "Its files use 1000 MB, so it needs at least 1.1 GB"},
		// 2 GB now + 3 GB free - 2 GB kept free = 3 GB at most.
		{5000, "room for at most 3 GB"},
	} {
		_, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", StorageMB: c.mb})
		if !errors.As(err, &invalid) || invalid.Field != "storageMb" || !strings.Contains(invalid.Message, c.want) {
			t.Errorf("%d MB: got %v, want %q", c.mb, err, c.want)
		}
	}
	// The size it has is always fine.
	if _, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", StorageMB: 2048}); err != nil {
		t.Errorf("the same size: %v", err)
	}
}

func TestPluginStorage_AFailedResizeKeepsTheOldSize(t *testing.T) {
	e := newStoragePluginEnv(t, 10*1024)
	file := makeDataFile(t, e, 2048)
	e.runner.failing = append(e.runner.failing, "resize2fs")
	_, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", StorageMB: 1024})
	var invalid *config.ValidationError
	if !errors.As(err, &invalid) || invalid.Field != "storageMb" || !strings.Contains(invalid.Message, "don't fit in 1 GB") {
		t.Errorf("got %v, want the files not fitting", err)
	}
	if p := e.plugin("adguard-home"); p.StorageMB != 2048 {
		t.Errorf("size = %d MB, want the old 2048", p.StorageMB)
	}
	if info, _ := os.Stat(file); info.Size() != 2048<<20 {
		t.Errorf("the file changed to %d bytes", info.Size())
	}
}

func TestPluginStorage_ChosenAtInstallAndKeptByUpdates(t *testing.T) {
	e := newPluginEnv(t)
	e.agent.diskFree = func(string) (int64, error) { return 20 << 30, nil }
	manifest := testPluginManifest("1.0")
	manifest["storageAdjustable"] = true
	preview, err := e.agent.StagePluginFile(strings.NewReader(string(writeTestPlugin(t, manifest, nil))))
	if err != nil {
		t.Fatal(err)
	}
	if !preview.StorageAdjustable || preview.StorageMB != 256 || preview.StorageMaxMB != 18*1024 {
		t.Errorf("preview = %+v", preview)
	}
	if _, err := e.agent.InstallPlugin(api.PluginInstallRequest{
		StagingID: preview.StagingID, Settings: map[string]string{"password": "s3cret-pass"}, StorageMB: 3072,
	}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if !e.runner.ran("fallocate --length 3072M") {
		t.Error("the data file should be made at the chosen size")
	}

	// An update keeps the size, whatever it asks for.
	manifest["version"] = "1.1"
	update, err := e.agent.StagePluginFile(strings.NewReader(string(writeTestPlugin(t, manifest, nil))))
	if err != nil {
		t.Fatal(err)
	}
	if update.StorageMB != 3072 {
		t.Errorf("the update's preview says %d MB, want the chosen 3072", update.StorageMB)
	}
	if _, err := e.agent.InstallPlugin(api.PluginInstallRequest{StagingID: update.StagingID, StorageMB: 512}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if p := e.plugin("adguard-home"); p.StorageMB != 3072 {
		t.Errorf("after the update: %d MB, want 3072", p.StorageMB)
	}
}

func TestPluginStorage_FixedSizesCantChange(t *testing.T) {
	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	if p := e.plugin("adguard-home"); p.StorageAdjustable || p.StorageMaxMB != 0 {
		t.Errorf("a fixed size shows no range: %+v", p.PluginStorage)
	}
	_, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", StorageMB: 1024})
	var invalid *config.ValidationError
	if !errors.As(err, &invalid) || invalid.Field != "storageMb" {
		t.Errorf("got %v, want a refusal", err)
	}
}

func TestPluginStorage_ShownOnItsOwnInSystemStorage(t *testing.T) {
	e := newStoragePluginEnv(t, 10*1024)
	makeDataFile(t, e, 64) // a sparse file takes no blocks; write some
	file := filepath.Join(e.paths.PluginDir("adguard-home"), pluginDataImage)
	if err := os.WriteFile(file, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	plugins, total := e.agent.pluginStorageUse()
	e.agent.mu.Unlock()
	if len(plugins) != 1 || plugins[0].Name != "AdGuard Home" || total < 1<<20 || plugins[0].GB < 0.001 {
		t.Errorf("plugins = %+v, total %d", plugins, total)
	}
}

func TestPluginStorage_Helpers(t *testing.T) {
	for mb, want := range map[int]string{512: "512 MB", 1024: "1 GB", 1536: "1.5 GB", 2048: "2 GB"} {
		if got := sizeText(mb); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", mb, got, want)
		}
	}
	if got := parseFilesystemUsedMB(dumpe2fsOutput(1024, 300)); got != 300 {
		t.Errorf("used = %d MB, want 300", got)
	}
	if got := parseFilesystemUsedMB("Block count: 10\n"); got != 0 {
		t.Errorf("without all the counts: %d, want 0", got)
	}
}
