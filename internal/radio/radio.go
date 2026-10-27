// Package radio finds the Wi-Fi adapters in the device and decides which one
// runs the hotspot.
//
// # Why this matters
//
// The Raspberry Pi's built-in Wi-Fi chip can only hold about 8 connected
// devices when it acts as a hotspot, which is not enough. So RasBeacon uses a
// USB Wi-Fi adapter for the hotspot and keeps the built-in chip for joining
// the upstream network (the "uplink").
//
// # How detection works
//
// Linux describes every network interface under /sys/class/net/<name>/.
// For Wi-Fi interfaces that directory contains:
//
//	phy80211/name     the radio's name, e.g. "phy1"
//	device/driver     a link to the driver, e.g. ".../drivers/rtw88_8822bu"
//	device            a link into the hardware tree; USB devices have "/usb"
//	                  in that path
//
// Whether a radio can run a hotspot ("AP mode") comes from `iw phy <name>
// info`, which lists "Supported interface modes". That command is run by the
// caller and passed in as text, so this package can be tested without real
// hardware.
package radio

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Role is the job a radio is given.
type Role string

const (
	// RoleHotspot broadcasts the network your devices join.
	RoleHotspot Role = "hotspot"
	// RoleUplink joins the upstream network (hotel, eduroam, ...).
	RoleUplink Role = "uplink"
	// RoleGuest broadcasts the guest network on a radio of its own (only
	// when the owner chooses it; otherwise the guest network shares the
	// hotspot's radio).
	RoleGuest Role = "guest"
	// RoleUnused means the radio was found but has no job.
	RoleUnused Role = "unused"
)

// HotspotInterfaceName is the name the hotspot radio is renamed to, so every
// other part of RasBeacon can refer to it without guessing.
const HotspotInterfaceName = "ap0"

// GuestInterfaceName is the guest network's interface: a second network
// hostapd adds on the hotspot's radio, or a radio of its own renamed to it.
const GuestInterfaceName = "guest0"

// Radio describes one Wi-Fi interface.
type Radio struct {
	Interface string `json:"interface"` // Linux interface name, e.g. "wlan1"
	// MAC is the radio's own (permanent) address. It names the radio for
	// the owner's role choices, because interface names change between
	// boots and USB ports.
	MAC    string `json:"mac"`
	Phy    string `json:"phy"`    // radio name, e.g. "phy1"
	Driver string `json:"driver"` // e.g. "brcmfmac" (built-in) or "rtw88_8822bu"
	IsUSB  bool   `json:"isUsb"`
	// USBID is "vendor:product" as shown by lsusb, e.g. "2357:012d" for a
	// TP-Link Archer T3U. Empty for built-in radios.
	USBID      string `json:"usbId"`
	SupportsAP bool   `json:"supportsAp"`
	// MaxAPs is how many hotspot networks the radio can broadcast at once
	// (see MaxAPInterfaces). The guest network needs 2.
	MaxAPs int `json:"maxAps"`
	// Has5GHz is true when the radio can use the 5 GHz band (see
	// Supports5GHz). Many built-in radios, like the Pi Zero 2 W's, can't.
	Has5GHz bool `json:"has5GHz"`
	// HasWPA3 is true when the radio can run a WPA3-only hotspot (see
	// SupportsWPA3AP).
	HasWPA3 bool `json:"hasWpa3"`
	// HasWiFi6 is true when the radio can run a Wi-Fi 6 (802.11ax)
	// hotspot (see SupportsWiFi6AP).
	HasWiFi6 bool `json:"hasWifi6"`
	Role     Role `json:"role"`
	// Chosen is true when the owner chose Role (System page); false when
	// RasBeacon did.
	Chosen bool `json:"chosen,omitempty"`
}

// PhyInfoFunc returns the output of `iw phy <phy> info`. It is a parameter so
// tests can supply canned output instead of running the command.
type PhyInfoFunc func(phy string) (string, error)

// Detect lists the Wi-Fi radios under sysClassNet (normally
// /sys/class/net): one interface per radio, sorted by interface name. Roles are not assigned yet; see
// AssignRoles.
func Detect(sysClassNet string, phyInfo PhyInfoFunc) ([]Radio, error) {
	entries, err := os.ReadDir(sysClassNet)
	if err != nil {
		return nil, fmt.Errorf("listing network interfaces: %w", err)
	}

	var radios []Radio
	for _, entry := range entries {
		name := entry.Name()
		ifaceDir := filepath.Join(sysClassNet, name)

		// Only Wi-Fi interfaces have a phy80211 entry.
		phyName, err := readTrimmed(filepath.Join(ifaceDir, "phy80211", "name"))
		if err != nil {
			continue
		}

		mac, _ := readTrimmed(filepath.Join(ifaceDir, "address"))
		r := Radio{
			Interface: name,
			MAC:       mac,
			Phy:       phyName,
			Driver:    linkBaseName(filepath.Join(ifaceDir, "device", "driver")),
			Role:      RoleUnused,
		}

		// The resolved "device" path goes through ".../usbN/..." for USB
		// adapters. For a USB network interface, "device" points at the USB
		// *interface* (e.g. 1-1:1.0); its parent directory is the USB
		// *device*, which holds the vendor and product IDs.
		devicePath, err := filepath.EvalSymlinks(filepath.Join(ifaceDir, "device"))
		if err == nil && strings.Contains(devicePath, "/usb") {
			r.IsUSB = true
			vendor, _ := readTrimmed(filepath.Join(devicePath, "..", "idVendor"))
			product, _ := readTrimmed(filepath.Join(devicePath, "..", "idProduct"))
			if vendor != "" && product != "" {
				r.USBID = vendor + ":" + product
			}
		}

		if info, err := phyInfo(phyName); err == nil {
			r.SupportsAP = SupportsAPMode(info)
			r.MaxAPs = MaxAPInterfaces(info)
			r.Has5GHz = Supports5GHz(info)
			r.HasWPA3 = SupportsWPA3AP(r.Driver, info)
			r.HasWiFi6 = SupportsWiFi6AP(info)
		}

		radios = append(radios, r)
	}

	sort.Slice(radios, func(i, j int) bool { return radios[i].Interface < radios[j].Interface })
	return onePerRadio(radios), nil
}

// onePerRadio keeps the first interface of each radio (phy). One radio can
// have several interfaces: while hostapd runs the guest network, the
// hotspot's radio has both ap0 and guest0. If rbd restarts then, guest0 must
// not look like a second radio, or it would be given the uplink.
func onePerRadio(radios []Radio) []Radio {
	seen := map[string]bool{}
	var kept []Radio
	for _, r := range radios {
		if seen[r.Phy] {
			continue
		}
		seen[r.Phy] = true
		kept = append(kept, r)
	}
	return kept
}

// AssignRoles decides what each radio does: the owner's choices first
// (chosen, by radio MAC address; nil for none), then automatically for the
// rest.
//
// The owner's choices are kept where they can work: a hotspot or guest
// radio must be able to run a hotspot, and each role goes to one radio.
// A choice for a radio that isn't plugged in is skipped with a warning, and
// its role is chosen automatically. Then, for roles nobody chose:
//  1. Hotspot: the first USB radio that supports AP mode.
//  2. If there is no such USB radio, a built-in radio that supports AP
//     mode, so you can still reach the setup page. The returned warning
//     explains the ~8 client limit.
//  3. Uplink: the first radio left that has no chosen role (normally the
//     built-in one).
//
// The guest role is never chosen automatically. It returns the radios with
// roles filled in, plus human-readable warnings.
func AssignRoles(radios []Radio, chosen map[string]Role) ([]Radio, []string) {
	result := make([]Radio, len(radios))
	copy(result, radios)
	var warnings []string
	// decided marks radios whose role the owner chose (unused included).
	decided := make([]bool, len(result))
	taken := map[Role]bool{}
	present := map[string]bool{}
	for i := range result {
		result[i].Role = RoleUnused
		present[result[i].MAC] = true
	}
	for i, r := range result {
		role, ok := chosen[r.MAC]
		if !ok || r.MAC == "" {
			continue
		}
		switch {
		case role == RoleUnused:
		case (role == RoleHotspot || role == RoleGuest) && !r.SupportsAP:
			warnings = append(warnings, fmt.Sprintf("The radio %s can't run a hotspot, so RasBeacon chose its job by itself.", r.Interface))
			continue
		case taken[role] || (role != RoleHotspot && role != RoleGuest && role != RoleUplink):
			continue // another radio has it already (or an unknown role)
		}
		result[i].Role = role
		result[i].Chosen = true
		decided[i] = true
		if role != RoleUnused {
			taken[role] = true
		}
	}
	for mac := range chosen {
		if !present[mac] {
			warnings = append(warnings, "A Wi-Fi adapter you chose a job for isn't plugged in, so RasBeacon chose the jobs by itself.")
			break
		}
	}

	// The automatic choices go through the radios in a steady order (see
	// autoOrder), not by interface name, which changes when radios are
	// renamed.
	order := autoOrder(result)
	if !taken[RoleHotspot] {
		hotspotIndex := -1
		for _, i := range order {
			if r := result[i]; !decided[i] && r.IsUSB && r.SupportsAP {
				hotspotIndex = i
				break
			}
		}
		if hotspotIndex == -1 {
			for _, i := range order {
				if r := result[i]; !decided[i] && r.SupportsAP {
					hotspotIndex = i
					warnings = append(warnings,
						"No USB Wi-Fi adapter found, so the built-in Wi-Fi is running the hotspot. "+
							"It can only hold about 8 devices and cannot also join an upstream network. "+
							"Plug in a supported USB adapter and reboot.")
					break
				}
			}
		}
		if hotspotIndex == -1 {
			warnings = append(warnings, "No Wi-Fi radio that can run a hotspot was found.")
			return result, warnings
		}
		result[hotspotIndex].Role = RoleHotspot
		decided[hotspotIndex] = true
	}

	if !taken[RoleUplink] {
		for _, i := range order {
			if !decided[i] {
				result[i].Role = RoleUplink
				break
			}
		}
	}
	return result, warnings
}

// autoOrder lists the radios' positions in the order automatic choices
// consider them: the radio running the hotspot now (named ap0) first, so a
// restart keeps it, then by phy number, the order the kernel found them in.
func autoOrder(radios []Radio) []int {
	order := make([]int, len(radios))
	for i := range order {
		order[i] = i
	}
	phyNumber := func(r Radio) int {
		n, err := strconv.Atoi(strings.TrimPrefix(r.Phy, "phy"))
		if err != nil {
			return 1 << 30 // unknown: last
		}
		return n
	}
	sort.SliceStable(order, func(a, b int) bool {
		ra, rb := radios[order[a]], radios[order[b]]
		if (ra.Interface == HotspotInterfaceName) != (rb.Interface == HotspotInterfaceName) {
			return ra.Interface == HotspotInterfaceName
		}
		return phyNumber(ra) < phyNumber(rb)
	})
	return order
}

// SupportsAPMode reads `iw phy <name> info` output and reports whether "AP"
// appears in the "Supported interface modes" list, which looks like:
//
//	Supported interface modes:
//		 * IBSS
//		 * managed
//		 * AP
//		 * monitor
func SupportsAPMode(iwPhyInfo string) bool {
	inModesList := false
	for _, line := range strings.Split(iwPhyInfo, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Supported interface modes:" {
			inModesList = true
			continue
		}
		if !inModesList {
			continue
		}
		// The list ends at the first line that is not a "* mode" item.
		if !strings.HasPrefix(trimmed, "* ") {
			return false
		}
		if strings.TrimPrefix(trimmed, "* ") == "AP" {
			return true
		}
	}
	return false
}

// MaxAPInterfaces reads `iw phy <name> info` output and returns how many
// access point (hotspot) interfaces the radio can run at the same time. The
// driver lists what it can combine under "valid interface combinations":
//
//	valid interface combinations:
//		 * #{ managed } <= 1, #{ AP, mesh point } <= 4,
//		   total <= 4, #channels <= 1
//
// Here the answer is 4. Radios that list no combinations can run only one
// interface; radios without AP mode, none.
func MaxAPInterfaces(iwPhyInfo string) int {
	if !SupportsAPMode(iwPhyInfo) {
		return 0
	}
	best := 1
	inCombinations := false
	var combination strings.Builder
	finish := func() {
		if n := apsInCombination(combination.String()); n > best {
			best = n
		}
		combination.Reset()
	}
	for _, line := range strings.Split(iwPhyInfo, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "valid interface combinations:":
			inCombinations = true
		case !inCombinations:
		case strings.HasPrefix(trimmed, "* "):
			finish() // a new combination starts
			combination.WriteString(strings.TrimPrefix(trimmed, "* "))
		case strings.HasPrefix(trimmed, "total") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "Radar"):
			combination.WriteString(" " + trimmed) // continuation line
		default:
			inCombinations = false // the list has ended
			finish()
		}
	}
	finish()
	return best
}

// apsInCombination returns how many AP interfaces one combination allows,
// for example 4 for "#{ managed } <= 1, #{ AP, mesh point } <= 4, total <= 4".
func apsInCombination(text string) int {
	aps, total := 0, -1
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "total <=") {
			total = atoi(strings.TrimSpace(strings.TrimPrefix(part, "total <=")))
		}
	}
	// Each "#{ types } <= n" group: n interfaces of any of these types.
	for rest := text; ; {
		start := strings.Index(rest, "#{")
		if start < 0 {
			break
		}
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			break
		}
		types := rest[start+2 : start+end]
		rest = rest[start+end+1:]
		limit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "<="))
		if i := strings.IndexAny(limit, ", "); i >= 0 {
			limit = limit[:i]
		}
		for _, t := range strings.Split(types, ",") {
			if strings.TrimSpace(t) == "AP" {
				aps += atoi(limit)
			}
		}
	}
	if total >= 0 && aps > total {
		return total
	}
	return aps
}

// atoi reads a small non-negative number, or returns 0.
func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// Supports5GHz reads `iw phy <name> info` output and reports whether the
// radio can use the 5 GHz band. iw lists each band the radio has as
// "Band 1:" (2.4 GHz), "Band 2:" (5 GHz) and so on, so a radio without a
// "Band 2:" section is 2.4 GHz only.
func Supports5GHz(iwPhyInfo string) bool {
	for _, line := range strings.Split(iwPhyInfo, "\n") {
		if strings.TrimSpace(line) == "Band 2:" {
			return true
		}
	}
	return false
}

// SupportsWiFi6AP reads `iw phy <name> info` output and reports whether the
// radio can run a Wi-Fi 6 (802.11ax, "HE") hotspot. iw lists, for each band,
// which modes may use HE:
//
//	HE Iftypes: managed, AP
//
// A radio that only joins Wi-Fi 6 networks lists "managed" alone.
func SupportsWiFi6AP(iwPhyInfo string) bool {
	for _, line := range strings.Split(iwPhyInfo, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "HE Iftypes:") {
			continue
		}
		for _, mode := range strings.Split(strings.TrimPrefix(line, "HE Iftypes:"), ",") {
			if strings.TrimSpace(mode) == "AP" {
				return true
			}
		}
	}
	return false
}

// SupportsWPA3AP reads `iw phy <name> info` output and reports whether the
// radio can run a WPA3 hotspot. WPA3 needs protected management frames,
// which work in one of two ways:
//
//   - Full-MAC chips (brcmfmac, the Pi's built-in Wi-Fi) do the WPA3
//     handshake in their firmware, which must say so: the extended feature
//     SAE_OFFLOAD_AP. ("Device supports SAE with AUTHENTICATE command" is
//     about joining WPA3 networks, not running one.)
//   - Other drivers (mt76, rtw88 and the like) let hostapd do the handshake;
//     the radio then only needs the BIP-CMAC-128 cipher, which iw lists as
//     "CMAC (00-0f-ac:6)" under "Supported Ciphers".
func SupportsWPA3AP(driver, iwPhyInfo string) bool {
	if driver == "brcmfmac" {
		return strings.Contains(iwPhyInfo, "SAE_OFFLOAD_AP")
	}
	return strings.Contains(iwPhyInfo, "(00-0f-ac:6)")
}

// ChannelAllowed reads `iw phy <name> info` output and reports whether the
// radio may run a hotspot on channel under the current country's rules.
// iw lists every channel with what the country allows, for example:
//
//   - 2412 MHz [1] (20.0 dBm)
//   - 2467 MHz [12] (disabled)
//   - 5260 MHz [52] (20.0 dBm) (no IR, radar detection)
//
// "disabled" means the channel may not be used at all, and "no IR" ("no
// initiating radiation") that the radio may not start a network on it, so
// a hotspot can't use either. listed is false when the channel isn't in
// the output at all, so the caller can't tell.
func ChannelAllowed(iwPhyInfo string, channel int) (allowed, listed bool) {
	want := fmt.Sprintf("[%d]", channel)
	for _, line := range strings.Split(iwPhyInfo, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "* ") || !strings.Contains(trimmed, " MHz "+want) {
			continue
		}
		forbidden := strings.Contains(trimmed, "(disabled)") || strings.Contains(trimmed, "no IR")
		return !forbidden, true
	}
	return false, false
}

// readTrimmed reads a small sysfs file and removes the trailing newline.
func readTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// linkBaseName returns the last part of a symlink's target, e.g. the driver
// name from ".../bus/usb/drivers/rtw88_8822bu". Empty if not a link.
func linkBaseName(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}
