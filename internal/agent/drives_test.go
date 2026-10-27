package agent

import (
	"errors"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
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

func TestDrives_APluginMountsItsDriveSoNothingOnItCanRun(t *testing.T) {
	e := newPluginEnv(t)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = usbLsblk

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
