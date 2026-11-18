package netns

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"rasbeacon/internal/fusedrive"
)

// requireDriveTools skips (or fails, in CI) without what the drive test
// needs: root, losetup, and exFAT's tools and driver.
func requireDriveTools(t *testing.T) {
	t.Helper()
	requireRoot(t)
	var missing []string
	for _, tool := range []string{"losetup", "mkfs.exfat", "/usr/sbin/mount.exfat-fuse"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) == 0 {
		return
	}
	if os.Getenv("RASBEACON_REQUIRE_NETNS") == "1" {
		t.Fatalf("the drive test cannot run: missing %s", strings.Join(missing, ", "))
	}
	t.Skipf("skipping the drive test: missing %s", strings.Join(missing, ", "))
}

// loopDrive makes a drive of 64 MB from a file, formatted by mkfs (nil:
// not formatted), and returns its device.
func loopDrive(t *testing.T, mkfs ...string) string {
	t.Helper()
	image := filepath.Join(t.TempDir(), "drive.img")
	if err := os.WriteFile(image, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(image, 64<<20); err != nil {
		t.Fatal(err)
	}
	if len(mkfs) > 0 {
		run(t, mkfs[0], append(mkfs[1:], image)...)
	}
	device := strings.TrimSpace(run(t, "losetup", "--find", "--show", image))
	t.Cleanup(func() { _ = exec.Command("losetup", "--detach", device).Run() })
	return device
}

// driverOf finds the process running as user, and its status lines.
func driverOf(t *testing.T, user int) (pid string, status map[string]string) {
	t.Helper()
	uid := strconv.Itoa(user)
	procs, _ := filepath.Glob("/proc/[0-9]*/status")
	for _, path := range procs {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fields := map[string]string{}
		for line := range strings.SplitSeq(string(data), "\n") {
			if name, value, ok := strings.Cut(line, ":"); ok {
				fields[name] = strings.TrimSpace(value)
			}
		}
		if strings.Fields(fields["Uid"]) != nil && strings.Fields(fields["Uid"])[0] == uid {
			return filepath.Base(filepath.Dir(path)), fields
		}
	}
	return "", nil
}

// mountOptions are the options and type /proc/mounts lists for dir ("" if
// nothing is mounted there).
func mountOptions(t *testing.T, dir string) (fsType, options string) {
	t.Helper()
	data, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if f := strings.Fields(line); len(f) >= 4 && f[1] == dir {
			fsType, options = f[2], f[3]
		}
	}
	return fsType, options
}

func TestDrives_ExFATIsReadByAConfinedProgram(t *testing.T) {
	requireDriveTools(t)
	device := loopDrive(t, "mkfs.exfat", "-L", "STICK")
	dir := t.TempDir()
	user := fusedrive.UserBase + 7
	if err := fusedrive.Mount(device, "exfat", dir, user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Unmount(dir, unix.MNT_DETACH) })

	// Mounted like the drives the kernel reads.
	fsType, options := mountOptions(t, dir)
	if fsType != "fuse.exfat" {
		t.Fatalf("mounted as %q", fsType)
	}
	for _, want := range []string{"nosuid", "nodev", "noexec", "nosymfollow", "allow_other", "default_permissions"} {
		if !strings.Contains(","+options+",", ","+want+",") {
			t.Errorf("mount options %q, without %s", options, want)
		}
	}

	// Its files work, and are open to everyone (as the plugins' users are
	// their own).
	if err := os.MkdirAll(filepath.Join(dir, "Movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "Movies", "film.txt")
	if err := os.WriteFile(file, []byte("on the drive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "on the drive" {
		t.Errorf("read back %q, %v", data, err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o777 {
		t.Errorf("file mode = %v, %v", info.Mode(), err)
	}

	// The driver: its own host user, no groups, no capabilities, no network
	// or user namespace of the Pi's.
	pid, status := driverOf(t, user)
	if pid == "" {
		t.Fatalf("no driver runs as user %d", user)
	}
	if status["Groups"] != "" || status["CapEff"] != "0000000000000000" || status["CapPrm"] != "0000000000000000" {
		t.Errorf("driver's groups %q, capabilities %s/%s", status["Groups"], status["CapPrm"], status["CapEff"])
	}
	if gid := strings.Fields(status["Gid"]); len(gid) == 0 || gid[0] != strconv.Itoa(user) {
		t.Errorf("driver's group = %q", status["Gid"])
	}
	for _, ns := range []string{"net", "user", "ipc"} {
		mine, _ := os.Readlink("/proc/self/ns/" + ns)
		its, _ := os.Readlink("/proc/" + pid + "/ns/" + ns)
		if its == "" || its == mine {
			t.Errorf("the driver's %s namespace is the Pi's (%q)", ns, its)
		}
	}
	// It opened the drive; the node it opened it by is gone.
	if _, err := os.Lstat(filepath.Join(fusedrive.NodeDir, strconv.Itoa(user))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the driver's device node is still there: %v", err)
	}

	// Unmounting ends it.
	if err := unix.Unmount(dir, 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for pid, _ := driverOf(t, user); pid != ""; pid, _ = driverOf(t, user) {
		if time.Now().After(deadline) {
			t.Fatal("the driver still runs after its drive was unmounted")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDrives_ADriveTheDriverCantReadIsntLeftMounted(t *testing.T) {
	requireDriveTools(t)
	device := loopDrive(t) // no filesystem on it
	dir := t.TempDir()
	user := fusedrive.UserBase + 8
	err := fusedrive.Mount(device, "exfat", dir, user)
	if err == nil {
		_ = unix.Unmount(dir, unix.MNT_DETACH)
		t.Fatal("a drive without a filesystem was mounted")
	}
	if !strings.Contains(err.Error(), "its driver can't read it") {
		t.Errorf("error = %v", err)
	}
	if fsType, _ := mountOptions(t, dir); fsType != "" {
		t.Errorf("%s is left mounted at the folder", fsType)
	}
	if _, err := os.Lstat(filepath.Join(fusedrive.NodeDir, strconv.Itoa(user))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the driver's device node is still there: %v", err)
	}
	if pid, _ := driverOf(t, user); pid != "" {
		t.Errorf("a driver runs: process %s", pid)
	}
}
