// Package quotafs holds a USB share to its quota, strictly. A share on a
// USB drive is a folder on the drive; nothing sets space aside for it, so
// the plugin (Samba) would only report the share as full, which Finder and
// File Explorer respect but a program writing directly doesn't have to.
//
// quotafs is a FUSE file system that shows the folder as it is (a
// "loopback" of it) and counts what its files hold: once that reaches the
// quota, it refuses anything that would add more (writing past a file's
// end, a new file, folder or link) with "no space left on device", and
// still lets everything else happen: reading, changing a file's bytes in
// place, renaming, and deleting, which makes room again. rbd runs one for
// each share on a USB drive (cmd/rbquotafs) and gives the plugin its view.
//
// Files are counted by their sizes. What they were when it started is
// added up from the folder, so files added or deleted on a computer while
// the drive was out count.
package quotafs

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	gofs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// Quota is how much a share may hold and what its files hold now, in
// bytes. It is shared by every file and folder of one mount.
type Quota struct {
	mu    sync.Mutex
	used  int64
	limit int64
}

// NewQuota starts the count at used bytes, out of limit.
func NewQuota(used, limit int64) *Quota {
	return &Quota{used: used, limit: limit}
}

// Used is what the files hold now.
func (q *Quota) Used() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.used
}

// Full reports whether nothing more fits: new files are refused.
func (q *Quota) Full() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.used >= q.limit
}

// Reserve takes room for bytes more, if it fits; a change that frees room
// (bytes below 0) always does.
func (q *Quota) Reserve(bytes int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if bytes > 0 && q.used+bytes > q.limit {
		return false
	}
	q.used += bytes
	return true
}

// Add counts bytes more (or fewer, below 0) without checking: for what
// already happened, such as a deleted file, or a write that came out
// shorter than reserved.
func (q *Quota) Add(bytes int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.used = max(0, q.used+bytes)
}

// FolderSize adds up the sizes of the files under dir (what NewQuota
// starts from).
func FolderSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// Mount shows source at mountpoint, held to q, until it is unmounted
// (Wait on the server). allowOther lets other users, the plugin's
// container among them, use it.
func Mount(source, mountpoint string, q *Quota, allowOther bool) (*fuse.Server, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(source, &st); err != nil {
		return nil, err
	}
	root := &gofs.LoopbackRoot{Path: source, Dev: uint64(st.Dev)}
	node := &node{LoopbackNode: &gofs.LoopbackNode{RootData: root}, quota: q}
	root.RootNode = node
	return gofs.Mount(mountpoint, node, &gofs.Options{
		MountOptions: fuse.MountOptions{AllowOther: allowOther, FsName: source, Name: "rbquota"},
	})
}

// node is a file or folder of the share: go-fuse's loopback, with what
// adds to the share checked against the quota.
type node struct {
	*gofs.LoopbackNode
	quota *Quota
}

// WrapChild makes every file and folder below the root a node too.
func (n *node) WrapChild(_ context.Context, ops gofs.InodeEmbedder) gofs.InodeEmbedder {
	return &node{LoopbackNode: ops.(*gofs.LoopbackNode), quota: n.quota}
}

// sizeOf is the size of the regular file name in this folder (0 for
// anything else, or a file with other names too: deleting one name of it
// frees nothing).
func (n *node) sizeOf(name string) int64 {
	var st syscall.Stat_t
	if err := syscall.Lstat(filepath.Join(n.path(), name), &st); err != nil {
		return 0
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Nlink > 1 {
		return 0
	}
	return st.Size
}

// path is this node's path on the drive.
func (n *node) path() string {
	return filepath.Join(n.RootData.Path, n.Path(n.Root()))
}

// Create makes a new file, unless the share is full, and counts what is
// written to it.
func (n *node) Create(ctx context.Context, name string, flags, mode uint32, out *fuse.EntryOut) (*gofs.Inode, gofs.FileHandle, uint32, syscall.Errno) {
	if n.quota.Full() {
		return nil, nil, 0, syscall.ENOSPC
	}
	inode, fh, fuseFlags, errno := n.LoopbackNode.Create(ctx, name, flags, mode, out)
	return inode, wrapFile(fh, n.quota), fuseFlags, errno
}

// Open opens a file; what is written to it is counted.
func (n *node) Open(ctx context.Context, flags uint32) (gofs.FileHandle, uint32, syscall.Errno) {
	fh, fuseFlags, errno := n.LoopbackNode.Open(ctx, flags)
	return wrapFile(fh, n.quota), fuseFlags, errno
}

// Mkdir makes a folder, unless the share is full.
func (n *node) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*gofs.Inode, syscall.Errno) {
	if n.quota.Full() {
		return nil, syscall.ENOSPC
	}
	return n.LoopbackNode.Mkdir(ctx, name, mode, out)
}

// Mknod makes a special file, unless the share is full.
func (n *node) Mknod(ctx context.Context, name string, mode, rdev uint32, out *fuse.EntryOut) (*gofs.Inode, syscall.Errno) {
	if n.quota.Full() {
		return nil, syscall.ENOSPC
	}
	return n.LoopbackNode.Mknod(ctx, name, mode, rdev, out)
}

// Symlink makes a link, unless the share is full.
func (n *node) Symlink(ctx context.Context, target, name string, out *fuse.EntryOut) (*gofs.Inode, syscall.Errno) {
	if n.quota.Full() {
		return nil, syscall.ENOSPC
	}
	return n.LoopbackNode.Symlink(ctx, target, name, out)
}

// Link gives a file another name, unless the share is full.
func (n *node) Link(ctx context.Context, target gofs.InodeEmbedder, name string, out *fuse.EntryOut) (*gofs.Inode, syscall.Errno) {
	if n.quota.Full() {
		return nil, syscall.ENOSPC
	}
	return n.LoopbackNode.Link(ctx, target, name, out)
}

// Unlink deletes a file, and frees its size.
func (n *node) Unlink(ctx context.Context, name string) syscall.Errno {
	size := n.sizeOf(name)
	errno := n.LoopbackNode.Unlink(ctx, name)
	if errno == 0 {
		n.quota.Add(-size)
	}
	return errno
}

// Rename moves a file; one it replaces is freed.
func (n *node) Rename(ctx context.Context, name string, newParent gofs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	replaced := int64(0)
	if target, ok := newParent.(*node); ok && !(target == n && name == newName) {
		replaced = target.sizeOf(newName)
	}
	errno := n.LoopbackNode.Rename(ctx, name, newParent, newName, flags)
	if errno == 0 {
		n.quota.Add(-replaced)
	}
	return errno
}

// Setattr changes a file's attributes; a new size is checked and counted
// (through the open file, when there is one).
func (n *node) Setattr(ctx context.Context, f gofs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	size, resizing := in.GetSize()
	if _, open := f.(*file); open || !resizing {
		return n.LoopbackNode.Setattr(ctx, f, in, out)
	}
	before := int64(0)
	if info, err := os.Lstat(n.path()); err == nil {
		before = info.Size()
	}
	if !n.quota.Reserve(int64(size) - before) {
		return syscall.ENOSPC
	}
	errno := n.LoopbackNode.Setattr(ctx, f, in, out)
	if errno != 0 {
		n.quota.Add(before - int64(size))
	}
	return errno
}

// file is an open file of the share: go-fuse's loopback file, with what
// makes it bigger checked against the quota.
type file struct {
	*gofs.LoopbackFile
	quota *Quota
}

// wrapFile makes an opened file count its growth (nil stays nil).
func wrapFile(fh gofs.FileHandle, q *Quota) gofs.FileHandle {
	if loop, ok := fh.(*gofs.LoopbackFile); ok {
		return &file{LoopbackFile: loop, quota: q}
	}
	return fh
}

// size is the file's size now (0 if it can't be read).
func (f *file) size(ctx context.Context) int64 {
	var attr fuse.AttrOut
	if f.LoopbackFile.Getattr(ctx, &attr) != 0 {
		return 0
	}
	return int64(attr.Size)
}

// PassthroughFd says no: the kernel would write to the file directly, and
// the quota would never see it.
func (f *file) PassthroughFd() (int, bool) {
	return -1, false
}

// grow reserves the room a change from off to end needs past the file's
// end, runs it, and gives back what it didn't use. done reports how far
// it got (end when it all happened).
func (f *file) grow(ctx context.Context, off, end int64, change func() (done int64, errno syscall.Errno)) syscall.Errno {
	before := f.size(ctx)
	reserved := max(0, end-before)
	if !f.quota.Reserve(reserved) {
		return syscall.ENOSPC
	}
	done, errno := change()
	f.quota.Add(max(0, done-before) - reserved)
	return errno
}

// Write writes, if what goes past the file's end fits.
func (f *file) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	var written uint32
	errno := f.grow(ctx, off, off+int64(len(data)), func() (int64, syscall.Errno) {
		n, errno := f.LoopbackFile.Write(ctx, data, off)
		written = n
		return off + int64(n), errno
	})
	return written, errno
}

// Writev writes several pieces in one go, by the same rule.
func (f *file) Writev(ctx context.Context, data [][]byte, off int64) (uint32, syscall.Errno) {
	length := int64(0)
	for _, piece := range data {
		length += int64(len(piece))
	}
	var written uint32
	errno := f.grow(ctx, off, off+length, func() (int64, syscall.Errno) {
		n, errno := f.LoopbackFile.Writev(ctx, data, off)
		written = n
		return off + int64(n), errno
	})
	return written, errno
}

// Allocate sets space aside for the file, if it fits.
func (f *file) Allocate(ctx context.Context, off, size uint64, mode uint32) syscall.Errno {
	return f.grow(ctx, int64(off), int64(off+size), func() (int64, syscall.Errno) {
		errno := f.LoopbackFile.Allocate(ctx, off, size, mode)
		if errno != 0 {
			return 0, errno
		}
		return f.size(ctx), 0
	})
}

// Setattr changes the file's attributes; a bigger size is checked first.
func (f *file) Setattr(ctx context.Context, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	size, resizing := in.GetSize()
	if !resizing {
		return f.LoopbackFile.Setattr(ctx, in, out)
	}
	before := f.size(ctx)
	if !f.quota.Reserve(int64(size) - before) {
		return syscall.ENOSPC
	}
	errno := f.LoopbackFile.Setattr(ctx, in, out)
	if errno != 0 {
		f.quota.Add(before - int64(size))
	}
	return errno
}
