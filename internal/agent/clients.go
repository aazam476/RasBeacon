package agent

// This file has the per-device controls on the Clients page:
//
//   - pausing a device's internet access: the firewall refuses to forward
//     anything from its MAC address (render.FirewallInput.PausedMACs). It
//     stays paused, also after a restart, until the owner allows it again;
//   - disconnecting it from the Wi-Fi (hostapd sends it away; it may come
//     back by itself, as when it walks out of range and back);
//   - forgetting its address lease, so it gets a fresh address next time
//     (dnsmasq's dhcp_release tool).

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"slices"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
	"rasbeacon/internal/stations"
	"rasbeacon/internal/system"
)

// hostapdControlDir is where hostapd's control sockets are (see
// hostapd.conf.tmpl).
const hostapdControlDir = "/run/hostapd"

// ErrNotConnected means the device isn't connected (any more).
var ErrNotConnected = errors.New("This device isn't connected.")

// normalizeMAC checks a device's MAC address with config.NormalizeMAC (the
// one rule for them), naming the "mac" field when it isn't one.
func normalizeMAC(mac string) (string, error) {
	normal, err := config.NormalizeMAC(mac)
	if err != nil {
		return "", &config.ValidationError{Field: "mac", Message: "Not a device address."}
	}
	return normal, nil
}

// pausedMACs lists the paused devices for the firewall.
func (a *Agent) pausedMACs() []string {
	macs := make([]string, 0, len(a.cfg.Clients.Paused))
	for mac := range a.cfg.Clients.Paused {
		macs = append(macs, mac)
	}
	// And those over their data limit (see usage.go).
	for _, mac := range a.usage.overLimit {
		if !slices.Contains(macs, mac) {
			macs = append(macs, mac)
		}
	}
	slices.Sort(macs)
	return macs
}

// pausedIPv4 lists the paused devices' addresses on the hotspot, from the
// DHCP leases, so the firewall drops traffic to them too.
func (a *Agent) pausedIPv4() []netip.Addr {
	paused := a.pausedMACs()
	if len(paused) == 0 {
		return nil
	}
	leases, _ := os.ReadFile(a.paths.DnsmasqLeases())
	var addrs []netip.Addr
	for _, lease := range stations.ParseLeases(string(leases)) {
		mac, err := normalizeMAC(lease.MAC)
		if err != nil || !slices.Contains(paused, mac) {
			continue
		}
		if addr, err := netip.ParseAddr(lease.IP); err == nil && addr.Is4() && !slices.Contains(addrs, addr) {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

// SetClientInternetAccess pauses (allowed false) or allows a device's
// internet access.
func (a *Agent) SetClientInternetAccess(mac string, allowed bool) error {
	mac, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	if allowed {
		delete(newCfg.Clients.Paused, mac)
	} else {
		if _, already := newCfg.Clients.Paused[mac]; already {
			return nil
		}
		if newCfg.Clients.Paused == nil {
			newCfg.Clients.Paused = map[string]time.Time{}
		}
		newCfg.Clients.Paused[mac] = a.now()
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("device internet access changed", "mac", mac, "allowed", allowed)
	return a.applyFirewall()
}

// findStation returns which of the hotspot radio's networks (ap0 or the
// guest network's interface) a device is connected to.
func (a *Agent) findStation(mac string) (iface string, ok bool) {
	for _, candidate := range []string{"ap0", render.GuestInterface} {
		dump, err := system.StationDump(a.runner, candidate)
		if err != nil {
			continue
		}
		for _, s := range stations.ParseStationDump(dump) {
			if s.MAC == mac {
				return candidate, true
			}
		}
	}
	return "", false
}

// DisconnectClient sends a device off the Wi-Fi.
func (a *Agent) DisconnectClient(mac string) error {
	mac, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	iface, ok := a.findStation(mac)
	if _, wired := a.uplink.wiredSince[mac]; !ok && wired {
		return &config.ValidationError{Field: "mac", Message: "This device is on the Ethernet cable, which can't be disconnected from here. Unplug it, or pause its internet access."}
	}
	if !ok {
		return &config.ValidationError{Field: "mac", Message: ErrNotConnected.Error()}
	}
	if _, err := a.runner.Run("hostapd_cli", "-p", hostapdControlDir, "-i", iface, "deauthenticate", mac); err != nil {
		return fmt.Errorf("disconnecting the device: %w", err)
	}
	slog.Info("device disconnected", "mac", mac, "interface", iface)
	return nil
}

// FlushClientLease makes dnsmasq forget a device's address, so it asks for
// a new one next time.
func (a *Agent) FlushClientLease(mac string) error {
	mac, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	leases, _ := os.ReadFile(a.paths.DnsmasqLeases())
	for _, lease := range stations.ParseLeases(string(leases)) {
		if lease.MAC != mac {
			continue
		}
		bridge := render.BridgeName
		if ip, err := netip.ParseAddr(lease.IP); err == nil && a.lan.Guest().Prefix.Masked().Contains(ip) {
			bridge = render.GuestBridgeName
		}
		if _, err := a.runner.Run("dhcp_release", bridge, lease.IP, mac); err != nil {
			return fmt.Errorf("forgetting the address: %w", err)
		}
		slog.Info("address lease forgotten", "mac", mac, "ip", lease.IP)
		return nil
	}
	return &config.ValidationError{Field: "mac", Message: "This device has no address lease."}
}
