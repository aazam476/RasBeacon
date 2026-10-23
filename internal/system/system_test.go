package system

import (
	"strings"
	"testing"
	"time"
)

// recorder is a Runner that records each command line.
type recorder struct{ lines []string }

func (r *recorder) Run(name string, args ...string) (string, error) {
	r.lines = append(r.lines, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	return "", nil
}

func TestHelpers_RunTheRightCommands(t *testing.T) {
	tests := []struct {
		name string
		call func(r Runner) error
		want []string
	}{
		{"restart a service", func(r Runner) error { return ServiceRestart(r, "x.service") }, []string{"systemctl restart x.service"}},
		{"stop a service", func(r Runner) error { return ServiceStop(r, "x.service") }, []string{"systemctl stop x.service"}},
		{"apply the firewall", func(r Runner) error { return ApplyFirewall(r, "/run/fw.nft") }, []string{"nft -f /run/fw.nft"}},
		{"rename a link", func(r Runner) error { return RenameLink(r, "wlan1", "ap0") }, []string{
			"ip link set dev wlan1 down", "ip link set dev wlan1 name ap0"}},
		{"set the country", func(r Runner) error { return SetRegulatoryCountry(r, "DE") }, []string{"iw reg set DE"}},
		{"bring a link up", func(r Runner) error { return SetLinkUp(r, "ap0") }, []string{"ip link set dev ap0 up"}},
		{"add to a bridge", func(r Runner) error { return AddToBridge(r, "ap0", "br-lan") }, []string{"ip link set dev ap0 master br-lan"}},
		{"remove from a bridge", func(r Runner) error { return RemoveFromBridge(r, "ap0") }, []string{"ip link set dev ap0 nomaster"}},
		{"flush addresses", func(r Runner) error { return FlushAddresses(r, "-6", "eth0") }, []string{"ip -6 address flush dev eth0"}},
		{"reload networkd", ReloadNetworkd, []string{"networkctl reload"}},
		{"ask wpa_supplicant", func(r Runner) error {
			_, err := WPACommand(r, "/run/wpa", "wlan0", "status")
			return err
		}, []string{"wpa_cli -p /run/wpa -i wlan0 status"}},
		{"read a radio", func(r Runner) error { _, err := PhyInfo(r, "phy0"); return err }, []string{"iw phy phy0 info"}},
		{"list stations", func(r Runner) error { _, err := StationDump(r, "ap0"); return err }, []string{"iw dev ap0 station dump"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{}
			if err := tc.call(r); err != nil {
				t.Fatal(err)
			}
			if strings.Join(r.lines, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("ran %q, want %q", r.lines, tc.want)
			}
		})
	}
}

func TestEnsureBridge_AddsItsAddressesAndBringsItUp(t *testing.T) {
	r := &recorder{}
	if err := EnsureBridge(r, "br-lan", "10.87.0.1/24", "fd87::1/64"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(r.lines, "\n")
	for _, want := range []string{"ip address replace 10.87.0.1/24 dev br-lan", "ip address replace fd87::1/64 dev br-lan", "ip link set dev br-lan up"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestExecRunner(t *testing.T) {
	r := ExecRunner{}
	if out, err := r.Run("sh", "-c", "echo hello"); err != nil || out != "hello\n" {
		t.Errorf("output = %q, %v", out, err)
	}
	// A failure says what the command printed on standard error.
	if _, err := r.Run("sh", "-c", "echo 'no such device' >&2; exit 1"); err == nil || !strings.Contains(err.Error(), "no such device") {
		t.Errorf("err = %v", err)
	}
	// A command that hangs is stopped.
	start := time.Now()
	if _, err := (ExecRunner{Timeout: 100 * time.Millisecond}).Run("sleep", "5"); err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("err = %v after %v, want it stopped", err, time.Since(start))
	}
}
