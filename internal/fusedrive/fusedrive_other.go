//go:build !linux

package fusedrive

import "errors"

// Mount mounts a drive with its driver: on Linux only.
func Mount(device, fsType, dir string, user int) error {
	return errors.New("drives are only mounted on Linux")
}
