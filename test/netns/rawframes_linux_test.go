//go:build linux

package netns

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

// sendFromManyAddresses sends count broadcast frames out of iface in the
// namespace ns, each from a different made-up MAC address, as a device
// trying to fill the firewall's sets would.
func sendFromManyAddresses(t *testing.T, ns, iface string, count int) {
	t.Helper()
	done := make(chan error)
	go func() {
		// This thread joins the namespace and is never handed back (no
		// UnlockOSThread): Go ends it with the goroutine, so nothing else
		// runs in the phone's namespace by mistake.
		runtime.LockOSThread()
		done <- func() error {
			handle, err := os.Open("/var/run/netns/" + ns)
			if err != nil {
				return err
			}
			defer handle.Close()
			if err := unix.Setns(int(handle.Fd()), unix.CLONE_NEWNET); err != nil {
				return err
			}
			link, err := net.InterfaceByName(iface)
			if err != nil {
				return err
			}
			const etherType = 0x88b5 // for local experiments (IEEE 802)
			// The socket takes it in network byte order.
			var wire [2]byte
			binary.BigEndian.PutUint16(wire[:], etherType)
			proto := int(binary.NativeEndian.Uint16(wire[:]))
			fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, proto)
			if err != nil {
				return err
			}
			defer unix.Close(fd)
			to := &unix.SockaddrLinklayer{Ifindex: link.Index, Protocol: uint16(proto)}
			for i := range count {
				frame := make([]byte, 60)
				copy(frame, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
				copy(frame[6:], []byte{0x02, 0xbd, 0x00, byte(i >> 16), byte(i >> 8), byte(i)})
				binary.BigEndian.PutUint16(frame[12:], etherType)
				if err := unix.Sendto(fd, frame, 0, to); err != nil {
					return fmt.Errorf("frame %d: %w", i, err)
				}
			}
			return nil
		}()
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
