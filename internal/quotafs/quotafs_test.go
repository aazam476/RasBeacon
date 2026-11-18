//go:build linux

package quotafs

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestQuota_Counts(t *testing.T) {
	q := NewQuota(100, 200)
	if !q.Reserve(100) || q.Used() != 200 || !q.Full() {
		t.Errorf("used = %d, full %v", q.Used(), q.Full())
	}
	if q.Reserve(1) {
		t.Error("nothing more fits")
	}
	if !q.Reserve(-50) || q.Used() != 150 {
		t.Errorf("freeing always fits: used = %d", q.Used())
	}
	q.Add(-1000)
	if q.Used() != 0 {
		t.Errorf("the count never goes below 0: %d", q.Used())
	}
}

func TestFolderSize(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "x"), make([]byte, 300), 0o644)
	os.WriteFile(filepath.Join(dir, "y"), make([]byte, 200), 0o644)
	if got := FolderSize(dir); got != 500 {
		t.Errorf("size = %d, want 500", got)
	}
}

// mounted mounts a share of 1 MiB over a folder holding 200 KiB already,
// for real (it needs /dev/fuse and root, so it is skipped elsewhere).
func mounted(t *testing.T) (source, view string, q *Quota) {
	t.Helper()
	if _, err := os.Stat("/dev/fuse"); err != nil || os.Geteuid() != 0 {
		t.Skip("needs /dev/fuse and root")
	}
	source, view = t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "old.bin"), make([]byte, 200<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	q = NewQuota(FolderSize(source), 1<<20)
	server, err := Mount(source, view, q, false, 0)
	if err != nil {
		t.Skipf("can't mount here: %v", err)
	}
	t.Cleanup(func() { _ = server.Unmount() })
	return source, view, q
}

func TestMount_HoldsTheShareToItsQuota(t *testing.T) {
	source, view, q := mounted(t)

	// 700 KiB more fit (900 of 1024).
	if err := os.WriteFile(filepath.Join(view, "a.bin"), make([]byte, 700<<10), 0o644); err != nil {
		t.Fatalf("writing what fits: %v", err)
	}
	// 200 KiB more don't: the copy fails with "no space left".
	err := os.WriteFile(filepath.Join(view, "b.bin"), make([]byte, 200<<10), 0o644)
	if !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("writing past the quota: %v, want no space left", err)
	}
	if q.Used() > 1<<20 {
		t.Errorf("used = %d, past the quota", q.Used())
	}
	// Filled up to the quota: no new file, folder or link.
	_ = os.Remove(filepath.Join(view, "b.bin"))
	f, err := os.OpenFile(filepath.Join(view, "a.bin"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(make([]byte, 124<<10))
	f.Close()
	if !q.Full() {
		t.Fatalf("used = %d, want it full", q.Used())
	}
	if _, err := os.Create(filepath.Join(view, "new.txt")); !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("a new file when full: %v", err)
	}
	if err := os.Mkdir(filepath.Join(view, "folder"), 0o755); !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("a new folder when full: %v", err)
	}
	if err := os.Symlink("a.bin", filepath.Join(view, "link")); !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("a new link when full: %v", err)
	}
	// Still: reading, changing bytes in place, and growing a file is refused.
	if data, err := os.ReadFile(filepath.Join(view, "old.bin")); err != nil || len(data) != 200<<10 {
		t.Errorf("reading when full: %d bytes, %v", len(data), err)
	}
	f, err = os.OpenFile(filepath.Join(view, "old.bin"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("hello"), 10); err != nil {
		t.Errorf("changing bytes in place when full: %v", err)
	}
	if err := f.Truncate(300 << 10); !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("growing a file when full: %v", err)
	}
	f.Close()
	if err := os.Truncate(filepath.Join(view, "old.bin"), 400<<10); !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("growing a file by its path when full: %v", err)
	}

	// Deleting makes room again, and the room is used.
	if err := os.Remove(filepath.Join(view, "old.bin")); err != nil {
		t.Fatalf("deleting when full: %v", err)
	}
	if q.Full() {
		t.Error("deleting should make room")
	}
	if err := os.WriteFile(filepath.Join(view, "c.bin"), make([]byte, 100<<10), 0o644); err != nil {
		t.Errorf("writing after deleting: %v", err)
	}
	// Shrinking frees too; a file replaced by a rename is freed.
	before := q.Used()
	if err := os.Truncate(filepath.Join(view, "a.bin"), 0); err != nil {
		t.Fatal(err)
	}
	if q.Used() >= before {
		t.Errorf("shrinking should free: %d then %d", before, q.Used())
	}
	before = q.Used()
	if err := os.Rename(filepath.Join(view, "a.bin"), filepath.Join(view, "c.bin")); err != nil {
		t.Fatal(err)
	}
	if q.Used() != before-100<<10 {
		t.Errorf("replacing c.bin should free its 100 KiB: %d then %d", before, q.Used())
	}
	// The files are really on the drive.
	data, err := os.ReadFile(filepath.Join(source, "c.bin"))
	if err != nil || !bytes.Equal(data, nil) && len(data) != 0 {
		t.Errorf("c.bin on the drive: %d bytes, %v", len(data), err)
	}
}

// A plugin's container runs in a user namespace of its own (see
// owners.go): the view shows the drive's owners as the container's host
// users, and stores what the container makes, and the owners it sets,
// with the container's own numbers, as the drive always had them.
func TestMount_ShowsOwnersAsThePluginsUserNamespaceSeesThem(t *testing.T) {
	if _, err := os.Stat("/dev/fuse"); err != nil || os.Geteuid() != 0 {
		t.Skip("needs /dev/fuse and root")
	}
	const base = 0x120000
	// Folders the container's users can reach (t.TempDir's aren't).
	top, err := os.MkdirTemp("", "owners")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(top) })
	source, view := filepath.Join(top, "drive"), filepath.Join(top, "view")
	for _, dir := range []string{top, source, view} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		os.Chmod(dir, 0o755)
	}
	old := filepath.Join(source, "old.txt")
	if err := os.WriteFile(old, []byte("from before"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Lchown(old, 7, 7)
	server, err := Mount(source, view, NewQuota(0, 1<<20), true, base)
	if err != nil {
		t.Skipf("can't mount here: %v", err)
	}
	t.Cleanup(func() { _ = server.Unmount() })

	owner := func(path string) uint32 {
		t.Helper()
		var st syscall.Stat_t
		if err := syscall.Lstat(path, &st); err != nil {
			t.Fatal(err)
		}
		return st.Uid
	}
	// The drive's 7 is the container's 7: base+7 on the host.
	if got := owner(filepath.Join(view, "old.txt")); got != base+7 {
		t.Errorf("old.txt in the view: owner %d, want %d", got, base+7)
	}
	// What the container's user 1000 makes is stored as 1000.
	made := exec.Command("/bin/sh", "-c", "echo hi > made.txt && mkdir folder && ln -s made.txt link")
	made.Dir = view
	made.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: base + 1000, Gid: base + 1000}}
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("the container's user can't make files: %v\n%s", err, out)
	}
	for _, name := range []string{"made.txt", "folder", "link"} {
		if got := owner(filepath.Join(source, name)); got != 1000 {
			t.Errorf("%s on the drive: owner %d, want the container's 1000", name, got)
		}
		if got := owner(filepath.Join(view, name)); got != base+1000 {
			t.Errorf("%s in the view: owner %d, want %d", name, got, base+1000)
		}
	}
	// An owner set in the view (the container's 5) is stored as 5.
	if err := os.Lchown(filepath.Join(view, "made.txt"), base+5, base+5); err != nil {
		t.Fatal(err)
	}
	if got := owner(filepath.Join(source, "made.txt")); got != 5 {
		t.Errorf("after chown in the view: owner %d on the drive, want 5", got)
	}
	// The Pi's own users are no users of the namespace: the drive's 0 is
	// the container's root, and what the Pi's root makes stays 0.
	if err := os.WriteFile(filepath.Join(view, "by-root.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if owner(filepath.Join(source, "by-root.txt")) != 0 || owner(filepath.Join(view, "by-root.txt")) != base {
		t.Error("the Pi's root makes files the container's root owns")
	}
}
