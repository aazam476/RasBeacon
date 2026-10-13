package agent

import (
	"errors"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/portal"
)

func TestDNS_OwnServersWhileTheVPNIsntInUse(t *testing.T) {
	e := newClaimedEnv(t, false)
	dnsmasq := func() string { return e.readFile(e.paths.DnsmasqConfig()) }
	if !strings.Contains(dnsmasq(), "server=127.0.0.53") {
		t.Fatal("by default devices use the network's DNS (through RasBeacon's resolver)")
	}

	if err := e.agent.UpdateDNSSettings(api.DNSSettings{Servers: []string{"1.1.1.1", " 2606:4700:4700::1111 "}}); err != nil {
		t.Fatal(err)
	}
	if got := dnsmasq(); !strings.Contains(got, "server=1.1.1.1\nserver=2606:4700:4700::1111") || strings.Contains(got, "server=127.0.0.53") {
		t.Errorf("dnsmasq should ask the owner's servers:\n%s", got)
	}
	if s := e.agent.DNSSettings(); len(s.Servers) != 2 || s.Servers[0] != "1.1.1.1" {
		t.Errorf("settings = %+v", s)
	}

	// While a login page is in the way, the network's DNS is used.
	e.agent.mu.Lock()
	e.agent.portal.result = portal.Result{State: portal.Portal}
	_ = e.agent.applyDHCP(false)
	e.agent.mu.Unlock()
	if !strings.Contains(dnsmasq(), "server=127.0.0.53") {
		t.Error("login pages need the network's DNS")
	}
	e.agent.mu.Lock()
	e.agent.portal.result = portal.Result{State: portal.Open}
	e.agent.mu.Unlock()

	// Mistakes, and back to the network's.
	var ve *config.ValidationError
	for _, bad := range [][]string{{"dns.google"}, {"127.0.0.1"}, {"0.0.0.0"}, {"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9"}} {
		if err := e.agent.UpdateDNSSettings(api.DNSSettings{Servers: bad}); !errors.As(err, &ve) || ve.Field != "servers" {
			t.Errorf("%v: got %v", bad, err)
		}
	}
	if err := e.agent.UpdateDNSSettings(api.DNSSettings{Servers: []string{}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dnsmasq(), "server=127.0.0.53") {
		t.Error("an empty list goes back to the network's DNS")
	}
}

func TestDNS_TheVPNsServersWinWhileItIsInUse(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	if err := e.agent.UpdateDNSSettings(api.DNSSettings{Servers: []string{"1.1.1.1"}}); err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(e.paths.DnsmasqConfig()); !strings.Contains(got, "server=10.66.0.1") || strings.Contains(got, "server=1.1.1.1") {
		t.Errorf("with the VPN in use, its DNS servers are used:\n%s", got)
	}
}
