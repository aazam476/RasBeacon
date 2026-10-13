package agent

// This file manages the WireGuard VPN and its kill switch.
//
//   - applyVPNConfig sets up (or removes) the tunnel when settings change.
//   - updateTunnel runs with every uplink check (every 5 seconds). It asks
//     the kill-switch state machine (internal/tunnel) where devices' traffic
//     should go, and when the answer changes it switches the routing rule,
//     the firewall and dnsmasq's DNS servers together.
//
// The pieces that keep traffic inside the tunnel ("no leaks"):
//
//   - Routing (internal/routing): devices' traffic goes to table 51820,
//     which only leads into wg0 or to "unreachable".
//   - Firewall: devices' traffic may only be forwarded to wg0.
//   - DNS: dnsmasq asks the VPN's DNS servers, which are routed into wg0 and
//     blocked everywhere else by the firewall's output rule.
//
// Optionally, the WireGuard server routes a public IPv6 network to
// RasBeacon (VPNConfig.RoutedIPv6Prefix). RasBeacon then announces it on the
// hotspot, so devices get public IPv6 addresses, and the firewall sends
// their IPv6 traffic through the tunnel without NAT (applyRoutedIPv6).
//
// Each of these alone would stop devices' traffic from leaving through the
// uplink. Only when the kill switch is OFF and the tunnel has been broken
// for a while ("direct") are they all switched to the uplink.

import (
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/portal"
	"rasbeacon/internal/render"
	"rasbeacon/internal/routing"
	"rasbeacon/internal/system"
	"rasbeacon/internal/tunnel"
	"rasbeacon/internal/wireguard"
)

// endpointRefreshInterval: while the tunnel isn't working, look up the
// server's name again this often, in case its address changed (the kernel
// never does this by itself).
const endpointRefreshInterval = time.Minute

// vpnState is what rbd knows about the tunnel between checks.
type vpnState struct {
	machine tunnel.Machine
	// applied is the state whose routing, firewall and DNS are in place.
	applied     tunnel.State
	peer        wireguard.PeerStatus
	lastRefresh time.Time
}

// activeProfile returns the VPN profile in use. ok is false if there is
// none.
func (a *Agent) activeProfile() (config.VPNProfile, bool) {
	return a.cfg.VPN.Profile(a.cfg.VPN.Active)
}

// wireguardConfig builds the tunnel settings for the profile in use. ok is
// false if there is none, or it isn't complete yet.
func (a *Agent) wireguardConfig() (c wireguard.Config, ok bool, err error) {
	p, found := a.activeProfile()
	if !found {
		return c, false, nil
	}
	return wireguardConfigFor(p, a.secrets.WireGuardKeys[p.Name])
}

// wireguardConfigFor builds the tunnel settings from a profile and its keys.
// ok is false if the server or RasBeacon's own key is still missing.
func wireguardConfigFor(p config.VPNProfile, keys config.WireGuardKeys) (c wireguard.Config, ok bool, err error) {
	if p.Peer.PublicKey == "" || keys.PrivateKey == "" {
		return c, false, nil
	}
	if c.PrivateKey, err = wireguard.ParseKey(keys.PrivateKey); err != nil {
		return c, false, fmt.Errorf("saved private key: %w", err)
	}
	if c.Peer.PublicKey, err = wireguard.ParseKey(p.Peer.PublicKey); err != nil {
		return c, false, fmt.Errorf("saved server key: %w", err)
	}
	if keys.PresharedKey != "" {
		if c.Peer.PresharedKey, err = wireguard.ParseKey(keys.PresharedKey); err != nil {
			return c, false, fmt.Errorf("saved preshared key: %w", err)
		}
		c.Peer.HasPresharedKey = true
	}
	c.Addresses, c.DNS, c.MTU = p.Addresses, p.DNS, p.MTU
	c.Peer.Endpoint, c.Peer.AllowedIPs, c.Peer.PersistentKeepalive = p.Peer.Endpoint, p.Peer.AllowedIPs, p.Peer.PersistentKeepalive
	return c, true, nil
}

// vpnActive reports whether the VPN is turned on and set up.
func (a *Agent) vpnActive() bool {
	_, ok, err := a.wireguardConfig()
	return a.cfg.VPN.Enabled && ok && err == nil
}

// clientsThroughTunnel reports whether devices' traffic must go into the
// tunnel right now (to be dropped there if it is down).
//
// This fails closed: while the VPN is on, only an explicit "direct" from
// the state machine (kill switch off, tunnel broken for a while) lets
// traffic out through the uplink. In particular, before the state machine
// has run at all, traffic is kept in the tunnel.
//
// Login mode (portal.go) also sends traffic outside the tunnel, limited to
// web and DNS by the firewall.
func (a *Agent) clientsThroughTunnel() bool {
	return a.vpnActive() && a.vpn.machine.State() != tunnel.Direct && !a.portal.loginMode
}

// applyVPNConfig sets up the tunnel for the current settings, or removes it
// when the VPN is off.
func (a *Agent) applyVPNConfig() error {
	wg, configured, err := a.wireguardConfig()
	if err != nil {
		return err
	}
	if !a.cfg.VPN.Enabled || !configured {
		for _, cmd := range routing.TunnelDown() {
			_, _ = a.runner.Run(cmd[0], cmd[1:]...) // errors: already gone
		}
		a.vpn = vpnState{}
		a.applyRoutedIPv6()
		return a.updateTunnel()
	}

	conf, err := render.WireGuard(render.WireGuardInput{
		PrivateKey:    wg.PrivateKey.String(),
		PeerPublicKey: wg.Peer.PublicKey.String(),
		PresharedKey:  presharedKeyText(wg.Peer),
		Endpoint:      wg.Peer.Endpoint,
		AllowedIPs:    wg.Peer.AllowedIPs,
		Keepalive:     wg.EffectiveKeepalive(),
	})
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(a.paths.WireGuardConfig(), []byte(conf), 0o600); err != nil {
		return err
	}
	if !system.LinkExists(a.runner, routing.TunnelInterface) {
		cmd := routing.CreateTunnel()
		if _, err := a.runner.Run(cmd[0], cmd[1:]...); err != nil {
			return fmt.Errorf("creating the WireGuard tunnel: %w", err)
		}
	}
	for _, cmd := range routing.TunnelUp(routing.TunnelSettings{
		ConfigFile: a.paths.WireGuardConfig(),
		Addresses:  wg.Addresses,
		MTU:        wg.EffectiveMTU(),
		DNS:        wg.DNS,
		IPv6:       wg.CarriesIPv6(),
		KernelIPv6: routing.KernelHasIPv6(a.paths.ProcSysNetIPv6()),
	}) {
		if _, err := a.runner.Run(cmd[0], cmd[1:]...); err != nil {
			return fmt.Errorf("setting up the WireGuard tunnel: %w", err)
		}
	}
	a.vpn.lastRefresh = a.now()
	a.applyRoutedIPv6()
	return a.updateTunnel()
}

// wantedRoutedIPv6 returns the public IPv6 network that should be on the
// hotspot now: the one the VPN server routes to RasBeacon, while the VPN
// is in use. Otherwise it returns an invalid (unset) prefix.
func (a *Agent) wantedRoutedIPv6() netip.Prefix {
	if !a.vpnActive() || !routing.KernelHasIPv6(a.paths.ProcSysNetIPv6()) {
		return netip.Prefix{}
	}
	p, _ := a.activeProfile()
	return p.RoutedIPv6Prefix
}

// applyRoutedIPv6 puts RasBeacon's address in the routed IPv6 network on
// the hotspot bridge (or takes it off). dnsmasq announces every network on
// the bridge, so devices pick addresses in it; the firewall rule that
// skips NAT for it follows with the next firewall reload.
func (a *Agent) applyRoutedIPv6() {
	want := a.wantedRoutedIPv6()
	if want == a.routedIPv6 {
		return
	}
	if a.routedIPv6.IsValid() {
		_, _ = a.runner.Run("ip", "-6", "address", "del", routerAddress(a.routedIPv6).String(), "dev", render.BridgeName)
	}
	a.routedIPv6 = netip.Prefix{}
	if want.IsValid() {
		if _, err := a.runner.Run("ip", "-6", "address", "replace", routerAddress(want).String(), "dev", render.BridgeName); err != nil {
			slog.Error("could not add the routed IPv6 network to the hotspot", "error", err)
			return
		}
		a.routedIPv6 = want
	}
	slog.Info("routed IPv6 network on the hotspot", "prefix", a.routedIPv6)
	if a.mode == api.ModeNormal {
		if err := a.applyFirewall(); err != nil {
			slog.Error("could not update the firewall for the routed IPv6 network", "error", err)
		}
		if err := a.applyDHCP(false); err != nil {
			slog.Error("could not announce the routed IPv6 network", "error", err)
		}
	}
}

// routerAddress is RasBeacon's own address in a /64: the first one (::1).
func routerAddress(network netip.Prefix) netip.Prefix {
	a := network.Masked().Addr().As16()
	a[15] = 1
	return netip.PrefixFrom(netip.AddrFrom16(a), network.Bits())
}

func presharedKeyText(p wireguard.Peer) string {
	if !p.HasPresharedKey {
		return ""
	}
	return p.PresharedKey.String()
}

// updateTunnel reads the tunnel's state, runs the kill-switch state
// machine, and applies the result if it changed. The caller holds a.mu.
func (a *Agent) updateTunnel() error {
	active := a.vpnActive()
	if active {
		out, _ := a.runner.Run("wg", "show", routing.TunnelInterface, "dump")
		a.vpn.peer, _ = wireguard.ParseDump(out)
	}
	state := a.vpn.machine.Update(tunnel.Inputs{
		Now:           a.now(),
		Enabled:       active,
		KillSwitch:    a.cfg.VPN.KillSwitch,
		LastHandshake: a.vpn.peer.LatestHandshake,
	})

	// While the tunnel isn't working, look up the server's address again
	// now and then (it may have changed, or the uplink's DNS works now).
	if active && state != tunnel.Up && a.now().Sub(a.vpn.lastRefresh) >= endpointRefreshInterval {
		a.vpn.lastRefresh = a.now()
		p, _ := a.activeProfile()
		cmd := routing.SetEndpoint(p.Peer.PublicKey, p.Peer.Endpoint)
		if _, err := a.runner.Run(cmd[0], cmd[1:]...); err != nil {
			slog.Warn("could not look up the VPN server", "endpoint", p.Peer.Endpoint, "error", err)
		}
	}

	if state == a.vpn.applied {
		return nil
	}
	slog.Info("VPN state changed", "from", a.vpn.applied, "to", state, "killSwitch", a.cfg.VPN.KillSwitch)
	a.vpn.applied = state
	return a.applyClientPath()
}

// applyClientPath makes the firewall, routing and DNS match where devices'
// traffic should go now (tunnel, uplink, or login mode). The firewall comes
// first: it is the guarantee, so when traffic must move into the tunnel the
// uplink is closed before anything else changes.
func (a *Agent) applyClientPath() error {
	// 1. Firewall: where devices' traffic may leave.
	if a.mode == api.ModeNormal {
		if err := a.applyFirewall(); err != nil {
			return err
		}
	}
	// 2. Routing: devices' traffic into table 51820 (tunnel) or not.
	for _, cmd := range routing.ClientsDirect() {
		_, _ = a.runner.Run(cmd[0], cmd[1:]...) // error: no rule yet
	}
	if a.clientsThroughTunnel() {
		for _, cmd := range routing.ClientsThroughTunnel(routing.KernelHasIPv6(a.paths.ProcSysNetIPv6())) {
			if _, err := a.runner.Run(cmd[0], cmd[1:]...); err != nil {
				return fmt.Errorf("routing devices into the tunnel: %w", err)
			}
		}
	}
	if a.mode != api.ModeNormal {
		return nil // dnsmasq follows when normal mode starts
	}
	// 3. DNS servers, IPv6 announcements and the login notice.
	return a.applyDHCP(false)
}

// egressInterfaces lists where devices' traffic may leave RasBeacon (see
// render.FirewallInput.Egress).
func (a *Agent) egressInterfaces() []string {
	switch {
	case a.mode != api.ModeNormal:
		return nil // nothing is forwarded before setup is finished
	case a.clientsThroughTunnel():
		return []string{routing.TunnelInterface}
	default:
		return a.uplinkInterfaces()
	}
}

// upstreamDNS returns the DNS servers dnsmasq passes devices' questions to.
func (a *Agent) upstreamDNS() []string {
	if !a.clientsThroughTunnel() {
		// The owner's own servers, except while a login page is in the way:
		// many only work with the network's DNS.
		custom := a.cfg.DNS.Servers
		if len(custom) > 0 && !a.portal.loginMode && a.portal.result.State != portal.Portal {
			return addrStrings(custom)
		}
		return []string{deviceResolver}
	}
	var servers []string
	p, _ := a.activeProfile()
	for _, dns := range p.DNS {
		servers = append(servers, dns.String())
	}
	return servers
}

// devicesHaveIPv6Internet reports whether devices may be told to use IPv6
// for the internet.
func (a *Agent) devicesHaveIPv6Internet() bool {
	if a.clientsThroughTunnel() {
		wg, _, _ := a.wireguardConfig()
		return a.vpn.machine.State() == tunnel.Up && wg.CarriesIPv6()
	}
	return a.uplink.status.IPv6
}

// tunnelDNSGuard returns the VPN's DNS servers for the firewall's output
// rule, which keeps them reachable only through the tunnel.
func (a *Agent) tunnelDNSGuard() []netip.Addr {
	if !a.vpnActive() {
		return nil
	}
	p, _ := a.activeProfile()
	return p.DNS
}

// vpnStatus describes the tunnel for the status page.
func (a *Agent) vpnStatus() api.VPNStatus {
	s := api.VPNStatus{
		Enabled:             a.cfg.VPN.Enabled,
		KillSwitch:          a.cfg.VPN.KillSwitch,
		State:               string(a.vpn.machine.State()),
		HandshakeAgeSeconds: -1,
		Endpoint:            a.vpn.peer.Endpoint,
		ReceivedBytes:       a.vpn.peer.ReceivedBytes,
		SentBytes:           a.vpn.peer.SentBytes,
	}
	if a.routedIPv6.IsValid() {
		s.RoutedIPv6Prefix = a.routedIPv6.String()
	}
	if !a.vpn.peer.LatestHandshake.IsZero() {
		s.HandshakeAgeSeconds = int(a.now().Sub(a.vpn.peer.LatestHandshake).Seconds())
	}
	return s
}

// vpnWarnings explains VPN problems on the status page.
func (a *Agent) vpnWarnings() []string {
	if !a.vpnActive() {
		if a.cfg.VPN.Enabled {
			return []string{"The VPN is turned on but its profile isn't complete: import a WireGuard file on the VPN page, or finish the profile's server details."}
		}
		return nil
	}
	switch a.vpn.machine.State() {
	case tunnel.Connecting, tunnel.Down:
		if a.cfg.VPN.KillSwitch {
			return []string{"The VPN tunnel is not working, and the kill switch is on: your devices have no internet until it works again."}
		}
	case tunnel.Direct:
		return []string{"The VPN tunnel is not working, and the kill switch is off: your devices are using the internet WITHOUT the VPN."}
	}
	return nil
}

// --- Settings (called through the API) ---

// VPNSettings returns the VPN page settings (without secrets).
func (a *Agent) VPNSettings() api.VPNSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := a.cfg.VPN
	s := api.VPNSettings{
		Enabled:       v.Enabled,
		KillSwitch:    v.KillSwitch,
		ActiveProfile: v.Active,
		Profiles:      []api.VPNProfile{},
	}
	for _, p := range v.Profiles {
		keys := a.secrets.WireGuardKeys[p.Name]
		profile := api.VPNProfile{
			Name:                p.Name,
			Addresses:           prefixStrings(p.Addresses),
			DNS:                 addrStrings(p.DNS),
			MTU:                 p.MTU,
			PeerPublicKey:       p.Peer.PublicKey,
			Endpoint:            p.Peer.Endpoint,
			AllowedIPs:          prefixStrings(p.Peer.AllowedIPs),
			PersistentKeepalive: p.Peer.PersistentKeepalive,
			HasPresharedKey:     keys.PresharedKey != "",
		}
		if p.RoutedIPv6Prefix.IsValid() {
			profile.RoutedIPv6Prefix = p.RoutedIPv6Prefix.String()
		}
		if key, err := wireguard.ParseKey(keys.PrivateKey); err == nil {
			profile.PublicKey = key.PublicKey().String()
		}
		s.Profiles = append(s.Profiles, profile)
	}
	return s
}

// SetVPNSwitches turns the VPN and its kill switch on or off.
func (a *Agent) SetVPNSwitches(req api.VPNSwitches) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	newCfg.VPN.Enabled, newCfg.VPN.KillSwitch = req.Enabled, req.KillSwitch
	return a.saveVPN(newCfg, a.secrets.Clone(), "")
}

// profileIndex finds a profile in newCfg, or answers "no such profile".
func profileIndex(v config.VPNConfig, name string) (int, error) {
	for i, p := range v.Profiles {
		if p.Name == name {
			return i, nil
		}
	}
	return -1, &config.ValidationError{Field: "name", Message: "There is no VPN profile called " + strconv.Quote(name) + "."}
}

// SaveVPNProfile saves the details typed into one profile's form.
func (a *Agent) SaveVPNProfile(req api.VPNProfileDetails) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	newSecrets := a.secrets.Clone()
	i, err := profileIndex(newCfg.VPN, req.Name)
	if err != nil {
		return err
	}
	p := &newCfg.VPN.Profiles[i]
	p.MTU = req.MTU
	p.Peer.PublicKey = strings.TrimSpace(req.PeerPublicKey)
	p.Peer.Endpoint = strings.TrimSpace(req.Endpoint)
	p.Peer.PersistentKeepalive = req.PersistentKeepalive

	if p.Addresses, err = parsePrefixes(req.Addresses, "addresses", true); err != nil {
		return err
	}
	if p.Peer.AllowedIPs, err = parsePrefixes(req.AllowedIPs, "allowedIps", false); err != nil {
		return err
	}
	if p.DNS, err = parseAddrs(req.DNS, "dns"); err != nil {
		return err
	}
	p.RoutedIPv6Prefix = netip.Prefix{}
	if text := strings.TrimSpace(req.RoutedIPv6Prefix); text != "" {
		if p.RoutedIPv6Prefix, err = netip.ParsePrefix(text); err != nil {
			return &config.ValidationError{Field: "routedIpv6Prefix", Message: "Must be an IPv6 network like 2001:db8:1234:5678::/64."}
		}
	}
	keys := newSecrets.WireGuardKeys[p.Name]
	if req.PrivateKey != "" {
		if _, err := wireguard.ParseKey(strings.TrimSpace(req.PrivateKey)); err != nil {
			return &config.ValidationError{Field: "privateKey", Message: err.Error()}
		}
		keys.PrivateKey = strings.TrimSpace(req.PrivateKey)
	}
	switch {
	case req.RemovePresharedKey:
		keys.PresharedKey = ""
	case req.PresharedKey != "":
		if _, err := wireguard.ParseKey(strings.TrimSpace(req.PresharedKey)); err != nil {
			return &config.ValidationError{Field: "presharedKey", Message: err.Error()}
		}
		keys.PresharedKey = strings.TrimSpace(req.PresharedKey)
	}
	newSecrets.WireGuardKeys = withKeys(newSecrets.WireGuardKeys, p.Name, keys)
	return a.saveVPN(newCfg, newSecrets, p.Name)
}

// ImportVPNProfile reads a WireGuard .conf file and keeps it as a new
// profile. The first profile is put in use; the switches stay as they are.
func (a *Agent) ImportVPNProfile(conf, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	if len(conf) > 16*1024 {
		return &config.ValidationError{Field: "conf", Message: "This file is too big to be a WireGuard configuration."}
	}
	if len(a.cfg.VPN.Profiles) >= config.MaxVPNProfiles {
		return &config.ValidationError{Field: "conf", Message: fmt.Sprintf("RasBeacon keeps at most %d VPN profiles. Remove one first.", config.MaxVPNProfiles)}
	}
	wg, err := wireguard.ParseConf(conf)
	if err != nil {
		return &config.ValidationError{Field: "conf", Message: err.Error()}
	}
	name = uniqueProfileName(a.cfg.VPN, strings.TrimSpace(name))
	if err := config.ValidateProfileName(name); err != nil {
		return err
	}
	newCfg := a.cfg.Clone()
	newSecrets := a.secrets.Clone()
	newCfg.VPN.Profiles = append(newCfg.VPN.Profiles, config.VPNProfile{
		Name:      name,
		Addresses: wg.Addresses,
		DNS:       wg.DNS,
		MTU:       wg.MTU,
		Peer: config.VPNPeer{
			PublicKey:           wg.Peer.PublicKey.String(),
			Endpoint:            wg.Peer.Endpoint,
			AllowedIPs:          wg.Peer.AllowedIPs,
			PersistentKeepalive: wg.Peer.PersistentKeepalive,
		},
	})
	if newCfg.VPN.Active == "" {
		newCfg.VPN.Active = name
	}
	newSecrets.WireGuardKeys = withKeys(newSecrets.WireGuardKeys, name, config.WireGuardKeys{
		PrivateKey:   wg.PrivateKey.String(),
		PresharedKey: presharedKeyText(wg.Peer),
	})
	slog.Info("WireGuard file imported", "profile", name, "endpoint", wg.Peer.Endpoint)
	return a.saveVPN(newCfg, newSecrets, name)
}

// uniqueProfileName returns name, or "wireguard.conf" if it is empty, with
// "-2", "-3", ... added before ".conf" if a profile already has it.
func uniqueProfileName(v config.VPNConfig, name string) string {
	if name == "" {
		name = "wireguard.conf"
	}
	if _, taken := v.Profile(name); !taken {
		return name
	}
	base := strings.TrimSuffix(name, ".conf")
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d.conf", base, n)
		if _, taken := v.Profile(candidate); !taken {
			return candidate
		}
	}
}

// SetActiveVPNProfile switches to another profile.
func (a *Agent) SetActiveVPNProfile(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	if _, err := profileIndex(a.cfg.VPN, name); err != nil {
		return err
	}
	if name == a.cfg.VPN.Active {
		return nil
	}
	newCfg := a.cfg.Clone()
	newCfg.VPN.Active = name
	slog.Info("VPN profile switched", "profile", name)
	return a.saveVPN(newCfg, a.secrets.Clone(), name)
}

// RemoveVPNProfile deletes a profile and its keys. If it was in use, the
// first remaining profile takes over; without any, the VPN is turned off.
func (a *Agent) RemoveVPNProfile(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	i, err := profileIndex(a.cfg.VPN, name)
	if err != nil {
		return err
	}
	newCfg := a.cfg.Clone()
	newSecrets := a.secrets.Clone()
	newCfg.VPN.Profiles = slices.Delete(newCfg.VPN.Profiles, i, i+1)
	delete(newSecrets.WireGuardKeys, name)
	if newCfg.VPN.Active == name {
		newCfg.VPN.Active = ""
		if len(newCfg.VPN.Profiles) > 0 {
			newCfg.VPN.Active = newCfg.VPN.Profiles[0].Name
		}
	}
	if len(newCfg.VPN.Profiles) == 0 {
		newCfg.VPN.Enabled = false
	}
	slog.Info("VPN profile removed", "profile", name, "active", newCfg.VPN.Active)
	return a.saveVPN(newCfg, newSecrets, "")
}

// GenerateVPNKey makes a new private key for one profile. The matching
// public key (shown on the VPN page) must then be added to the WireGuard
// server.
func (a *Agent) GenerateVPNKey(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	if _, err := profileIndex(a.cfg.VPN, name); err != nil {
		return err
	}
	key, err := wireguard.GeneratePrivateKey()
	if err != nil {
		return err
	}
	newSecrets := a.secrets.Clone()
	keys := newSecrets.WireGuardKeys[name]
	keys.PrivateKey = key.String()
	newSecrets.WireGuardKeys = withKeys(newSecrets.WireGuardKeys, name, keys)
	return a.saveVPN(a.cfg.Clone(), newSecrets, name)
}

// withKeys sets one profile's keys in a (possibly nil) map.
func withKeys(all map[string]config.WireGuardKeys, name string, keys config.WireGuardKeys) map[string]config.WireGuardKeys {
	if all == nil {
		all = map[string]config.WireGuardKeys{}
	}
	all[name] = keys
	return all
}

// checkProfile checks that a profile is valid, as far as it is filled in.
// configured is false while its server or key is still missing.
func checkProfile(p config.VPNProfile, keys config.WireGuardKeys) (configured bool, err error) {
	wg, configured, err := wireguardConfigFor(p, keys)
	if err != nil {
		return false, &config.ValidationError{Field: "privateKey", Message: err.Error()}
	}
	if configured {
		if err := wg.Validate(); err != nil {
			return false, &config.ValidationError{Field: "conf", Message: err.Error()}
		}
	}
	if prefix := p.RoutedIPv6Prefix; prefix.IsValid() {
		if err := config.ValidateRoutedIPv6Prefix(prefix); err != nil {
			return false, &config.ValidationError{Field: "routedIpv6Prefix", Message: err.Error()}
		}
		if configured && !wg.CarriesIPv6() {
			return false, &config.ValidationError{Field: "routedIpv6Prefix",
				Message: "The VPN must carry IPv6 for this: it needs an IPv6 address and ::/0 in AllowedIPs."}
		}
	}
	return configured, nil
}

// saveVPN checks, saves and applies new VPN settings. changed is the
// profile that was edited ("" for none), which is checked too even if it
// isn't in use.
func (a *Agent) saveVPN(newCfg config.Config, newSecrets config.Secrets, changed string) error {
	if p, ok := newCfg.VPN.Profile(changed); ok {
		if _, err := checkProfile(p, newSecrets.WireGuardKeys[p.Name]); err != nil {
			return err
		}
	}
	active, hasActive := newCfg.VPN.Profile(newCfg.VPN.Active)
	configured := false
	if hasActive {
		var err error
		if configured, err = checkProfile(active, newSecrets.WireGuardKeys[active.Name]); err != nil {
			return err
		}
	}
	if newCfg.VPN.Enabled && !configured {
		return &config.ValidationError{Field: "enabled", Message: "Import a WireGuard file (or finish the profile's server details and keys) before turning the VPN on."}
	}

	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	previous := a.cfg.VPN.Active
	a.cfg, a.secrets = newCfg, newSecrets
	if previous != newCfg.VPN.Active {
		// Another server: start from a fresh tunnel, so nothing of the old
		// profile (addresses, routes to its DNS servers) stays behind. The
		// firewall keeps devices' traffic in the tunnel meanwhile.
		for _, cmd := range routing.TunnelDown() {
			_, _ = a.runner.Run(cmd[0], cmd[1:]...) // errors: already gone
		}
	}
	slog.Info("VPN settings saved", "enabled", newCfg.VPN.Enabled, "killSwitch", newCfg.VPN.KillSwitch, "profile", newCfg.VPN.Active)
	// Start the state machine again from "connecting" so the new settings
	// are judged on their own.
	a.vpn.machine = tunnel.Machine{}
	a.vpn.applied = ""
	return a.applyVPNConfig()
}

// --- Small helpers for text <-> addresses ---

func prefixStrings(prefixes []netip.Prefix) []string {
	out := []string{}
	for _, p := range prefixes {
		out = append(out, p.String())
	}
	return out
}

func addrStrings(addrs []netip.Addr) []string {
	out := []string{}
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

// parsePrefixes reads addresses like "10.66.0.2/32". A bare address counts
// as a single address. keepHost keeps host bits (for tunnel addresses).
func parsePrefixes(items []string, field string, keepHost bool) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			addr, err := netip.ParseAddr(item)
			if err != nil {
				return nil, &config.ValidationError{Field: field, Message: fmt.Sprintf("%q is not an IP address.", item)}
			}
			item = fmt.Sprintf("%s/%d", addr, addr.BitLen())
		}
		p, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, &config.ValidationError{Field: field, Message: fmt.Sprintf("%q is not an address with network size, like 10.66.0.2/32.", item)}
		}
		if !keepHost {
			p = p.Masked()
		}
		out = append(out, p)
	}
	return out, nil
}

func parseAddrs(items []string, field string) ([]netip.Addr, error) {
	var out []netip.Addr
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, &config.ValidationError{Field: field, Message: fmt.Sprintf("%q is not an IP address.", item)}
		}
		out = append(out, addr)
	}
	return out, nil
}
