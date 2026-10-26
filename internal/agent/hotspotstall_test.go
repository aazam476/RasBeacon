package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/config"
)

// The phone and laptop used in these tests, with their leases.
const (
	stallPhone  = "3a:1f:22:aa:bb:cc"
	stallLaptop = "92:4e:fe:11:22:33"
	stallLeases = "1790800000 3a:1f:22:aa:bb:cc 10.87.0.23 Alis-iPhone *\n" +
		"1790800000 92:4e:fe:11:22:33 10.87.0.31 Alis-MacBook *\n"
)

// stationBlock is one device in `iw dev ap0 station dump` output.
func stationBlock(mac string, connected int, rx, tx uint64) string {
	return fmt.Sprintf("Station %s (on ap0)\n\tinactive time:\t20 ms\n\trx packets:\t%d\n\ttx packets:\t%d\n"+
		"\tconnected time:\t%d seconds\n\tMFP:\t\tno\n", mac, rx, tx, connected)
}

// stallDump is the station dump key the fake runner answers.
const stallDump = "iw dev ap0 station dump"

// newStallEnv is a set-up device whose hotspot is in its bridge, with two
// devices that have leases.
func newStallEnv(t *testing.T) (*testEnv, *hotspotClock) {
	t.Helper()
	e, clock := newHotspotCheckEnv(t)
	mustWrite(t, e.paths.DnsmasqLeases(), stallLeases)
	mustWrite(t, e.paths.ProcUptime(), "4000.50 9000.00\n")
	return e, clock
}

// stallMinute runs a check with the given station dump.
func (e *testEnv) stallMinute(clock *hotspotClock, dump string) {
	e.runner.outputs[stallDump] = dump
	e.checkHotspotAt(clock)
}

// stallFirstMinute gives both devices a starting point: the phone has
// sent 100 packets and received 50.
func (e *testEnv) stallFirstMinute(clock *hotspotClock) {
	e.stallMinute(clock, stationBlock(stallPhone, 600, 100, 50)+stationBlock(stallLaptop, 600, 10, 10))
	e.runner.commands = nil
}

func TestHotspotStall_RestartsWhenTheHotspotStopsSending(t *testing.T) {
	e, clock := newStallEnv(t)
	e.runner.failing = []string{"ping"}
	e.runner.outputs["systemctl show -p ActiveEnterTimestampMonotonic --value rasbeacon-hostapd.service"] = "1000500000\n"
	e.runner.outputs["iw dev ap0 info"] = "Interface ap0\n\tchannel 40 (5200 MHz), width: 80 MHz, center1: 5210 MHz\n"
	e.runner.outputs["dmesg"] = "[  1.0] usb 1-1.2: new device\n[ 20.0] rtw88_8822bu 1-1.2:1.0: Firmware version 27.2.0\n"
	e.runner.outputs["ip neigh show dev br-lan"] = "10.87.0.23 lladdr 3a:1f:22:aa:bb:cc FAILED\n"
	e.runner.prefixOutputs = map[string]string{"journalctl --no-pager -o short-iso --since @": "2026-10-19T11:55:00 rbd[1]: DNS servers saved\n"}
	e.stallFirstMinute(clock)

	// A minute later the phone has sent 300 more packets, and the Pi has
	// sent it nothing. The laptop is idle.
	e.stallMinute(clock, stationBlock(stallPhone, 660, 400, 50)+stationBlock(stallLaptop, 660, 10, 10))

	if !e.runner.ran("ping -c 1 -W 1 10.87.0.23") {
		t.Errorf("the phone should be pinged first; commands: %v", e.runner.commands)
	}
	if e.runner.ran("ping -c 1 -W 1 10.87.0.31") {
		t.Error("an idle device (sending nothing) must not be pinged")
	}
	if !e.runner.ran("systemctl restart rasbeacon-hostapd.service") {
		t.Fatalf("a stalled hotspot should be restarted; commands: %v", e.runner.commands)
	}

	stalls := e.agent.readStalls()
	if len(stalls) != 1 {
		t.Fatalf("the stall should be saved; got %d records", len(stalls))
	}
	s := stalls[0]
	if !s.Restarted || s.UptimeSeconds != 4000 || s.HotspotUptimeSeconds != 3000 {
		t.Errorf("times or restart wrong: %+v", s)
	}
	if s.RadioChannel != "channel 40 (5200 MHz), width: 80 MHz, center1: 5210 MHz" || s.Security == "" || s.Band == "" {
		t.Errorf("hotspot details wrong: %+v", s)
	}
	if len(s.Devices) != 2 || s.Devices[0].MAC != stallPhone || !s.Devices[0].Stalled || s.Devices[1].Stalled {
		t.Errorf("only the phone should be marked stalled: %+v", s.Devices)
	}
	if s.Devices[0].RxPackets != 400 || s.Devices[0].TxPackets != 50 {
		t.Errorf("counters not kept: %+v", s.Devices[0])
	}
	if len(s.Kernel) != 2 || !strings.Contains(s.Before[0], "DNS servers saved") || !strings.Contains(s.Neighbors, "FAILED") {
		t.Errorf("kernel, log or neighbor lines missing: %+v", s)
	}

	// The diagnostics report lists it, with MAC addresses partly hidden.
	report := e.agent.Diagnostics()
	for _, want := range []string{"hotspot stalls: 1 recorded", "hotspotUptime=3000s", "stalled=true", "DNS servers saved", "the hotspot stopped sending data to 1 of its devices"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q", want)
		}
	}
	if strings.Contains(report, stallPhone) {
		t.Error("the report must hide the end of MAC addresses")
	}
}

func TestHotspotStall_ADeviceThatAnswersIsFine(t *testing.T) {
	e, clock := newStallEnv(t)
	e.stallFirstMinute(clock)
	// The phone sends and receives nothing back, but answers the ping.
	e.stallMinute(clock, stationBlock(stallPhone, 660, 400, 50)+stationBlock(stallLaptop, 660, 10, 10))
	if !e.runner.ran("ping -c 1 -W 1 10.87.0.23") {
		t.Fatal("the phone should be pinged")
	}
	if e.runner.ran("systemctl restart") || len(e.agent.readStalls()) != 0 {
		t.Error("a device that answers means the hotspot works")
	}
}

func TestHotspotStall_ARadioThatSendsDuringThePingIsFine(t *testing.T) {
	e, clock := newStallEnv(t)
	e.stallFirstMinute(clock)
	// The phone is asleep and doesn't answer in time, but the radio sends
	// it the ping: its tx count moves.
	e.runner.failing = []string{"ping"}
	e.runner.effect = func(args []string) {
		if args[0] == "ping" {
			e.runner.outputs[stallDump] = stationBlock(stallPhone, 661, 401, 51) + stationBlock(stallLaptop, 661, 10, 10)
		}
	}
	e.stallMinute(clock, stationBlock(stallPhone, 660, 400, 50)+stationBlock(stallLaptop, 660, 10, 10))
	if e.runner.ran("systemctl restart") {
		t.Error("the radio could still send, so this is no stall")
	}
}

func TestHotspotStall_OnlyAFullMinuteCounts(t *testing.T) {
	e, clock := newStallEnv(t)
	e.runner.failing = []string{"ping"}

	// The first check has nothing to compare with.
	e.stallMinute(clock, stationBlock(stallPhone, 600, 100, 50))
	if e.runner.ran("ping") {
		t.Error("the first check must not ping anyone")
	}
	// The phone joined again (its connected time went down).
	e.stallMinute(clock, stationBlock(stallPhone, 30, 400, 50))
	if e.runner.ran("ping") {
		t.Error("a device that joined again has no full minute to compare")
	}
	// The Pi sent it something: fine.
	e.stallMinute(clock, stationBlock(stallPhone, 90, 500, 60))
	if e.runner.ran("ping") {
		t.Error("a device the Pi sends to is fine")
	}
}

func TestHotspotStall_ADeviceWithoutAnAddressIsLeftAlone(t *testing.T) {
	e, clock := newStallEnv(t)
	e.runner.failing = []string{"ping"}
	other := "aa:bb:cc:dd:ee:ff" // no lease
	e.stallMinute(clock, stationBlock(other, 600, 100, 5))
	e.stallMinute(clock, stationBlock(other, 660, 300, 5))
	if e.runner.ran("ping") || e.runner.ran("systemctl restart") {
		t.Errorf("without an address it can't be pinged; commands: %v", e.runner.commands)
	}
}

func TestHotspotStall_NotRestartedAgainSoSoonButStillRecorded(t *testing.T) {
	e, clock := newStallEnv(t)
	e.runner.failing = []string{"ping"}
	e.stallFirstMinute(clock)
	e.stallMinute(clock, stationBlock(stallPhone, 660, 400, 50))
	// The restart cleared the counters; the next minute starts over.
	e.stallMinute(clock, stationBlock(stallPhone, 30, 10, 5))
	e.runner.commands = nil
	e.stallMinute(clock, stationBlock(stallPhone, 90, 200, 5))
	if e.runner.ran("systemctl restart") {
		t.Error("the hotspot must not be restarted twice within the pause")
	}
	stalls := e.agent.readStalls()
	if len(stalls) != 2 || !stalls[0].Restarted || stalls[1].Restarted {
		t.Errorf("both stalls should be recorded, the second without a restart: %+v", stalls)
	}
}

func TestHotspotStall_ChecksTheGuestNetworkToo(t *testing.T) {
	e := newGuestCapableEnv(t)
	if err := e.agent.UpdateGuestSettings(guestOn); err != nil {
		t.Fatal(err)
	}
	clock := &hotspotClock{now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return clock.now }
	mustWrite(t, e.paths.DnsmasqLeases(), "1790800000 3a:1f:22:aa:bb:cc 10.87.1.40 guest-phone *\n")
	e.runner.failing = []string{"ping"}
	e.runner.outputs["iw dev guest0 station dump"] = stationBlock(stallPhone, 600, 100, 50)
	e.checkHotspotAt(clock)
	e.runner.outputs["iw dev guest0 station dump"] = stationBlock(stallPhone, 660, 400, 50)
	e.checkHotspotAt(clock)
	stalls := e.agent.readStalls()
	if len(stalls) != 1 || stalls[0].Devices[0].Interface != "guest0" {
		t.Errorf("a guest device's stall should be recorded: %+v", stalls)
	}
}

func TestHotspotStall_KeepsTheLatestRecords(t *testing.T) {
	e, _ := newStallEnv(t)
	for i := range stallsKept + 5 {
		e.agent.saveStall(hotspotStall{UptimeSeconds: i})
	}
	stalls := e.agent.readStalls()
	if len(stalls) != stallsKept || stalls[0].UptimeSeconds != 5 || stalls[stallsKept-1].UptimeSeconds != stallsKept+4 {
		t.Errorf("should keep the newest %d, oldest first; got %d starting at %d", stallsKept, len(stalls), stalls[0].UptimeSeconds)
	}
	// An unreadable file counts as none, and is replaced on the next stall.
	mustWrite(t, e.paths.HotspotStallsFile(), "not json")
	if e.agent.readStalls() != nil {
		t.Error("a broken file should read as no records")
	}
	e.agent.saveStall(hotspotStall{UptimeSeconds: 1})
	var saved []hotspotStall
	if err := config.ReadJSON(e.paths.HotspotStallsFile(), &saved); err != nil || len(saved) != 1 {
		t.Errorf("got %v, %v", saved, err)
	}
}

func TestStallReport(t *testing.T) {
	if got := stallReport(nil); got != "hotspot stalls: none recorded\n" {
		t.Errorf("got %q", got)
	}
	older := hotspotStall{Time: time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)}
	newer := hotspotStall{Time: time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC), Kernel: []string{"k"}, Before: []string{"b"}}
	got := stallReport([]hotspotStall{older, newer})
	if strings.Index(got, "11:00:00") > strings.Index(got, "10:00:00") {
		t.Error("the newest stall should come first")
	}
	if !strings.Contains(got, "  k\n") || !strings.Contains(got, "  b\n") {
		t.Errorf("kernel and log lines missing: %q", got)
	}
}

func TestHostapdUptimeSeconds(t *testing.T) {
	e, _ := newStallEnv(t)
	key := "systemctl show -p ActiveEnterTimestampMonotonic --value rasbeacon-hostapd.service"
	for _, tc := range []struct {
		out  string
		want int
	}{
		{"1000500000\n", 3000}, // started 1000.5 s after boot; the Pi is up 4000 s
		{"0\n", -1},            // not running
		{"garbage", -1},
	} {
		e.runner.outputs[key] = tc.out
		if got := e.agent.hostapdUptimeSeconds(); got != tc.want {
			t.Errorf("%q: got %d, want %d", tc.out, got, tc.want)
		}
	}
	delete(e.runner.outputs, key)
	e.runner.failing = []string{"systemctl show"}
	if got := e.agent.hostapdUptimeSeconds(); got != -1 {
		t.Errorf("a failed command should give -1, got %d", got)
	}
}

func TestChannelLineAndLastLines(t *testing.T) {
	if got := channelLine("Interface ap0\n\ttype AP\n\tchannel 36 (5180 MHz), width: 80 MHz\n"); got != "channel 36 (5180 MHz), width: 80 MHz" {
		t.Errorf("got %q", got)
	}
	if got := channelLine("Interface ap0\n"); got != "" {
		t.Errorf("no channel line should give \"\", got %q", got)
	}
	if got := lastLines("a\n\nb\nc\n", 2); len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("got %q", got)
	}
}
