//go:build linux

package dhcpprobe

import (
	"fmt"
	"net"
	"syscall"
	"time"
)

// ethernetTypeIPv4 is the EtherType of IPv4 packets, in network byte order
// as packet sockets expect it.
var ethernetTypeIPv4 = htons(0x0800)

func probe(iface string, timeout time.Duration) (Result, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return Result{}, err
	}

	// SOCK_DGRAM packet socket: we send and receive whole IPv4 packets and
	// the kernel adds/removes the Ethernet header.
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(ethernetTypeIPv4))
	if err != nil {
		return Result{}, fmt.Errorf("opening packet socket: %w", err)
	}
	defer syscall.Close(fd)

	// Only receive packets from this interface.
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{Protocol: ethernetTypeIPv4, Ifindex: ifi.Index}); err != nil {
		return Result{}, fmt.Errorf("binding to %s: %w", iface, err)
	}

	xid := newTransactionID()
	broadcast := &syscall.SockaddrLinklayer{
		Protocol: ethernetTypeIPv4,
		Ifindex:  ifi.Index,
		Halen:    6,
		Addr:     [8]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	}
	if err := syscall.Sendto(fd, buildDiscover(ifi.HardwareAddr, xid), 0, broadcast); err != nil {
		return Result{}, fmt.Errorf("sending DHCPDISCOVER on %s: %w", iface, err)
	}

	// Read packets until an offer arrives or time runs out. Each read waits
	// at most 200 ms so the deadline is checked regularly.
	readTimeout := syscall.NsecToTimeval((200 * time.Millisecond).Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &readTimeout); err != nil {
		return Result{}, err
	}
	buffer := make([]byte, 1500)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, _, err := syscall.Recvfrom(fd, buffer, 0)
		if err != nil {
			continue // timeout (EAGAIN) or interrupted: check the deadline
		}
		if server, err := parseOffer(buffer[:n], xid); err == nil {
			return Result{ServerFound: true, ServerAddress: server}, nil
		}
	}
	return Result{ServerFound: false}, nil
}

// htons converts a 16-bit number to network byte order (big-endian).
func htons(v uint16) uint16 { return v<<8 | v>>8 }
