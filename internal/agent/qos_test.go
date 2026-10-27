package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
	"rasbeacon/internal/stations"
)

// withIfindex gives fake interfaces index numbers, as the kernel does.
func (e *testEnv) withIfindex(t *testing.T, numbers map[string]int) {
	t.Helper()
	for dev, n := range numbers {
		mustWrite(t, filepath.Join(e.paths.SysClassNet(), dev, "ifindex"), strconv.Itoa(n)+"\n")
	}
}

// newQoSEnv is a set-up RasBeacon whose uplink and hotspot bridge exist.
func newQoSEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	e := newClaimedEnv(t, false)
	uplinks := e.agent.egressInterfaces()
	if len(uplinks) == 0 {
		t.Fatal("test setup: no egress interface")
	}
	e.withIfindex(t, map[string]int{render.BridgeName: 3, uplinks[0]: 4})
	return e, uplinks[0]
}

// A priority device with speeds set: the firewall sorts its traffic and
// the queues are set up on the uplink and for downloads.
func TestQoS_PriorityDeviceAndSpeeds(t *testing.T) {
	e, uplink := newQoSEnv(t)

	// Speeds alone shape nothing: there is no priority device yet.
	settings, err := e.agent.UpdateQoSSettings(api.QoSSettings{UploadKbps: 20_000, DownloadKbps: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Active || e.runner.ran("htb") {
		t.Error("nothing should be shaped without a priority device")
	}

	if err := e.agent.SetClientPriority("3A:1F:22:AA:BB:CC", true); err != nil {
		t.Fatal(err)
	}
	rules := e.readFile(e.paths.FirewallRules())
	for _, want := range []string{"ether saddr { 3a:1f:22:aa:bb:cc } ct label set 64", "meta priority set 1:10", "oifname @lans meta priority set 1:20"} {
		if !strings.Contains(rules, want) {
			t.Errorf("firewall has no %q", want)
		}
	}
	for _, want := range []string{
		"tc qdisc replace dev " + uplink + " root handle 1: htb default 30",
		"tc class replace dev " + uplink + " parent 1: classid 1:1 htb rate 18000kbit ceil 18000kbit",
		"ip link add rbifb0 type ifb",
		"tc qdisc add dev br-lan clsact",
	} {
		if !e.runner.ran(want) {
			t.Errorf("not run: %q", want)
		}
	}
	got := e.agent.QoSSettings()
	if !got.Active || got.PriorityDevices != 1 || got.UploadKbps != 20_000 || got.DownloadKbps != 100_000 || got.Problem != "" {
		t.Errorf("settings: %+v", got)
	}

	// Nothing changed: the queues aren't set up again.
	before := e.runner.count("htb default 30")
	e.agent.refreshQoS()
	if e.runner.count("htb default 30") != before {
		t.Error("unchanged settings must not set the queues up again")
	}
	// The uplink was made again (a new index number): its queue is gone, so
	// it is set up again.
	e.withIfindex(t, map[string]int{uplink: 9})
	e.agent.refreshQoS()
	if e.runner.count("dev "+uplink+" root handle 1: htb") != 2 {
		t.Error("a re-made interface must get its queue again")
	}

	// The last priority device off: the queues and the rules go away.
	if err := e.agent.SetClientPriority("3a:1f:22:aa:bb:cc", false); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("tc qdisc del dev "+uplink+" root") || !e.runner.ran("ip link del rbifb0") {
		t.Error("the queues should be removed")
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "chain qos") {
		t.Error("the firewall should no longer sort traffic")
	}
	if e.agent.QoSSettings().Active {
		t.Error("should be off")
	}
	// Turning off a device that isn't one is fine.
	if err := e.agent.SetClientPriority("3a:1f:22:aa:bb:cc", false); err != nil {
		t.Error(err)
	}
}

// The device's Clients page entry says it is a priority device.
func TestQoS_ShownOnTheDevice(t *testing.T) {
	e, _ := newQoSEnv(t)
	if err := e.agent.SetClientPriority("3a:1f:22:aa:bb:cc", true); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	for _, mac := range []string{"3a:1f:22:aa:bb:cc", "3a:1f:22:aa:bb:dd"} {
		c := stations.Station{MAC: mac}
		e.agent.setUsage(&c)
		if c.Priority != (mac == "3a:1f:22:aa:bb:cc") {
			t.Errorf("%s: priority %v", mac, c.Priority)
		}
	}
}

// At most 16 priority devices; speeds must make sense; nothing before setup.
func TestQoS_Rules(t *testing.T) {
	e, _ := newQoSEnv(t)
	for i := range config.MaxPriorityDevices {
		if err := e.agent.SetClientPriority(fmt.Sprintf("3a:1f:22:aa:bb:%02x", 0x10+i), true); err != nil {
			t.Fatal(err)
		}
	}
	var verr *config.ValidationError
	err := e.agent.SetClientPriority("3a:1f:22:aa:bb:ee", true)
	if !errors.As(err, &verr) || verr.Field != "priority" || !strings.Contains(verr.Message, "At most 16") {
		t.Errorf("a 17th: %v", err)
	}
	if err := e.agent.SetClientPriority("nope", true); err == nil {
		t.Error("a broken MAC address must be refused")
	}
	for field, req := range map[string]api.QoSSettings{
		"uploadKbps":   {UploadKbps: 999},
		"downloadKbps": {DownloadKbps: config.MaxSpeedKbps + 1},
	} {
		_, err := e.agent.UpdateQoSSettings(req)
		if !errors.As(err, &verr) || verr.Field != field {
			t.Errorf("%s: %v", field, err)
		}
	}

	setup := newTestEnv(t)
	if err := setup.agent.SetClientPriority("3a:1f:22:aa:bb:cc", true); !errors.Is(err, ErrWrongMode) {
		t.Errorf("before setup: %v", err)
	}
	if _, err := setup.agent.UpdateQoSSettings(api.QoSSettings{}); !errors.Is(err, ErrWrongMode) {
		t.Errorf("before setup: %v", err)
	}
}

// When tc refuses, the half-made queues are removed and the Internet page
// says why priority doesn't work. A missing fq_codel is fine.
func TestQoS_QueuesCantBeSetUp(t *testing.T) {
	e, _ := newQoSEnv(t)
	e.runner.failing = []string{"ip link add rbifb0"}
	if _, err := e.agent.UpdateQoSSettings(api.QoSSettings{DownloadKbps: 50_000}); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.SetClientPriority("3a:1f:22:aa:bb:cc", true); err != nil {
		t.Fatal(err)
	}
	got := e.agent.QoSSettings()
	if got.Active || !strings.Contains(got.Problem, "couldn't set up its queues") {
		t.Errorf("settings: %+v", got)
	}
	if !e.runner.ran("tc qdisc del dev br-lan clsact") {
		t.Error("what was set up should be removed")
	}

	e2, _ := newQoSEnv(t)
	e2.runner.failing = []string{"tc qdisc replace dev rbifb0 parent 1:10 fq_codel"}
	if _, err := e2.agent.UpdateQoSSettings(api.QoSSettings{DownloadKbps: 50_000}); err != nil {
		t.Fatal(err)
	}
	if err := e2.agent.SetClientPriority("3a:1f:22:aa:bb:cc", true); err != nil {
		t.Fatal(err)
	}
	if got := e2.agent.QoSSettings(); !got.Active || got.Problem != "" {
		t.Errorf("without fq_codel: %+v", got)
	}
}
