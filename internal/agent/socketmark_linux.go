package agent

import "syscall"

// setSocketMark puts the mark on a socket (SO_MARK, Linux only), so the
// routing rules can send its packets into the VPN tunnel. See pluginDialer.
func setSocketMark(fd uintptr, mark int) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark)
}
