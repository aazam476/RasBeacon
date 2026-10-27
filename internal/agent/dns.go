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
	"os/user"
	"strconv"
	"strings"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/portal"
	"rasbeacon/internal/render"
	"rasbeacon/internal/system"
)

// dohUser is the DoH forwarder's own user (image/layer/rasbeacon.yaml), so
// its traffic can be routed through the VPN (routing.ProgramThroughTunnel).
const dohUser = "rbdoh"

// dohUID returns the DoH forwarder's user ID, if that user exists.
func dohUID() (int, bool) {
	u, err := user.Lookup(dohUser)
	if err != nil {
		return 0, false
	}
	uid, err := strconv.Atoi(u.Uid)
	return uid, err == nil
}

const (
	dohService = "rasbeacon-doh.service"
	// dohForwarder is where the DoH forwarder answers dnsmasq.
	dohForwarder = "127.0.0.1#5053"
)

// DNSSettings returns the owner's DNS servers ([] for the network's), in
// the order they were listed.
func (a *Agent) DNSSettings() api.DNSSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	// An empty list, not null, when there are none: the web UI joins it.
	servers := append([]string{}, a.cfg.DNS.Ordered()...)
	return api.DNSSettings{Servers: servers, WithVPN: a.cfg.DNS.WithVPN}
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
	newCfg.DNS = config.DNSConfig{WithVPN: req.WithVPN}
	for _, item := range req.Servers {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if addr, err := netip.ParseAddr(item); err == nil {
			newCfg.DNS.Servers = append(newCfg.DNS.Servers, addr)
			newCfg.DNS.Order = append(newCfg.DNS.Order, addr.String())
			continue
		}
		doh, err := config.ParseDoH(item)
		if err != nil {
			return &config.ValidationError{Field: "servers", Message: fmt.Sprintf(
				"%q is not an IP address or a DNS-over-HTTPS address like https://dns.example/dns-query.", item)}
		}
		newCfg.DNS.DoH = append(newCfg.DNS.DoH, doh)
		newCfg.DNS.Order = append(newCfg.DNS.Order, doh)
	}
	if err := newCfg.DNS.Validate(); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	// Only how many: a DoH address can hold a personal account ID (such as
	// https://dns.nextdns.io/abc123), and the log goes into diagnostics.
	slog.Info("DNS servers saved", "plain", len(newCfg.DNS.Servers), "doh", len(newCfg.DNS.DoH), "withVPN", newCfg.DNS.WithVPN)
	if a.vpnActive() {
		// The tunnel's DNS routes and the firewall's guard change too.
		if err := a.applyVPNConfig(); err != nil {
			return err
		}
		return a.applyClientPath()
	}
	return a.applyDHCP(false)
}

// ownDNSInUse reports whether the owner's DNS servers apply now: some are
// chosen, the VPN isn't carrying devices' traffic (it brings its own)
// unless they are also meant for the VPN, and no login page is in the way
// (many only work with the network's DNS).
func (a *Agent) ownDNSInUse() bool {
	d := a.cfg.DNS
	return len(d.Servers)+len(d.DoH) > 0 && (d.WithVPN || !a.clientsThroughTunnel()) &&
		!a.portal.loginMode && a.portal.result.State != portal.Portal
}

// ownDNSThroughTunnel lists the owner's plain DNS servers that must be
// reached through the tunnel while it carries devices' traffic: all of them
// with "also while the VPN is on", except plugins' (they are on RasBeacon
// itself) and ones on the hotspot or guest network (a Pi-hole plugged into
// RasBeacon, say), which the tunnel can't reach.
func (a *Agent) ownDNSThroughTunnel() []netip.Addr {
	if !a.cfg.DNS.WithVPN {
		return nil
	}
	var out []netip.Addr
	for _, s := range a.cfg.DNS.Servers {
		if !render.PluginSubnet.Contains(s) && !a.lan.Prefix.Contains(s) && !a.lan.Guest().Prefix.Contains(s) {
			out = append(out, s)
		}
	}
	return out
}

// applyDoH starts the DoH forwarder with the owner's DoH servers while they
// are in use, restarts it when they change, and stops it otherwise. The
// forwarder looks up the DoH servers' names with the owner's plain DNS
// servers first, then the network's (RasBeacon's resolver).
func (a *Agent) applyDoH(captive bool) error {
	want := ""
	if !captive && len(a.cfg.DNS.DoH) > 0 && a.ownDNSInUse() {
		// The DoH servers' names are looked up with the owner's plain
		// servers, then the network's; through the VPN, the VPN's instead
		// (the network's resolver would ask outside the tunnel).
		last := []string{deviceResolver}
		if a.clientsThroughTunnel() {
			p, _ := a.activeProfile()
			last = addrStrings(p.DNS)
		}
		bootstrap := append(addrStrings(a.cfg.DNS.Servers), last...)
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
	// Only root may read it (systemd reads it as root to start rbdoh): the
	// DoH addresses can hold a personal account ID.
	if err := config.WriteFileAtomic(a.paths.DoHEnv(), []byte(want), 0o600); err != nil {
		return err
	}
	if err := system.ServiceRestart(a.runner, dohService); err != nil {
		return err
	}
	a.dohEnv = want
	return nil
}
