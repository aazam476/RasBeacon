package fusedrive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// mountFlags are the drives' mount options (see plugin.MountOptions):
// nothing on the drive runs, it holds no devices or set-user-ID programs,
// and no link on it is followed.
const mountFlags = unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC | unix.MS_NOATIME | unix.MS_NOSYMFOLLOW

// startTime is how long a driver may take to read the drive and start
// serving it.
const startTime = time.Minute

// Mount mounts device, a drive with the filesystem fsType (see Handles),
// at dir, read by its driver as the host user user (from UserBase, one
// per drive mounted at once). It returns once the driver serves the drive;
// unmounting dir ends the driver. The caller is root.
func Mount(device, fsType, dir string, user int) error {
	driver, ok := drivers[fsType]
	if !ok {
		return fmt.Errorf("%s drives are mounted by the kernel", fsType)
	}
	if err := checkUser(user); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Stat(device, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFBLK {
		return fmt.Errorf("%s isn't a drive", device)
	}
	node, err := makeNode(st.Rdev, user)
	if err != nil {
		return fmt.Errorf("the drive can't be handed to its driver: %w", err)
	}
	// Once the driver has opened the drive, nothing opens it by this node
	// again: a driver taken over can't reach a drive plugged in later,
	// which may get the same device number.
	defer os.Remove(node)

	fuse, err := os.OpenFile("/dev/fuse", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer fuse.Close() // the driver has its own copy
	// The driver's user owns the connection; allow_other lets everyone
	// else in (rbd, rbquotafs, the plugins), as the kernel's drives do,
	// with the permissions the driver shows (default_permissions).
	data := fmt.Sprintf("fd=%d,rootmode=40000,user_id=%d,group_id=%d,allow_other,default_permissions", fuse.Fd(), user, user)
	if err := unix.Mount(device, dir, "fuse."+fsType, mountFlags, data); err != nil {
		return fmt.Errorf("the drive can't be mounted: %w", err)
	}
	if err := start(driver, node, fuse, user); err != nil {
		// Its mount would wait for a driver forever.
		_ = unix.Unmount(dir, unix.MNT_DETACH)
		return err
	}
	return nil
}

// makeNode makes the device node with the number rdev that only user may
// open, in NodeDir, which only root can list.
func makeNode(rdev uint64, user int) (string, error) {
	if err := os.MkdirAll(NodeDir, 0o711); err != nil {
		return "", err
	}
	if err := os.Chmod(NodeDir, 0o711); err != nil {
		return "", err
	}
	node := filepath.Join(NodeDir, strconv.Itoa(user))
	// One left behind by an rbd that stopped while a driver started.
	if err := os.Remove(node); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := unix.Mknod(node, unix.S_IFBLK|0o600, int(rdev)); err != nil {
		return "", err
	}
	if err := os.Chown(node, user, user); err != nil {
		os.Remove(node)
		return "", err
	}
	return node, nil
}

// start runs the driver, confined (see the package comment), and waits
// until it has read the drive and serves it in the background.
func start(driver, node string, fuse *os.File, user int) error {
	ctx, cancel := context.WithTimeout(context.Background(), startTime)
	defer cancel()
	cmd := exec.CommandContext(ctx, driver, driverArgs(node)...)
	cmd.ExtraFiles = []*os.File{fuse} // its file descriptor 3
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS,
		// Its only user and group is user on the Pi; without groups (rbd's
		// would still count on the Pi's files) and so without capabilities.
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: namespaceUser, HostID: user, Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: namespaceUser, HostID: user, Size: 1}},
		GidMappingsEnableSetgroups: true, // to drop rbd's groups; only its own can be set
		Credential:                 &syscall.Credential{Uid: namespaceUser, Gid: namespaceUser, Groups: []uint32{}},
		Setsid:                     true,
	}
	// The driver reads the drive, then serves it from a process of its
	// own and exits: its output ends there too.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("its driver can't read it: %s", driverMessage(output, err))
	}
	return nil
}
