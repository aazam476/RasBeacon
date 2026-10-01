// Package routing lists the commands that set up the WireGuard tunnel and
// send devices' traffic into it.
//
// The functions only return commands (as argument lists); the agent runs
// them. The network namespace tests run exactly the same commands, so what
// is tested is what runs on the device.
//
// # How devices' traffic reaches the tunnel
//
// Linux picks a route by looking up "routing tables" in the order of its
// "rules". RasBeacon adds:
//
//	rule 100:  traffic arriving from the hotspot (iif br-lan) -> table 51820
//	table 51820:  default route through wg0
//	              "unreachable" default route as a fallback
//
// RasBeacon's own traffic (the encrypted tunnel packets, DNS lookups for the
// VPN server's name, the clock) doesn't come from the hotspot, so it keeps
// using the normal ("main") table and leaves through the uplink.
//
// The fallback is the routing half of the kill switch: if wg0 disappears,
// its route goes with it and devices' traffic hits "unreachable" instead of
// falling through to the uplink. The firewall is the other half.
package routing

import (
	"fmt"
	"net/netip"
	"strconv"
)

const (
	// TunnelInterface is the WireGuard interface.
	TunnelInterface = "wg0"
	// TunnelTable is the routing table for devices' traffic.
	TunnelTable = "51820"
	// rulePriority places RasBeacon's rule before the main table (32766).
	rulePriority = "100"
	// fallbackMetric makes the "unreachable" route lose to the wg0 route.
	fallbackMetric = "4096"
)

// TunnelSettings describes the tunnel to set up.
type TunnelSettings struct {
	// ConfigFile holds the keys and the server in `wg setconf` format
	// (see render.WireGuard).
	ConfigFile string
	Addresses  []netip.Prefix
	MTU        int
	// DNS servers are reached through the tunnel (see TunnelUp).
	DNS []netip.Addr
	// IPv6 is true when the tunnel carries devices' IPv6 traffic.
	IPv6 bool
}

// TunnelUp returns the commands that configure wg0 and its routes. Run them
// after `ip link add wg0 type wireguard` (see CreateTunnel). They can be run
// again at any time to apply changed settings.
func TunnelUp(s TunnelSettings) [][]string {
	cmds := [][]string{
		// Keys and server.
		{"wg", "setconf", TunnelInterface, s.ConfigFile},
		// Addresses inside the tunnel.
		{"ip", "address", "flush", "dev", TunnelInterface},
	}
	for _, a := range s.Addresses {
		cmds = append(cmds, []string{"ip", "address", "add", a.String(), "dev", TunnelInterface})
	}
	cmds = append(cmds,
		[]string{"ip", "link", "set", "dev", TunnelInterface, "mtu", strconv.Itoa(s.MTU), "up"},

		// Table 51820: everything into the tunnel, or nowhere.
		[]string{"ip", "-4", "route", "replace", "default", "dev", TunnelInterface, "table", TunnelTable},
		[]string{"ip", "-4", "route", "replace", "unreachable", "default", "metric", fallbackMetric, "table", TunnelTable},
		[]string{"ip", "-6", "route", "replace", "unreachable", "default", "metric", fallbackMetric, "table", TunnelTable},
	)
	if s.IPv6 {
		cmds = append(cmds, []string{"ip", "-6", "route", "replace", "default", "dev", TunnelInterface, "table", TunnelTable})
	} else {
		// The tunnel has no IPv6: devices' IPv6 traffic must not go anywhere.
		cmds = append(cmds, []string{"ip", "-6", "route", "flush", "dev", TunnelInterface, "table", TunnelTable})
	}

	// The VPN's DNS servers: RasBeacon's DNS server (dnsmasq) asks them on
	// behalf of devices. dnsmasq is RasBeacon's own program, so its queries
	// use the main table; this route makes them go through the tunnel. If
	// the tunnel is down they die inside wg0 instead of leaking out.
	for _, dns := range s.DNS {
		cmds = append(cmds, []string{"ip", "route", "replace", hostPrefix(dns), "dev", TunnelInterface})
	}
	return cmds
}

// CreateTunnel returns the command that creates wg0. It fails harmlessly if
// wg0 already exists.
func CreateTunnel() []string {
	return []string{"ip", "link", "add", "dev", TunnelInterface, "type", "wireguard"}
}

// TunnelDown returns the commands that remove the tunnel completely (when
// the VPN is turned off). Errors from them can be ignored: they mean the
// thing was already gone.
func TunnelDown() [][]string {
	return append(ClientsDirect(),
		[]string{"ip", "link", "delete", "dev", TunnelInterface}, // also removes its routes
		[]string{"ip", "-4", "route", "flush", "table", TunnelTable},
		[]string{"ip", "-6", "route", "flush", "table", TunnelTable},
	)
}

// ClientsThroughTunnel returns the commands that send devices' traffic to
// table 51820. Run ClientsDirect first so the rule is never added twice.
func ClientsThroughTunnel(lanInterface string) [][]string {
	return [][]string{
		{"ip", "-4", "rule", "add", "priority", rulePriority, "iif", lanInterface, "lookup", TunnelTable},
		{"ip", "-6", "rule", "add", "priority", rulePriority, "iif", lanInterface, "lookup", TunnelTable},
	}
}

// ClientsDirect returns the commands that remove RasBeacon's rule, so
// devices' traffic uses the main table (the uplink). Errors mean the rule
// was not there.
func ClientsDirect() [][]string {
	return [][]string{
		{"ip", "-4", "rule", "delete", "priority", rulePriority},
		{"ip", "-6", "rule", "delete", "priority", rulePriority},
	}
}

// SetEndpoint returns the command that (re)sets the server's address. The
// wg tool looks up the host name when it runs, so running this again after
// the uplink changes picks up a new address.
func SetEndpoint(peerPublicKey, endpoint string) []string {
	return []string{"wg", "set", TunnelInterface, "peer", peerPublicKey, "endpoint", endpoint}
}

func hostPrefix(a netip.Addr) string {
	return fmt.Sprintf("%s/%d", a, a.BitLen())
}
