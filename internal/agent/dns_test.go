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
	for _, bad := range [][]string{{"not a server"}, {"http://dns.google/dns-query"}, {"https://user:pw@dns.example/q"}, {"127.0.0.1"}, {"0.0.0.0"}, {"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9"}} {
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

func TestDNS_DoHServersGoThroughTheForwarder(t *testing.T) {
	e := newClaimedEnv(t, false)
	dnsmasq := func() string { return e.readFile(e.paths.DnsmasqConfig()) }
	e.runner.commands = nil
	err := e.agent.UpdateDNSSettings(api.DNSSettings{Servers: []string{"9.9.9.9", "dns.google", "https://Cloudflare-DNS.com/dns-query"}})
	if err != nil {
		t.Fatal(err)
	}
	// Plain servers stay with dnsmasq; DoH ones go through the forwarder.
	if got := dnsmasq(); !strings.Contains(got, "server=9.9.9.9\nserver=127.0.0.1#5053") || strings.Contains(got, "server=127.0.0.53") {
		t.Errorf("dnsmasq:\n%s", got)
	}
	// The DoH servers' names are looked up with the owner's plain server,
	// then the network's.
	want := "RBDOH_SERVERS=https://dns.google/dns-query https://cloudflare-dns.com/dns-query\n" +
		"RBDOH_BOOTSTRAP=9.9.9.9 127.0.0.53\n"
	if got := e.readFile(e.paths.DoHEnv()); got != want {
		t.Errorf("doh.env = %q", got)
	}
	if !e.runner.ran("systemctl restart rasbeacon-doh.service") {
		t.Errorf("the forwarder should start: %v", e.runner.commands)
	}
	s := e.agent.DNSSettings()
	if strings.Join(s.Servers, " ") != "9.9.9.9 https://dns.google/dns-query https://cloudflare-dns.com/dns-query" {
		t.Errorf("settings = %+v", s)
	}

	// Nothing changed: no restart.
	e.runner.commands = nil
	e.agent.mu.Lock()
	_ = e.agent.applyDHCP(false)
	e.agent.mu.Unlock()
	if e.runner.ran("rasbeacon-doh.service") {
		t.Errorf("the forwarder was touched without a change: %v", e.runner.commands)
	}

	// While a login page is in the way, the forwarder stops.
	e.agent.mu.Lock()
	e.agent.portal.result = portal.Result{State: portal.Portal}
	_ = e.agent.applyDHCP(false)
	e.agent.mu.Unlock()
	if !e.runner.ran("systemctl stop rasbeacon-doh.service") || strings.Contains(dnsmasq(), "#5053") {
		t.Errorf("login pages need the network's DNS: %v", e.runner.commands)
	}
	e.agent.mu.Lock()
	e.agent.portal.result = portal.Result{State: portal.Open}
	e.runner.commands = nil
	_ = e.agent.applyDHCP(false)
	e.agent.mu.Unlock()
	if !e.runner.ran("systemctl restart rasbeacon-doh.service") {
		t.Errorf("the forwarder should start again: %v", e.runner.commands)
	}

	// DoH only: the network's resolver looks the names up.
	if err := e.agent.UpdateDNSSettings(api.DNSSettings{Servers: []string{"https://1.1.1.1/dns-query"}}); err != nil {
		t.Fatal(err)
	}
	if got := dnsmasq(); !strings.Contains(got, "server=127.0.0.1#5053") || strings.Contains(got, "server=9.9.9.9") {
		t.Errorf("dnsmasq:\n%s", got)
	}
	if got := e.readFile(e.paths.DoHEnv()); !strings.Contains(got, "RBDOH_BOOTSTRAP=127.0.0.53\n") {
		t.Errorf("doh.env = %q", got)
	}

	// Back to the network's: the forwarder stops.
	e.runner.commands = nil
	if err := e.agent.UpdateDNSSettings(api.DNSSettings{}); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("systemctl stop rasbeacon-doh.service") {
		t.Errorf("the forwarder should stop: %v", e.runner.commands)
	}
}

func TestDNS_TheForwarderStopsWhileTheVPNIsInUse(t *testing.T) {
	e, _ := newVPNEnv(t, true)
	if err := e.agent.UpdateDNSSettings(api.DNSSettings{Servers: []string{"dns.google"}}); err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(e.paths.DnsmasqConfig()); strings.Contains(got, "#5053") {
		t.Errorf("with the VPN in use, its DNS servers are used:\n%s", got)
	}
	if e.runner.ran("systemctl restart rasbeacon-doh.service") {
		t.Error("the forwarder isn't needed while the VPN carries the devices' traffic")
	}
}
