package agent

// Plugins' users: each plugin's container runs in a user namespace of its
// own (see plugin.UserBase), so that a program breaking out of it is a
// user of no importance on the Pi rather than root. Its users and groups
// 0 to 65535 are the host's from its base on.
//
// The files it had before keep their owners as stored, 0 for its root:
//
//   - its data and its shares on internal storage are ext4, mounted into
//     the container idmapped, which shows the stored owners to it as its
//     own;
//   - a whole USB drive (an older plugin's "usbDrive") is mounted idmapped
//     when its filesystem can be (ext4, btrfs); FAT, exFAT and NTFS drives
//     are mounted with every file open to everyone (see
//     plugin.MountOptions), so their owners don't matter;
//   - shares on USB drives, and encrypted shares, reach it through
//     rbquotafs, which shows and stores owners the same way itself (see
//     quotafs/owners.go): FUSE can't be idmapped, and gocryptfs makes files
//     as the user asking, who couldn't in folders the stored root owns.
//
// The devices it may use (I2C, GPIO) are root's: its base user and group
// are given them by an access list (grantDevice).

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"syscall"

	"golang.org/x/sys/unix"
)

// idmapCapable reports whether the filesystem mounted at path can be
// mounted idmapped into a container: the ones RasBeacon mounts that the
// kernel can idmap (not FAT, exFAT, NTFS or FUSE, whatever the kernel).
func idmapCapable(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	switch st.Type {
	case unix.EXT4_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC:
		return true
	}
	return false
}

// A POSIX access list as the kernel stores it in the extended attribute
// system.posix_acl_access: a version, then entries of a tag, permissions
// and a user or group ID (see acl(5)).
const (
	aclXattr   = "system.posix_acl_access"
	aclVersion = 2

	aclUserObj  = 0x01
	aclUser     = 0x02
	aclGroupObj = 0x04
	aclGroup    = 0x08
	aclMask     = 0x10
	aclOther    = 0x20

	aclNoID = 0xffffffff // the entries for the owner, mask and others
)

// aclEntry is one entry of an access list.
type aclEntry struct {
	tag, perm uint16
	id        uint32
}

// grantDevice lets the user and group base (a plugin's root, see
// plugin.UserBase) read and write the device at path, by an access list
// next to its owner, group and mode, which stay as they are. Entries for
// others' bases, from earlier plugins, are kept.
func grantDevice(path string, base int) error {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return err
	}
	entries := []aclEntry{
		{aclUserObj, uint16(st.Mode>>6) & 7, aclNoID},
		{aclGroupObj, uint16(st.Mode>>3) & 7, aclNoID},
		{aclOther, uint16(st.Mode) & 7, aclNoID},
	}
	// The access list it has, if any: its named users and groups stay.
	buf := make([]byte, 1024)
	if n, err := unix.Getxattr(path, aclXattr, buf); err == nil && n >= 4 && binary.LittleEndian.Uint32(buf) == aclVersion {
		for b := buf[4:n]; len(b) >= 8; b = b[8:] {
			e := aclEntry{binary.LittleEndian.Uint16(b), binary.LittleEndian.Uint16(b[2:]), binary.LittleEndian.Uint32(b[4:])}
			if (e.tag == aclUser || e.tag == aclGroup) && e.id != uint32(base) {
				entries = append(entries, e)
			}
		}
	}
	const readWrite = 4 | 2
	entries = append(entries, aclEntry{aclUser, readWrite, uint32(base)}, aclEntry{aclGroup, readWrite, uint32(base)})
	// The mask is what the named entries and the group may have.
	mask := uint16(0)
	for _, e := range entries {
		if e.tag == aclGroupObj || e.tag == aclUser || e.tag == aclGroup {
			mask |= e.perm
		}
	}
	entries = append(entries, aclEntry{aclMask, mask, aclNoID})
	// The kernel wants them by tag, then ID.
	slices.SortFunc(entries, func(a, b aclEntry) int {
		return cmp.Or(cmp.Compare(a.tag, b.tag), cmp.Compare(a.id, b.id))
	})
	data := binary.LittleEndian.AppendUint32(nil, aclVersion)
	for _, e := range entries {
		data = binary.LittleEndian.AppendUint16(data, e.tag)
		data = binary.LittleEndian.AppendUint16(data, e.perm)
		data = binary.LittleEndian.AppendUint32(data, e.id)
	}
	if err := unix.Setxattr(path, aclXattr, data, 0); err != nil {
		if errors.Is(err, syscall.EOPNOTSUPP) {
			return fmt.Errorf("%s can't have an access list here", path)
		}
		return err
	}
	return nil
}
