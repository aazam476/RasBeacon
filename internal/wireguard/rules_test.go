package wireguard

import (
	"strings"
	"testing"
)

// conf is a working WireGuard file; each case below breaks one thing.
const conf = `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.66.0.2/32
DNS = 10.66.0.1

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
`

func TestParseConf_EachRule(t *testing.T) {
	if _, err := ParseConf(conf); err != nil {
		t.Fatalf("the working file: %v", err)
	}
	replace := func(old, new string) string { return strings.Replace(conf, old, new, 1) }
	for want, text := range map[string]string{
		"unknown section":                  replace("[Peer]", "[Server]"),
		`expected "Name = value"`:          replace("DNS = 10.66.0.1", "DNS 10.66.0.1"),
		"setting outside [Interface]":      "MTU = 1400\n" + conf,
		"appears twice":                    replace("Endpoint", "PersistentKeepalive = 25\nPersistentKeepalive = 30\nEndpoint"),
		"needs a PrivateKey":               replace("PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=\n", ""),
		"needs a [Peer] section":           conf[:strings.Index(conf, "[Peer]")],
		"needs an Address":                 replace("Address = 10.66.0.2/32\n", ""),
		"MTU must be between":              replace("DNS = 10.66.0.1", "DNS = 10.66.0.1\nMTU = 9000"),
		"PersistentKeepalive must be":      replace("AllowedIPs", "PersistentKeepalive = 99999\nAllowedIPs"),
		"needs the server's PublicKey":     replace("PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\n", ""),
		"Endpoint must look like":          replace("vpn.example.com:51820", "vpn.example.com"),
		"port must be a number":            replace("vpn.example.com:51820", "vpn.example.com:99999"),
		"characters that are not allowed":  replace("vpn.example.com:51820", "vpn_ex ample.com:51820"),
		"not an address with network size": replace("10.66.0.2/32", "10.66.0.2/99"),
	} {
		_, err := ParseConf(text)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
}
