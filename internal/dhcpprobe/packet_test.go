package dhcpprobe

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestBuildDiscover_HeadersAndOptions(t *testing.T) {
	mac := net.HardwareAddr{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	p := buildDiscover(mac, 0xcafe0001)

	if len(p) != 20+8+300 {
		t.Fatalf("packet length %d, want 328", len(p))
	}
	if ipChecksum(p[:20]) != 0 {
		t.Error("IPv4 header checksum is wrong (a valid header sums to zero)")
	}
	if binary.BigEndian.Uint16(p[20:22]) != 68 || binary.BigEndian.Uint16(p[22:24]) != 67 {
		t.Error("UDP ports should be 68 -> 67")
	}
	dhcp := p[28:]
	if dhcp[0] != opRequest || binary.BigEndian.Uint32(dhcp[4:8]) != 0xcafe0001 {
		t.Error("wrong op or transaction ID")
	}
	if net.HardwareAddr(dhcp[28:34]).String() != mac.String() {
		t.Error("client MAC address not filled in")
	}
	if dhcp[240] != optionMessageType || dhcp[242] != messageDiscover {
		t.Error("first option should say DHCPDISCOVER")
	}
}

// fakeOffer builds the reply a server would send.
func fakeOffer(xid uint32, messageType byte) []byte {
	p := buildDiscover(net.HardwareAddr{2, 0, 0, 0, 0, 1}, xid)
	copy(p[12:16], []byte{192, 168, 1, 1}) // source: the server
	binary.BigEndian.PutUint16(p[20:22], 67)
	binary.BigEndian.PutUint16(p[22:24], 68)
	dhcp := p[28:]
	dhcp[0] = opReply
	options := []byte{optionMessageType, 1, messageType, optionServerID, 4, 192, 168, 1, 254, optionEnd}
	copy(dhcp[240:], options)
	return p
}

func TestParseOffer(t *testing.T) {
	server, err := parseOffer(fakeOffer(42, messageOffer), 42)
	if err != nil {
		t.Fatalf("offer not recognised: %v", err)
	}
	if server != "192.168.1.254" {
		t.Errorf("server = %s, want the server ID option 192.168.1.254", server)
	}
}

func TestParseOffer_IgnoresOtherTransactionsAndOurOwnDiscover(t *testing.T) {
	if _, err := parseOffer(fakeOffer(42, messageOffer), 43); err == nil {
		t.Error("an offer for another transaction must be ignored")
	}
	if _, err := parseOffer(fakeOffer(42, 5 /* ACK */), 42); err == nil {
		t.Error("only offers count")
	}
	if _, err := parseOffer(buildDiscover(net.HardwareAddr{2, 0, 0, 0, 0, 1}, 42), 42); err == nil {
		t.Error("our own DISCOVER (seen on the wire) must not count as an offer")
	}
	if _, err := parseOffer([]byte{0x45, 0, 0}, 42); err == nil {
		t.Error("short packets must be ignored")
	}
}
