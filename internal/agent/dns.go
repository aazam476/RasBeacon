package agent

// The DNS servers devices use while their traffic isn't going through the
// VPN (the Internet page's "DNS servers"): plain DNS servers, and
// DNS-over-HTTPS servers, which dnsmasq asks through RasBeacon's DoH
// forwarder (cmd/rbdoh). See upstreamDNS in vpn.go for when they apply.

import (
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/portal"
	"rasbeacon/internal/system"
)

const (
	dohService = "rasbeacon-doh.service"
	// dohForwarder is where the DoH forwarder answers dnsmasq.
	dohForwarder = "127.0.0.1#5053"
)

// DNSSettings returns the owner's DNS servers ([] for the network's): the
// plain ones, then the DoH ones.
func (a *Agent) DNSSettings() api.DNSSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return api.DNSSettings{Servers: append(addrStrings(a.cfg.DNS.Servers), a.cfg.DNS.DoH...)}
}

// UpdateDNSSettings saves the DNS servers and tells dnsmasq at once. Each
// is an IP address (plain DNS) or a DNS-over-HTTPS address
// (https://dns.example/dns-query, or just its host name).
func (a *Agent) UpdateDNSSettings(req api.DNSSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	newCfg.DNS = config.DNSConfig{}
	for _, item := range req.Servers {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if addr, err := netip.ParseAddr(item); err == nil {
			newCfg.DNS.Servers = append(newCfg.DNS.Servers, addr)
			continue
		}
		doh, err := config.ParseDoH(item)
		if err != nil {
			return &config.ValidationError{Field: "servers", Message: fmt.Sprintf(
				"%q is not an IP address or a DNS-over-HTTPS address like https://dns.example/dns-query.", item)}
		}
		newCfg.DNS.DoH = append(newCfg.DNS.DoH, doh)
	}
	if err := newCfg.DNS.Validate(); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("DNS servers saved", "servers", addrStrings(newCfg.DNS.Servers), "doh", newCfg.DNS.DoH)
	return a.applyDHCP(false)
}

// ownDNSInUse reports whether the owner's DNS servers apply now: some are
// chosen, the VPN isn't carrying devices' traffic (it brings its own), and
// no login page is in the way (many only work with the network's DNS).
func (a *Agent) ownDNSInUse() bool {
	d := a.cfg.DNS
	return len(d.Servers)+len(d.DoH) > 0 && !a.clientsThroughTunnel() &&
		!a.portal.loginMode && a.portal.result.State != portal.Portal
}

// applyDoH starts the DoH forwarder with the owner's DoH servers while they
// are in use, restarts it when they change, and stops it otherwise. The
// forwarder looks up the DoH servers' names with the owner's plain DNS
// servers first, then the network's (RasBeacon's resolver).
func (a *Agent) applyDoH(captive bool) error {
	want := ""
	if !captive && len(a.cfg.DNS.DoH) > 0 && a.ownDNSInUse() {
		bootstrap := append(addrStrings(a.cfg.DNS.Servers), deviceResolver)
		want = "RBDOH_SERVERS=" + strings.Join(a.cfg.DNS.DoH, " ") + "\n" +
			"RBDOH_BOOTSTRAP=" + strings.Join(bootstrap, " ") + "\n"
	}
	if want == a.dohEnv {
		return nil
	}
	if want == "" {
		a.dohEnv = ""
		_ = os.Remove(a.paths.DoHEnv())
		return system.ServiceStop(a.runner, dohService)
	}
	if err := config.WriteFileAtomic(a.paths.DoHEnv(), []byte(want), 0o644); err != nil {
		return err
	}
	if err := system.ServiceRestart(a.runner, dohService); err != nil {
		return err
	}
	a.dohEnv = want
	return nil
}
