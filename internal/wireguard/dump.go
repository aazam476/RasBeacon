package wireguard

// This file reads `wg show wg0 dump`: whether the tunnel has reached the
// server lately, and how much it sent and received.

import (
	"strconv"
	"strings"
	"time"
)

// PeerStatus is what `wg show wg0 dump` reports about the server.
type PeerStatus struct {
	// Endpoint is the server address in use, e.g. "203.0.113.5:51820".
	Endpoint string
	// LatestHandshake is zero if there has been no handshake yet.
	LatestHandshake time.Time
	ReceivedBytes   int64
	SentBytes       int64
}

// ParseDump reads `wg show wg0 dump`. The first line describes the
// interface; the next one (tab-separated) describes the peer:
//
//	public-key  preshared-key  endpoint  allowed-ips  latest-handshake  rx  tx  keepalive
//
// latest-handshake is in seconds since 1970, and 0 means "never".
func ParseDump(out string) (PeerStatus, bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return PeerStatus{}, false
	}
	fields := strings.Split(lines[1], "\t")
	if len(fields) < 8 {
		return PeerStatus{}, false
	}
	var s PeerStatus
	if fields[2] != "(none)" {
		s.Endpoint = fields[2]
	}
	if seconds, err := strconv.ParseInt(fields[4], 10, 64); err == nil && seconds > 0 {
		s.LatestHandshake = time.Unix(seconds, 0)
	}
	s.ReceivedBytes, _ = strconv.ParseInt(fields[5], 10, 64)
	s.SentBytes, _ = strconv.ParseInt(fields[6], 10, 64)
	return s, true
}
