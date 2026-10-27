package render

import (
	"strings"
	"testing"
)

func TestFirewall_PriorityDevices(t *testing.T) {
	in := FirewallInput{
		Egress: []string{"wlan1"}, Guest: true,
		QoS: &QoSRule{PriorityMACs: []string{"3A:1F:22:AA:BB:CC", "3a:1f:22:aa:bb:dd"}, Upload: true, Download: true},
	}
	got, err := Firewall(in)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "firewall_qos", got)
	for _, want := range []string{
		"type filter hook forward priority mangle;",
		"iifname @lans ether saddr { 3a:1f:22:aa:bb:cc, 3a:1f:22:aa:bb:dd } ct label set 64",
		`iifname @lans oifname != @lans oifname != "rbplug0" meta priority set 1:20`,
		`iifname @lans oifname != @lans oifname != "rbplug0" ct label 64 meta priority set 1:10`,
		`iifname != @lans iifname != "rbplug0" oifname @lans meta priority set 1:20`,
		`iifname != @lans iifname != "rbplug0" oifname @lans ct label 64 meta priority set 1:10`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q", want)
		}
	}
	// The priority class must win: it comes after the normal one.
	if strings.Index(got, "@lans ct label 64 meta priority set 1:10") < strings.Index(got, "oifname @lans meta priority set 1:20") {
		t.Error("the priority class must be set after the normal one")
	}

	// Only upload shaped, no priority devices yet: only the normal class,
	// only for upload.
	upload := in
	upload.QoS = &QoSRule{Upload: true}
	got, err = Firewall(upload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "meta priority set 1:20") || strings.Contains(got, "ct label") || strings.Contains(got, "oifname @lans meta priority") {
		t.Errorf("upload only, no devices:\n%s", got)
	}

	// Nothing shaped, or setup mode: no chain.
	for name, f := range map[string]FirewallInput{
		"no direction": {Egress: []string{"wlan1"}, QoS: &QoSRule{PriorityMACs: []string{"3a:1f:22:aa:bb:cc"}}},
		"setup mode":   {QoS: &QoSRule{Upload: true}},
	} {
		got, err := Firewall(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "chain qos") {
			t.Errorf("%s: no qos chain expected", name)
		}
	}

	bad := in
	bad.QoS = &QoSRule{PriorityMACs: []string{"nope"}, Upload: true}
	if _, err := Firewall(bad); err == nil {
		t.Error("a wrong MAC address should be refused")
	}
}

// lines shows commands one per line, as they'd be typed.
func lines(commands []Command) string {
	var b strings.Builder
	for _, c := range commands {
		b.WriteString(strings.Join(c.Args, " "))
		if c.Optional {
			b.WriteString("   # may fail")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestQoS_Commands(t *testing.T) {
	in := QoSInput{Upload: []string{"wg0"}, UploadKbps: 20_000, LANs: []string{"br-lan", "br-guest"}, DownloadKbps: 100_000}
	if !in.Active() {
		t.Fatal("should be active")
	}
	got := lines(QoS(in)) + "# removal\n" + lines(QoSRemove(in))
	checkGolden(t, "qos_commands", got)
	for _, want := range []string{
		// 90% of 20 Mb/s up, three quarters of it for priority devices.
		"tc qdisc replace dev wg0 root handle 1: htb default 30",
		"tc class replace dev wg0 parent 1: classid 1:1 htb rate 18000kbit ceil 18000kbit",
		"tc class replace dev wg0 parent 1:1 classid 1:10 htb rate 13500kbit ceil 18000kbit prio 0",
		"tc class replace dev wg0 parent 1:1 classid 1:20 htb rate 4500kbit ceil 18000kbit prio 1",
		// Downloads: both bridges into one ifb at 90 Mb/s.
		"ip link add rbifb0 type ifb",
		"tc class replace dev rbifb0 parent 1: classid 1:1 htb rate 90000kbit ceil 90000kbit",
		"tc filter add dev br-guest egress protocol all pref 1 u32 match u32 0 0 action mirred egress redirect dev rbifb0",
		"tc qdisc del dev br-lan clsact   # may fail",
		"tc qdisc add dev br-lan clsact",
		"ip link del rbifb0   # may fail",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("no %q in\n%s", want, got)
		}
	}

	// One direction only.
	up := QoSInput{Upload: []string{"eth0"}, UploadKbps: 5_000, LANs: []string{"br-lan"}}
	if got := lines(QoS(up)); strings.Contains(got, "rbifb0") || !strings.Contains(got, "dev eth0") {
		t.Errorf("upload only:\n%s", got)
	}
	if got := lines(QoSRemove(up)); strings.Contains(got, "rbifb0") {
		t.Errorf("upload only removal:\n%s", got)
	}
	down := QoSInput{UploadKbps: 5_000, LANs: []string{"br-lan"}, DownloadKbps: 5_000}
	if got := lines(QoS(down)); strings.Contains(got, "root handle 1: htb default 30\ntc class replace dev br-lan") || !strings.Contains(got, "dev rbifb0") {
		t.Errorf("download only (no upload interface):\n%s", got)
	}
	if (QoSInput{Upload: []string{"eth0"}, LANs: []string{"br-lan"}}).Active() {
		t.Error("no speeds: nothing to shape")
	}
	if len(QoS(QoSInput{})) != 0 || len(QoSRemove(QoSInput{})) != 0 {
		t.Error("nothing to do for nothing")
	}
	if ShapedKbps(1) != 0 || !strings.Contains(lines(htb("x", 0)), "rate 1kbit") {
		t.Error("tiny rates must stay valid for tc")
	}
	if s := (Command{Args: []string{"ip", "link"}, Optional: true}).String(); s != "[ip link] (optional)" {
		t.Errorf("String() = %q", s)
	}
}
