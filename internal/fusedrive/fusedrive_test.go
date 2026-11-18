package fusedrive

import (
	"errors"
	"slices"
	"testing"
)

// The real mounts are in test/netns (as root): TestDrives_ExFATIsReadByAConfinedProgram.

func TestHandles(t *testing.T) {
	for fsType, want := range map[string]bool{
		"exfat": true,
		// The kernel reads these: FAT is old and simple, ext4 and btrfs
		// keep owners for plugins (idmapped), and NTFS has no driver that
		// runs without root (see the package comment).
		"vfat": false, "ext4": false, "btrfs": false, "ntfs": false, "": false,
	} {
		if got := Handles(fsType); got != want {
			t.Errorf("Handles(%q) = %v", fsType, got)
		}
	}
}

func TestCheckUser(t *testing.T) {
	for _, user := range []int{UserBase, UserBase + Users - 1} {
		if err := checkUser(user); err != nil {
			t.Errorf("user %d: %v", user, err)
		}
	}
	// Not root, not a user of the Pi's, not a plugin's.
	for _, user := range []int{0, 1000, UserBase - 1, UserBase + Users, 1<<20 + 1<<16} {
		if err := checkUser(user); err == nil {
			t.Errorf("user %d is allowed", user)
		}
	}
	if err := Mount("/dev/null", "exfat", t.TempDir(), 0); err == nil {
		t.Error("Mount took root as the driver's user")
	}
	if err := Mount("/dev/null", "ntfs", t.TempDir(), UserBase); err == nil {
		t.Error("Mount took a filesystem the kernel reads")
	}
}

func TestDriverArgs(t *testing.T) {
	args := driverArgs("/dev/rasbeacon-drives/1048512")
	// Everyone may use the files, as on the FAT drives the kernel reads; the
	// connection is the one rbd mounted, as file descriptor 3.
	if !slices.Equal(args, []string{"-o", "uid=0,gid=0,umask=0000", "/dev/rasbeacon-drives/1048512", "/dev/fd/3"}) {
		t.Errorf("args = %q", args)
	}
}

func TestDriverMessage(t *testing.T) {
	output := "FUSE exfat 1.4.0 (libfuse3)\nERROR: exFAT file system is not found.\n"
	if got := driverMessage([]byte(output), errors.New("exit status 1")); got != "exFAT file system is not found" {
		t.Errorf("message = %q", got)
	}
	if got := driverMessage(nil, errors.New("signal: killed")); got != "signal: killed" {
		t.Errorf("message without output = %q", got)
	}
}
