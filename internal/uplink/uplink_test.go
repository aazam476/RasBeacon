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
	if !s.Connected() || s.SSID != `Hotel "Guest"` || s.FrequencyMHz != 5180 || s.KeyMgmt != "WPA2-PSK" {
		t.Errorf("got %+v", s)
	}
	if BandName(s.FrequencyMHz) != "5GHz" {
		t.Errorf("band = %s", BandName(s.FrequencyMHz))
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
