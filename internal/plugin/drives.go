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
// run, and it can't hold device files or set-user-ID programs.
func MountOptions(fsType string) (kind string, options string) {
	options = "nosuid,nodev,noexec,noatime"
	switch fsType {
	case "ntfs":
		return "ntfs3", options
	case "vfat", "exfat":
		return fsType, options + ",uid=0,gid=0,umask=0000"
	}
	return fsType, options
}
