// Package dhcpprobe checks whether a DHCP server answers on a network
// interface. RasBeacon uses it before letting Ethernet join the hotspot
// network in client-port mode: if a DHCP server answers, the cable is
// probably plugged into a hotel's wall socket rather than a laptop, and
// RasBeacon must not hand out addresses onto someone else's network.
//
// # How it works
//
// A DHCP client starts by shouting "is there a DHCP server?" (a DHCPDISCOVER
// message) to everyone on the network; servers reply with an offer. We send
// one DHCPDISCOVER and wait a few seconds for any offer. We never accept the
// offer, so no address is actually taken.
//
// The interface has no IP address at this point, so the normal socket API
// can't be used. Instead we use a "packet socket" that sends and receives
// raw IPv4 packets on one interface, and build the IPv4 and UDP headers
// ourselves (see packet.go).
package dhcpprobe

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// Result is what the probe found.
type Result struct {
	// ServerFound is true when a DHCP server answered.
	ServerFound bool
	// ServerAddress is the answering server's IP address, if found.
	ServerAddress string
}

// Probe sends a DHCPDISCOVER on iface and waits up to timeout for an offer.
// The interface must be up. Probe needs root (CAP_NET_RAW).
func Probe(iface string, timeout time.Duration) (Result, error) {
	return probe(iface, timeout) // see probe_linux.go
}

// newTransactionID returns a random number that identifies our request, so
// we only count replies to it.
func newTransactionID() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}
