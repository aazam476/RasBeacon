package config

import (
	"net/netip"
	"reflect"
	"testing"
	"time"
)

// Copies (Clone) share nothing with the original, and older settings files
// are read the way they were meant (Ordered, MoveVPNIntoProfile).

func TestConfigClone_SharesNothing(t *testing.T) {
	size := 2
	original := Config{
		Uplink: UplinkConfig{Networks: []SavedNetwork{{ID: "n1", SSID: "Cafe", Enterprise: &EnterpriseConfig{Identity: "me"}}}},
		VPN: VPNConfig{Profiles: []VPNProfile{{
			Name:      "home.conf",
			Addresses: []netip.Prefix{netip.MustParsePrefix("10.66.0.2/32")},
			DNS:       []netip.Addr{netip.MustParseAddr("10.66.0.1")},
			Peer:      VPNPeer{AllowedIPs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}},
		}}},
		Clients: ClientsConfig{
			Paused: map[string]time.Time{"3a:1f:22:aa:bb:cc": {}},
			Names:  map[string]string{"3a:1f:22:aa:bb:cc": "Phone"},
		},
		DNS: DNSConfig{Servers: []netip.Addr{netip.MustParseAddr("1.1.1.1")}, DoH: []string{"https://dns.example/dns-query"}},
		Plugins: PluginsConfig{
			Sources: []string{"example/plugins"},
			Installed: map[string]InstalledPlugin{"notes": {
				Ports:    map[string]int{"tcp/3000": 3000},
				Settings: map[string]string{"title": "Notes"},
				Fetch:    &PluginFetch{URL: "https://example.com/notes.rba"},
			}},
		},
		Swap: SwapConfig{SizeGB: &size},
	}
	want := original.Clone()
	changed := original.Clone()

	changed.Uplink.Networks[0].Enterprise.Identity = "someone else"
	changed.VPN.Profiles[0].Addresses[0] = netip.MustParsePrefix("10.0.0.9/32")
	changed.VPN.Profiles[0].DNS[0] = netip.MustParseAddr("9.9.9.9")
	changed.VPN.Profiles[0].Peer.AllowedIPs[0] = netip.MustParsePrefix("10.0.0.0/8")
	changed.Clients.Paused["new"] = time.Time{}
	changed.Clients.Names["3a:1f:22:aa:bb:cc"] = "Laptop"
	changed.DNS.Servers[0] = netip.MustParseAddr("8.8.8.8")
	changed.DNS.DoH[0] = "https://other.example/dns-query"
	changed.Plugins.Sources[0] = "other/plugins"
	notes := changed.Plugins.Installed["notes"]
	notes.Ports["tcp/3000"] = 8080
	notes.Settings["title"] = "Changed"
	notes.Fetch.URL = "https://example.com/other.rba"
	*changed.Swap.SizeGB = 0

	if !reflect.DeepEqual(original, want) {
		t.Errorf("changing the copy changed the original:\n got %+v\nwant %+v", original, want)
	}
}

func TestSecretsClone_SharesNothing(t *testing.T) {
	original := Secrets{
		UplinkPasswords: map[string]string{"n1": "cafe-pass"},
		WireGuardKeys:   map[string]WireGuardKeys{"home.conf": {PrivateKey: "key"}},
		Plugins:         map[string]map[string]string{"notes": {"password": "secret"}},
	}
	changed := original.Clone()
	changed.UplinkPasswords["n1"] = "changed"
	changed.WireGuardKeys["home.conf"] = WireGuardKeys{PrivateKey: "changed"}
	changed.Plugins["notes"]["password"] = "changed"

	if original.UplinkPasswords["n1"] != "cafe-pass" ||
		original.WireGuardKeys["home.conf"].PrivateKey != "key" ||
		original.Plugins["notes"]["password"] != "secret" {
		t.Errorf("changing the copy changed the original: %+v", original)
	}
}

func TestDNSConfig_Ordered(t *testing.T) {
	plain := []netip.Addr{netip.MustParseAddr("9.9.9.9"), netip.MustParseAddr("1.1.1.1")}
	doh := []string{"https://dns.example/dns-query"}

	// Saved before v0.2.0: no order, so the plain servers come first.
	old := DNSConfig{Servers: plain, DoH: doh}
	if got, want := old.Ordered(), []string{"9.9.9.9", "1.1.1.1", "https://dns.example/dns-query"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered() without an order = %v, want %v", got, want)
	}

	// Saved since: the order as typed.
	typed := DNSConfig{Servers: plain, DoH: doh, Order: []string{"https://dns.example/dns-query", "1.1.1.1", "9.9.9.9"}}
	if got := typed.Ordered(); !reflect.DeepEqual(got, typed.Order) {
		t.Errorf("Ordered() = %v, want %v", got, typed.Order)
	}
}

func TestVPNConfig_Profile(t *testing.T) {
	v := VPNConfig{Profiles: []VPNProfile{{Name: "home.conf"}, {Name: "work.conf"}}}
	if p, ok := v.Profile("work.conf"); !ok || p.Name != "work.conf" {
		t.Errorf("Profile(work.conf) = %+v, %v", p, ok)
	}
	if _, ok := v.Profile("missing.conf"); ok {
		t.Error("Profile(missing.conf) found a profile")
	}
}

func TestMoveVPNIntoProfile(t *testing.T) {
	t.Run("a version 2 server becomes the profile in use, with its keys", func(t *testing.T) {
		raw := []byte(`{"vpn": {"addresses": ["10.66.0.2/32"], "peer": {"publicKey": "server-key", "endpoint": "vpn.example.com:51820"}}}`)
		var c Config
		s := Secrets{WireGuardPrivateKey: "private", WireGuardPresharedKey: "shared"}
		if err := MoveVPNIntoProfile(raw, &c, &s); err != nil {
			t.Fatal(err)
		}
		if c.VPN.Active != "wireguard.conf" || len(c.VPN.Profiles) != 1 {
			t.Fatalf("VPN = %+v, want one profile wireguard.conf in use", c.VPN)
		}
		if p := c.VPN.Profiles[0]; p.Peer.PublicKey != "server-key" || p.Peer.Endpoint != "vpn.example.com:51820" {
			t.Errorf("profile = %+v, want the old server", p)
		}
		if got := s.WireGuardKeys["wireguard.conf"]; got != (WireGuardKeys{PrivateKey: "private", PresharedKey: "shared"}) {
			t.Errorf("keys = %+v", got)
		}
		if s.WireGuardPrivateKey != "" || s.WireGuardPresharedKey != "" {
			t.Error("the old key fields should be emptied")
		}
	})

	t.Run("nothing set up stays nothing", func(t *testing.T) {
		var c Config
		var s Secrets
		if err := MoveVPNIntoProfile([]byte(`{"vpn": {}}`), &c, &s); err != nil {
			t.Fatal(err)
		}
		if len(c.VPN.Profiles) != 0 || s.WireGuardKeys != nil {
			t.Errorf("VPN = %+v, keys = %+v, want none", c.VPN, s.WireGuardKeys)
		}
	})

	t.Run("a broken file is an error", func(t *testing.T) {
		var c Config
		var s Secrets
		if err := MoveVPNIntoProfile([]byte(`{`), &c, &s); err == nil {
			t.Error("want an error")
		}
	})
}
