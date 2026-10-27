package agent

import (
	"errors"
	"strings"
	"testing"

	"rasbeacon/internal/config"
)

func TestMAC_WiFiClonedRandomAndOwn(t *testing.T) {
	e := newClaimedEnv(t, false)
	// The radio's own address, as ip reports it while another is set.
	e.runner.outputs["ip -json link show dev wlan0"] = `[{"ifname":"wlan0","address":"3c:22:fb:01:02:03","permaddr":"dc:a6:32:00:00:01"}]`
	mustWrite(t, e.paths.SysClassNet()+"/wlan0/address", "dc:a6:32:00:00:01\n")

	settings := e.agent.UplinkSettings()
	settings.WiFiMAC = &config.MACChoice{Mode: config.MACCloned, Address: "3C:22:FB:AA:BB:CC"}
	if err := e.agent.UpdateUplinkSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ip link set dev wlan0 down", "ip link set dev wlan0 address 3c:22:fb:aa:bb:cc", "ip link set dev wlan0 up"} {
		if !e.runner.ran(want) {
			t.Errorf("did not run %q", want)
		}
	}
	if !e.runner.ran("systemctl restart rasbeacon-wpa@wlan0.service") {
		t.Error("wpa_supplicant must start again with the new address")
	}
	if got := e.agent.UplinkSettings().WiFiMAC; got.Mode != config.MACCloned || got.Address != "3c:22:fb:aa:bb:cc" {
		t.Errorf("saved = %+v", got)
	}

	// Saving again without a change doesn't touch the radio.
	e.runner.commands = nil
	if err := e.agent.UpdateUplinkSettings(e.agent.UplinkSettings()); err != nil {
		t.Fatal(err)
	}
	if e.runner.ran("address 3c:22:fb:aa:bb:cc") {
		t.Error("no change: the address stays")
	}

	// Random: a new one, kept when saved again.
	settings = e.agent.UplinkSettings()
	settings.WiFiMAC = &config.MACChoice{Mode: config.MACRandom}
	if err := e.agent.UpdateUplinkSettings(settings); err != nil {
		t.Fatal(err)
	}
	random := e.agent.UplinkSettings().WiFiMAC.Address
	if random == "" || random == "3c:22:fb:aa:bb:cc" {
		t.Fatalf("random = %q", random)
	}
	settings = e.agent.UplinkSettings()
	settings.WiFiMAC = &config.MACChoice{Mode: config.MACRandom}
	if err := e.agent.UpdateUplinkSettings(settings); err != nil || e.agent.UplinkSettings().WiFiMAC.Address != random {
		t.Errorf("the random address should stay: %v", err)
	}

	// The Pi's own again: its permanent address comes back (the radio
	// has the random one now).
	mustWrite(t, e.paths.SysClassNet()+"/wlan0/address", random+"\n")
	settings.WiFiMAC = &config.MACChoice{}
	if err := e.agent.UpdateUplinkSettings(settings); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("ip link set dev wlan0 address dc:a6:32:00:00:01") {
		t.Error("the radio's own address should come back")
	}
}

func TestMAC_EthernetAndChecks(t *testing.T) {
	e := newClaimedEnv(t, true)
	e.runner.outputs["ip -json link show dev eth0"] = `[{"ifname":"eth0","address":"dc:a6:32:00:00:09"}]`
	settings := e.agent.UplinkSettings()
	settings.EthernetMAC = &config.MACChoice{Mode: config.MACCloned, Address: "3c:22:fb:01:02:03"}
	if err := e.agent.UpdateUplinkSettings(settings); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.readFile(e.networkdFile("00-rasbeacon-eth0.network")), "MACAddress=3c:22:fb:01:02:03") {
		t.Error("networkd should set the cloned address on Ethernet")
	}

	var ve *config.ValidationError
	settings.EthernetMAC = &config.MACChoice{Mode: config.MACCloned, Address: "dc:a6:32:00:00:09"}
	if err := e.agent.UpdateUplinkSettings(settings); !errors.As(err, &ve) || ve.Field != "ethernetMac" || !strings.Contains(ve.Message, "own") {
		t.Errorf("RasBeacon's own address: %v", err)
	}
	settings.EthernetMAC = &config.MACChoice{Mode: config.MACCloned, Address: "01:00:5e:00:00:01"}
	if err := e.agent.UpdateUplinkSettings(settings); !errors.As(err, &ve) || ve.Field != "ethernetMac" {
		t.Errorf("a group address: %v", err)
	}
	settings.WiFiMAC = &config.MACChoice{Mode: "x"}
	settings.EthernetMAC = nil
	if err := e.agent.UpdateUplinkSettings(settings); !errors.As(err, &ve) || ve.Field != "wifiMac" {
		t.Errorf("unknown mode: %v", err)
	}
}
