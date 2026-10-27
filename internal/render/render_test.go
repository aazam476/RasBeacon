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
	"net/netip"
	"os"
	"path/filepath"
	"slices"
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

func TestHostapd_WPA3Only(t *testing.T) {
	got, err := Hostapd(HotspotInput{
		Interface: "ap0", SSID: "RasBeacon-4F2A", Band: config.Band2GHz,
		Country: "US", Security: config.SecurityWPA3, Password: "kq7m-2xvd-h8pt-rn4w",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "hostapd_wpa3_2ghz", got)
	if !strings.Contains(got, "wpa_key_mgmt=SAE\nieee80211w=2") || strings.Contains(got, "WPA-PSK") {
		t.Error("WPA3 only: SAE alone, protected management frames required")
	}
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

func TestHostapd_GuestNetwork(t *testing.T) {
	in := HotspotInput{
		Interface: "ap0", SSID: "RasBeacon-4F2A", Band: config.Band2GHz,
		Country: "US", Security: config.SecurityWPA2WPA3, Password: "kq7m-2xvd-h8pt-rn4w",
		Guest: &GuestNetwork{SSID: "RasBeacon-4F2A-Guest", Password: "visitors-welcome", BSSID: "52:3e:aa:12:34:56"},
	}
	got, err := Hostapd(in)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "hostapd_guest", got)
	guest := got[strings.Index(got, "bss=guest0"):]
	for _, want := range []string{"bridge=br-guest", "ap_isolate=1", "wpa_passphrase=visitors-welcome", "bssid=52:3e:aa:12:34:56"} {
		if !strings.Contains(guest, want) {
			t.Errorf("guest section lacks %q", want)
		}
	}

	in.Guest.Password = "short"
	if _, err := Hostapd(in); err == nil {
		t.Error("a too-short guest password must be refused")
	}
	in.Guest.Password = "visitors-welcome\nwpa=0"
	if _, err := Hostapd(in); err == nil {
		t.Error("a guest password with a line break must be refused")
	}
}

func TestGuestBSSID(t *testing.T) {
	for radio, want := range map[string]string{
		"50:3e:aa:12:34:56": "52:3e:aa:12:34:56", // sets the locally administered bit
		"52:3e:aa:12:34:56": "52:3e:aa:12:34:57", // already set: change the last bit
	} {
		if got, err := GuestBSSID(radio); err != nil || got != want {
			t.Errorf("GuestBSSID(%s) = %s, %v; want %s", radio, got, err, want)
		}
	}
	if _, err := GuestBSSID("not-a-mac"); err == nil {
		t.Error("expected an error")
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

// lanWithIPv6 is the default hotspot network plus a private IPv6 network.
var lanWithIPv6 = LAN{
	Prefix:     DefaultLAN.Prefix,
	IPv6Prefix: netip.MustParsePrefix("fd12:3456:789a:1::1/64"),
}

func TestDnsmasq_NormalModeIsNotCaptive(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{
		LAN: lanWithIPv6, LeaseFile: "/run/rbd/dnsmasq.leases", Captive: false,
		Upstream: []string{"127.0.0.53"}, IPv6DefaultRoute: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "dnsmasq_normal", got)
	if strings.Contains(got, "address=/#/") {
		t.Error("normal mode must not redirect every name")
	}
	if !strings.Contains(got, "server=127.0.0.53") {
		t.Error("normal mode should pass internet names to the device's resolver")
	}
	if !strings.Contains(got, "ra-param=br-lan,60,1800") {
		t.Error("with an IPv6 path, devices should get an IPv6 default route")
	}
}

func TestDnsmasq_NoIPv6PathAnnouncesRouterLifetimeZero(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{
		LAN: lanWithIPv6, LeaseFile: "/run/rbd/dnsmasq.leases",
		Upstream: []string{"127.0.0.53"}, IPv6DefaultRoute: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "dnsmasq_normal_no_ipv6_route", got)
	if !strings.Contains(got, "ra-param=br-lan,10,0") {
		t.Error("without an IPv6 path, router lifetime must be 0")
	}
}

func TestDnsmasq_SetupModeHasNoUpstreamOrIPv6(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "x", Captive: true, Upstream: []string{"127.0.0.53"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "server=") || strings.Contains(got, "enable-ra") {
		t.Error("setup mode answers every name itself and has no IPv6")
	}
}

func TestDnsmasq_PortalNoticeAnswersProbeNames(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{
		LAN: DefaultLAN, LeaseFile: "x", Upstream: []string{"10.66.0.1"},
		NoticeHostnames: []string{"captive.apple.com", "connectivitycheck.gstatic.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "dnsmasq_portal_notice", got)
	for _, want := range []string{"address=/captive.apple.com/10.87.0.1", `dhcp-option=114,"http://10.87.0.1/api/v1/captive"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got, "address=/#/") {
		t.Error("only the probe names may be answered locally")
	}
}

func TestDnsmasq_GuestNetworkAndMetered(t *testing.T) {
	guest := DefaultLAN.Guest()
	got, err := Dnsmasq(DnsmasqInput{
		LAN: DefaultLAN, LeaseFile: "/run/rbd/dnsmasq.leases", Upstream: []string{"127.0.0.53"},
		Guest: &guest, Metered: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "dnsmasq_guest_metered", got)
	for _, want := range []string{
		"interface=br-guest",
		"dhcp-range=set:guest,10.87.1.20,10.87.1.250,255.255.255.0,1h",
		"dhcp-option=tag:guest,option:router,10.87.1.1",
		"dhcp-option=43,ANDROID_METERED",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	setup, _ := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, Captive: true, Guest: &guest, Metered: true})
	if strings.Contains(setup, "br-guest") || strings.Contains(setup, "METERED") {
		t.Error("the setup network has no guest network and no metered hint")
	}
}

func TestDnsmasq_RejectsUpstreamThatIsNotAnAddress(t *testing.T) {
	_, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "x", Upstream: []string{"1.1.1.1\naddress=/#/1.2.3.4"}})
	if err == nil {
		t.Fatal("upstream must be a plain IP address")
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

func TestFirewall_ForwardsAndNATsToUplinks(t *testing.T) {
	got, err := Firewall(FirewallInput{Egress: []string{"eth0", "wlan0"}})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_uplinks", got)
	for _, want := range []string{
		`elements = { "eth0", "wlan0" }`,
		`iifname @lans oifname @egress accept`,
		`elements = { "br-lan" }`,
		"oifname @egress masquerade",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestFirewall_TunnelOnlyAndDNSGuard(t *testing.T) {
	got, err := Firewall(FirewallInput{
		Egress:          []string{"wg0"},
		TunnelInterface: "wg0",
		TunnelDNS:       []netip.Addr{netip.MustParseAddr("10.66.0.1"), netip.MustParseAddr("fd00:66::1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_tunnel", got)
	for _, want := range []string{
		`elements = { "wg0" }`,
		`ip daddr { 10.66.0.1 } oifname != "wg0" reject`,
		`ip6 daddr { fd00:66::1 } oifname != "wg0" reject`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got, `"eth0"`) {
		t.Error("with the VPN in use, nothing may be forwarded to the uplinks")
	}
}

func TestFirewall_PortalOnlyAllowsWebAndDNS(t *testing.T) {
	got, err := Firewall(FirewallInput{Egress: []string{"wlan0"}, PortalOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_portal", got)
	if strings.Contains(got, `iifname "br-lan" oifname @egress accept`) {
		t.Error("login mode must not allow all traffic")
	}
	if !strings.Contains(got, "tcp dport { 80, 443 } accept") {
		t.Error("login mode must allow web traffic")
	}
}

func TestFirewall_GuestNetworkIsolated(t *testing.T) {
	got, err := Firewall(FirewallInput{Egress: []string{"wlan0"}, Guest: true})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_guest", got)
	if !strings.Contains(got, `elements = { "br-lan", "br-guest" }`) {
		t.Error("guests must be able to use the internet")
	}
	input := got[strings.Index(got, "chain input"):strings.Index(got, "chain forward")]
	if !strings.Contains(input, `iifname "br-guest" reject`) {
		t.Error("guests must not reach the web UI or SSH")
	}
	if strings.Contains(input, `iifname "br-guest" tcp dport`) {
		t.Error("no TCP port may be opened for guests")
	}
	without, _ := Firewall(FirewallInput{Egress: []string{"wlan0"}})
	if strings.Contains(without, "br-guest") {
		t.Error("no guest rules without a guest network")
	}
}

func TestFirewall_RoutedIPv6SkipsNATOnlyInTheTunnel(t *testing.T) {
	routed := netip.MustParsePrefix("2001:db8:1234:5678::/64")
	got, err := Firewall(FirewallInput{Egress: []string{"wg0"}, TunnelInterface: "wg0", RoutedIPv6: routed})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_routed_ipv6", got)
	nat := got[strings.Index(got, "chain postrouting_nat"):]
	exempt := strings.Index(nat, `oifname "wg0" ip6 saddr 2001:db8:1234:5678::/64 accept`)
	if exempt < 0 || exempt > strings.Index(nat, "masquerade") {
		t.Errorf("the routed network must be exempted from NAT before masquerading:\n%s", nat)
	}
	if _, err := Firewall(FirewallInput{Egress: []string{"wlan0"}, RoutedIPv6: routed}); err == nil {
		t.Error("a routed network without the tunnel must be refused")
	}
	if _, err := Firewall(FirewallInput{TunnelInterface: "wg0", RoutedIPv6: netip.MustParsePrefix("fd00:1::/64")}); err == nil {
		t.Error("a private network must be refused")
	}
}

func TestFirewall_LoginBrowserOnlyReachesUplinks(t *testing.T) {
	got, err := Firewall(FirewallInput{Egress: []string{"wg0"}, TunnelInterface: "wg0",
		Browser: &BrowserRule{UID: 997, Uplinks: []string{"eth0", "wlan0"}}})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_browser", got)
	output := got[strings.Index(got, "chain output"):]
	for _, want := range []string{
		`meta skuid 997 oifname "lo" ip daddr 127.0.0.53 meta l4proto { tcp, udp } th dport 53 accept`,
		`meta skuid 997 oifname != { "eth0", "wlan0" } reject`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q", want)
		}
	}
	none, _ := Firewall(FirewallInput{Browser: &BrowserRule{UID: 997}})
	if !strings.Contains(none, "meta skuid 997 reject") {
		t.Error("without uplinks the browser may reach nothing")
	}
	if _, err := Firewall(FirewallInput{Browser: &BrowserRule{UID: 0}}); err == nil {
		t.Error("root must never be the browser user")
	}
}

func TestFirewall_PausedDevicesAreNotForwarded(t *testing.T) {
	got, err := Firewall(FirewallInput{Egress: []string{"wlan0"}, PausedMACs: []string{"3A:1F:22:AA:BB:CC", "8e:00:11:22:33:44"},
		PausedIPv4: []netip.Addr{netip.MustParseAddr("10.87.0.23")},
		PausedIPv6: []netip.Addr{netip.MustParseAddr("fd12:3456:789a:1::23")}})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_paused", got)
	forward := got[strings.Index(got, "chain forward"):]
	rule := `iifname @lans ether saddr { 3a:1f:22:aa:bb:cc, 8e:00:11:22:33:44 } reject`
	at := strings.Index(forward, rule)
	if at < 0 || at > strings.Index(forward, "ct state established,related accept") {
		t.Errorf("paused devices must be refused before established connections are let through:\n%s", forward)
	}
	drop := strings.Index(forward, `oifname @lans ip daddr { 10.87.0.23 } drop`)
	if drop < 0 || drop > strings.Index(forward, "ct state established,related accept") {
		t.Errorf("nothing may reach a paused device, not even answers:\n%s", forward)
	}
	if _, err := Firewall(FirewallInput{PausedMACs: []string{"3a:1f:22:aa:bb:cc } accept #"}}); err == nil {
		t.Error("a bad MAC address must be refused")
	}
}

func TestFirewall_EstablishedOnlyOnCurrentPath(t *testing.T) {
	got, _ := Firewall(FirewallInput{Egress: []string{"wg0"}})
	forward := got[strings.Index(got, "chain forward"):strings.Index(got, "chain prerouting_nat")]
	if strings.Contains(forward, "\t\tct state established,related accept") {
		t.Error("forwarded replies must be tied to the current egress, not accepted everywhere")
	}
}

func TestFirewall_NoUplinksMeansNoForwarding(t *testing.T) {
	got, err := Firewall(FirewallInput{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "masquerade") || strings.Contains(got, "oifname") {
		t.Error("without uplinks nothing may be forwarded")
	}
}

func TestFirewall_RejectsStrangeInterfaceNames(t *testing.T) {
	if _, err := Firewall(FirewallInput{Egress: []string{`eth0" accept`}}); err == nil {
		t.Fatal("interface names with quotes must be rejected")
	}
}

func TestWPASupplicant_OpenAndPersonalNetworksInOrder(t *testing.T) {
	got, err := WPASupplicant(WPAInput{
		Country: "US",
		Networks: []WPANetwork{
			{SSID: "Home", Security: config.UplinkPersonal, Password: `pass "word" 123`},
			{SSID: "Hotel Guest", Security: config.UplinkOpen},
			{SSID: "Hidden Lab", Security: config.UplinkPersonal, Password: "correct-horse", Hidden: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "wpa_supplicant", got)
	// "Home" is first in the list, so it must get the highest priority.
	homeBlock := got[strings.Index(got, "# Name: Home"):strings.Index(got, "# Name: Hotel Guest")]
	if !strings.Contains(homeBlock, "priority=3\n") {
		t.Error(`"Home" should have priority 3 (highest)`)
	}
}

func TestWPASupplicant_RejectsPasswordThatCouldInjectSettings(t *testing.T) {
	_, err := WPASupplicant(WPAInput{Country: "US", Networks: []WPANetwork{
		{SSID: "x", Security: config.UplinkPersonal, Password: "password123\"\n}\nnetwork={"},
	}})
	if err == nil {
		t.Fatal("a password containing a newline must be rejected")
	}
}

func TestWPASupplicant_EnterpriseMethods(t *testing.T) {
	got, err := WPASupplicant(WPAInput{
		Country: "US",
		Networks: []WPANetwork{
			{SSID: "eduroam", Security: config.UplinkEnterprise, Password: `p"w`, Enterprise: &WPAEnterprise{
				Method: config.EAPTTLSPAP, Identity: "me@example.edu", AnonymousIdentity: "anonymous@example.edu",
				ServerDomain: "login.example.edu", CACert: "/persistent/rasbeacon/certs/a1-ca.pem",
			}},
			{SSID: "CorpNet", Security: config.UplinkEnterprise, Password: "secret", Enterprise: &WPAEnterprise{
				Method: config.EAPPEAP, Identity: "alice", ServerDomain: "radius.corp.example",
				CACert: "/persistent/rasbeacon/certs/b2-ca.pem",
			}},
			{SSID: "LabNet", Security: config.UplinkEnterprise, Password: "key-passphrase", Enterprise: &WPAEnterprise{
				Method: config.EAPTLS, Identity: "bob", ServerDomain: "lab.example",
				CACert: "/c/ca.pem", ClientCertFile: "/c/client.pem", ClientKeyFile: "/c/key.pem",
			}},
			{SSID: "NoCheck", Security: config.UplinkEnterprise, Password: "pw", Enterprise: &WPAEnterprise{
				Method: config.EAPPEAP, Identity: "carol",
			}},
			{SSID: "NameOnly", Security: config.UplinkEnterprise, Password: "pw", Enterprise: &WPAEnterprise{
				Method: config.EAPPEAP, Identity: "dave", ServerDomain: "radius.example.com", CACert: SystemCAFile,
			}},
			{SSID: "FirstTime", Security: config.UplinkEnterprise, Password: "pw", Enterprise: &WPAEnterprise{
				Method: config.EAPPEAP, Identity: "erin", CACert: ProbeServerCert, Probe: true,
			}},
			{SSID: "Trusted", Security: config.UplinkEnterprise, Password: "pw", Enterprise: &WPAEnterprise{
				Method: config.EAPPEAP, Identity: "frank", CACert: TrustedServerCert(strings.Repeat("ab", 32)),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "wpa_supplicant_enterprise", got)
	for _, want := range []string{
		"eap=TTLS", `phase2="auth=PAP"`, `domain_suffix_match="login.example.edu"`,
		"eap=PEAP", `phase2="auth=MSCHAPV2"`,
		"eap=TLS", `private_key="/c/key.pem"`, "private_key_passwd=6b65792d70617373706872617365",
		"identity=6d65406578616d706c652e656475",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestWPASupplicant_EnterpriseRejectsUnsafeValues(t *testing.T) {
	_, err := WPASupplicant(WPAInput{Country: "US", Networks: []WPANetwork{
		{SSID: "x", Security: config.UplinkEnterprise, Password: "pw", Enterprise: &WPAEnterprise{
			Method: config.EAPPEAP, Identity: "a", ServerDomain: "evil\"\nca_cert=\"/x", CACert: "/c/ca.pem",
		}},
	}})
	if err == nil {
		t.Fatal("a server name with quotes and newlines must be rejected")
	}
}

func TestNetworkd_UplinkAndUnmanaged(t *testing.T) {
	uplink, err := NetworkdUplink("eth0", EthernetRouteMetric)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "networkd_uplink_eth0", uplink)

	unmanaged, err := NetworkdUnmanaged([]string{"br-lan", "ap0"})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "networkd_unmanaged", unmanaged)
	if !strings.Contains(unmanaged, "Name=br-lan ap0") {
		t.Error("both interfaces should be matched")
	}
}

func TestChooseLANPrefix_AvoidsUpstreamNetwork(t *testing.T) {
	p := netip.MustParsePrefix
	tests := []struct {
		name     string
		upstream []netip.Prefix
		want     string
	}{
		{"no uplink", nil, "10.87.0.1/24"},
		{"typical hotel", []netip.Prefix{p("192.168.1.57/24")}, "10.87.0.1/24"},
		{"same network as ours", []netip.Prefix{p("10.87.0.12/24")}, "10.88.0.1/24"},
		{"same network as our guests", []netip.Prefix{p("10.87.1.30/24")}, "10.88.0.1/24"},
		{"huge 10/8 network", []netip.Prefix{p("10.40.3.9/8")}, "172.31.87.1/24"},
		{"10/8 and 172.16/12", []netip.Prefix{p("10.1.1.1/8"), p("172.20.0.5/12")}, "192.168.87.1/24"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChooseLANPrefix(tt.upstream); got.String() != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestULALANAddress(t *testing.T) {
	got, err := ULALANAddress("fd12:3456:789a::/48")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "fd12:3456:789a:1::1/64" {
		t.Errorf("got %s", got)
	}
	if _, err := ULALANAddress("10.0.0.0/8"); err == nil {
		t.Error("an IPv4 prefix must be rejected")
	}
}

func TestLAN_Helpers(t *testing.T) {
	if DefaultLAN.Address() != "10.87.0.1" {
		t.Errorf("Address = %s", DefaultLAN.Address())
	}
	if DefaultLAN.Netmask() != "255.255.255.0" {
		t.Errorf("Netmask = %s", DefaultLAN.Netmask())
	}
	for bits, want := range map[int]string{0: "0.0.0.0", 20: "255.255.240.0", 32: "255.255.255.255"} {
		if got := Netmask(bits); got != want {
			t.Errorf("Netmask(%d) = %s, want %s", bits, got, want)
		}
	}
	if DefaultLAN.hostAddress(20) != "10.87.0.20" {
		t.Errorf("hostAddress(20) = %s", DefaultLAN.hostAddress(20))
	}
}

func TestWireGuard(t *testing.T) {
	got, err := WireGuard(WireGuardInput{
		PrivateKey:    "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=",
		PeerPublicKey: "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=",
		Endpoint:      "vpn.example.com:51820",
		AllowedIPs:    []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
		Keepalive:     25,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "wg0", got)
	if strings.Contains(got, "PresharedKey") {
		t.Error("no preshared key was given")
	}
	if !strings.Contains(got, "AllowedIPs = 0.0.0.0/0, ::/0") {
		t.Error("AllowedIPs missing")
	}
	if _, err := WireGuard(WireGuardInput{PrivateKey: "x\nPostUp = rm", PeerPublicKey: "y", Endpoint: "a:1"}); err == nil {
		t.Error("keys with line breaks must be rejected")
	}
}

// While the server's name hasn't been looked up yet, the file has no
// Endpoint line at all (rbd sets the address later with `wg set`).
func TestWireGuard_WithoutAnEndpointYet(t *testing.T) {
	got, err := WireGuard(WireGuardInput{
		PrivateKey:    "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=",
		PeerPublicKey: "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=",
		AllowedIPs:    []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")},
		Keepalive:     25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Endpoint =") {
		t.Errorf("no Endpoint line expected:\n%s", got)
	}
	if _, err := WireGuard(WireGuardInput{Endpoint: "a:1\nPostUp = rm"}); err == nil {
		t.Error("an endpoint with a line break must be rejected")
	}
}

func TestDnsmasq_UpstreamWithAPort(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "x", Upstream: []string{"9.9.9.9", "127.0.0.1#5053"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "server=9.9.9.9\nserver=127.0.0.1#5053\n") {
		t.Errorf("dnsmasq.conf:\n%s", got)
	}
	for _, bad := range []string{"127.0.0.1#", "127.0.0.1#99999", "dns.google#53", "1.1.1.1\nserver=6.6.6.6"} {
		if _, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "x", Upstream: []string{bad}}); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestWPASupplicant_RandomMACPerNetwork(t *testing.T) {
	got, err := WPASupplicant(WPAInput{Country: "US", Networks: []WPANetwork{
		{SSID: "Hotel", Security: config.UplinkOpen, RandomMAC: true},
		{SSID: "Home", Security: config.UplinkPersonal, Password: "espresso-1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	hotel, _, _ := strings.Cut(got[strings.Index(got, "# Name: Hotel"):], "# Name: Home")
	if !strings.Contains(hotel, "mac_addr=1") {
		t.Errorf("the hotel network uses a random MAC:\n%s", hotel)
	}
	if strings.Count(got, "mac_addr=1") != 1 || !strings.Contains(got, "rand_addr_lifetime=2147483647") {
		t.Errorf("only the hotel network, and the random MAC is kept until a restart:\n%s", got)
	}
}

func TestFirewall_Plugins(t *testing.T) {
	adguard := PluginRule{Address: netip.MustParseAddr("10.89.0.2"), Internet: true,
		Ports: []PluginPort{{Protocol: "tcp", Host: 3000, Container: 3000}}}
	samba := PluginRule{Address: netip.MustParseAddr("10.89.0.3"), LANAccess: true,
		Ports: []PluginPort{{Protocol: "tcp", Host: 445, Container: 445}}}
	got, err := Firewall(FirewallInput{Egress: []string{"wg0"}, TunnelInterface: "wg0", Guest: true,
		LANAddress: netip.MustParseAddr("10.87.0.1"), Plugins: []PluginRule{adguard, samba}})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_plugins", got)
	for _, want := range []string{
		// Only from the hotspot, only to RasBeacon's own address.
		`iifname "br-lan" ip daddr 10.87.0.1 tcp dport 3000 dnat ip to 10.89.0.2:3000`,
		`iifname "br-lan" oifname "rbplug0" ct status dnat accept`,
		// Only AdGuard may use the internet and RasBeacon's DNS.
		`iifname "rbplug0" ip saddr { 10.89.0.2 } oifname @egress accept`,
		`iifname "rbplug0" ip saddr { 10.89.0.2 } ip daddr 10.89.0.1 meta l4proto { tcp, udp } th dport 53 accept`,
		`iifname "rbplug0" reject`,
		// Only Samba may reach your devices.
		`iifname "rbplug0" ip saddr { 10.89.0.3 } oifname "br-lan" accept`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got, `iifname "br-guest" ip daddr`) || strings.Contains(got, `oifname "br-guest"`) {
		t.Error("guests must not reach plugins, nor plugins guests")
	}

	// In login mode plugins don't get out (devices only reach the login page).
	portal, _ := Firewall(FirewallInput{Egress: []string{"wlan0"}, PortalOnly: true,
		LANAddress: netip.MustParseAddr("10.87.0.1"), Plugins: []PluginRule{adguard}})
	if strings.Contains(portal, `ip saddr { 10.89.0.2 } oifname @egress accept`) {
		t.Error("plugins must not use the internet in login mode")
	}

	for _, bad := range []FirewallInput{
		{LANAddress: netip.MustParseAddr("10.87.0.1"), Plugins: []PluginRule{{Address: netip.MustParseAddr("10.87.0.9")}}},
		{LANAddress: netip.MustParseAddr("10.87.0.1"), Plugins: []PluginRule{adguard, {Address: netip.MustParseAddr("10.89.0.4"),
			Ports: []PluginPort{{Protocol: "tcp", Host: 3000, Container: 80}}}}},
		{Plugins: []PluginRule{adguard}},
	} {
		if _, err := Firewall(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestDnsmasq_AnswersPlugins(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "/l", Upstream: []string{"127.0.0.53"}, Plugins: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "interface=rbplug0\nno-dhcp-interface=rbplug0\n") {
		t.Errorf("dnsmasq must answer plugins' DNS, without DHCP:\n%s", got)
	}
}

func TestDnsmasq_Hostname(t *testing.T) {
	got, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "x", Hostname: "travel-pi"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"address=/rasbeacon.lan/10.87.0.1", "address=/travel-pi.lan/10.87.0.1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if _, err := Dnsmasq(DnsmasqInput{LAN: DefaultLAN, LeaseFile: "x", Hostname: "a/b"}); err == nil {
		t.Error("a name with a slash was accepted")
	}
}

func TestHostapd_AllowListAndDeviceLimit(t *testing.T) {
	in := HotspotInput{
		Interface: "ap0", SSID: "RasBeacon-4F2A", Band: config.Band2GHz,
		Country: "US", Security: config.SecurityWPA2WPA3, Password: "kq7m-2xvd-h8pt-rn4w",
		AcceptMACFile: "/run/rbd/hostapd.accept", MaxDevices: 12,
		Guest: &GuestNetwork{SSID: "RasBeacon-4F2A-Guest", Password: "visitors-welcome", BSSID: "52:3e:aa:12:34:56"},
	}
	got, err := Hostapd(in)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "hostapd_access", got)
	main, guest, _ := strings.Cut(got, "bss=guest0")
	for _, want := range []string{"macaddr_acl=1", "accept_mac_file=/run/rbd/hostapd.accept", "max_num_sta=12"} {
		if !strings.Contains(main, want) {
			t.Errorf("main section lacks %q", want)
		}
	}
	if strings.Contains(guest, "macaddr_acl") || !strings.Contains(guest, "max_num_sta=12") {
		t.Errorf("guest section: the allow list must not apply, the limit must:\n%s", guest)
	}

	in.MaxDevices = config.MaxDevicesLimit + 1
	if _, err := Hostapd(in); err == nil {
		t.Error("a limit above the DHCP range was accepted")
	}
}

// Fixed addresses become dhcp-host lines; one outside the hotspot's network
// or RasBeacon's own address is refused.
func TestDnsmasq_FixedAddresses(t *testing.T) {
	in := DnsmasqInput{LAN: DefaultLAN, LeaseFile: "x", FixedAddresses: []FixedAddress{{MAC: "3A:1F:22:AA:BB:CC", IP: "10.87.0.50"}}}
	got, err := Dnsmasq(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "dhcp-host=3a:1f:22:aa:bb:cc,10.87.0.50\n") {
		t.Errorf("no dhcp-host line:\n%s", got)
	}
	for _, bad := range []FixedAddress{{"3a:1f:22:aa:bb:cc", "192.168.1.5"}, {"3a:1f:22:aa:bb:cc", "10.87.0.1"}, {"nonsense", "10.87.0.50"}} {
		in.FixedAddresses = []FixedAddress{bad}
		if _, err := Dnsmasq(in); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestFirewall_PortForwardsAndPinholes(t *testing.T) {
	routed := netip.MustParsePrefix("2001:db8:1234:5678::/64")
	phone := netip.MustParseAddr("10.87.0.50")
	in := FirewallInput{
		Egress: []string{"wg0"}, TunnelInterface: "wg0", RoutedIPv6: routed, Uplinks: []string{"wlan1"},
		LANAddress: netip.MustParseAddr("10.87.0.1"),
		PortForwards: []PortForward{
			{Protocol: "tcp", From: 8080, To: 8080, Address: phone, InsidePort: 80},
			{Protocol: "udp", From: 27015, To: 27030, Address: phone},
		},
		Pinholes: []Pinhole{
			{Protocol: "tcp", From: 80, To: 80, Addresses: []netip.Addr{netip.MustParseAddr("2001:db8:1234:5678::a")}},
			// A device without an address (yet) opens nothing.
			{Protocol: "udp", From: 9000, To: 9010},
		},
	}
	got, err := Firewall(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"tcp dport 8080 ct mark set 0x5246 dnat ip to 10.87.0.50:80",
		"udp dport 27015-27030 ct mark set 0x5246 dnat ip to 10.87.0.50",
		`iifname "wg0" oifname "br-lan" ip6 daddr { 2001:db8:1234:5678::a } tcp dport 80 accept`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dport 9000") {
		t.Error("a pinhole without addresses must open nothing")
	}

	// Without the routed network there are no pinholes.
	plain := in
	plain.RoutedIPv6 = netip.Prefix{}
	if got, _ := Firewall(plain); strings.Contains(got, "ip6 daddr {") {
		t.Error("pinholes without the routed network")
	}

	bad := map[string]func(*FirewallInput){
		"pinhole protocol":        func(f *FirewallInput) { f.Pinholes = []Pinhole{{Protocol: "icmp", From: 1, To: 1}} },
		"pinhole ports":           func(f *FirewallInput) { f.Pinholes = []Pinhole{{Protocol: "tcp", From: 9, To: 1}} },
		"pinhole outside network": func(f *FirewallInput) { f.Pinholes[0].Addresses = []netip.Addr{netip.MustParseAddr("2001:db8::1")} },
		"forward protocol":        func(f *FirewallInput) { f.PortForwards[0].Protocol = "sctp" },
		"forward range and port":  func(f *FirewallInput) { f.PortForwards[1].InsidePort = 80 },
		"forward to IPv6":         func(f *FirewallInput) { f.PortForwards[0].Address = netip.MustParseAddr("fd00::1") },
		"forward without LAN":     func(f *FirewallInput) { f.LANAddress = netip.Addr{} },
		"forward uplink name":     func(f *FirewallInput) { f.Uplinks = []string{"wlan1; drop"} },
	}
	for name, change := range bad {
		f := in
		f.PortForwards = slices.Clone(in.PortForwards)
		f.Pinholes = slices.Clone(in.Pinholes)
		change(&f)
		if _, err := Firewall(f); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
}
