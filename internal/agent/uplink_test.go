package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// newClaimedEnv returns a fake device that has finished setup and runs its
// hotspot. withEthernet adds an Ethernet port (eth0) with no cable.
func newClaimedEnv(t *testing.T, withEthernet bool) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	if withEthernet {
		e.setEthernetCable(false, "0")
	}
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	return e
}

// setEthernetCable fakes plugging (or unplugging) the Ethernet cable.
// changes is the kernel's plug/unplug counter.
func (e *testEnv) setEthernetCable(plugged bool, changes string) {
	carrier := "0"
	if plugged {
		carrier = "1"
	}
	mustWrite(e.t, filepath.Join(e.paths.SysClassNet(), "eth0", "carrier"), carrier)
	mustWrite(e.t, filepath.Join(e.paths.SysClassNet(), "eth0", "carrier_changes"), changes)
}

// check runs one uplink check, as the background monitor would.
func (e *testEnv) check() {
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	e.agent.checkUplink()
}

func (e *testEnv) networkdFile(name string) string {
	return filepath.Join(e.paths.NetworkdDir(), name)
}

func TestSetupMode_NothingIsForwarded(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	rules := e.readFile(e.paths.FirewallRules())
	if strings.Contains(rules, "masquerade") {
		t.Error("devices' traffic must not be forwarded before setup is finished")
	}
	// Forwarding is switched on only after the firewall was loaded.
	if got := strings.TrimSpace(e.readFile(e.paths.IPv4Forwarding())); got != "1" {
		t.Errorf("ip_forward = %q, want 1", got)
	}
}

func TestNormalMode_ForwardsToWiFiUplinkAndWritesUplinkConfigs(t *testing.T) {
	e := newClaimedEnv(t, true)

	rules := e.readFile(e.paths.FirewallRules())
	if !strings.Contains(rules, `elements = { "eth0", "wlan0" }`) {
		t.Errorf("firewall should forward to Ethernet and the built-in radio:\n%s", rules)
	}
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "server=127.0.0.53") {
		t.Error("dnsmasq should pass names to the device's resolver")
	}
	if !strings.Contains(e.readFile(e.networkdFile("00-rasbeacon-eth0.network")), "DHCP=ipv4") {
		t.Error("Ethernet should get an address by DHCP")
	}
	if !strings.Contains(e.readFile(e.networkdFile("00-rasbeacon-wlan0.network")), "RouteMetric=600") {
		t.Error("Wi-Fi uplink should have the higher (less preferred) route metric")
	}
	if !strings.Contains(e.readFile(e.networkdFile("00-rasbeacon-unmanaged.network")), "Name=br-lan ap0") {
		t.Error("networkd must leave the hotspot alone")
	}
	if !e.runner.ran("systemctl restart rasbeacon-wpa@wlan0.service") {
		t.Error("wpa_supplicant should run on the built-in radio")
	}
	info, err := os.Stat(e.paths.WPASupplicantConfig())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("wpa_supplicant.conf must exist and be root-only: %v", err)
	}
}

func TestUpdateUplinkSettings_AddsNetworksAndKeepsPasswordsWhenEmpty(t *testing.T) {
	e := newClaimedEnv(t, false)

	err := e.agent.UpdateUplinkSettings(api.UplinkSettings{
		EthernetRole: config.EthernetUplink,
		Networks: []api.SavedNetwork{
			{SSID: "Home", Security: config.UplinkPersonal, Password: "home-password-1"},
			{SSID: "Hotel Guest", Security: config.UplinkOpen},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := e.agent.UplinkSettings()
	if len(settings.Networks) != 2 || settings.Networks[0].ID == "" || !settings.Networks[0].HasPassword {
		t.Fatalf("unexpected settings: %+v", settings)
	}
	wpa := e.readFile(e.paths.WPASupplicantConfig())
	if !strings.Contains(wpa, `psk="home-password-1"`) || !strings.Contains(wpa, "key_mgmt=NONE") {
		t.Errorf("wpa_supplicant.conf is missing a network:\n%s", wpa)
	}
	if strings.Contains(e.readFile(e.paths.ConfigFile()), "home-password-1") {
		t.Error("passwords belong in secrets.json, never config.json")
	}

	// Reorder without sending the password again: it must be kept.
	e.runner.commands = nil
	home, hotel := settings.Networks[0], settings.Networks[1]
	home.Password, hotel.Password = "", ""
	err = e.agent.UpdateUplinkSettings(api.UplinkSettings{
		EthernetRole: config.EthernetUplink,
		Networks:     []api.SavedNetwork{hotel, home},
	})
	if err != nil {
		t.Fatal(err)
	}
	wpa = e.readFile(e.paths.WPASupplicantConfig())
	if !strings.Contains(wpa, `psk="home-password-1"`) {
		t.Error("the saved password should be kept when none is sent")
	}
	if strings.Index(wpa, "# Name: Hotel Guest") > strings.Index(wpa, "# Name: Home") {
		t.Error("the new order should be saved")
	}
	if !e.runner.ran("systemctl restart rasbeacon-wpa@wlan0.service") {
		t.Error("wpa_supplicant should restart when its networks change")
	}
}

func TestUpdateUplinkSettings_RejectsBadInput(t *testing.T) {
	e := newClaimedEnv(t, false)
	tests := []struct {
		name      string
		req       api.UplinkSettings
		wantField string
	}{
		{"personal without password", api.UplinkSettings{EthernetRole: config.EthernetUplink,
			Networks: []api.SavedNetwork{{SSID: "Home", Security: config.UplinkPersonal}}}, "networks.0.password"},
		{"made-up ID", api.UplinkSettings{EthernetRole: config.EthernetUplink,
			Networks: []api.SavedNetwork{{ID: "nope", SSID: "Home", Security: config.UplinkOpen}}}, "networks.0.id"},
		{"bad Ethernet role", api.UplinkSettings{EthernetRole: "both"}, "ethernetRole"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.agent.UpdateUplinkSettings(tt.req)
			var verr *config.ValidationError
			if !errors.As(err, &verr) || verr.Field != tt.wantField {
				t.Errorf("got %v, want an error for %s", err, tt.wantField)
			}
		})
	}
}

func TestUpdateUplinkSettings_OnlyAfterSetup(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	err := e.agent.UpdateUplinkSettings(api.UplinkSettings{EthernetRole: config.EthernetUplink})
	if !errors.Is(err, ErrWrongMode) {
		t.Errorf("got %v, want ErrWrongMode", err)
	}
}

// switchToClientPort sets the Ethernet port to client-port mode.
func (e *testEnv) switchToClientPort() {
	e.t.Helper()
	err := e.agent.UpdateUplinkSettings(api.UplinkSettings{EthernetRole: config.EthernetClientPort})
	if err != nil {
		e.t.Fatal(err)
	}
}

func TestEthernetClientPort_JoinsHotspotOnlyWhenNoDHCPServerAnswers(t *testing.T) {
	e := newClaimedEnv(t, true)
	e.switchToClientPort()

	// Ethernet is no longer an uplink: networkd leaves it alone and nothing
	// is forwarded to it.
	if _, err := os.Stat(e.networkdFile("00-rasbeacon-eth0.network")); !os.IsNotExist(err) {
		t.Error("the Ethernet uplink file should be removed")
	}
	if !strings.Contains(e.readFile(e.networkdFile("00-rasbeacon-unmanaged.network")), "eth0") {
		t.Error("networkd should leave the client port alone")
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), `elements = { "wlan0" }`) {
		t.Error("only the Wi-Fi uplink should remain")
	}

	// No cable: nothing happens.
	e.check()
	if e.probes != 0 || e.runner.ran("master br-lan") {
		t.Fatal("without a cable there is nothing to check")
	}

	// A laptop is plugged in: no DHCP server answers, so it joins.
	e.setEthernetCable(true, "1")
	e.check()
	if e.probes != 1 || !e.runner.ran("ip link set dev eth0 master br-lan") {
		t.Fatal("the port should be checked and then join the hotspot network")
	}

	// Nothing changes: no new check.
	e.check()
	if e.probes != 1 {
		t.Error("the check should only run when the cable is plugged in again")
	}

	// The cable is moved to a hotel wall socket (unplug + plug = 2 more
	// changes). A DHCP server answers: the port must leave the hotspot.
	e.dhcpServer = "192.168.1.1"
	e.setEthernetCable(true, "3")
	e.check()
	if !e.runner.ran("ip link set dev eth0 nomaster") {
		t.Error("the port must leave the hotspot network before checking again")
	}
	status := e.agent.Status()
	if status.Uplink.Ethernet.ForeignDHCPServer != "192.168.1.1" {
		t.Errorf("the foreign DHCP server should be reported, got %+v", status.Uplink.Ethernet)
	}
	if !strings.Contains(strings.Join(status.Warnings, " "), "Client port") {
		t.Errorf("a warning should explain the problem: %v", status.Warnings)
	}
}

// fakeUplink makes the fake runner report an uplink on iface with address.
func (e *testEnv) fakeUplink(iface, address string, prefixLen string, ipv6 bool) {
	ip, gateway := address, "192.0.2.1"
	e.runner.outputs["ip -json route show default"] =
		`[{"dst":"default","gateway":"` + gateway + `","dev":"` + iface + `","metric":600}]`
	e.runner.outputs["ip -json address show"] =
		`[{"ifname":"` + iface + `","addr_info":[{"family":"inet","local":"` + ip + `","prefixlen":` + prefixLen + `,"scope":"global"}]}]`
	if ipv6 {
		e.runner.outputs["ip -json -6 route show default"] = `[{"dst":"default","gateway":"fe80::1","dev":"` + iface + `","metric":600}]`
	} else {
		e.runner.outputs["ip -json -6 route show default"] = "[]"
	}
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 status"] =
		"wpa_state=COMPLETED\nssid=Hotel\nfreq=5180\nkey_mgmt=WPA2-PSK\n"
}

func TestCheckUplink_ReportsConnectionAndIPv6(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.fakeUplink("wlan0", "192.168.4.20", "24", true)
	e.check()

	u := e.agent.Status().Uplink
	if !u.Connected || u.Kind != "wifi" || u.Address != "192.168.4.20" || !u.IPv6 {
		t.Errorf("unexpected uplink status: %+v", u)
	}
	if u.WiFi.State != "connected" || u.WiFi.SSID != "Hotel" || u.WiFi.Band != "5GHz" {
		t.Errorf("Wi-Fi status = %+v", u.WiFi)
	}
	// IPv6 appeared, so devices are now told they may use it.
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "ra-param=br-lan,60,1800") {
		t.Error("dnsmasq should announce an IPv6 route once the uplink has IPv6")
	}

	// IPv6 goes away again: router lifetime 0.
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "ra-param=br-lan,10,0") {
		t.Error("dnsmasq should stop announcing an IPv6 route")
	}
}

func TestCheckUplink_MovesHotspotNetworkWhenUpstreamUsesIt(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.fakeUplink("wlan0", "10.87.0.50", "24", false)
	e.check()

	if got := e.agent.SetupState().LANAddress; got != "10.88.0.1" {
		t.Fatalf("hotspot address = %s, want 10.88.0.1", got)
	}
	if !e.runner.ran("ip address replace 10.88.0.1/24 dev br-lan") {
		t.Error("the bridge should get the new address")
	}
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "dhcp-range=set:lan,10.88.0.20,10.88.0.250") {
		t.Error("DHCP should hand out addresses in the new network")
	}
	if !strings.Contains(strings.Join(e.agent.Status().Warnings, " "), "http://10.88.0.1/") {
		t.Error("the status page should explain the move")
	}
}

func TestCheckUplink_MovingTheNetworkMakesDevicesReconnect(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	e.runner.commands = nil

	// A campus network whose range (10.87.0.0/16) covers the hotspot's.
	e.fakeUplink("wlan0", "10.87.200.9", "16", false)
	e.check()
	if got := e.agent.SetupState().LANAddress; got != "10.88.0.1" {
		t.Fatalf("hotspot address = %s, want 10.88.0.1", got)
	}
	// Devices still hold 10.87.0.x addresses: the hotspot restarts after
	// DHCP has the new range, so they reconnect and get new ones.
	dhcp, hotspot := -1, -1
	for i, c := range e.runner.commands {
		switch c {
		case "systemctl restart rasbeacon-dnsmasq.service":
			dhcp = i
		case "systemctl restart rasbeacon-hostapd.service":
			hotspot = i
		}
	}
	if dhcp < 0 || hotspot < dhcp {
		t.Errorf("want dnsmasq, then hostapd restarted; commands: %v", e.runner.commands)
	}

	// The clash ends: the hotspot stays where it is (moving back would
	// disconnect everyone again), and nothing restarts on later checks.
	e.runner.commands = nil
	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.check()
	e.check()
	if got := e.agent.SetupState().LANAddress; got != "10.88.0.1" {
		t.Errorf("hotspot address = %s, want it to stay 10.88.0.1", got)
	}
	if e.runner.ran("rasbeacon-hostapd.service") {
		t.Errorf("the hotspot must not restart again; commands: %v", e.runner.commands)
	}
}

func TestCheckUplink_EveryCandidateClashes_DoesntRestartOverAndOver(t *testing.T) {
	e := newClaimedEnv(t, false)
	// 0.0.0.0/0 overlaps every candidate network.
	e.fakeUplink("wlan0", "10.1.1.1", "0", false)
	e.runner.commands = nil
	e.check()
	e.check()
	if e.runner.ran("rasbeacon-hostapd.service") {
		t.Errorf("nothing to move to, so nothing should restart; commands: %v", e.runner.commands)
	}
}

func TestScanNearby_HidesOwnHotspot(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 scan_results"] =
		"bssid / frequency / signal level / flags / ssid\n" +
			"aa:00:00:00:00:01\t2437\t-30\t[WPA2-PSK-CCMP][SAE-CCMP][ESS]\tAli's Travel Wi-Fi\n" +
			"aa:00:00:00:00:02\t2412\t-60\t[ESS]\tHotel Guest\n"
	e.runner.outputs["iw dev wlan0 scan dump"] = "BSS aa:00:00:00:00:02(on wlan0)\n\tfreq: 2412\n\tSSID: Hotel Guest\n\tHT capabilities:\n\tHE capabilities:\n"
	resp, err := e.agent.ScanNearby()
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Networks) != 1 || resp.Networks[0].SSID != "Hotel Guest" {
		t.Errorf("got %+v, want only Hotel Guest", resp.Networks)
	}
	if resp.Networks[0].Standard != "wifi6" {
		t.Errorf("the hotel offers Wi-Fi 6: %+v", resp.Networks[0])
	}
}

func TestScanNearby_HidesAndRefusesTheGuestNetworkToo(t *testing.T) {
	e := newGuestCapableEnv(t)
	if err := e.agent.UpdateGuestSettings(guestOn); err != nil {
		t.Fatal(err)
	}
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 scan_results"] =
		"bssid / frequency / signal level / flags / ssid\n" +
			"52:3e:aa:12:34:56\t2437\t-30\t[WPA2-PSK-CCMP][ESS]\tAli's Guests\n" +
			"aa:00:00:00:00:02\t2412\t-60\t[ESS]\tHotel Guest\n"
	e.runner.outputs["iw dev wlan0 scan dump"] = "BSS aa:00:00:00:00:02(on wlan0)\n\tfreq: 2412\n\tSSID: Hotel Guest\n\tHT capabilities:\n\tHE capabilities:\n"
	resp, err := e.agent.ScanNearby()
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Networks) != 1 || resp.Networks[0].SSID != "Hotel Guest" {
		t.Errorf("got %+v, want only Hotel Guest", resp.Networks)
	}
	if resp.Networks[0].Standard != "wifi6" {
		t.Errorf("the hotel offers Wi-Fi 6: %+v", resp.Networks[0])
	}

	// Saving either of RasBeacon's own networks as the uplink is refused.
	for _, own := range []string{"Ali's Travel Wi-Fi", "Ali's Guests"} {
		err := e.agent.UpdateUplinkSettings(api.UplinkSettings{
			EthernetRole: config.EthernetUplink,
			Networks:     []api.SavedNetwork{{SSID: own, Security: config.UplinkPersonal, Password: "some-password-1"}},
		})
		var invalid *config.ValidationError
		if !errors.As(err, &invalid) || invalid.Field != "networks.0.ssid" {
			t.Errorf("%s: got %v, want it refused", own, err)
		}
	}
}
