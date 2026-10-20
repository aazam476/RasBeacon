package agent

import (
	"os"
	"strings"
	"testing"
)

func TestSetHostname_SavesAppliesAndRefusesNamesInUse(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	if !e.runner.ran("hostname rasbeacon") {
		t.Error("the default name was not set at start")
	}

	for _, bad := range []string{"", "-pi", "my pi", "pi.lan", strings.Repeat("a", 64)} {
		if err := e.agent.SetHostname(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	lease := "1999999999 aa:bb:cc:dd:ee:ff 10.87.0.50 laptop *\n"
	if err := os.WriteFile(e.paths.DnsmasqLeases(), []byte(lease), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.SetHostname("Laptop"); err == nil {
		t.Error("a name a device uses was accepted")
	}

	if err := e.agent.SetHostname("Travel-Pi"); err != nil {
		t.Fatal(err)
	}
	if got := e.agent.SetupState().Hostname; got != "travel-pi.lan" {
		t.Errorf("hostname = %q", got)
	}
	e.timers.runAll()
	if !e.runner.ran("systemctl reboot") {
		t.Error("RasBeacon did not restart")
	}
}
