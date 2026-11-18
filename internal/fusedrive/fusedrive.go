// Package fusedrive mounts a USB drive with a program that reads its
// filesystem (a FUSE driver: exfat-fuse for exFAT) instead of the kernel's
// driver, and confines that program.
//
// A drive comes from anywhere, and its filesystem is whatever a computer
// wrote there, or what someone made to break the code that reads it. A
// flaw in the kernel's driver is the whole Pi's; in a program it is that
// program's, so the program gets as little as it needs:
//
//   - its own host user (UserBase on, one per drive mounted at once), which
//     is nobody on the Pi: an ordinary user of a user namespace of its
//     own, without groups or capabilities, that reads only what everyone
//     may read (not RasBeacon's secrets) and writes nothing of the Pi's;
//   - no network (a network namespace without interfaces), and its own
//     IPC and host name;
//   - the drive through a device node only its user may open, made for it
//     and removed once it has opened the drive, and /dev/fuse opened and
//     mounted by rbd, which hands it over (libfuse's /dev/fd/N), so the
//     program needs no privilege to mount;
//   - a mount with nosuid, nodev, noexec and nosymfollow, like the drives
//     the kernel reads (see plugin.MountOptions).
//
// The image's exfat-fuse is RasBeacon's build (image/exfat-fuse): Debian's,
// with a patch so files grow without writing zeros, as the kernel's driver
// grows them. A share's reservation on a drive is then taken at once.
//
// NTFS stays with the kernel (ntfs3): Debian's ntfs-3g opens /dev/fuse
// and mounts by itself, which only root may, and built with libfuse it
// refuses block devices to anyone but root.
package fusedrive

import (
	"fmt"
	"strings"
)

const (
	// Users is how many drives can be mounted this way at once.
	Users = 64
	// UserBase is the first driver's host user: right below the plugins'
	// users (plugin.UserBase starts above 1<<20), and far above the
	// Pi's own.
	UserBase = 1<<20 - Users
	// NodeDir holds the device node a driver opens the drive with while it
	// starts. It is on /dev: other filesystems are mounted nodev.
	NodeDir = "/dev/rasbeacon-drives"
	// namespaceUser is the driver's user inside its user namespace. Not
	// root, which has every capability there; exfat-fuse looks up its
	// user's name, and user 1 is "daemon" on every Debian system.
	namespaceUser = 1
)

// drivers are the programs that read each kind of filesystem, by the
// name lsblk gives it.
var drivers = map[string]string{"exfat": "/usr/sbin/mount.exfat-fuse"}

// Handles reports whether drives with the filesystem fsType (as lsblk
// names it) are mounted by a program rather than the kernel.
func Handles(fsType string) bool {
	_, ok := drivers[fsType]
	return ok
}

// driverArgs are the driver's arguments: every file shows as root's and
// open to everyone, as on the FAT drives the kernel reads (see
// plugin.MountOptions); the drive is read from node, and served on the
// /dev/fuse rbd mounted, its file descriptor 3.
func driverArgs(node string) []string {
	return []string{"-o", "uid=0,gid=0,umask=0000", node, "/dev/fd/3"}
}

// checkUser refuses a user outside the drivers' own.
func checkUser(user int) error {
	if user < UserBase || user >= UserBase+Users {
		return fmt.Errorf("user %d isn't one of the drive drivers' users", user)
	}
	return nil
}

// driverMessage is what the driver said when it couldn't start: its
// errors (exfat-fuse starts them with "ERROR: "), or what happened to it.
func driverMessage(output []byte, err error) string {
	var said []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if text, ok := strings.CutPrefix(strings.TrimSpace(line), "ERROR: "); ok {
			said = append(said, strings.TrimSuffix(text, "."))
		}
	}
	if len(said) == 0 {
		return err.Error()
	}
	return strings.Join(said, "; ")
}
