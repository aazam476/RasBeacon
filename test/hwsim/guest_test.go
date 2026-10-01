package hwsim

// The guest network: hostapd runs RasBeacon's generated configuration with
// both networks on one simulated radio, and a simulated phone joins each.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
	"rasbeacon/internal/uplink"
)

func TestGuestNetwork(t *testing.T) {
	ap, sta := requireHwsim(t)

	// rbd creates both bridges before starting hostapd.
	for _, bridge := range []string{render.BridgeName, render.GuestBridgeName} {
		exec.Command("ip", "link", "del", bridge).Run()
		if out, err := exec.Command("ip", "link", "add", bridge, "type", "bridge").CombinedOutput(); err != nil {
			t.Fatalf("creating %s: %v %s", bridge, err, out)
		}
		exec.Command("ip", "link", "set", bridge, "up").Run()
		t.Cleanup(func() { exec.Command("ip", "link", "del", bridge).Run() })
	}

	mac, err := os.ReadFile(filepath.Join("/sys/class/net", ap, "address"))
	if err != nil {
		t.Fatal(err)
	}
	guestBSSID, err := render.GuestBSSID(strings.TrimSpace(string(mac)))
	if err != nil {
		t.Fatal(err)
	}
	conf, err := render.Hostapd(render.HotspotInput{
		Interface: ap, SSID: "RasBeacon-Test", Band: config.Band2GHz, Country: "US",
		Security: config.SecurityWPA2WPA3, Password: "owner-password-1",
		Guest: &render.GuestNetwork{SSID: "RasBeacon-Test-Guest", Password: "guest-password-1", BSSID: guestBSSID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	confFile := filepath.Join(dir, "hostapd.conf")
	if err := os.WriteFile(confFile, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("hostapd", confFile)
	logFile := filepath.Join(dir, "hostapd.log")
	log, _ := os.Create(logFile)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting hostapd: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			data, _ := os.ReadFile(logFile)
			t.Logf("hostapd log:\n%s", tail(string(data), 40))
		}
	})
	time.Sleep(3 * time.Second)

	// hostapd created the guest interface and put it in the guest bridge.
	master, err := os.Readlink(filepath.Join("/sys/class/net", render.GuestInterface, "master"))
	if err != nil || filepath.Base(master) != render.GuestBridgeName {
		t.Fatalf("%s should be in %s (got %q, %v)", render.GuestInterface, render.GuestBridgeName, master, err)
	}

	phone := campus{staInterface: sta}
	t.Run("guest joins the guest network", func(t *testing.T) {
		status := phone.joinPersonal(t, "RasBeacon-Test-Guest", "guest-password-1")
		if !status.Connected() {
			t.Fatalf("the guest should connect, final state %+v", status)
		}
		if !strings.EqualFold(status.BSSID, guestBSSID) {
			t.Errorf("connected to %s, want the guest network's own address %s", status.BSSID, guestBSSID)
		}
	})
	t.Run("owner joins the main network", func(t *testing.T) {
		if status := phone.joinPersonal(t, "RasBeacon-Test", "owner-password-1"); !status.Connected() {
			t.Fatalf("the owner should connect, final state %+v", status)
		}
	})
	t.Run("the owner's password doesn't open the guest network", func(t *testing.T) {
		if status := phone.joinPersonal(t, "RasBeacon-Test-Guest", "owner-password-1"); status.Connected() {
			t.Fatal("connected with the wrong password")
		}
	})
}

// joinPersonal joins a password-protected network with RasBeacon's own
// wpa_supplicant configuration and returns the final state.
func (c campus) joinPersonal(t *testing.T, ssid, password string) uplink.WiFiStatus {
	t.Helper()
	conf, err := render.WPASupplicant(render.WPAInput{Country: "US", Networks: []render.WPANetwork{
		{SSID: ssid, Security: config.UplinkPersonal, Password: password},
	}})
	if err != nil {
		t.Fatal(err)
	}
	confFile := filepath.Join(t.TempDir(), "wpa_supplicant.conf")
	if err := os.WriteFile(confFile, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("wpa_supplicant", "-i", c.staInterface, "-c", confFile, "-D", "nl80211")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.stopSupplicant(cmd)

	var status uplink.WiFiStatus
	deadline := time.Now().Add(connectTimeout)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("wpa_cli", "-p", wpaControlDir, "-i", c.staInterface, "status").Output()
		status = uplink.ParseWPAStatus(string(out))
		if status.Connected() {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	return status
}
