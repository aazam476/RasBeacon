package radio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// iwOutputWithAP is a shortened real `iw phy info` output from a radio that
// can run a hotspot.
const iwOutputWithAP = `Wiphy phy1
	Supported interface modes:
		 * IBSS
		 * managed
		 * AP
		 * monitor
	Band 1:
`

const iwOutputWithoutAP = `Wiphy phy0
	Supported interface modes:
		 * managed
		 * monitor
	Supported commands:
		 * AP
`

func TestSupportsAPMode(t *testing.T) {
	if !SupportsAPMode(iwOutputWithAP) {
		t.Error("expected AP support to be detected")
	}
	// "* AP" under "Supported commands" must not count as an interface mode.
	if SupportsAPMode(iwOutputWithoutAP) {
		t.Error("AP listed outside 'Supported interface modes' must not count")
	}
}

// fakeSysfs builds a directory tree that looks like /sys/class/net with:
//   - wlan0: built-in Broadcom radio (brcmfmac), no USB
//   - wlan1: USB TP-Link Archer T3U (rtw88_8822bu, 2357:012d)
//   - eth0:  wired Ethernet (not Wi-Fi, must be ignored)
func fakeSysfs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	classNet := filepath.Join(root, "class", "net")
	devices := filepath.Join(root, "devices")
	drivers := filepath.Join(root, "drivers")

	mkdir := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, content string) {
		mkdir(filepath.Dir(p))
		if err := os.WriteFile(p, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) {
		mkdir(filepath.Dir(name))
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}

	// Built-in radio on the SDIO bus.
	builtinDev := filepath.Join(devices, "platform", "mmc1", "mmc1:0001:1")
	mkdir(builtinDev)
	mkdir(filepath.Join(drivers, "brcmfmac"))
	write(filepath.Join(classNet, "wlan0", "phy80211", "name"), "phy0")
	link(builtinDev, filepath.Join(classNet, "wlan0", "device"))
	link(filepath.Join(drivers, "brcmfmac"), filepath.Join(builtinDev, "driver"))

	// USB radio: the interface directory (1-1:1.0) sits inside the USB device
	// directory (1-1), which holds idVendor/idProduct.
	usbDevice := filepath.Join(devices, "usb1", "1-1")
	usbInterface := filepath.Join(usbDevice, "1-1:1.0")
	mkdir(usbInterface)
	write(filepath.Join(usbDevice, "idVendor"), "2357")
	write(filepath.Join(usbDevice, "idProduct"), "012d")
	mkdir(filepath.Join(drivers, "rtw88_8822bu"))
	write(filepath.Join(classNet, "wlan1", "phy80211", "name"), "phy1")
	link(usbInterface, filepath.Join(classNet, "wlan1", "device"))
	link(filepath.Join(drivers, "rtw88_8822bu"), filepath.Join(usbInterface, "driver"))

	// Wired Ethernet: no phy80211 directory.
	mkdir(filepath.Join(classNet, "eth0"))

	return classNet
}

func TestDetect_FindsBuiltInAndUSBRadios(t *testing.T) {
	classNet := fakeSysfs(t)
	phyInfo := func(phy string) (string, error) { return iwOutputWithAP, nil }

	radios, err := Detect(classNet, phyInfo)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(radios) != 2 {
		t.Fatalf("found %d radios, want 2 (eth0 must be ignored): %+v", len(radios), radios)
	}

	builtin, usb := radios[0], radios[1]
	if builtin.Interface != "wlan0" || builtin.Driver != "brcmfmac" || builtin.IsUSB {
		t.Errorf("built-in radio detected wrong: %+v", builtin)
	}
	if usb.Interface != "wlan1" || usb.Driver != "rtw88_8822bu" || !usb.IsUSB || usb.USBID != "2357:012d" {
		t.Errorf("USB radio detected wrong: %+v", usb)
	}
}

// While the guest network is on, the hotspot's USB radio has two
// interfaces, ap0 and guest0. After an rbd restart the radio is still one
// radio: guest0 must not become the uplink, the built-in wlan0 must.
func TestDetect_ARadioWithTwoInterfacesIsOneRadio(t *testing.T) {
	classNet := fakeSysfs(t)
	// The hotspot radio was renamed ap0; hostapd added guest0 on it.
	if err := os.Rename(filepath.Join(classNet, "wlan1"), filepath.Join(classNet, "ap0")); err != nil {
		t.Fatal(err)
	}
	guest := filepath.Join(classNet, "guest0")
	os.MkdirAll(filepath.Join(guest, "phy80211"), 0o755)
	os.WriteFile(filepath.Join(guest, "phy80211", "name"), []byte("phy1\n"), 0o644)

	radios, err := Detect(classNet, func(string) (string, error) { return iwOutputWithAP, nil })
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range radios {
		names = append(names, r.Interface)
	}
	if strings.Join(names, " ") != "ap0 wlan0" {
		t.Fatalf("radios = %v, want ap0 and wlan0 (guest0 is ap0's radio)", names)
	}
	assigned, _ := AssignRoles(radios)
	if assigned[0].Role != RoleHotspot || assigned[1].Role != RoleUplink {
		t.Errorf("roles = %+v", assigned)
	}
}

func TestAssignRoles_USBAdapterRunsHotspot(t *testing.T) {
	radios := []Radio{
		{Interface: "wlan0", Driver: "brcmfmac", SupportsAP: true},
		{Interface: "wlan1", Driver: "rtw88_8822bu", IsUSB: true, SupportsAP: true},
	}
	got, warnings := AssignRoles(radios)

	if got[1].Role != RoleHotspot {
		t.Errorf("USB radio role = %q, want hotspot", got[1].Role)
	}
	if got[0].Role != RoleUplink {
		t.Errorf("built-in radio role = %q, want uplink", got[0].Role)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestAssignRoles_NoUSBAdapterFallsBackWithWarning(t *testing.T) {
	radios := []Radio{{Interface: "wlan0", Driver: "brcmfmac", SupportsAP: true}}
	got, warnings := AssignRoles(radios)

	if got[0].Role != RoleHotspot {
		t.Errorf("built-in radio should run the hotspot as a fallback, got %q", got[0].Role)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "about 8 devices") {
		t.Errorf("expected a warning about the client limit, got %v", warnings)
	}
}

func TestAssignRoles_NoAPCapableRadio(t *testing.T) {
	radios := []Radio{{Interface: "wlan0", SupportsAP: false}}
	got, warnings := AssignRoles(radios)
	if got[0].Role == RoleHotspot {
		t.Error("a radio without AP support must not be given the hotspot role")
	}
	if len(warnings) == 0 {
		t.Error("expected a warning")
	}
}

func TestMaxAPInterfaces(t *testing.T) {
	modes := "\tSupported interface modes:\n\t\t * managed\n\t\t * AP\n\t\t * monitor\n"
	cases := []struct {
		name string
		info string
		want int
	}{
		{"no AP mode", "\tSupported interface modes:\n\t\t * managed\n", 0},
		{"AP, no combinations listed", modes, 1},
		{
			// mac80211_hwsim (the simulated radios in test/hwsim).
			"hwsim", modes + "\tvalid interface combinations:\n" +
				"\t\t * #{ managed } <= 2048, #{ AP, mesh point } <= 2048, #{ P2P-client, P2P-GO } <= 2048, #{ P2P-device } <= 1,\n" +
				"\t\t   total <= 2048, #channels <= 1, STA/AP BI must match\n" +
				"\tHT Capability overrides:\n", 2048,
		},
		{
			// The Raspberry Pi's built-in radio (brcmfmac).
			"brcmfmac", modes + "\tvalid interface combinations:\n" +
				"\t\t * #{ managed } <= 1, #{ P2P-device } <= 1, #{ P2P-client, P2P-GO } <= 1,\n" +
				"\t\t   total <= 3, #channels <= 2\n" +
				"\t\t * #{ managed } <= 1, #{ AP } <= 1, #{ P2P-client } <= 1, #{ P2P-device } <= 1,\n" +
				"\t\t   total <= 4, #channels <= 1\n", 1,
		},
		{
			"limited by total", modes + "\tvalid interface combinations:\n" +
				"\t\t * #{ managed } <= 1, #{ AP } <= 4,\n\t\t   total <= 2, #channels <= 1\n", 2,
		},
	}
	for _, c := range cases {
		if got := MaxAPInterfaces(c.info); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// iwOutputDualBand is a shortened `iw phy info` output of a radio with both
// bands, after the country was set: channel 12 is off, and 52 needs radar
// detection, so a hotspot can't start there.
const iwOutputDualBand = `Wiphy phy1
	Band 1:
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
			* 2437 MHz [6] (20.0 dBm)
			* 2467 MHz [12] (disabled)
	Band 2:
		Frequencies:
			* 5180 MHz [36] (23.0 dBm)
			* 5260 MHz [52] (20.0 dBm) (no IR, radar detection)
`

func TestSupports5GHz(t *testing.T) {
	if !Supports5GHz(iwOutputDualBand) {
		t.Error("a radio with a Band 2 section has 5 GHz")
	}
	if Supports5GHz(iwOutputWithAP) {
		t.Error("a radio with only Band 1 is 2.4 GHz only")
	}
}

func TestSupportsWPA3AP(t *testing.T) {
	// Excerpts of real `iw phy` output from the owner's Pi 4.
	const rtw88 = "Wiphy phy2\n\tSupported Ciphers:\n\t\t* WEP40 (00-0f-ac:1)\n\t\t* CCMP-128 (00-0f-ac:4)\n" +
		"\t\t* CMAC (00-0f-ac:6)\n\t\t* CMAC-256 (00-0f-ac:13)\n"
	const brcmfmac = "Wiphy phy1\n\tSupported Ciphers:\n\t\t* CCMP-128 (00-0f-ac:4)\n\t\t* CMAC (00-0f-ac:6)\n" +
		"\tDevice supports SAE with AUTHENTICATE command\n\tSupported extended features:\n\t\t* [ 4WAY_HANDSHAKE_STA_PSK ]: 4-way handshake with PSK in station mode\n"
	for _, tc := range []struct {
		name, driver, info string
		want               bool
	}{
		{"a soft-MAC radio with CMAC", "rtw88_8822bu", rtw88, true},
		{"a soft-MAC radio without it", "mt76x2u", "Wiphy phy2\n\tSupported Ciphers:\n\t\t* CCMP-128 (00-0f-ac:4)\n", false},
		// It lists CMAC and joins WPA3 networks, but can't run a WPA3 hotspot.
		{"brcmfmac without SAE offload", "brcmfmac", brcmfmac, false},
		{"brcmfmac with SAE offload", "brcmfmac", brcmfmac + "\t\t* [ SAE_OFFLOAD_AP ]: support for SAE authentication offload in AP mode\n", true},
	} {
		if got := SupportsWPA3AP(tc.driver, tc.info); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestChannelAllowed(t *testing.T) {
	tests := []struct {
		channel        int
		allowed, known bool
	}{
		{1, true, true},
		{36, true, true},
		{12, false, true}, // disabled in this country
		{52, false, true}, // "no IR": may not start a network
		{149, false, false},
	}
	for _, tt := range tests {
		allowed, known := ChannelAllowed(iwOutputDualBand, tt.channel)
		if allowed != tt.allowed || known != tt.known {
			t.Errorf("channel %d = allowed %v, listed %v; want %v, %v", tt.channel, allowed, known, tt.allowed, tt.known)
		}
	}
}

func TestSupportsWiFi6AP(t *testing.T) {
	// Excerpts of `iw phy` output: an MT7921AU (Wi-Fi 6, AP on 2.4 and 5 GHz)
	// and a radio that only joins Wi-Fi 6 networks.
	const mt7921 = "Wiphy phy3\n\tBand 1:\n\t\tHE Iftypes: managed, AP\n\t\t\tHE MAC Capabilities (0x080112a09801):\n\tBand 2:\n\t\tHE Iftypes: managed, AP\n"
	const stationOnly = "Wiphy phy1\n\tBand 1:\n\t\tHE Iftypes: managed\n"
	for name, tc := range map[string]struct {
		info string
		want bool
	}{
		"AP listed":     {mt7921, true},
		"managed only":  {stationOnly, false},
		"no Wi-Fi 6":    {"Wiphy phy0\n\tBand 1:\n\t\tCapabilities: 0x1862\n", false},
		"AP without HE": {"Wiphy phy0\n\tSupported interface modes:\n\t\t * AP\n", false},
	} {
		if got := SupportsWiFi6AP(tc.info); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
