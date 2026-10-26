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
	"encoding/json"
	"fmt"
	"net"
	"slices"
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

	// Filled in by the agent:
	// Band is the hotspot's band ("2.4GHz" or "5GHz").
	Band string `json:"band"`
	// DownMbps (towards the device) and UpMbps are its current rates.
	DownMbps float64 `json:"downMbps"`
	UpMbps   float64 `json:"upMbps"`
	// InternetAccess is false while the owner has paused it.
	InternetAccess bool `json:"internetAccess"`
	// PausedSeconds is how long ago it was paused (0 when not paused).
	PausedSeconds int `json:"pausedSeconds,omitempty"`
	// IsThisDevice marks the device viewing the page.
	IsThisDevice bool `json:"isThisDevice,omitempty"`
	// Wired marks a device on the Ethernet client port. It has no band,
	// signal or measured traffic, and can't be sent off like a Wi-Fi device.
	Wired bool `json:"wired,omitempty"`
	// Name is the owner's name for the device ("" if none).
	Name string `json:"name,omitempty"`
	// DownBytes and UpBytes are the data it used since the counters were
	// reset (Wi-Fi devices only; the radio counts them).
	DownBytes uint64 `json:"downBytes"`
	UpBytes   uint64 `json:"upBytes"`
	// LimitReached is true while it is paused for using more than the data
	// limit on a metered connection.
	LimitReached bool `json:"limitReached,omitempty"`

	// RxBytes and TxBytes are the radio's byte counters for the device
	// (Tx is towards it), used to work out the rates.
	RxBytes uint64 `json:"-"`
	TxBytes uint64 `json:"-"`
	// RxPackets and TxPackets count the radio's packets from and to the
	// device. The hotspot's stall check (internal/agent/hotspotstall.go)
	// watches them: a device that keeps sending while the Pi sends it
	// nothing means the hotspot has stopped delivering.
	RxPackets uint64 `json:"-"`
	TxPackets uint64 `json:"-"`
	// MFP is true when the device uses protected management frames
	// (always with WPA3), for the stall log.
	MFP bool `json:"-"`
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
		case "rx bytes":
			current.RxBytes, _ = strconv.ParseUint(value, 10, 64)
		case "tx bytes":
			current.TxBytes, _ = strconv.ParseUint(value, 10, 64)
		case "rx packets":
			current.RxPackets, _ = strconv.ParseUint(value, 10, 64)
		case "tx packets":
			current.TxPackets, _ = strconv.ParseUint(value, 10, 64)
		case "MFP":
			current.MFP = value == "yes"
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

// ParseBridgeFDB reads `bridge -json fdb show br <bridge> brport <port>`:
// the devices the bridge has seen behind one port, by MAC address. It
// leaves out the port's own addresses ("permanent", "self") and multicast
// addresses. A device stays in the table while it sends something now and
// then (the kernel forgets it after 5 minutes of silence).
func ParseBridgeFDB(out string) ([]string, error) {
	var entries []struct {
		MAC   string   `json:"mac"`
		State string   `json:"state"`
		Flags []string `json:"flags"`
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("reading the bridge table: %w", err)
	}
	var macs []string
	for _, e := range entries {
		hw, err := net.ParseMAC(e.MAC)
		if err != nil || len(hw) != 6 || hw[0]&1 == 1 || e.State == "permanent" || slices.Contains(e.Flags, "self") {
			continue
		}
		if !slices.Contains(macs, hw.String()) {
			macs = append(macs, hw.String())
		}
	}
	return macs, nil
}

// Neighbor is one entry of the kernel's neighbor table: an address a
// device on a local network uses, and the device's MAC address.
type Neighbor struct {
	IP  string
	MAC string
}

// ParseNeighbors reads `ip -json neigh show`: which device uses which
// address. Entries without a MAC address (lookups that failed) are left
// out. For IPv6 this is the only list of a device's addresses, since
// devices choose their own (there is no DHCP lease to read).
func ParseNeighbors(out string) ([]Neighbor, error) {
	var entries []struct {
		Dst    string `json:"dst"`
		LLAddr string `json:"lladdr"`
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("reading the neighbor table: %w", err)
	}
	var neighbors []Neighbor
	for _, e := range entries {
		hw, err := net.ParseMAC(e.LLAddr)
		if err != nil || len(hw) != 6 || e.Dst == "" {
			continue
		}
		neighbors = append(neighbors, Neighbor{IP: e.Dst, MAC: hw.String()})
	}
	return neighbors, nil
}
