package agent

import (
	"os"
	"strings"
	"testing"

	"rasbeacon/internal/api"
)

func TestAccessSettings_AllowListAndLimitReachHostapd(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()

	if err := e.agent.UpdateAccessSettings(api.AccessSettings{AllowListOn: true}); err == nil {
		t.Error("an empty allow list was accepted: nobody could join")
	}
	if err := e.agent.UpdateAccessSettings(api.AccessSettings{Allowed: []string{"not-a-mac"}}); err == nil {
		t.Error("a bad address was accepted")
	}
	if err := e.agent.UpdateAccessSettings(api.AccessSettings{MaxDevices: 232}); err == nil {
		t.Error("a limit above 231 was accepted")
	}

	req := api.AccessSettings{AllowListOn: true, Allowed: []string{"3A-1F-22-AA-BB-CC", "3a:1f:22:aa:bb:cc", "02:00:00:00:00:01"}, MaxDevices: 5}
	if err := e.agent.UpdateAccessSettings(req); err != nil {
		t.Fatal(err)
	}
	got := e.agent.AccessSettings()
	if !got.AllowListOn || got.MaxDevices != 5 || strings.Join(got.Allowed, ",") != "3a:1f:22:aa:bb:cc,02:00:00:00:00:01" {
		t.Errorf("saved %+v", got)
	}
	hostapd := e.readFile(e.paths.HostapdConfig())
	for _, want := range []string{"macaddr_acl=1", "accept_mac_file=" + e.paths.HostapdAcceptFile(), "max_num_sta=5"} {
		if !strings.Contains(hostapd, want) {
			t.Errorf("hostapd.conf lacks %q", want)
		}
	}
	if report := e.agent.Diagnostics(); !strings.Contains(report, "access: allow list on (2 addresses), device limit 5") ||
		strings.Contains(report, "3a:1f:22:aa:bb:cc") {
		t.Error("diagnostics should say how many devices are allowed, without their addresses")
	}
	list, _ := os.ReadFile(e.paths.HostapdAcceptFile())
	if string(list) != "3a:1f:22:aa:bb:cc\n02:00:00:00:00:01\n" {
		t.Errorf("allow list file = %q", list)
	}

	// Off again: hostapd lets everyone with the password in.
	if err := e.agent.UpdateAccessSettings(api.AccessSettings{Allowed: got.Allowed}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.HostapdConfig()), "macaddr_acl") {
		t.Error("the allow list stayed on")
	}
}
