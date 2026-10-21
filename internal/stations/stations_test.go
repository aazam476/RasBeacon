package stations

import (
	"reflect"
	"testing"
)

// A shortened real `iw dev ap0 station dump` with two devices.
const sampleDump = `Station 3a:1f:22:aa:bb:cc (on ap0)
	inactive time:	1200 ms
	rx bytes:	123456
	signal:  	-48 [-50, -52] dBm
	tx bitrate:	144.4 MBit/s MCS 15 short GI
	connected time:	367 seconds
Station 8E:00:11:22:33:44 (on ap0)
	inactive time:	40 ms
	signal:  	-71 [-73, -74] dBm
	connected time:	12 seconds
`

const sampleLeases = `1790800000 3a:1f:22:aa:bb:cc 10.87.0.23 Alis-iPhone 01:3a:1f:22:aa:bb:cc
1790800100 aa:aa:aa:aa:aa:aa 10.87.0.99 old-laptop *
`

func TestParseStationDump(t *testing.T) {
	got := ParseStationDump(sampleDump)
	if len(got) != 2 {
		t.Fatalf("got %d stations, want 2", len(got))
	}
	if got[0].MAC != "3a:1f:22:aa:bb:cc" || got[0].SignalDBm != -48 || got[0].ConnectedSeconds != 367 {
		t.Errorf("first station parsed wrong: %+v", got[0])
	}
	if got[1].MAC != "8e:00:11:22:33:44" {
		t.Errorf("MAC should be lowercased, got %q", got[1].MAC)
	}
}

func TestParseStationDump_Empty(t *testing.T) {
	if got := ParseStationDump(""); len(got) != 0 {
		t.Errorf("no devices should give an empty list, got %+v", got)
	}
}

func TestMerge_OnlyConnectedDevicesAreListed(t *testing.T) {
	merged := Merge(ParseStationDump(sampleDump), ParseLeases(sampleLeases))

	// The old laptop has a lease but is not connected, so it must not count.
	if len(merged) != 2 {
		t.Fatalf("got %d devices, want 2", len(merged))
	}
	phone := merged[0] // longest connected first
	if phone.Hostname != "Alis-iPhone" || phone.IP != "10.87.0.23" {
		t.Errorf("lease info not merged: %+v", phone)
	}
	if merged[1].IP != "" {
		t.Errorf("device without a lease should have no IP yet, got %q", merged[1].IP)
	}
}

func TestParseBridgeFDB_DevicesBehindThePort(t *testing.T) {
	out := `[{"mac":"33:33:00:00:00:01","ifname":"eth0","flags":["self"],"state":"permanent"},` +
		`{"mac":"dc:a6:32:01:02:03","ifname":"eth0","vlan":1,"master":"br-lan","state":"permanent"},` +
		`{"mac":"01:00:5e:00:00:01","ifname":"eth0","master":"br-lan","state":""},` +
		`{"mac":"A4:83:E7:12:34:56","ifname":"eth0","master":"br-lan","state":""},` +
		`{"mac":"a4:83:e7:12:34:56","ifname":"eth0","flags":["extern_learn"],"master":"br-lan","state":""}]`
	got, err := ParseBridgeFDB(out)
	if err != nil || len(got) != 1 || got[0] != "a4:83:e7:12:34:56" {
		t.Errorf("got %v, %v; want only the laptop", got, err)
	}
	if got, err := ParseBridgeFDB(""); err != nil || got != nil {
		t.Errorf("empty table: got %v, %v", got, err)
	}
	if _, err := ParseBridgeFDB("not json"); err == nil {
		t.Error("expected an error")
	}
}

func TestParseNeighbors(t *testing.T) {
	out := `[{"dst":"fe80::1c2d:3eff:fe4f:5a6b","dev":"br-lan","lladdr":"3A:1F:22:AA:BB:CC","state":["STALE"]},
{"dst":"2001:db8::5","dev":"br-lan","state":["FAILED"]},
{"dst":"2001:db8::7","dev":"br-lan","lladdr":"3a:1f:22:aa:bb:cc","state":["REACHABLE"]}]`
	got, err := ParseNeighbors(out)
	want := []Neighbor{{IP: "fe80::1c2d:3eff:fe4f:5a6b", MAC: "3a:1f:22:aa:bb:cc"}, {IP: "2001:db8::7", MAC: "3a:1f:22:aa:bb:cc"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ParseNeighbors = %+v, %v; want %+v", got, err, want)
	}
	if got, err := ParseNeighbors(""); got != nil || err != nil {
		t.Errorf("an empty table = %+v, %v", got, err)
	}
	if _, err := ParseNeighbors("not json"); err == nil {
		t.Error("broken output should be an error")
	}
}
