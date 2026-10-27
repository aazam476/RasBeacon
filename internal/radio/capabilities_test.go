package radio

import (
	"os"
	"strings"
	"testing"
)

func TestParseBandCaps_MT7921AU(t *testing.T) {
	info, err := os.ReadFile("testdata/mt7921au.txt")
	if err != nil {
		t.Fatal(err)
	}
	bands := ParseBandCaps(string(info))

	twoFour := bands[Band24]
	if !twoFour.HasHT || twoFour.HT != 0x9ff || twoFour.HasVHT {
		t.Errorf("2.4 GHz: %+v", twoFour)
	}
	// The managed block's "MU Beamformer" isn't the AP's.
	if !twoFour.HESUBeamformee || twoFour.HESUBeamformer || twoFour.HEMUBeamformer {
		t.Errorf("2.4 GHz HE (AP): %+v", twoFour)
	}
	five := bands[Band5]
	if !five.HasVHT || five.VHT != 0x339071b2 || !five.HESUBeamformer || !five.HESUBeamformee {
		t.Errorf("5 GHz: %+v", five)
	}
	six := bands[Band6]
	if six.HasHT || six.HasVHT || !six.HESUBeamformee || six.HESUBeamformer {
		t.Errorf("6 GHz: %+v", six)
	}
}

func TestHTFlags(t *testing.T) {
	for _, tc := range []struct {
		ht   uint16
		wide bool
		want string
	}{
		{0x9ff, true, "[LDPC][SHORT-GI-20][SHORT-GI-40][TX-STBC][RX-STBC1][MAX-AMSDU-7935]"},
		{0x9ff, false, "[LDPC][SHORT-GI-20][TX-STBC][RX-STBC1][MAX-AMSDU-7935]"},
		{0x22e, true, "[SHORT-GI-20][RX-STBC12]"}, // 0x22e: bits 1-3, 5, 9
		{0x0300, false, "[RX-STBC123]"},
		{0, true, ""},
	} {
		if got := HTFlags(tc.ht, tc.wide); got != tc.want {
			t.Errorf("HTFlags(%#x, %v) = %s, want %s", tc.ht, tc.wide, got, tc.want)
		}
	}
}

func TestVHTFlags(t *testing.T) {
	for _, tc := range []struct {
		vht  uint32
		want string
	}{
		{0x339071b2, "[MAX-MPDU-11454][RXLDPC][SHORT-GI-80][TX-STBC-2BY1][RX-STBC-1][SU-BEAMFORMEE][BF-ANTENNA-4][MU-BEAMFORMEE][MAX-A-MPDU-LEN-EXP7][RX-ANTENNA-PATTERN][TX-ANTENNA-PATTERN]"},
		// An SU beamformer with 2 sounding dimensions, 2 RX STBC streams,
		// a 7991-byte MPDU and an MU beamformer.
		{1 | 2<<8 | 1<<11 | 1<<16 | 1<<19, "[MAX-MPDU-7991][RX-STBC-12][SU-BEAMFORMER][SOUNDING-DIMENSION-2][MU-BEAMFORMER]"},
		{0, ""},
	} {
		if got := VHTFlags(tc.vht); got != tc.want {
			t.Errorf("VHTFlags(%#x) = %s, want %s", tc.vht, got, tc.want)
		}
	}
}

func TestParseBandCaps_ChannelsAnd6GHz(t *testing.T) {
	info, err := os.ReadFile("testdata/mt7921au.txt")
	if err != nil {
		t.Fatal(err)
	}
	bands := ParseBandCaps(string(info))
	if !bands[Band5].Channels[36] || bands[Band5].Channels[52] {
		t.Errorf("5 GHz channels: %v", bands[Band5].Channels)
	}
	// 6 GHz channel 1 is "no IR"; 37 is allowed. 2.4 GHz has its own 1.
	if bands[Band6].Channels[1] || !bands[Band6].Channels[37] || !bands[Band24].Channels[1] {
		t.Errorf("6 GHz channels: %v, 2.4 GHz: %v", bands[Band6].Channels, bands[Band24].Channels)
	}
	if !bands[Band6].HEAP || !bands[Band6].Known() || (BandCaps{}).Known() {
		t.Error("6 GHz runs Wi-Fi 6 in AP mode")
	}
}

// Detect reads each radio's capabilities, and whether it can run a 6 GHz
// hotspot.
func TestDetect_CapabilitiesAnd6GHz(t *testing.T) {
	classNet := fakeSysfs(t)
	info, err := os.ReadFile("testdata/mt7921au.txt")
	if err != nil {
		t.Fatal(err)
	}
	radios, err := Detect(classNet, func(phy string) (string, error) {
		if phy == "phy1" {
			return string(info), nil
		}
		return iwOutputWithAP, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if radios[0].Has6GHz || !radios[1].Has6GHz || !radios[1].HasWiFi6 || radios[1].Caps[Band5].VHT != 0x339071b2 {
		t.Errorf("radios: %+v", radios)
	}
}

// Under the US rules every 6 GHz channel is "no IR", yet the adapter can
// still run a 6 GHz hotspot once the owner picks a country that allows it.
func TestDetect_6GHzWhateverTheCountryNow(t *testing.T) {
	info, err := os.ReadFile("testdata/mt7921au.txt")
	if err != nil {
		t.Fatal(err)
	}
	us := strings.ReplaceAll(string(info), "(23.0 dBm)", "(no IR)")
	if caps := ParseBandCaps(us)[Band6]; caps.Channels[37] || !caps.HEAP {
		t.Fatalf("test setup: %+v", caps)
	}
	radios, err := Detect(fakeSysfs(t), func(phy string) (string, error) {
		if phy == "phy1" {
			return us, nil
		}
		return iwOutputWithAP, nil
	})
	if err != nil || !radios[1].Has6GHz {
		t.Errorf("radios: %+v %v", radios, err)
	}
}

func TestSupportsWiFi7AP(t *testing.T) {
	if !SupportsWiFi7AP("\tBand 4:\n\t\tEHT Iftypes: managed, AP\n") ||
		SupportsWiFi7AP("\tBand 2:\n\t\tEHT Iftypes: managed\n\t\tHE Iftypes: managed, AP\n") {
		t.Error("EHT AP")
	}
}
