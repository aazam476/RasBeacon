//go:build linux

package agent

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/unix"

	"rasbeacon/internal/plugin"
)

// deviceACLUsers reads a file's access list: its named users and groups,
// as "u:1179648:rw" and "g:1179648:rw".
func deviceACLUsers(t *testing.T, path string) []string {
	t.Helper()
	buf := make([]byte, 1024)
	n, err := unix.Getxattr(path, aclXattr, buf)
	if err != nil {
		t.Fatalf("%s has no access list: %v", path, err)
	}
	var named []string
	for b := buf[4:n]; len(b) >= 8; b = b[8:] {
		tag, perm, id := binary.LittleEndian.Uint16(b), binary.LittleEndian.Uint16(b[2:]), binary.LittleEndian.Uint32(b[4:])
		rw := map[uint16]string{4: "r", 2: "w", 6: "rw", 7: "rwx", 0: "-"}[perm]
		switch tag {
		case aclUser:
			named = append(named, fmt.Sprintf("u:%d:%s", id, rw))
		case aclGroup:
			named = append(named, fmt.Sprintf("g:%d:%s", id, rw))
		}
	}
	return named
}

// checkDeviceGiven fails the test unless the device at path was given to
// the user and group base by its access list.
func checkDeviceGiven(t *testing.T, path string, base int) {
	t.Helper()
	users := deviceACLUsers(t, path)
	for _, want := range []string{fmt.Sprintf("u:%d:rw", base), fmt.Sprintf("g:%d:rw", base)} {
		if !slices.Contains(users, want) {
			t.Errorf("%s's access list = %v, want %s", path, users, want)
		}
	}
}

// A device a plugin may use is given to its root (its base user and
// group) by an access list: its owner, group and mode stay, and so do
// what other plugins were given.
func TestGrantDevice(t *testing.T) {
	device := filepath.Join(t.TempDir(), "i2c-1")
	if err := os.WriteFile(device, nil, 0o660); err != nil {
		t.Fatal(err)
	}
	os.Chmod(device, 0o660)
	if err := grantDevice(device, 1179648); err != nil {
		t.Skipf("no access lists here: %v", err)
	}
	if err := grantDevice(device, 1245184); err != nil {
		t.Fatal(err)
	}
	// Again: nothing doubles.
	if err := grantDevice(device, 1179648); err != nil {
		t.Fatal(err)
	}
	got := deviceACLUsers(t, device)
	want := []string{"u:1179648:rw", "u:1245184:rw", "g:1179648:rw", "g:1245184:rw"}
	if !slices.Equal(got, want) {
		t.Errorf("access list %v, want %v", got, want)
	}
	info, _ := os.Stat(device)
	if info.Mode().Perm() != 0o660 {
		t.Errorf("mode %o: the owner and group keep theirs (the mask is rw)", info.Mode().Perm())
	}
	if err := grantDevice(filepath.Join(t.TempDir(), "missing"), 1179648); err == nil {
		t.Error("a device that isn't there can't be given")
	}
}

// The plugin's shares in its user namespace: one on internal storage is
// its own ext4 filesystem, idmapped; an encrypted one comes through
// rbquotafs, which shows its owners as the namespace sees them (gocryptfs
// makes files as the user asking, who couldn't in the stored root's
// folders); one on a USB drive too.
func TestShares_ReachThePluginsUserNamespace(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	base := plugin.UserBase("10.89.0.2")
	files := e.paths.PluginShareMount("adguard-home", "s2")
	if !e.runner.ran("--volume " + files + ":/shares/s2:idmap") {
		t.Error("a share on internal storage should be idmapped")
	}
	if e.runner.ran(e.paths.ShareQuotaDir("adguard-home", "s1") + ":/shares/s1:idmap") {
		t.Error("a FUSE view can't be idmapped")
	}
	if _, err := setEncryption(e, "s2", true, ""); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := encryption(e, "s2"); got.State != "on" {
		t.Fatalf("encryption = %+v", got)
	}
	open, view := e.paths.SharePlainDir("adguard-home", "s2"), e.paths.ShareQuotaDir("adguard-home", "s2")
	if !e.runner.ran(fmt.Sprintf("rbquotafs -limit %d -user-base %d %s %s", 512<<20, base, open, view)) {
		t.Error("an encrypted share on internal storage should come through rbquotafs, without a reservation")
	}
	if !e.runner.ran("--volume " + view + ":/shares/s2 ") {
		t.Error("the plugin should get the quota view of the encrypted share")
	}
	// Stopping lets go of it too.
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("umount " + view) {
		t.Error("stopping should unmount the encrypted share's quota view")
	}
}
