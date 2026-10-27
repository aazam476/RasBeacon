package radio

// What a radio can do on each band, read from `iw phy <name> info`, so the
// hotspot claims exactly that: devices then use the fastest rates both
// sides support (short guard intervals, LDPC coding, beamforming, more
// spatial streams), and hostapd never claims something the driver can't
// do (it refuses to start then).
//
// iw prints, for each band, the 802.11n ("HT") and 802.11ac ("VHT")
// capability fields as numbers, then their meaning line by line:
//
//	Band 2:
//		Capabilities: 0x19ef
//			RX LDPC
//			...
//		VHT Capabilities (0x339071b2):
//			Max MPDU length: 11454
//			...
//
// The numbers are the fields as the standard defines them (IEEE 802.11-2020
// 9.4.2.55 and 9.4.2.157), which map one to one onto hostapd's ht_capab and
// vht_capab flags. Wi-Fi 6 ("HE") capabilities are filled in by hostapd from
// the driver by itself, except beamforming, which must be asked for.

import (
	"strconv"
	"strings"
)

// Band numbers as iw prints them ("Band 1:").
const (
	Band24 = 1
	Band5  = 2
	Band6  = 4
)

// BandCaps is what a radio can do on one band.
type BandCaps struct {
	// HT and VHT are the 802.11n and 802.11ac capability fields; HasHT and
	// HasVHT say whether iw listed them.
	HT     uint16
	HasHT  bool
	VHT    uint32
	HasVHT bool
	// HEAP is true when the band can run a Wi-Fi 6 access point; the rest
	// is its beamforming, from the band's Wi-Fi 6 capabilities for AP mode.
	HEAP                                           bool
	HESUBeamformer, HESUBeamformee, HEMUBeamformer bool
	// Channels are the band's channels, true where the current country
	// lets the radio run a hotspot (not disabled, not "no IR").
	Channels map[int]bool
}

// Known reports whether iw listed any capability for the band.
func (c BandCaps) Known() bool {
	return c.HasHT || c.HasVHT || c.HESUBeamformer || c.HESUBeamformee || c.HEMUBeamformer
}

// ParseBandCaps reads each band's capabilities from `iw phy <name> info`,
// by band number (Band24, Band5, Band6).
func ParseBandCaps(iwPhyInfo string) map[int]BandCaps {
	bands := map[int]BandCaps{}
	band := 0
	// heIndent is the indentation of the "HE Iftypes:" line of an AP
	// block being read (0: not in one); lines indented more belong to it.
	heIndent := 0
	for _, line := range strings.Split(iwPhyInfo, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if strings.HasPrefix(trimmed, "Band ") && strings.HasSuffix(trimmed, ":") {
			band, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(trimmed, "Band "), ":"))
			heIndent = 0
			continue
		}
		if band == 0 {
			continue
		}
		caps := bands[band]
		switch {
		case heIndent > 0 && indent > heIndent:
			switch trimmed {
			case "SU Beamformer":
				caps.HESUBeamformer = true
			case "SU Beamformee":
				caps.HESUBeamformee = true
			case "MU Beamformer":
				caps.HEMUBeamformer = true
			}
		case strings.HasPrefix(trimmed, "HE Iftypes:"):
			heIndent = 0
			for _, mode := range strings.Split(strings.TrimPrefix(trimmed, "HE Iftypes:"), ",") {
				if strings.TrimSpace(mode) == "AP" {
					heIndent = indent
					caps.HEAP = true
				}
			}
		case strings.HasPrefix(trimmed, "* ") && strings.Contains(trimmed, " MHz ["):
			heIndent = 0
			// * 5180 MHz [36] (23.0 dBm), or with (disabled) or (no IR)
			open, end := strings.Index(trimmed, "["), strings.Index(trimmed, "]")
			if channel, err := strconv.Atoi(trimmed[open+1 : max(end, open+1)]); err == nil {
				if caps.Channels == nil {
					caps.Channels = map[int]bool{}
				}
				caps.Channels[channel] = !strings.Contains(trimmed, "(disabled)") && !strings.Contains(trimmed, "no IR")
			}
		case strings.HasPrefix(trimmed, "Capabilities: 0x"):
			heIndent = 0
			if v, err := strconv.ParseUint(strings.TrimPrefix(trimmed, "Capabilities: 0x"), 16, 16); err == nil {
				caps.HT, caps.HasHT = uint16(v), true
			}
		case strings.HasPrefix(trimmed, "VHT Capabilities (0x"):
			heIndent = 0
			hex := strings.TrimSuffix(strings.TrimPrefix(trimmed, "VHT Capabilities (0x"), "):")
			if v, err := strconv.ParseUint(hex, 16, 32); err == nil {
				caps.VHT, caps.HasVHT = uint32(v), true
			}
		default:
			if heIndent > 0 && indent <= heIndent {
				heIndent = 0 // the AP block ended
			}
		}
		bands[band] = caps
	}
	return bands
}

// HTFlags turns an 802.11n capability field into hostapd's ht_capab flags,
// for a channel 40 MHz wide (wide) or 20 MHz. The 40 MHz direction
// ([HT40+] or [HT40-]) is added by the caller, which knows the channel.
// Power save and greenfield are left to hostapd's defaults.
func HTFlags(ht uint16, wide bool) string {
	var flags []string
	add := func(bit uint16, flag string) {
		if ht&bit != 0 {
			flags = append(flags, flag)
		}
	}
	add(1<<0, "[LDPC]")
	add(1<<5, "[SHORT-GI-20]")
	if wide {
		add(1<<6, "[SHORT-GI-40]")
	}
	add(1<<7, "[TX-STBC]")
	switch (ht >> 8) & 3 {
	case 1:
		flags = append(flags, "[RX-STBC1]")
	case 2:
		flags = append(flags, "[RX-STBC12]")
	case 3:
		flags = append(flags, "[RX-STBC123]")
	}
	add(1<<11, "[MAX-AMSDU-7935]")
	return strings.Join(flags, "")
}

// VHTFlags turns an 802.11ac capability field into hostapd's vht_capab
// flags for an 80 MHz channel (160 MHz isn't used).
func VHTFlags(vht uint32) string {
	var flags []string
	add := func(bit uint32, flag string) {
		if vht&bit != 0 {
			flags = append(flags, flag)
		}
	}
	switch vht & 3 {
	case 1:
		flags = append(flags, "[MAX-MPDU-7991]")
	case 2:
		flags = append(flags, "[MAX-MPDU-11454]")
	}
	add(1<<4, "[RXLDPC]")
	add(1<<5, "[SHORT-GI-80]")
	add(1<<7, "[TX-STBC-2BY1]")
	if n := (vht >> 8) & 7; n >= 1 && n <= 4 {
		flags = append(flags, "[RX-STBC-"+"1234"[:n]+"]")
	}
	add(1<<11, "[SU-BEAMFORMER]")
	add(1<<12, "[SU-BEAMFORMEE]")
	// Beamformee antennas and sounding dimensions are stored as count - 1.
	if n := (vht >> 13) & 7; vht&(1<<12) != 0 && n >= 1 {
		flags = append(flags, "[BF-ANTENNA-"+strconv.Itoa(int(n)+1)+"]")
	}
	if n := (vht >> 16) & 7; vht&(1<<11) != 0 && n >= 1 {
		flags = append(flags, "[SOUNDING-DIMENSION-"+strconv.Itoa(int(n)+1)+"]")
	}
	add(1<<19, "[MU-BEAMFORMER]")
	add(1<<20, "[MU-BEAMFORMEE]")
	if n := (vht >> 23) & 7; n > 0 {
		flags = append(flags, "[MAX-A-MPDU-LEN-EXP"+strconv.Itoa(int(n))+"]")
	}
	add(1<<28, "[RX-ANTENNA-PATTERN]")
	add(1<<29, "[TX-ANTENNA-PATTERN]")
	return strings.Join(flags, "")
}
