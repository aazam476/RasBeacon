package agent

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/render"
)

// The radios' own addresses in newRadiosEnv.
const (
	builtInMAC = "b8:27:eb:00:00:01"
	usbMAC     = "50:3e:aa:12:34:56"
	secondMAC  = "00:c0:ca:00:00:03"
)

// newRadiosEnv is a set-up RasBeacon with three radios: the built-in one
// (wlan0), the USB adapter (wlan1) and a second USB adapter (wlan2), all
// able to run a hotspot. Renaming an interface renames it in the fake
// /sys/class/net too, as the kernel does.
func newRadiosEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	classNet := e.paths.SysClassNet()
	mustWrite(t, filepath.Join(classNet, "wlan2", "phy80211", "name"), "phy2")
	device := filepath.Join(e.paths.Root, "sys", "devices", "usb1", "1-2", "1-2:1.0")
	if err := os.MkdirAll(device, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(device, filepath.Join(classNet, "wlan2", "device")); err != nil {
		t.Fatal(err)
	}
	for iface, mac := range map[string]string{"wlan0": builtInMAC, "wlan1": usbMAC, "wlan2": secondMAC} {
		mustWrite(t, filepath.Join(classNet, iface, "address"), mac+"\n")
	}
	e.runner.outputs["iw phy phy2 info"] = iwInfoTwoAPs
	e.runner.effect = func(args []string) {
		// ip link set dev <from> name <to>
		if len(args) == 7 && args[0] == "ip" && args[3] == "dev" && args[5] == "name" {
			_ = os.Rename(filepath.Join(classNet, args[4]), filepath.Join(classNet, args[6]))
		}
	}
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	return e
}

// roleOf is the role a radio got, by its own address.
func roleOf(a *Agent, mac string) (radio.Role, string) {
	for _, r := range a.radios {
		if r.MAC == mac {
			return r.Role, r.Interface
		}
	}
	return "", ""
}

func TestRadioRoles_AutomaticAtFirst(t *testing.T) {
	e := newRadiosEnv(t)
	for mac, want := range map[string]radio.Role{builtInMAC: radio.RoleUplink, usbMAC: radio.RoleHotspot, secondMAC: radio.RoleUnused} {
		if role, iface := roleOf(e.agent, mac); role != want {
			t.Errorf("%s (%s): %s, want %s", mac, iface, role, want)
		}
	}
	if e.agent.Status().RadioRolesPending != 0 {
		t.Error("nothing should wait to be kept")
	}
}

// The second adapter runs the guest network on its own: after the restart
// it is guest0 with a hostapd file of its own, run by the same hostapd.
// Keeping the change stops the countdown.
func TestRadioRoles_GuestRadioThenKeep(t *testing.T) {
	e := newRadiosEnv(t)
	// The hotspot's adapter can't broadcast two networks: no guest network.
	if e.agent.GuestSettings().Supported {
		t.Fatal("test setup: the hotspot's adapter shouldn't run a guest network")
	}
	if err := e.agent.UpdateRadioRoles(map[string]string{secondMAC: "guest"}); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	if !e.runner.ran("systemctl restart rbd.service") {
		t.Fatal("rbd should restart with the new roles")
	}

	a := e.restart()
	if err := a.UpdateGuestSettings(guestOn); err != nil {
		t.Fatal(err)
	}
	if role, iface := roleOf(a, secondMAC); role != radio.RoleGuest || iface != radio.GuestInterfaceName {
		t.Errorf("the second adapter: %s as %s", role, iface)
	}
	if role, iface := roleOf(a, usbMAC); role != radio.RoleHotspot || iface != radio.HotspotInterfaceName {
		t.Errorf("the first adapter: %s as %s", role, iface)
	}
	guestConf := e.readFile(e.paths.HostapdGuestConfig())
	for _, want := range []string{"interface=guest0", "bridge=br-guest", "ap_isolate=1", "channel=11\n", "ssid2=" + hex.EncodeToString([]byte("Ali's Guests"))} {
		if !strings.Contains(guestConf, want) {
			t.Errorf("the guest radio's file lacks %q", want)
		}
	}
	if strings.Contains(e.readFile(e.paths.HostapdConfig()), "bss=guest0") {
		t.Error("the hotspot's file must not have a guest network of its own")
	}
	if env := e.readFile(e.paths.HostapdEnv()); env != "GUEST_CONF="+e.paths.HostapdGuestConfig()+"\n" {
		t.Errorf("hostapd.env = %q", env)
	}
	if !a.GuestSettings().Supported {
		t.Error("the guest network is supported by its own radio")
	}
	if left := a.Status().RadioRolesPending; left < 170 || left > 180 {
		t.Errorf("seconds left to keep: %d", left)
	}

	if err := a.KeepRadioRoles(); err != nil {
		t.Fatal(err)
	}
	if a.Status().RadioRolesPending != 0 {
		t.Error("kept: nothing waits")
	}
	// The countdown runs out: nothing goes back.
	restarts := e.runner.count("systemctl restart rbd.service")
	e.timers.runAll()
	if e.runner.count("systemctl restart rbd.service") != restarts || a.cfg.Radios.Roles[secondMAC] != "guest" {
		t.Error("kept roles must stay")
	}
}

// Nobody keeps the new roles (the owner lost the hotspot): RasBeacon goes
// back to the previous ones by itself, and the radios get their old names.
func TestRadioRoles_NotKeptGoesBack(t *testing.T) {
	e := newRadiosEnv(t)
	if err := e.agent.UpdateRadioRoles(map[string]string{builtInMAC: "hotspot", usbMAC: "uplink"}); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	a := e.restart()
	if role, iface := roleOf(a, builtInMAC); role != radio.RoleHotspot || iface != radio.HotspotInterfaceName {
		t.Fatalf("the built-in radio: %s as %s", role, iface)
	}
	// The adapter that was ap0 got another name first.
	if role, iface := roleOf(a, usbMAC); role != radio.RoleUplink || iface != "wlanp1" {
		t.Errorf("the adapter: %s as %s", role, iface)
	}
	if !e.runner.ran("systemctl stop rasbeacon-hostapd.service") {
		t.Error("hostapd should stop before its radios are renamed")
	}

	e.timers.runAll() // three minutes pass
	if len(a.cfg.Radios.Roles) != 0 {
		t.Errorf("the roles should go back to automatic: %v", a.cfg.Radios.Roles)
	}
	if _, err := os.Stat(e.paths.RadioRolesPendingFile()); !errors.Is(err, os.ErrNotExist) {
		t.Error("nothing should wait any more")
	}
	a = e.restart()
	if role, iface := roleOf(a, usbMAC); role != radio.RoleHotspot || iface != radio.HotspotInterfaceName {
		t.Errorf("back to the adapter: %s as %s", role, iface)
	}
}

// Go back at once; two changes in a row go back to the roles before both.
func TestRadioRoles_Revert(t *testing.T) {
	e := newRadiosEnv(t)
	if err := e.agent.UpdateRadioRoles(map[string]string{secondMAC: "unused"}); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.KeepRadioRoles(); err != nil {
		t.Fatal(err)
	}
	for _, roles := range []map[string]string{{secondMAC: "hotspot"}, {secondMAC: "hotspot", usbMAC: "guest"}} {
		if err := e.agent.UpdateRadioRoles(roles); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.agent.RevertRadioRoles(); err != nil {
		t.Fatal(err)
	}
	if got := e.agent.cfg.Radios.Roles; len(got) != 1 || got[secondMAC] != "unused" {
		t.Errorf("should go back to the kept roles: %v", got)
	}
	// Nothing to go back to: fine.
	if err := e.agent.RevertRadioRoles(); err != nil {
		t.Error(err)
	}
}

func TestRadioRoles_Rules(t *testing.T) {
	e := newRadiosEnv(t)
	for name, roles := range map[string]map[string]string{
		"a radio that isn't there": {"00:00:5e:00:53:01": "hotspot"},
		"two hotspots":             {usbMAC: "hotspot", secondMAC: "hotspot"},
		"not a job":                {usbMAC: "router"},
		"no hotspot at all":        {builtInMAC: "unused", usbMAC: "unused", secondMAC: "unused"},
	} {
		var verr *config.ValidationError
		if err := e.agent.UpdateRadioRoles(roles); !errors.As(err, &verr) || verr.Field != "roles" {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A radio that can't run a hotspot can't be one.
	e.agent.radios[2].SupportsAP = false
	if err := e.agent.UpdateRadioRoles(map[string]string{secondMAC: "guest"}); err == nil ||
		!strings.Contains(err.Error(), "can't run a hotspot") {
		t.Errorf("guest on a radio without AP: %v", err)
	}

	setup := newTestEnv(t)
	setup.start()
	if err := setup.agent.UpdateRadioRoles(nil); !errors.Is(err, ErrWrongMode) {
		t.Errorf("before setup: %v", err)
	}
}

// The guest interface has one name everywhere.
func TestRadioRoles_GuestInterfaceName(t *testing.T) {
	if render.GuestInterface != radio.GuestInterfaceName {
		t.Errorf("render says %q, radio %q", render.GuestInterface, radio.GuestInterfaceName)
	}
}
