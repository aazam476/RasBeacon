package dhcpprobe

// This file builds and reads the bytes of DHCP packets. It has no network
// code, so it can be tested anywhere.
//
// A DHCPDISCOVER on the wire, from the outside in:
//
//	IPv4 header (20 bytes)   from 0.0.0.0 to 255.255.255.255, protocol UDP
//	UDP header  (8 bytes)    from port 68 (client) to port 67 (server)
//	DHCP message             described in RFC 2131 (layout below)

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
)

// Port numbers and message types from RFC 2131.
const (
	clientPort = 68
	serverPort = 67

	opRequest = 1 // client to server
	opReply   = 2 // server to client

	optionMessageType = 53
	optionServerID    = 54
	optionParamList   = 55
	optionEnd         = 255

	messageDiscover = 1
	messageOffer    = 2
)

// magicCookie marks the start of DHCP options (RFC 2131 section 3).
var magicCookie = []byte{99, 130, 83, 99}

// buildDiscover returns a complete IPv4 packet holding a DHCPDISCOVER from
// the given hardware (MAC) address.
func buildDiscover(mac net.HardwareAddr, xid uint32) []byte {
	// --- DHCP message (fixed part is 236 bytes) ---
	dhcp := make([]byte, 236)
	dhcp[0] = opRequest
	dhcp[1] = 1 // hardware type: Ethernet
	dhcp[2] = 6 // hardware address length
	binary.BigEndian.PutUint32(dhcp[4:8], xid)
	// Flags: ask the server to broadcast its reply, because we have no
	// address it could send to.
	binary.BigEndian.PutUint16(dhcp[10:12], 0x8000)
	copy(dhcp[28:34], mac) // client hardware address
	dhcp = append(dhcp, magicCookie...)
	dhcp = append(dhcp,
		optionMessageType, 1, messageDiscover,
		optionParamList, 3, 1, 3, 6, // ask for netmask, router, DNS
		optionEnd)
	// Some old servers ignore messages shorter than 300 bytes.
	for len(dhcp) < 300 {
		dhcp = append(dhcp, 0)
	}

	// --- UDP header ---
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[0:2], clientPort)
	binary.BigEndian.PutUint16(udp[2:4], serverPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(8+len(dhcp)))
	// udp[6:8] checksum 0 means "no checksum", which IPv4 allows.

	// --- IPv4 header ---
	ip := make([]byte, 20)
	ip[0] = 0x45 // version 4, header length 5 x 4 bytes
	binary.BigEndian.PutUint16(ip[2:4], uint16(20+8+len(dhcp)))
	ip[8] = 64 // time to live
	ip[9] = 17 // protocol: UDP
	// Source 0.0.0.0 (bytes 12-15 stay zero); destination 255.255.255.255.
	copy(ip[16:20], []byte{255, 255, 255, 255})
	binary.BigEndian.PutUint16(ip[10:12], ipChecksum(ip))

	packet := append(ip, udp...)
	return append(packet, dhcp...)
}

// ipChecksum computes the IPv4 header checksum (RFC 791): the ones'
// complement of the ones' complement sum of the header's 16-bit words.
func ipChecksum(header []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(header); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[i : i+2]))
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

var errNotAnOffer = errors.New("not a DHCP offer for us")

// parseOffer checks whether packet (a raw IPv4 packet) is a DHCPOFFER
// answering transaction xid, and returns the server's address.
func parseOffer(packet []byte, xid uint32) (string, error) {
	if len(packet) < 20 || packet[0]>>4 != 4 || packet[9] != 17 {
		return "", errNotAnOffer // not IPv4 UDP
	}
	headerLen := int(packet[0]&0x0f) * 4
	if len(packet) < headerLen+8+240 {
		return "", errNotAnOffer
	}
	udp := packet[headerLen:]
	if binary.BigEndian.Uint16(udp[2:4]) != clientPort {
		return "", errNotAnOffer
	}
	dhcp := udp[8:]
	if dhcp[0] != opReply || binary.BigEndian.Uint32(dhcp[4:8]) != xid ||
		string(dhcp[236:240]) != string(magicCookie) {
		return "", errNotAnOffer
	}

	// Walk the options: each is [code, length, value...].
	sourceIP, _ := netip.AddrFromSlice(packet[12:16])
	server := sourceIP.String()
	isOffer := false
	options := dhcp[240:]
	for i := 0; i < len(options); {
		code := options[i]
		if code == optionEnd {
			break
		}
		if code == 0 { // padding
			i++
			continue
		}
		if i+1 >= len(options) || i+2+int(options[i+1]) > len(options) {
			break // truncated option
		}
		value := options[i+2 : i+2+int(options[i+1])]
		switch {
		case code == optionMessageType && len(value) == 1:
			isOffer = value[0] == messageOffer
		case code == optionServerID && len(value) == 4:
			addr, _ := netip.AddrFromSlice(value)
			server = addr.String()
		}
		i += 2 + len(value)
	}
	if !isOffer {
		return "", errNotAnOffer
	}
	return server, nil
}
