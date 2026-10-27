package uplink

import (
	"net/netip"
	"testing"
)

func TestParseDefaultRoutes_BestFirst(t *testing.T) {
	out := `[{"dst":"default","gateway":"192.168.4.1","dev":"wlan0","protocol":"dhcp","metric":600,"flags":[]},
	         {"dst":"default","gateway":"10.20.0.1","dev":"eth0","protocol":"dhcp","metric":100,"flags":[]}]`
	routes, err := ParseDefaultRoutes(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Interface != "eth0" || routes[0].Gateway != "10.20.0.1" {
		t.Fatalf("got %+v, want eth0 first", routes)
	}
}

func TestParseDefaultRoutes_NoneIsNotAnError(t *testing.T) {
	for _, out := range []string{"", "[]\n"} {
		routes, err := ParseDefaultRoutes(out)
		if err != nil || len(routes) != 0 {
			t.Errorf("%q: got %v, %v", out, routes, err)
		}
	}
}

func TestParseAddresses_GlobalOnly(t *testing.T) {
	out := `[{"ifindex":1,"ifname":"lo","addr_info":[{"family":"inet","local":"127.0.0.1","prefixlen":8,"scope":"host"}]},
	{"ifindex":2,"ifname":"eth0","addr_info":[
		{"family":"inet","local":"192.168.1.57","prefixlen":24,"scope":"global"},
		{"family":"inet6","local":"2001:db8::5","prefixlen":64,"scope":"global"},
		{"family":"inet6","local":"fe80::1","prefixlen":64,"scope":"link"}]}]`
	got, err := ParseAddresses(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("192.168.1.57/24"), netip.MustParsePrefix("2001:db8::5/64")}
	if len(got["eth0"]) != 2 || got["eth0"][0] != want[0] || got["eth0"][1] != want[1] {
		t.Errorf("eth0 = %v, want %v", got["eth0"], want)
	}
	if len(got["lo"]) != 0 {
		t.Errorf("loopback should be left out, got %v", got["lo"])
	}
}

func TestParseWPAStatus_Connected(t *testing.T) {
	out := "bssid=aa:bb:cc:dd:ee:ff\nfreq=5180\nssid=Hotel \\\"Guest\\\"\nid=0\nmode=station\n" +
		"pairwise_cipher=CCMP\nkey_mgmt=WPA2-PSK\nwpa_state=COMPLETED\nip_address=192.168.4.20\n"
	s := ParseWPAStatus(out)
	if !s.Connected() || s.SSID != `Hotel "Guest"` || s.FrequencyMHz != 5180 || s.KeyMgmt != "WPA2-PSK" || s.BSSID != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("got %+v", s)
	}
	if BandName(s.FrequencyMHz) != "5GHz" {
		t.Errorf("band = %s", BandName(s.FrequencyMHz))
	}
}

func TestParseWPAStatus_Generation(t *testing.T) {
	if s := ParseWPAStatus("wpa_state=COMPLETED\nfreq=2437\nwifi_generation=5\n"); s.Generation != 5 {
		t.Errorf("generation = %d", s.Generation)
	}
	for _, c := range []struct {
		generation, freq int
		want             string
	}{{4, 2437, "wifi4"}, {5, 5180, "wifi5"}, {6, 5180, "wifi6"}, {6, 6135, "wifi6e"}, {7, 6135, "wifi7"}, {0, 5180, ""}, {3, 2437, ""}} {
		if got := StandardOf(c.generation, c.freq); got != c.want {
			t.Errorf("StandardOf(%d, %d) = %q, want %q", c.generation, c.freq, got, c.want)
		}
	}
}

func TestParseWPAStatus_Scanning(t *testing.T) {
	s := ParseWPAStatus("wpa_state=SCANNING\n")
	if s.Connected() {
		t.Error("scanning is not connected")
	}
}

func TestParseScanResults(t *testing.T) {
	out := "bssid / frequency / signal level / flags / ssid\n" +
		"aa:00:00:00:00:01\t2437\t-71\t[WPA2-PSK-CCMP][ESS]\tHome\n" +
		"aa:00:00:00:00:02\t5180\t-52\t[WPA2-PSK-CCMP][ESS]\tHome\n" + // same name, stronger
		"aa:00:00:00:00:03\t2412\t-60\t[ESS]\tHotel Guest\n" +
		"aa:00:00:00:00:04\t5500\t-66\t[WPA2-EAP-CCMP][ESS]\teduroam\n" +
		"aa:00:00:00:00:05\t2462\t-80\t[RSN-SAE-CCMP][ESS]\tCaf\\xc3\\xa9\n" +
		"aa:00:00:00:00:06\t2462\t-40\t[WPA2-PSK-CCMP][ESS]\t\n" // hidden
	got := ParseScanResults(out)
	want := []NearbyNetwork{
		{SSID: "Home", SignalDBM: -52, FrequencyMHz: 5180, Security: SecurityPersonal, Label: "WPA2-Personal"},
		{SSID: "Hotel Guest", SignalDBM: -60, FrequencyMHz: 2412, Security: SecurityOpen, Label: "Open"},
		{SSID: "eduroam", SignalDBM: -66, FrequencyMHz: 5500, Security: SecurityEnterprise, Label: "WPA2-Enterprise"},
		{SSID: "Café", SignalDBM: -80, FrequencyMHz: 2462, Security: SecurityPersonal, Label: "WPA3-Personal"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d networks, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("network %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDecodeSSID(t *testing.T) {
	tests := map[string]string{
		`plain`:            "plain",
		`back\\slash`:      `back\slash`,
		`tab\there`:        "tab\there",
		`\xe2\x98\x95 cup`: "☕ cup",
		`broken\x4`:        `broken\x4`,
	}
	for in, want := range tests {
		if got := DecodeSSID(in); got != want {
			t.Errorf("DecodeSSID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSignalPoll(t *testing.T) {
	if got := ParseSignalPoll("RSSI=-61\nLINKSPEED=433\nNOISE=9999\nFREQUENCY=5180\n"); got != -61 {
		t.Errorf("got %d, want -61", got)
	}
	if got := ParseSignalPoll("FAIL\n"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestSecurityLabel(t *testing.T) {
	tests := map[string]string{
		"[ESS]":                           "Open",
		"[WPA2-PSK-CCMP][ESS]":            "WPA2-Personal",
		"[WPA2-PSK+SAE-CCMP][ESS]":        "WPA2/WPA3-Personal",
		"[RSN-PSK+SAE-CCMP][ESS]":         "WPA2/WPA3-Personal",
		"[RSN-SAE-CCMP][ESS]":             "WPA3-Personal",
		"[WPA2-EAP-CCMP][ESS]":            "WPA2-Enterprise",
		"[WPA2-EAP+EAP-SHA256-CCMP][ESS]": "WPA2/WPA3-Enterprise",
		"[RSN-EAP-SHA256-CCMP][ESS]":      "WPA3-Enterprise",
		"[WEP][ESS]":                      "WEP (not supported)",
		"[RSN-OWE-CCMP][ESS]":             "Enhanced Open (OWE)",
	}
	for flags, want := range tests {
		if got := securityLabel(flags); got != want {
			t.Errorf("securityLabel(%q) = %q, want %q", flags, got, want)
		}
	}
}

func TestParseWPAStatus_LoginFailed(t *testing.T) {
	s := ParseWPAStatus("wpa_state=ASSOCIATED\nSupplicant PAE state=HELD\nEAP state=FAILURE\n")
	if !s.LoginFailed || s.Connected() {
		t.Errorf("got %+v, want a failed login", s)
	}
}

func TestParseTempDisabled(t *testing.T) {
	out := "network id / ssid / bssid / flags\n0\teduroam\tany\t[TEMP-DISABLED]\n1\tHome\tany\t[CURRENT]\n"
	got := ParseTempDisabled(out)
	if len(got) != 1 || got[0] != "eduroam" {
		t.Errorf("got %v", got)
	}
}

func TestParseLink(t *testing.T) {
	for name, tc := range map[string]struct {
		out  string
		want Link
	}{
		"Wi-Fi 6 at 80 MHz": {
			"Connected to 3a:1f:22:aa:bb:cc (on wlan1)\n\tSSID: Hotel\n\tfreq: 5180\n\tsignal: -52 dBm\n" +
				"\trx bitrate: 1200.9 MBit/s 80MHz HE-MCS 11 HE-NSS 2 HE-GI 0 HE-DCM 0\n" +
				"\ttx bitrate: 864.7 MBit/s 80MHz HE-MCS 8 HE-NSS 2 HE-GI 0 HE-DCM 0\n",
			Link{Standard: "wifi6", WidthMHz: 80, RxMbps: 1200.9, TxMbps: 864.7},
		},
		"Wi-Fi 6E": {
			"\tfreq: 6135.0\n\trx bitrate: 2401.9 MBit/s 160MHz HE-MCS 11 HE-NSS 2\n\ttx bitrate: 6.0 MBit/s\n",
			Link{Standard: "wifi6e", WidthMHz: 160, RxMbps: 2401.9, TxMbps: 6},
		},
		"Wi-Fi 7": {
			"\tfreq: 6115\n\trx bitrate: 2882.3 MBit/s 160MHz EHT-MCS 13 EHT-NSS 2 EHT-GI 0\n",
			Link{Standard: "wifi7", WidthMHz: 160, RxMbps: 2882.3},
		},
		"Wi-Fi 5, one way at 40 MHz": {
			"\tfreq: 5745\n\trx bitrate: 433.3 MBit/s VHT-MCS 9 80MHz short GI VHT-NSS 1\n\ttx bitrate: 200.0 MBit/s MCS 7 40MHz short GI\n",
			Link{Standard: "wifi5", WidthMHz: 80, RxMbps: 433.3, TxMbps: 200},
		},
		"Wi-Fi 4 at 20 MHz": {
			"\tfreq: 2437\n\trx bitrate: 72.2 MBit/s MCS 7 short GI\n",
			Link{Standard: "wifi4", WidthMHz: 20, RxMbps: 72.2},
		},
		"802.11g":       {"\tfreq: 2412\n\trx bitrate: 54.0 MBit/s\n", Link{WidthMHz: 20, RxMbps: 54}},
		"not connected": {"Not connected.\n", Link{}},
	} {
		if got := ParseLink(tc.out); got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
}

func TestScanStandards(t *testing.T) {
	dump := "BSS 3a:1f:22:aa:bb:01(on wlan1)\n\tfreq: 2437\n\tSSID: Hotel\n\tHT capabilities:\n\t\tCapabilities: 0x1ad\n" +
		"BSS 3a:1f:22:aa:bb:02(on wlan1)\n\tfreq: 5180.0\n\tSSID: Hotel\n\tHT capabilities:\n\tVHT capabilities:\n\tHE capabilities:\n" +
		"BSS 3a:1f:22:aa:bb:03(on wlan1)\n\tfreq: 6135\n\tSSID: Lab\n\tHE capabilities:\n" +
		"BSS 3a:1f:22:aa:bb:04(on wlan1)\n\tfreq: 6135\n\tSSID: Lab 7\n\tHE capabilities:\n\tEHT capabilities:\n" +
		"BSS 3a:1f:22:aa:bb:05(on wlan1)\n\tfreq: 5745\n\tSSID: Café\\x20Two\n\tVHT capabilities:\n" +
		"BSS 3a:1f:22:aa:bb:06(on wlan1)\n\tfreq: 2412\n\tSSID: Old\n" +
		"BSS 3a:1f:22:aa:bb:07(on wlan1)\n\tfreq: 2412\n\tSSID: \n\tHT capabilities:\n"
	got := ScanStandards(dump)
	want := map[string]string{"Hotel": "wifi6", "Lab": "wifi6e", "Lab 7": "wifi7", "Café Two": "wifi5"}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for ssid, standard := range want {
		if got[ssid] != standard {
			t.Errorf("%s: %q, want %q", ssid, got[ssid], standard)
		}
	}
}
