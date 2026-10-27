package agent

// Fixed addresses ("Always use this address" on the Clients page): a device
// that always gets the same address on the hotspot, so that other devices
// (or a port forwarding rule) can find it there. dnsmasq hands it out
// (dhcp-host in dnsmasq.conf).

import (
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"strings"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
)

// SetFixedAddress makes the device mac always get address on the hotspot,
// or, with an empty address, lets it get any address again.
func (a *Agent) SetFixedAddress(mac, address string) error {
	mac, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	address = strings.TrimSpace(address)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	if address != "" {
		if err := a.checkFixedAddress(mac, address); err != nil {
			return err
		}
	}
	newCfg := a.cfg.Clone()
	if address == "" {
		delete(newCfg.Clients.FixedAddresses, mac)
	} else {
		if newCfg.Clients.FixedAddresses == nil {
			newCfg.Clients.FixedAddresses = map[string]string{}
		}
		newCfg.Clients.FixedAddresses[mac] = address
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("fixed address changed", "mac", mac, "address", address)
	// dnsmasq hands it out from now on. The device itself only asks for an
	// address when it reconnects or renews its lease.
	if err := a.applyDHCP(false); err != nil {
		return err
	}
	// Port forwarding rules for this device follow it to its new address.
	for _, r := range a.cfg.PortForwarding.Rules {
		if r.MAC == mac {
			return a.applyFirewall()
		}
	}
	return nil
}

// checkFixedAddress checks an address the owner chose for device mac: it
// must be on the hotspot's network, not RasBeacon's own address, and not
// another device's. The caller holds a.mu.
func (a *Agent) checkFixedAddress(mac, address string) error {
	ip, err := netip.ParseAddr(address)
	network := a.lan.Prefix.Masked()
	switch {
	case err != nil || !ip.Is4():
		return &config.ValidationError{Field: "address", Message: "Enter an address like " + a.lan.Prefix.Addr().Next().String() + "."}
	case !network.Contains(ip) || ip == network.Addr() || ip == lastAddress(network):
		return &config.ValidationError{Field: "address", Message: fmt.Sprintf("Use an address from %s to %s.", network.Addr().Next(), lastAddress(network).Prev())}
	case ip == a.lan.Prefix.Addr():
		return &config.ValidationError{Field: "address", Message: "That is RasBeacon's own address."}
	}
	for other, taken := range a.cfg.Clients.FixedAddresses {
		if other != mac && taken == ip.String() {
			name := a.cfg.Clients.Names[other]
			if name == "" {
				name = other
			}
			return &config.ValidationError{Field: "address", Message: fmt.Sprintf("%s already uses this address.", name)}
		}
	}
	return nil
}

// lastAddress is a network's last address (its broadcast address).
func lastAddress(network netip.Prefix) netip.Addr {
	b := network.Addr().As4()
	host := uint32(1)<<(32-network.Bits()) - 1
	n := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]) | host
	return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
}

// fixedAddresses lists the fixed addresses for dnsmasq, sorted by MAC so
// the file stays the same from one write to the next. An address that is no
// longer on the hotspot's network (RasBeacon moved it because the uplink
// used the same one) is left out until it fits again. The caller holds a.mu.
func (a *Agent) fixedAddresses() []render.FixedAddress {
	var list []render.FixedAddress
	network := a.lan.Prefix.Masked()
	for _, mac := range slices.Sorted(maps.Keys(a.cfg.Clients.FixedAddresses)) {
		address := a.cfg.Clients.FixedAddresses[mac]
		ip, err := netip.ParseAddr(address)
		if err != nil || !network.Contains(ip) || ip == a.lan.Prefix.Addr() || ip == network.Addr() || ip == lastAddress(network) {
			continue
		}
		list = append(list, render.FixedAddress{MAC: mac, IP: address})
	}
	return list
}
