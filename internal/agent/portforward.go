package agent

// Port forwarding (Internet page → Port forwarding): connections from the
// internet to a port on RasBeacon's uplink go to a device on the hotspot
// (at its fixed address) or to a plugin. The firewall does the work (see
// port_forwards in firewall.nft.tmpl); here are the rules' checks, and
// what the page shows.

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"unicode/utf8"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
)

// maxPortForwards is the most rules the page keeps.
const maxPortForwards = 50

// riskyPorts are ports often attacked from the internet, with what usually
// listens there. Forwarding them is allowed, with a warning.
var riskyPorts = map[int]string{
	22: "SSH", 23: "Telnet", 135: "Windows RPC", 139: "Windows file sharing", 445: "Windows file sharing",
	1433: "SQL Server", 3306: "MySQL", 3389: "Remote Desktop", 5900: "VNC",
}

// PortForwardingSettings is the Port forwarding page.
func (a *Agent) PortForwardingSettings() api.PortForwardingSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := api.PortForwardingSettings{Rules: []api.PortForwardRule{}, Reachability: "offline"}
	if a.uplink.status.Connected {
		address, _, _ := strings.Cut(a.uplink.status.Address, "/")
		s.OutsideAddress = address
		s.Reachability = reachability(address)
	}
	for _, r := range a.cfg.PortForwarding.Rules {
		rule := api.PortForwardRule{
			ID: r.ID, Name: r.Name, Protocol: r.Protocol, OutsidePort: r.OutsidePort, OutsidePortEnd: r.OutsidePortEnd,
			MAC: r.MAC, Plugin: r.Plugin, InsidePort: r.InsidePort, Enabled: r.Enabled,
			Warning: portWarning(r),
		}
		rule.Target, rule.Address = a.forwardTarget(r)
		s.Rules = append(s.Rules, rule)
	}
	return s
}

// reachability says whether devices on the internet can reach address
// (see api.PortForwardingSettings.Reachability).
func reachability(address string) string {
	ip, err := netip.ParseAddr(address)
	switch {
	case err != nil:
		return "offline"
	case netip.MustParsePrefix("100.64.0.0/10").Contains(ip):
		return "shared"
	case ip.IsPrivate():
		return "private"
	default:
		return "public"
	}
}

// portWarning is the warning for a rule that forwards a risky port, or "".
func portWarning(r config.PortForwardRule) string {
	last := max(r.OutsidePort, r.OutsidePortEnd)
	for port := r.OutsidePort; port <= last; port++ {
		if what, ok := riskyPorts[port]; ok {
			return fmt.Sprintf("Port %d (%s) is often attacked from the internet. Use a strong password there, or another outside port.", port, what)
		}
	}
	return ""
}

// forwardTarget is the name of where a rule goes, and its address there
// ("" when the device has no fixed address). The caller holds a.mu.
func (a *Agent) forwardTarget(r config.PortForwardRule) (name, address string) {
	if r.Plugin != "" {
		p, ok := a.cfg.Plugins.Installed[r.Plugin]
		if !ok {
			return r.Plugin, ""
		}
		return a.pluginName(r.Plugin), p.Address.String()
	}
	name = a.cfg.Clients.Names[r.MAC]
	if name == "" {
		name = r.MAC
	}
	return name, a.cfg.Clients.FixedAddresses[r.MAC]
}

// UpdatePortForwarding saves the whole list of rules (each checked) and
// loads them into the firewall.
func (a *Agent) UpdatePortForwarding(req api.PortForwardingSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	if len(req.Rules) > maxPortForwards {
		return &config.ValidationError{Field: "rules", Message: fmt.Sprintf("RasBeacon keeps at most %d rules.", maxPortForwards)}
	}
	var rules []config.PortForwardRule
	for _, in := range req.Rules {
		r, err := a.checkPortForward(in)
		if err != nil {
			return err
		}
		for _, other := range rules {
			if r.Enabled && other.Enabled && portsOverlap(r, other) {
				return &config.ValidationError{Field: "outsidePort", Message: fmt.Sprintf("%q already forwards this port.", other.Name)}
			}
		}
		rules = append(rules, r)
	}
	newCfg := a.cfg.Clone()
	newCfg.PortForwarding.Rules = rules
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("port forwarding saved", "rules", len(rules))
	return a.applyFirewall()
}

// checkPortForward checks one rule from the page and returns it as it is
// saved. The caller holds a.mu.
func (a *Agent) checkPortForward(in api.PortForwardRule) (config.PortForwardRule, error) {
	r := config.PortForwardRule{
		ID: in.ID, Name: strings.TrimSpace(in.Name), Protocol: in.Protocol, OutsidePort: in.OutsidePort,
		OutsidePortEnd: in.OutsidePortEnd, InsidePort: in.InsidePort, Enabled: in.Enabled, Plugin: in.Plugin,
	}
	if r.ID == "" {
		r.ID = newNetworkID()
	}
	if r.OutsidePortEnd == r.OutsidePort {
		r.OutsidePortEnd = 0
	}
	invalid := func(field, message string) (config.PortForwardRule, error) {
		return r, &config.ValidationError{Field: field, Message: message}
	}
	switch {
	case r.Name == "" || utf8.RuneCountInString(r.Name) > 40 || strings.ContainsFunc(r.Name, func(c rune) bool { return c < 0x20 }):
		return invalid("name", "Give the rule a name of up to 40 characters.")
	case r.Protocol != "tcp" && r.Protocol != "udp" && r.Protocol != "both":
		return invalid("protocol", "Choose TCP, UDP or both.")
	case r.OutsidePort < 1 || r.OutsidePort > 65535:
		return invalid("outsidePort", "Use a port from 1 to 65535.")
	case r.OutsidePortEnd != 0 && (r.OutsidePortEnd < r.OutsidePort || r.OutsidePortEnd > 65535):
		return invalid("outsidePort", "The range must end on a port after the first, up to 65535.")
	case r.InsidePort < 0 || r.InsidePort > 65535:
		return invalid("insidePort", "Use a port from 1 to 65535, or leave it empty for the same port.")
	case r.OutsidePortEnd != 0 && r.InsidePort != 0 && r.InsidePort != r.OutsidePort:
		return invalid("insidePort", "A range keeps its ports: leave the inside port empty.")
	case r.Protocol != "tcp" && r.OutsidePort <= 68 && max(r.OutsidePort, r.OutsidePortEnd) >= 67:
		return invalid("outsidePort", "RasBeacon needs UDP ports 67 and 68 to get its own address.")
	}
	if r.InsidePort == r.OutsidePort {
		r.InsidePort = 0
	}
	if r.Plugin != "" {
		if _, ok := a.cfg.Plugins.Installed[r.Plugin]; !ok || in.MAC != "" {
			return invalid("target", "Choose a device or an installed plugin.")
		}
		return r, nil
	}
	mac, err := config.NormalizeMAC(in.MAC)
	if err != nil {
		return invalid("target", "Choose a device or an installed plugin.")
	}
	r.MAC = mac
	if r.Enabled && a.cfg.Clients.FixedAddresses[mac] == "" {
		return invalid("target", "Give this device a fixed address first, so the rule always finds it.")
	}
	return r, nil
}

// portsOverlap reports whether two rules forward a port in common on the
// same protocol.
func portsOverlap(x, y config.PortForwardRule) bool {
	if x.Protocol != "both" && y.Protocol != "both" && x.Protocol != y.Protocol {
		return false
	}
	return x.OutsidePort <= max(y.OutsidePort, y.OutsidePortEnd) && y.OutsidePort <= max(x.OutsidePort, x.OutsidePortEnd)
}

// portForwards are the enabled rules as the firewall needs them; a rule
// whose device has no fixed address (any more) is left out. The caller
// holds a.mu.
func (a *Agent) portForwards() []render.PortForward {
	var list []render.PortForward
	for _, r := range a.cfg.PortForwarding.Rules {
		_, address := a.forwardTarget(r)
		ip, err := netip.ParseAddr(address)
		if !r.Enabled || err != nil || !ip.Is4() {
			continue
		}
		protocols := []string{r.Protocol}
		if r.Protocol == "both" {
			protocols = []string{"tcp", "udp"}
		}
		for _, protocol := range protocols {
			list = append(list, render.PortForward{
				Protocol: protocol, From: r.OutsidePort, To: max(r.OutsidePort, r.OutsidePortEnd),
				Address: ip, InsidePort: r.InsidePort,
			})
		}
	}
	return list
}
