//go:build !linux

package netns

import "syscall"

// markSocket is a stub: these tests only run on Linux (see
// socketmark_linux_test.go), but the package must still build elsewhere.
func markSocket(mark int) func(network, address string, c syscall.RawConn) error {
	return nil
}
