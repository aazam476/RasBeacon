//go:build linux

package quotafs

// Owners in a plugin's user namespace.
//
// A plugin's container runs in a user namespace of its own: its users 0
// to 65535 (root included) are the host's users base+0 to base+65535 (see
// plugin.UserBase), so that a program that breaks out of it is nobody on
// the Pi. Files keep the container's own numbers on the drive, 0 for its
// root, as they always had, and as the kernel's idmapped mounts show them
// for the plugin's other folders: the view shows them shifted to the
// container's host users, and stores what the container makes, or the
// owners it sets, shifted back.
//
// rbquotafs does the work itself because FUSE can't be idmapped, and a
// file the container doesn't own looks like nobody's to it.

import (
	"context"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fuse"
)

// userRange is how many user (and group) IDs a container has.
const userRange = 1 << 16

// owners shifts file owners between the drive and the view. A base of 0
// shifts nothing (a plugin without a user namespace).
type owners struct{ base uint32 }

// view is an owner as the view shows it: the drive's 0 to 65535 as the
// container's host users.
func (o owners) view(id uint32) uint32 {
	if o.base == 0 || id >= userRange {
		return id
	}
	return id + o.base
}

// disk is an owner as the drive stores it: the container's host users as
// 0 to 65535. Others (the Pi's own root, say) stay as they are.
func (o owners) disk(id uint32) uint32 {
	if o.base == 0 || id < o.base || id-o.base >= userRange {
		return id
	}
	return id - o.base
}

// showAttr shifts attributes read from the drive for the view.
func (o owners) showAttr(a *fuse.Attr) {
	a.Uid, a.Gid = o.view(a.Uid), o.view(a.Gid)
}

// showStatx shifts extended attributes read from the drive for the view.
func (o owners) showStatx(s *fuse.Statx) {
	s.Uid, s.Gid = o.view(s.Uid), o.view(s.Gid)
}

// storeSetattr shifts the owner a change of attributes sets (chown) back
// for the drive.
func (o owners) storeSetattr(in *fuse.SetAttrIn) {
	if uid, ok := in.GetUID(); ok {
		in.Uid = o.disk(uid)
	}
	if gid, ok := in.GetGID(); ok {
		in.Gid = o.disk(gid)
	}
}

// storeNew gives what the caller just made at path the owner the drive
// stores: go-fuse's loopback made it the caller's own (the host's IDs).
// The view's attributes of it stay as they are: the caller's own.
func (o owners) storeNew(ctx context.Context, path string) {
	caller, ok := fuse.FromContext(ctx)
	if !ok || o.base == 0 {
		return
	}
	uid, gid := o.disk(caller.Uid), o.disk(caller.Gid)
	if uid != caller.Uid || gid != caller.Gid {
		_ = syscall.Lchown(path, int(uid), int(gid))
	}
}
