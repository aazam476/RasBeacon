package quotafs

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// extend makes the file at path grow from size to grown bytes, its new
// part really taken on the drive. fallocate does that at once where the
// drive's filesystem can (ext4, NTFS); elsewhere (exFAT, FAT32) making the
// file longer takes the space too, as those can't have holes in files.
func extend(path string, size, grown int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	err = unix.Fallocate(int(f.Fd()), 0, size, grown-size)
	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) {
		err = f.Truncate(grown)
	}
	if err != nil {
		// Whatever part of it was made is given back.
		_ = f.Truncate(size)
	}
	return err
}
