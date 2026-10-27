package devsim

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/portal"
)

// The simulated device has what rbd reads at start: two radios (one on
// USB), an unplugged Ethernet port, the board's name and two phones'
// leases. Setup can run again on the same folder (rbd -dev restarts).
func TestSetup_MakesAFakeDevice(t *testing.T) {
	root := t.TempDir()
	for range 2 {
		if _, err := Setup(root); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := Setup(root)
	for _, iface := range []string{"wlan0", "wlan1"} {
		if _, err := os.Stat(filepath.Join(p.SysClassNet(), iface, "phy80211", "name")); err != nil {
			t.Errorf("%s has no radio: %v", iface, err)
		}
	}
	// wlan1 is the USB adapter: its device link leads to the USB IDs.
	device, err := filepath.EvalSymlinks(filepath.Join(p.SysClassNet(), "wlan1", "device"))
	if err != nil || !strings.Contains(device, "usb1") {
		t.Errorf("wlan1's device = %q, %v", device, err)
	}
	read := func(path string) string {
		data, _ := os.ReadFile(path)
		return string(data)
	}
	if read(filepath.Join(p.SysClassNet(), "eth0", "carrier")) != "0" {
		t.Error("the Ethernet cable should be unplugged")
	}
	if !strings.Contains(read(p.DeviceTreeModel()), "Simulated Raspberry Pi") {
		t.Error("the board's name is missing")
	}
	if strings.Count(read(p.DnsmasqLeases()), "\n") != 2 {
		t.Error("two phones should have leases")
	}
}

func TestSetup_ReportsAFolderItCantWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	os.WriteFile(file, nil, 0o644)
	if _, err := Setup(file); err == nil {
		t.Error("a root that is a file can't hold a device")
	}
}

func TestRunner_AnswersWhatRbdAsks(t *testing.T) {
	var r Runner
	for _, tc := range []struct {
		command []string
		want    string
	}{
		{[]string{"iw", "phy", "phy1", "info"}, "#{ AP } <= 2"},
		{[]string{"iw", "dev", "ap0", "station", "dump"}, "Station 3a:1f:22:aa:bb:cc"},
		{[]string{"systemctl", "is-active", "rasbeacon-hostapd.service"}, "active"},
		{[]string{"ip", "-json", "route", "show", "default"}, `"dev":"wlan0"`},
		{[]string{"ip", "-json", "-6", "route", "show", "default"}, "[]"},
		{[]string{"ip", "-json", "address", "show"}, "192.168.4.20"},
		{[]string{"wpa_cli", "-i", "wlan0", "status"}, "wpa_state=COMPLETED"},
		{[]string{"wpa_cli", "-i", "wlan0", "signal_poll"}, "RSSI=-58"},
		{[]string{"wpa_cli", "-i", "wlan0", "scan_results"}, "eduroam"},
		{[]string{"wg", "show", "wg0", "dump"}, "203.0.113.5:51820"},
		{[]string{"nft", "-f", "x"}, ""}, // everything else does nothing
	} {
		got, err := r.Run(tc.command[0], tc.command[1:]...)
		if err != nil || !strings.Contains(got, tc.want) {
			t.Errorf("%v = %q, %v; want it to contain %q", tc.command, got, err, tc.want)
		}
	}
	// No real link exists, so rbd creates the ones it needs.
	if _, err := r.Run("ip", "link", "show", "dev", "wg0"); err == nil {
		t.Error("ip link show should say the link doesn't exist")
	}
}

func TestDHCPProbe_FindsNoServer(t *testing.T) {
	result, err := DHCPProbe("eth0", time.Second)
	if err != nil || result.ServerFound {
		t.Errorf("got %+v, %v; want no server", result, err)
	}
}

// The simulated hotel: open at first, "needs a login" when told, and open
// again after logging in.
func TestHotel_LoginFlow(t *testing.T) {
	if err := StartHotel("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	hotel.mu.Lock()
	base := "http://" + hotel.address
	hotel.mu.Unlock()
	get := func(path string) string {
		t.Helper()
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	if got := PortalProbe(context.Background()); got.State != portal.Open {
		t.Errorf("at first the network is open, got %+v", got)
	}
	get("/require-login")
	got := PortalProbe(context.Background())
	if got.State != portal.Portal || got.LoginURL != base+"/" {
		t.Errorf("after /require-login: %+v", got)
	}
	if page := get("/"); !strings.Contains(page, "Room number") {
		t.Errorf("login page = %q", page)
	}
	// The room number is shown back, escaped.
	if page := get("/login?room=%3Cb%3E12"); !strings.Contains(page, "&lt;b&gt;12") {
		t.Errorf("welcome page = %q", page)
	}
	if got := PortalProbe(context.Background()); got.State != portal.Open {
		t.Errorf("after logging in the network is open, got %+v", got)
	}
}

func TestSpeedtestServer_Answers(t *testing.T) {
	address, err := StartSpeedtestServer()
	if err != nil || !strings.HasPrefix(address, "http://127.0.0.1:") {
		t.Fatalf("address %q, %v", address, err)
	}
}
