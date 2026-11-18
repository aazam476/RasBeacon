package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/fusedrive"
	"rasbeacon/internal/plugin"
)

// Plugins that keep their files on a USB drive, and small readings rbd
// takes from the system (the board, memory, uptime).

// usbLsblk is lsblk's answer with one USB stick (exFAT, "STICK").
const usbLsblk = `{"blockdevices":[
 {"name":"sda","path":"/dev/sda","size":64000000000,"fstype":null,"label":null,"uuid":null,"tran":"usb","type":"disk",
  "children":[{"name":"sda1","path":"/dev/sda1","size":64000000000,"fstype":"exfat","label":"STICK","uuid":"1234-ABCD","tran":null,"type":"part"}]}]}`

// drivePlugin is the test plugin, keeping its files on a USB drive.
func drivePlugin(t *testing.T, optional bool) []byte {
	manifest := testPluginManifest("1.0")
	manifest["usbDrive"] = map[string]any{"path": "/media", "optional": optional}
	manifest["permissions"] = []string{"internet", "usb-drive"}
	return writeTestPlugin(t, manifest, nil)
}

func installOnDrive(e *testEnv, data []byte, drive string) (api.Plugin, error) {
	e.t.Helper()
	preview, err := e.agent.StagePluginFile(strings.NewReader(string(data)))
	if err != nil {
		return api.Plugin{}, err
	}
	p, err := e.agent.InstallPlugin(api.PluginInstallRequest{
		StagingID: preview.StagingID, Drive: drive, Settings: map[string]string{"password": "s3cret-pass"},
	})
	e.runBackground()
	return p, err
}

// ntfsLsblk is usbLsblk with the stick formatted NTFS, which the kernel
// reads.
var ntfsLsblk = strings.Replace(usbLsblk, `"fstype":"exfat"`, `"fstype":"ntfs"`, 1)

func TestDrives_APluginMountsItsDriveSoNothingOnItCanRun(t *testing.T) {
	e := newPluginEnv(t)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = ntfsLsblk

	drives, err := e.agent.PluginDrives()
	if err != nil || len(drives.Drives) != 1 || drives.Drives[0].Label != "STICK" || drives.Drives[0].UsedBy != "" {
		t.Fatalf("drives = %+v, %v", drives, err)
	}

	if _, err := installOnDrive(e, drivePlugin(t, false), "1234-ABCD"); err != nil {
		t.Fatal(err)
	}
	var mount string
	for _, c := range e.runner.commands {
		if strings.HasPrefix(c, "mount -t") {
			mount = c
		}
	}
	if !strings.Contains(mount, "/dev/sda1") || !strings.Contains(mount, "noexec") || !strings.Contains(mount, "nosuid") {
		t.Errorf("mount = %q, want the stick mounted without running anything on it", mount)
	}
	// The list says who uses it now.
	if drives, _ := e.agent.PluginDrives(); drives.Drives[0].UsedBy != "AdGuard Home" {
		t.Errorf("used by = %q", drives.Drives[0].UsedBy)
	}
}

// An exFAT drive is read by a program, confined, as a user of its own
// (internal/fusedrive), not by the kernel.
func TestDrives_ExFATIsReadByItsOwnProgram(t *testing.T) {
	e := newPluginEnv(t)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = usbLsblk
	if _, err := installOnDrive(e, drivePlugin(t, false), "1234-ABCD"); err != nil {
		t.Fatal(err)
	}
	dir := e.paths.DriveMountDir("1234-ABCD")
	driver := fmt.Sprintf("fusedrive /dev/sda1 exfat %s %d", dir, fusedrive.UserBase)
	if !e.runner.ran(driver) {
		t.Errorf("the stick should be read by its program as user %d; ran %q", fusedrive.UserBase, e.runner.commands)
	}
	if e.runner.ran("mount -t exfat") {
		t.Error("the kernel should not read an exFAT drive")
	}
	if p := e.plugin("adguard-home"); p.State != api.PluginRunning {
		t.Errorf("plugin = %s %q", p.State, p.Message)
	}

	// Stopped, the drive is let go of, and its user with it.
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if len(e.agent.plugins.driverUsers) != 0 {
		t.Errorf("driver users = %v after the drive was let go of", e.agent.plugins.driverUsers)
	}

	// rbd restarted: the program ended with it, and its mount is still
	// there, useless; it is unmounted before the drive is read again.
	e.runner.mu.Lock()
	e.runner.commands = nil
	e.runner.mu.Unlock()
	if err := e.agent.StartPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	umount, read := -1, -1
	for i, c := range e.runner.commands {
		switch {
		case c == "umount "+dir && read < 0:
			umount = i
		case c == driver:
			read = i
		}
	}
	if umount < 0 || read < umount {
		t.Errorf("want umount %s before the drive is read again; ran %q", dir, e.runner.commands)
	}

	// A drive its program can't read: the plugin doesn't start, and the
	// user is free again.
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	e.runner.failing = append(e.runner.failing, "fusedrive")
	if err := e.agent.StartPlugin("adguard-home"); err == nil {
		e.runBackground()
	}
	if p := e.plugin("adguard-home"); p.State != api.PluginFailed || !strings.Contains(p.Message, "can't be opened") {
		t.Errorf("plugin = %s %q, want failed: the drive can't be opened", p.State, p.Message)
	}
	if len(e.agent.plugins.driverUsers) != 0 {
		t.Errorf("driver users = %v after the drive couldn't be read", e.agent.plugins.driverUsers)
	}
}

// Each drive's program runs as a user of its own, so one can't reach
// another drive; a user is reused once its drive is let go of.
func TestDrives_EachDrivesProgramHasItsOwnUser(t *testing.T) {
	e := newTestEnv(t)
	a := e.agent
	first, err := a.takeDriverUser("1111-AAAA")
	if err != nil || first != fusedrive.UserBase {
		t.Fatalf("first = %d, %v", first, err)
	}
	if again, _ := a.takeDriverUser("1111-AAAA"); again != first {
		t.Errorf("the same drive got %d, then %d", first, again)
	}
	second, _ := a.takeDriverUser("2222-BBBB")
	if second == first {
		t.Errorf("two drives share user %d", first)
	}
	a.freeDriverUser("1111-AAAA")
	if third, _ := a.takeDriverUser("3333-CCCC"); third != first {
		t.Errorf("the free user %d wasn't reused: got %d", first, third)
	}
	// There are only so many.
	for i := 2; i < fusedrive.Users; i++ { // two are taken
		if _, err := a.takeDriverUser(fmt.Sprintf("%04X-0000", i)); err != nil {
			t.Fatalf("drive %d: %v", i, err)
		}
	}
	if _, err := a.takeDriverUser("FFFF-FFFF"); err == nil || !strings.Contains(err.Error(), "unplug one") {
		t.Errorf("one drive too many: %v", err)
	}
}

func TestDrives_TheChosenDriveIsChecked(t *testing.T) {
	tests := []struct {
		name     string
		optional bool
		drive    string
		want     string
	}{
		{"a drive is needed", false, "", "Choose a USB drive."},
		{"a made-up ID", false, "../../etc", "Choose a USB drive from the list."},
		// An optional drive may be left out, but one that is given is
		// checked the same way.
		{"a made-up ID for an optional drive", true, "../../etc", "Choose a USB drive from the list."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPluginEnv(t)
			_, err := installOnDrive(e, drivePlugin(t, tc.optional), tc.drive)
			var verr *config.ValidationError
			if !errors.As(err, &verr) || verr.Field != "drive" || verr.Message != tc.want {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("a plugin without a drive can't be given one", func(t *testing.T) {
		e := newPluginEnv(t)
		_, err := installOnDrive(e, writeTestPlugin(t, testPluginManifest("1.0"), nil), "1234-ABCD")
		var verr *config.ValidationError
		if !errors.As(err, &verr) || verr.Field != "drive" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("an optional drive can be left out", func(t *testing.T) {
		e := newPluginEnv(t)
		if _, err := installOnDrive(e, drivePlugin(t, true), ""); err != nil {
			t.Errorf("err = %v", err)
		}
	})
}

func TestDrives_AnUnpluggedDriveStopsThePluginWithAReason(t *testing.T) {
	e := newPluginEnv(t)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = `{"blockdevices":[]}`
	if _, err := installOnDrive(e, drivePlugin(t, false), "1234-ABCD"); err != nil {
		t.Fatal(err)
	}
	if p := e.plugin("adguard-home"); p.State == api.PluginRunning || !strings.Contains(p.Message, "isn't plugged in") {
		t.Errorf("plugin = %s %q, want it stopped because its drive is missing", p.State, p.Message)
	}
}

func TestDrives_APluginRunsWithoutItsUnpluggedOptionalDrive(t *testing.T) {
	e := newPluginEnv(t)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = `{"blockdevices":[]}`
	if _, err := installOnDrive(e, drivePlugin(t, true), "1234-ABCD"); err != nil {
		t.Fatal(err)
	}
	if p := e.plugin("adguard-home"); p.State != api.PluginRunning {
		t.Errorf("plugin = %s %q, want it running without its optional drive", p.State, p.Message)
	}
	driveDir := e.paths.DriveMountDir("1234-ABCD")
	for _, c := range e.runner.commands {
		if strings.HasPrefix(c, "podman run") && strings.Contains(c, driveDir) {
			t.Errorf("the unplugged drive must not be given to the container: %s", c)
		}
	}
}

func TestReadings_BoardMemoryAndUptime(t *testing.T) {
	boards := map[string]string{
		"Raspberry Pi 5 Model B Rev 1.0\x00":      "pi5",
		"Raspberry Pi 4 Model B Rev 1.4\x00":      "pi4",
		"Raspberry Pi Compute Module 4 Rev 1.0":   "pi4",
		"Raspberry Pi Compute Module 5 Rev 1.0":   "pi5",
		"Raspberry Pi Zero 2 W Rev 1.0\x00":       "zero2w",
		"Raspberry Pi 3 Model B Plus Rev 1.3\x00": "pi3",
		"Some other board":                        "",
	}
	for model, want := range boards {
		e := newTestEnv(t)
		mustWrite(t, e.paths.DeviceTreeModel(), model)
		if got := e.agent.pluginBoard(); got != want {
			t.Errorf("pluginBoard(%q) = %q, want %q", model, got, want)
		}
	}

	e := newTestEnv(t)
	if memory, swap := e.agent.memoryMB(); memory != 0 || swap != 0 {
		t.Errorf("without /proc/meminfo: %d, %d", memory, swap)
	}
	mustWrite(t, e.paths.ProcMeminfo(), "MemTotal:        1024000 kB\nMemAvailable:     512000 kB\nSwapTotal:       2048000 kB\nSwapFree:        2048000 kB\n")
	if memory, swap := e.agent.memoryMB(); memory != 1000 || swap != 2000 {
		t.Errorf("memoryMB() = %d, %d, want 1000, 2000", memory, swap)
	}

	if got := e.agent.uptimeSeconds(); got != 0 {
		t.Errorf("without /proc/uptime: %d", got)
	}
	mustWrite(t, e.paths.ProcUptime(), "12345.67 45678.90\n")
	if got := e.agent.uptimeSeconds(); got != 12345 {
		t.Errorf("uptimeSeconds() = %d, want 12345", got)
	}
}
