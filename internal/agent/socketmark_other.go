//go:build !linux

package agent

// This file is setSocketMark for systems other than Linux (see
// socketmark_linux.go).

// setSocketMark does nothing on systems without SO_MARK. It lets rbd -dev
// build and run on a Mac, where there are no VPN routing rules to follow.
func setSocketMark(fd uintptr, mark int) error {
	return nil
}
