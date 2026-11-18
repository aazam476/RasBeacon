package plugin

// This file lists the USB drives a plugin may be given (from lsblk) and
// chooses how each kind of file system is mounted.

import (
	"encoding/json"
	"slices"
)

// Drive is a USB drive's partition a plugin may use.
type Drive struct {
	UUID   string `json:"uuid"`
	Device string `json:"device"`
	Label  string `json:"label"`
	FSType string `json:"fsType"`
	Size   int64  `json:"size"`
}

// DriveFilesystems are the kinds of drive RasBeacon mounts.
var DriveFilesystems = []string{"vfat", "exfat", "ext4", "ntfs", "btrfs"}

// LsblkArgs ask lsblk for the block devices, as ParseDrives reads them.
var LsblkArgs = []string{"--json", "--bytes", "--tree", "--output", "NAME,PATH,SIZE,FSTYPE,LABEL,UUID,TRAN,TYPE"}

// lsblkDevice is one block device (a disk or a partition) in lsblk's
// JSON answer, with its partitions as children.
type lsblkDevice struct {
	Path     string        `json:"path"`
	Size     int64         `json:"size"`
	FSType   string        `json:"fstype"`
	Label    string        `json:"label"`
	UUID     string        `json:"uuid"`
	Tran     string        `json:"tran"`
	Type     string        `json:"type"`
	Children []lsblkDevice `json:"children"`
}

// ParseDrives lists the partitions (or whole disks without partitions) on
// USB disks that hold a filesystem RasBeacon mounts.
func ParseDrives(lsblkJSON string) ([]Drive, error) {
	var out struct {
		Blockdevices []lsblkDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(lsblkJSON), &out); err != nil {
		return nil, err
	}
	var drives []Drive
	add := func(d lsblkDevice) {
		if d.UUID != "" && slices.Contains(DriveFilesystems, d.FSType) {
			drives = append(drives, Drive{UUID: d.UUID, Device: d.Path, Label: d.Label, FSType: d.FSType, Size: d.Size})
		}
	}
	for _, disk := range out.Blockdevices {
		if disk.Tran != "usb" || disk.Type != "disk" {
			continue
		}
		add(disk)
		for _, part := range disk.Children {
			add(part)
		}
	}
	return drives, nil
}

// MountOptions are how a drive is mounted for a plugin: nothing on it can
// run, it can't hold device files or set-user-ID programs, and no link on
// it is followed (nosymfollow).
//
// A drive comes from anywhere, and a link on it can point to any file on
// the Pi ("Movies" to "/etc"). RasBeacon's own programs work on the drive
// as root, by paths: rbquotafs in a share's folder, gocryptfs in its
// encrypted files. With nosymfollow the kernel refuses to pass through a
// link in such a path, so none of them can be sent outside the drive by
// what is on it. Links can still be made and read there, and a plugin
// still follows the links in its shares: it sees a share through
// rbquotafs, where the kernel follows them inside the plugin's own
// container. (A plugin that gets a whole drive directly, an older
// "usbDrive" one, doesn't follow links on it any more.)
func MountOptions(fsType string) (kind string, options string) {
	options = "nosuid,nodev,noexec,noatime,nosymfollow"
	// FAT, exFAT and NTFS keep no owners RasBeacon's plugins could use
	// (and their drivers can't idmap): every file is open to everyone, as
	// the plugin's users are its own (see plugin.UserBase). The drive is
	// in a folder only root can reach.
	switch fsType {
	case "ntfs":
		return "ntfs3", options + ",uid=0,gid=0,umask=0000"
	case "vfat", "exfat":
		return fsType, options + ",uid=0,gid=0,umask=0000"
	}
	return fsType, options
}
