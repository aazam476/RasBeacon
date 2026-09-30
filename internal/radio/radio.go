// Package radio finds the Wi-Fi adapters in the device and decides which one
// runs the hotspot.
//
// # Why this matters
//
// The Raspberry Pi's built-in Wi-Fi chip can only hold about 8 connected
// devices when it acts as a hotspot, which is not enough. So RasBeacon uses a
// USB Wi-Fi adapter for the hotspot and keeps the built-in chip for joining
// the upstream network (the "uplink", added in milestone 2).
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
	"strings"
)

// Role is the job a radio is given.
type Role string

const (
	// RoleHotspot broadcasts the network your devices join.
	RoleHotspot Role = "hotspot"
	// RoleUplink joins the upstream network (hotel, eduroam, ...).
	RoleUplink Role = "uplink"
	// RoleUnused means the radio was found but has no job.
	RoleUnused Role = "unused"
)

// HotspotInterfaceName is the name the hotspot radio is renamed to, so every
// other part of RasBeacon can refer to it without guessing.
const HotspotInterfaceName = "ap0"

// Radio describes one Wi-Fi interface.
type Radio struct {
	Interface string `json:"interface"` // Linux interface name, e.g. "wlan1"
	Phy       string `json:"phy"`       // radio name, e.g. "phy1"
	Driver    string `json:"driver"`    // e.g. "brcmfmac" (built-in) or "rtw88_8822bu"
	IsUSB     bool   `json:"isUsb"`
	// USBID is "vendor:product" as shown by lsusb, e.g. "2357:012d" for a
	// TP-Link Archer T3U. Empty for built-in radios.
	USBID      string `json:"usbId"`
	SupportsAP bool   `json:"supportsAp"`
	Role       Role   `json:"role"`
}

// PhyInfoFunc returns the output of `iw phy <phy> info`. It is a parameter so
// tests can supply canned output instead of running the command.
type PhyInfoFunc func(phy string) (string, error)

// Detect lists the Wi-Fi interfaces under sysClassNet (normally
// /sys/class/net), sorted by interface name. Roles are not assigned yet; see
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

		r := Radio{
			Interface: name,
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
		}

		radios = append(radios, r)
	}

	sort.Slice(radios, func(i, j int) bool { return radios[i].Interface < radios[j].Interface })
	return radios, nil
}

// AssignRoles decides which radio runs the hotspot and which joins the uplink.
//
// Rules, in order:
//  1. Hotspot: the first USB radio that supports AP mode.
//  2. If there is no such USB radio, fall back to a built-in radio that
//     supports AP mode, so you can still reach the setup page. The returned
//     warning explains the ~8 client limit.
//  3. Uplink: the first remaining radio (normally the built-in one).
//
// It returns the radios with roles filled in, plus human-readable warnings.
func AssignRoles(radios []Radio) ([]Radio, []string) {
	result := make([]Radio, len(radios))
	copy(result, radios)
	var warnings []string

	hotspotIndex := -1
	for i, r := range result {
		if r.IsUSB && r.SupportsAP {
			hotspotIndex = i
			break
		}
	}
	if hotspotIndex == -1 {
		for i, r := range result {
			if r.SupportsAP {
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

	for i := range result {
		if i != hotspotIndex {
			result[i].Role = RoleUplink
			break
		}
	}
	return result, warnings
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
