package agent

import (
	"errors"
	"strings"
	"testing"

	"rasbeacon/internal/config"
)

func TestUplinkControls_WiFi(t *testing.T) {
	e := newClaimedEnv(t, false)
	wpa := "wpa_cli -p " + e.paths.WPAControlDir() + " -i wlan0 "

	// Nothing to disconnect or renew without an uplink.
	var ve *config.ValidationError
	if err := e.agent.DisconnectUplink(); !errors.As(err, &ve) {
		t.Errorf("got %v", err)
	}
	if err := e.agent.RenewDHCPLease(); !errors.As(err, &ve) {
		t.Errorf("got %v", err)
	}

	e.fakeUplink("wlan0", "192.168.4.20", "24", false)
	e.runner.outputs[wpa+"status"] = "bssid=aa:bb:cc:dd:ee:ff\nid=1\nwpa_state=COMPLETED\nssid=Hotel\nfreq=5180\n"
	e.check()

	if err := e.agent.RenewDHCPLease(); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("networkctl renew wlan0") {
		t.Errorf("commands: %v", e.runner.commands)
	}

	// wpa_supplicant using no saved network (or not answering): the
	// error says so, without a stray "<nil>".
	e.runner.outputs[wpa+"status"] = "wpa_state=SCANNING\n"
	if err := e.agent.DisconnectUplink(); err == nil || strings.Contains(err.Error(), "<nil>") || !strings.Contains(err.Error(), "isn't using any saved network") {
		t.Errorf("got %v", err)
	}
	e.runner.outputs[wpa+"status"] = "bssid=aa:bb:cc:dd:ee:ff\nid=1\nwpa_state=COMPLETED\nssid=Hotel\nfreq=5180\n"

	// Disconnecting pauses the network in use, and lets it back later.
	e.timers.pending = nil
	if err := e.agent.DisconnectUplink(); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran(wpa + "disable_network 1") {
		t.Errorf("commands: %v", e.runner.commands)
	}
	if len(e.timers.delays) != 1 || e.timers.delays[0] != uplinkPauseTime {
		t.Fatalf("timers: %v", e.timers.delays)
	}
	// Saving the networks ends the pause early.
	if err := e.agent.UpdateUplinkSettings(e.agent.UplinkSettings()); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran(wpa + "enable_network all") {
		t.Error("saving the networks should allow the paused one again")
	}
	e.timers.runAll()
	if !e.runner.ran(wpa + "enable_network 1") {
		t.Error("the network should be allowed again after the pause")
	}

	// There is no Ethernet port on this test device.
	if err := e.agent.RescanEthernetPort(); !errors.As(err, &ve) {
		t.Errorf("got %v", err)
	}
}

func TestUplinkControls_Ethernet(t *testing.T) {
	e := newClaimedEnv(t, true)
	e.setEthernetCable(true, "1")
	e.fakeUplink("eth0", "192.168.1.20", "24", false)
	e.check()

	e.timers.pending, e.timers.delays = nil, nil
	if err := e.agent.DisconnectUplink(); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("networkctl down eth0") {
		t.Errorf("commands: %v", e.runner.commands)
	}
	e.timers.runAll()
	if !e.runner.ran("networkctl up eth0") {
		t.Error("the cable should be used again after the pause")
	}

	e.runner.commands = nil
	if err := e.agent.RescanEthernetPort(); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("networkctl up eth0") || !e.runner.ran("networkctl reconfigure eth0") {
		t.Errorf("commands: %v", e.runner.commands)
	}

	// As a client port, rescanning runs the safety check again.
	e.agent.mu.Lock()
	e.agent.cfg.Ethernet.Role = config.EthernetClientPort
	e.agent.mu.Unlock()
	e.check()
	probes := e.probes
	if err := e.agent.RescanEthernetPort(); err != nil {
		t.Fatal(err)
	}
	if e.probes != probes+1 {
		t.Errorf("the safety check should run again: %d probes, want %d", e.probes, probes+1)
	}
}
