//go:build !linux

package agent

// setSocketMark does nothing on systems without SO_MARK. It lets rbd -dev
// build and run on a Mac, where there are no VPN routing rules to follow.
func setSocketMark(fd uintptr, mark int) error {
	return nil
}
