package netns

import "syscall"

// markSocket returns a dialer Control function that marks the connection
// (SO_MARK), as rbd marks its downloads for plugins.
func markSocket(mark int) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var markErr error
		err := c.Control(func(fd uintptr) {
			markErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark)
		})
		if err != nil {
			return err
		}
		return markErr
	}
}
