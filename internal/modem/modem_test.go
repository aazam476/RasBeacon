package modem

import (
	"net/netip"
	"slices"
	"testing"
)

// Excerpts of `mmcli -J` output for a Quectel EM12-G (QMI).
const (
	listOut = `{"modem-list":["/org/freedesktop/ModemManager1/Modem/0"]}`

	lockedOut = `{"modem":{"dbus-path":"/org/freedesktop/ModemManager1/Modem/0","generic":{
		"manufacturer":"Quectel","model":"EM12-G","state":"locked","state-failed-reason":"--",
		"unlock-required":"sim-pin","unlock-retries":["sim-pin (3)","sim-puk (10)"],
		"signal-quality":{"recent":"no","value":"0"},"access-technologies":[],
		"sim":"/org/freedesktop/ModemManager1/SIM/0","bearers":[],
		"ports":["cdc-wdm0 (qmi)","ttyUSB2 (at)","wwan0 (net)"]},
		"3gpp":{"operator-name":"--"}}}`

	connectedOut = `{"modem":{"dbus-path":"/org/freedesktop/ModemManager1/Modem/0","generic":{
		"manufacturer":"Quectel","model":"EM12-G","state":"connected","state-failed-reason":"--",
		"unlock-required":"none","unlock-retries":["sim-pin (3)"],
		"signal-quality":{"recent":"yes","value":"75"},"access-technologies":["lte"],
		"sim":"/org/freedesktop/ModemManager1/SIM/0","bearers":["/org/freedesktop/ModemManager1/Bearer/1"],
		"ports":["cdc-wdm0 (qmi)","wwan0 (net)"]},
		"3gpp":{"operator-name":"T-Mobile"}}}`

	bearerOut = `{"bearer":{"status":{"connected":"yes","interface":"wwan0"},
		"ipv4-config":{"method":"static","address":"10.164.33.17","prefix":"30","gateway":"10.164.33.18",
		"dns":["10.177.0.34","10.177.0.210"],"mtu":"1500"}}}`
)

func TestList(t *testing.T) {
	got, err := List(listOut)
	if err != nil || !slices.Equal(got, []string{"0"}) {
		t.Errorf("got %v, %v", got, err)
	}
	if got, _ := List(`{"modem-list":[]}`); len(got) != 0 {
		t.Errorf("no modems: %v", got)
	}
	if _, err := List("not json"); err == nil {
		t.Error("bad output should be an error")
	}
}

func TestParse_LockedAndConnected(t *testing.T) {
	m, err := Parse(lockedOut)
	if err != nil {
		t.Fatal(err)
	}
	if m.Index != "0" || m.State != "locked" || m.UnlockRequired != "sim-pin" || m.PINTries != 3 || m.SIM != "0" ||
		m.DataPort != "wwan0" || m.Operator != "" || m.FailedReason != "" || m.Name() != "Quectel EM12-G" {
		t.Errorf("locked modem = %+v", m)
	}

	m, err = Parse(connectedOut)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != "connected" || m.UnlockRequired != "" || m.SignalPercent != 75 || m.Operator != "T-Mobile" ||
		!slices.Equal(m.Bearers, []string{"1"}) || m.Network() != "4G" {
		t.Errorf("connected modem = %+v", m)
	}
	if _, err := Parse("{"); err == nil {
		t.Error("bad output should be an error")
	}
}

func TestParseBearer(t *testing.T) {
	b, err := ParseBearer(bearerOut)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Connected || b.Interface != "wwan0" || b.Method != "static" || b.MTU != 1500 ||
		b.Address != netip.MustParsePrefix("10.164.33.17/30") || b.Gateway != netip.MustParseAddr("10.164.33.18") ||
		len(b.DNS) != 2 {
		t.Errorf("bearer = %+v", b)
	}
	b, _ = ParseBearer(`{"bearer":{"status":{"connected":"no","interface":"--"},"ipv4-config":{"method":"--","address":"--","prefix":"--","gateway":"--","dns":[],"mtu":"--"}}}`)
	if b.Connected || b.Interface != "" || b.Address.IsValid() || b.MTU != 0 {
		t.Errorf("disconnected bearer = %+v", b)
	}
	if _, err := ParseBearer("x"); err == nil {
		t.Error("bad output should be an error")
	}
}

func TestNameAndNetwork(t *testing.T) {
	if (Modem{}).Name() != "Mobile modem" {
		t.Error("a modem without a name")
	}
	for _, tc := range []struct {
		tech []string
		want string
	}{
		{[]string{"lte", "5gnr"}, "5G"},
		{[]string{"umts"}, "3G"},
		{[]string{"hspa-plus", "lte"}, "4G"},
		{[]string{"gsm"}, ""},
		{nil, ""},
	} {
		if got := (Modem{AccessTech: tc.tech}).Network(); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.tech, got, tc.want)
		}
	}
}
