// Package stations lists the devices connected to the hotspot.
//
// Two sources are combined:
//
//   - `iw dev ap0 station dump`: the Wi-Fi radio's list of associated
//     devices. This is the source of truth for "who is connected right now",
//     so the client count never includes devices that already left.
//   - dnsmasq's lease file: which IP address and device name (hostname) each
//     device was given.
//
// Phones use private ("randomized") MAC addresses, so the same phone can show
// up with a different MAC on another day. That's fine: we only ever count
// devices that are associated right now.
package stations

import (
	"bufio"
	"sort"
	"strconv"
	"strings"
)

// Station is one connected device.
type Station struct {
	MAC              string `json:"mac"`
	IP               string `json:"ip"`       // empty until the device gets a DHCP lease
	Hostname         string `json:"hostname"` // empty if the device didn't send one
	SignalDBm        int    `json:"signalDbm"`
	ConnectedSeconds int    `json:"connectedSeconds"`
}

// ParseStationDump reads `iw dev <iface> station dump` output, which lists
// one block per device:
//
//	Station 3a:1f:22:aa:bb:cc (on ap0)
//		inactive time:	1200 ms
//		signal:  	-48 [-50, -52] dBm
//		connected time:	367 seconds
//		...
func ParseStationDump(output string) []Station {
	var result []Station
	var current *Station

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "Station ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				result = append(result, Station{MAC: strings.ToLower(fields[1])})
				current = &result[len(result)-1]
			}
			continue
		}
		if current == nil {
			continue
		}

		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "signal":
			// "-48 [-50, -52] dBm": the first number is the combined signal.
			current.SignalDBm = firstInt(value)
		case "connected time":
			// "367 seconds"
			current.ConnectedSeconds = firstInt(value)
		}
	}
	return result
}

// Lease is one entry from dnsmasq's lease file.
type Lease struct {
	MAC      string
	IP       string
	Hostname string
}

// ParseLeases reads dnsmasq's lease file. Each line is:
//
//	<expiry time> <MAC> <IP> <hostname or *> <client id or *>
func ParseLeases(content string) []Lease {
	var leases []Lease
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		leases = append(leases, Lease{MAC: strings.ToLower(fields[1]), IP: fields[2], Hostname: hostname})
	}
	return leases
}

// Merge adds IP addresses and hostnames from leases to the connected
// stations, and sorts the result by how long each device has been connected
// (newest last).
func Merge(connected []Station, leases []Lease) []Station {
	byMAC := map[string]Lease{}
	for _, l := range leases {
		byMAC[l.MAC] = l
	}

	merged := make([]Station, len(connected))
	for i, s := range connected {
		if lease, ok := byMAC[s.MAC]; ok {
			s.IP = lease.IP
			s.Hostname = lease.Hostname
		}
		merged[i] = s
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].ConnectedSeconds > merged[j].ConnectedSeconds
	})
	return merged
}

// firstInt returns the first whole number in s (which may be negative), or 0.
func firstInt(s string) int {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return n
}
