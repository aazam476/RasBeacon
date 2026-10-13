package agent

// The DNS servers devices use while their traffic isn't going through the
// VPN (the Internet page's "DNS servers"). See upstreamDNS in vpn.go for
// when they apply.

import (
	"log/slog"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// DNSSettings returns the owner's DNS servers ([] for the network's).
func (a *Agent) DNSSettings() api.DNSSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return api.DNSSettings{Servers: addrStrings(a.cfg.DNS.Servers)}
}

// UpdateDNSSettings saves the DNS servers and tells dnsmasq at once.
func (a *Agent) UpdateDNSSettings(req api.DNSSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	servers, err := parseAddrs(req.Servers, "servers")
	if err != nil {
		return err
	}
	newCfg := a.cfg.Clone()
	newCfg.DNS.Servers = servers
	if err := newCfg.DNS.Validate(); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("DNS servers saved", "servers", addrStrings(servers))
	return a.applyDHCP(false)
}
