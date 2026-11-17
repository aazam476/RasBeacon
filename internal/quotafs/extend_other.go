//go:build !linux

package quotafs

import "os"

// extend makes the file name in dir grow from size to grown bytes
// (RasBeacon runs on Linux; elsewhere, only so the package builds, it just
// makes the file longer).
func extend(dir *os.Root, name string, size, grown int64) error {
	f, err := dir.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(grown); err != nil {
		_ = f.Truncate(size)
		return err
	}
	return nil
}
