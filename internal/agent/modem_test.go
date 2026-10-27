package agent

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// fakeModem makes mmcli report one modem in state, with a bearer.
func (e *testEnv) fakeModem(state, unlock string) {
	e.runner.outputs["mmcli -L -J"] = `{"modem-list":["/org/freedesktop/ModemManager1/Modem/0"]}`
	e.runner.outputs["mmcli -m 0 -J"] = `{"modem":{"dbus-path":"/org/freedesktop/ModemManager1/Modem/0","generic":{
		"manufacturer":"Quectel","model":"EM12-G","state":"` + state + `","state-failed-reason":"--",
		"unlock-required":"` + unlock + `","unlock-retries":["sim-pin (3)"],
		"signal-quality":{"value":"75"},"access-technologies":["lte"],
		"sim":"/org/freedesktop/ModemManager1/SIM/0","bearers":["/org/freedesktop/ModemManager1/Bearer/1"],
		"ports":["cdc-wdm0 (qmi)","wwan0 (net)"]},"3gpp":{"operator-name":"T-Mobile"}}}`
	e.runner.outputs["mmcli -b 1 -J"] = `{"bearer":{"status":{"connected":"yes","interface":"wwan0"},
		"ipv4-config":{"method":"static","address":"10.164.33.17","prefix":"30","gateway":"10.164.33.18",
		"dns":["10.177.0.34"],"mtu":"1500"}}}`
}

func (e *testEnv) modemState() *api.ModemStatus {
	return e.agent.Status().Uplink.Modem
}

func TestModem_NoneWithoutModemManager(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.check()
	if e.modemState() != nil {
		t.Error("no modem without ModemManager's answer")
	}
	// Without any uplink, nothing is "the modem" (both names are empty),
	// and the connection isn't metered.
	if s := e.agent.Status(); s.Uplink.Kind != "" || s.Hotspot.Metered {
		t.Errorf("kind %q, metered %v", s.Uplink.Kind, s.Hotspot.Metered)
	}
	e.runner.outputs["mmcli -L -J"] = `{"modem-list":[]}`
	e.check()
	if e.modemState() != nil {
		t.Error("no modem in the list")
	}
}

func TestModem_PINIsTriedOnce(t *testing.T) {
	e := newClaimedEnv(t, false)
	e.fakeModem("locked", "sim-pin")
	e.check()
	if m := e.modemState(); m == nil || m.State != "pin-needed" || m.PINTries != 3 || m.Name != "Quectel EM12-G" {
		t.Fatalf("modem = %+v", m)
	}

	// The owner saves a wrong PIN: it is sent once, refused, and never again.
	if err := e.agent.UpdateModemSettings(api.ModemSettings{PIN: "1111"}); err != nil {
		t.Fatal(err)
	}
	e.runner.failing = append(e.runner.failing, "mmcli -i 0 --pin=1111")
	e.check()
	e.check()
	e.check()
	if n := e.runner.count("--pin=1111"); n != 1 {
		t.Errorf("the PIN was sent %d times; it must be once", n)
	}
	if m := e.modemState(); m.State != "pin-refused" {
		t.Errorf("state = %q", m.State)
	}

	// A new PIN is tried, once.
	if err := e.agent.UpdateModemSettings(api.ModemSettings{PIN: "4711"}); err != nil {
		t.Fatal(err)
	}
	e.check()
	e.check()
	if n := e.runner.count("--pin=4711"); n != 1 {
		t.Errorf("the new PIN was sent %d times", n)
	}
	if !e.agent.ModemSettings().HasPIN {
		t.Error("the PIN is saved")
	}

	// A blocked SIM: nothing to send.
	e.fakeModem("locked", "sim-puk")
	e.check()
	if m := e.modemState(); m.State != "puk-needed" {
		t.Errorf("state = %q", m.State)
	}
}

func TestModem_TurnsOnAndConnects(t *testing.T) {
	e := newClaimedEnv(t, false)
	clock := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return clock.now }
	if err := e.agent.UpdateModemSettings(api.ModemSettings{APN: "fast.t-mobile.com", Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}

	e.fakeModem("disabled", "none")
	e.check()
	if !e.runner.ran("mmcli -m 0 -e") || e.modemState().State != "starting" {
		t.Error("a modem that is off is turned on")
	}

	e.fakeModem("registered", "none")
	e.runner.failing = append(e.runner.failing, "mmcli -m 0 --simple-connect")
	e.check()
	if !e.runner.ran("mmcli -m 0 --simple-connect=ip-type=ipv4,apn=fast.t-mobile.com,user=u,password=p") {
		t.Errorf("connect with the APN: %v", e.runner.commands)
	}
	if m := e.modemState(); m.State != "connecting" || m.Operator != "T-Mobile" || m.Network != "4G" || m.SignalPercent != 75 {
		t.Errorf("modem = %+v", m)
	}
	// Not again right away, then again after a while.
	e.check()
	if e.runner.count("--simple-connect") != 1 {
		t.Error("connecting again too soon")
	}
	clock.advance(modemConnectEvery + time.Second)
	e.check()
	if e.runner.count("--simple-connect") != 2 || e.modemState().Error == "" {
		t.Errorf("connect again after a while, with the error shown: %+v", e.modemState())
	}

	// Connected: its data port gets the address, and is an uplink.
	e.fakeModem("connected", "none")
	e.check()
	network := e.readFile(e.networkdFile("00-rasbeacon-wwan0.network"))
	for _, want := range []string{"Address=10.164.33.17/30", "Gateway=10.164.33.18", "DNS=10.177.0.34", "Metric=950", "MTUBytes=1500"} {
		if !strings.Contains(network, want) {
			t.Errorf("networkd file has no %q:\n%s", want, network)
		}
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), `"wwan0"`) {
		t.Error("devices' traffic may leave through the modem")
	}
	e.fakeUplink("wwan0", "10.164.33.17", "30", false)
	e.check()
	if u := e.agent.Status().Uplink; u.Kind != "modem" || u.Modem.State != "connected" || !e.agent.Status().Hotspot.Metered {
		t.Errorf("uplink = %+v", u)
	}

	// Unplugged: the port is no longer an uplink.
	e.runner.outputs["mmcli -L -J"] = `{"modem-list":[]}`
	e.check()
	if _, err := os.Stat(e.networkdFile("00-rasbeacon-wwan0.network")); !os.IsNotExist(err) {
		t.Error("the modem's networkd file should go")
	}
}

func TestModem_SettingsAreChecked(t *testing.T) {
	e := newClaimedEnv(t, false)
	for field, req := range map[string]api.ModemSettings{
		"apn":      {APN: "a,b"},
		"username": {Username: `"x"`},
		"password": {Password: "a=b"},
		"pin":      {PIN: "12a4"},
	} {
		var ve *config.ValidationError
		if err := e.agent.UpdateModemSettings(req); !errors.As(err, &ve) || ve.Field != field {
			t.Errorf("%s: got %v", field, err)
		}
	}
	if err := e.agent.UpdateModemSettings(api.ModemSettings{APN: "internet", Password: "secret", PIN: "1234"}); err != nil {
		t.Fatal(err)
	}
	s := e.agent.ModemSettings()
	if s.APN != "internet" || !s.HasPassword || !s.HasPIN || s.Password != "" || s.PIN != "" {
		t.Errorf("settings = %+v", s)
	}
	if err := e.agent.UpdateModemSettings(api.ModemSettings{APN: "internet", ForgetPassword: true, ForgetPIN: true}); err != nil {
		t.Fatal(err)
	}
	if s := e.agent.ModemSettings(); s.HasPassword || s.HasPIN {
		t.Errorf("forgotten: %+v", s)
	}
}
