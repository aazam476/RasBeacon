package render

// These are "golden file" tests. Each test renders a config and compares it
// with a saved copy in testdata/. The saved copies double as documentation:
// open testdata/*.golden to see exactly what RasBeacon writes on the device.
//
// If you change a template on purpose, regenerate the saved copies with:
//
//	go test ./internal/render -update
//
// and review the differences with `git diff` before committing.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/config"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// checkGolden compares got with testdata/<name>.golden.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (run with -update to create it): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file.\nRun `go test ./internal/render -update` if the change is intended.\n--- got ---\n%s", path, got)
	}
}

func TestHostapd_SetupNetworkOpen(t *testing.T) {
	got, err := Hostapd(HotspotInput{
		Interface: "ap0", SSID: "RasBeacon-Setup-4F2A", Band: config.Band2GHz,
		Country: "US", Open: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "hostapd_setup_open", got)
	if strings.Contains(got, "wpa=") {
		t.Error("open setup network must not contain WPA settings")
	}
}

func TestHostapd_WPA2WPA3On2GHz(t *testing.T) {
	got, err := Hostapd(HotspotInput{
		Interface: "ap0", SSID: "RasBeacon-4F2A", Band: config.Band2GHz,
		Country: "US", Security: config.SecurityWPA2WPA3, Password: "kq7m-2xvd-h8pt-rn4w",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "hostapd_wpa2wpa3_2ghz", got)
}

func TestHostapd_WPA2OnlyOn5GHzChannel149(t *testing.T) {
	got, err := Hostapd(HotspotInput{
		Interface: "ap0", SSID: `Ali's "Travel" Wi-Fi ☕`, Band: config.Band5GHz, Channel: 149,
		Country: "US", Security: config.SecurityWPA2, Password: "correct horse battery",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "hostapd_wpa2_5ghz_ch149", got)
	if !strings.Contains(got, "vht_oper_centr_freq_seg0_idx=155") {
		t.Error("channel 149 should use 80 MHz centre channel 155")
	}
}

func TestHostapd_5GHzChannelPairsAndChannel165(t *testing.T) {
	render := func(channel int) string {
		got, err := Hostapd(HotspotInput{
			Interface: "ap0", SSID: "x", Band: config.Band5GHz, Channel: channel,
			Country: "US", Security: config.SecurityWPA2WPA3, Password: "password123",
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if !strings.Contains(render(40), "ht_capab=[HT40-]") {
		t.Error("channel 40 pairs with 36 below it, so it needs HT40-")
	}
	if !strings.Contains(render(44), "ht_capab=[HT40+]") {
		t.Error("channel 44 pairs with 48 above it, so it needs HT40+")
	}
	ch165 := render(165)
	if !strings.Contains(ch165, "vht_oper_chwidth=0") || strings.Contains(ch165, "vht_oper_chwidth=1") {
		t.Error("channel 165 cannot use 80 MHz")
	}
}

func TestHostapd_RejectsPasswordThatCouldInjectSettings(t *testing.T) {
	_, err := Hostapd(HotspotInput{
		Interface: "ap0", SSID: "x", Band: config.Band2GHz, Country: "US",
		Security: config.SecurityWPA2WPA3, Password: "password123\nap_isolate=1",
	})
	if err == nil {
		t.Fatal("a password containing a newline must be rejected")
	}
}

func TestDnsmasq_SetupModeIsCaptive(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "/run/rbd/dnsmasq.leases", Captive: true})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "dnsmasq_setup", got)
	if !strings.Contains(got, "address=/#/10.87.0.1") {
		t.Error("setup mode should answer every name with RasBeacon's address")
	}
}

func TestDnsmasq_NormalModeIsNotCaptive(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "/run/rbd/dnsmasq.leases", Captive: false})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "dnsmasq_normal", got)
	if strings.Contains(got, "address=/#/") {
		t.Error("normal mode must not redirect every name")
	}
}

func TestFirewall_SSHOffByDefault(t *testing.T) {
	got, err := Firewall(FirewallInput{SSHEnabled: false})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_ssh_off", got)
	if strings.Contains(got, "dport 22") {
		t.Error("SSH port must not be opened when SSH is off")
	}
}

func TestFirewall_SSHOnOnlyFromHotspotSide(t *testing.T) {
	got, err := Firewall(FirewallInput{SSHEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_ssh_on", got)
	if !strings.Contains(got, `iifname "br-lan" tcp dport 22 accept`) {
		t.Error("SSH should be allowed only from the hotspot bridge")
	}
}

func TestLAN_Helpers(t *testing.T) {
	if DefaultLAN.Address() != "10.87.0.1" {
		t.Errorf("Address = %s", DefaultLAN.Address())
	}
	if DefaultLAN.Netmask() != "255.255.255.0" {
		t.Errorf("Netmask = %s", DefaultLAN.Netmask())
	}
	if DefaultLAN.hostAddress(20) != "10.87.0.20" {
		t.Errorf("hostAddress(20) = %s", DefaultLAN.hostAddress(20))
	}
}
