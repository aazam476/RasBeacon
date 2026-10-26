package agent

// This file manages the uplink: RasBeacon's connection to the internet,
// over Ethernet or Wi-Fi.
//
// # Who does what
//
//   - systemd-networkd gets addresses on the uplinks (DHCP for IPv4, router
//     advertisements for IPv6). rbd only writes its settings files.
//   - wpa_supplicant joins upstream Wi-Fi networks. It picks the most
//     preferred saved network that is in range. rbd writes its config.
//   - The kernel sends traffic out through the uplink with the best route.
//     Ethernet's routes have a lower "metric" than Wi-Fi's, so a plugged-in
//     cable wins.
//   - rbd checks the uplink every 5 seconds (checkUplink) and reacts:
//     moving the hotspot network if it clashes with the upstream network,
//     updating IPv6 announcements, and running the Ethernet client-port
//     safety check.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/dhcpprobe"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/render"
	"rasbeacon/internal/system"
	"rasbeacon/internal/uplink"
)

const (
	// ethernetInterface is the built-in Ethernet port on every supported Pi.
	ethernetInterface = "eth0"
	// wpaService is a systemd "template" unit: rasbeacon-wpa@wlan0.service
	// runs wpa_supplicant on wlan0.
	wpaService = "rasbeacon-wpa@%s.service"
	// deviceResolver is systemd-resolved's local DNS server. It asks the DNS
	// servers of whichever uplink is in use.
	deviceResolver = "127.0.0.53"
	// UplinkCheckInterval is how often RunUplinkMonitor calls checkUplink.
	UplinkCheckInterval = 5 * time.Second
	// dhcpProbeTimeout is how long the Ethernet safety check waits for a
	// DHCP server to answer. Real servers answer within a second.
	dhcpProbeTimeout = 3 * time.Second
	// scanWait is how long a Wi-Fi scan takes to collect results.
	scanWait = 4 * time.Second
)

// ErrNoWiFiUplink means there is no radio free to join a Wi-Fi network.
var ErrNoWiFiUplink = errors.New("there is no Wi-Fi radio available for joining a network (the only radio runs the hotspot)")

// uplinkState is what rbd knows about the uplink between checks.
type uplinkState struct {
	// status is the result of the last check, shown on the status page.
	status api.UplinkStatus
	// ethernetCarrierChanges is the Ethernet port's plug/unplug counter at
	// the last client-port safety check ("" = check again).
	ethernetCarrierChanges string
	// ethernetInBridge is true when Ethernet (client port) has joined the
	// hotspot network.
	ethernetInBridge bool
	// wiredSince is when each device on the Ethernet client port was first
	// seen (see wiredClients).
	wiredSince map[string]time.Time
	// foreignDHCPServer is the DHCP server found by the safety check.
	foreignDHCPServer string
	// connectedSince is when the uplink last became connected.
	connectedSince time.Time
}

// --- Which interfaces are uplinks ---

// wifiUplinkInterface returns the radio used to join upstream Wi-Fi, or ""
// if there is none.
func (a *Agent) wifiUplinkInterface() string {
	for _, r := range a.radios {
		if r.Role == radio.RoleUplink {
			return r.Interface
		}
	}
	return ""
}

// hasEthernet reports whether the device has an Ethernet port.
func (a *Agent) hasEthernet() bool {
	_, err := os.Stat(filepath.Join(a.paths.SysClassNet(), ethernetInterface))
	return err == nil
}

// uplinkInterfaces lists the interfaces that may carry devices' traffic to
// the internet.
func (a *Agent) uplinkInterfaces() []string {
	var names []string
	if a.hasEthernet() && a.cfg.Ethernet.Role == config.EthernetUplink {
		names = append(names, ethernetInterface)
	}
	if wifi := a.wifiUplinkInterface(); wifi != "" {
		names = append(names, wifi)
	}
	return names
}

// --- Applying the uplink settings ---

// applyUplinkConfig writes the settings for systemd-networkd and
// wpa_supplicant, and restarts only what actually changed (so saving an
// unrelated setting does not drop the Wi-Fi uplink).
func (a *Agent) applyUplinkConfig() error {
	if err := a.applyNetworkdConfig(); err != nil {
		return err
	}

	// Ethernet. In client-port mode networkd leaves the port alone, so rbd
	// brings it up itself and removes any address left from its time as an
	// uplink. It joins the hotspot network only after the safety check (see
	// checkEthernetClientPort).
	if a.hasEthernet() {
		if a.cfg.Ethernet.Role == config.EthernetClientPort {
			_ = system.FlushAddresses(a.runner, "-4", ethernetInterface)
			if err := system.SetLinkUp(a.runner, ethernetInterface); err != nil {
				slog.Warn("could not bring up Ethernet", "error", err)
			}
		} else if a.uplink.ethernetInBridge {
			if err := system.RemoveFromBridge(a.runner, ethernetInterface); err != nil {
				return err
			}
			a.uplink.ethernetInBridge = false
		}
		// Run the safety check again at the next check, in case the role
		// just changed.
		a.uplink.ethernetCarrierChanges = ""
		a.uplink.foreignDHCPServer = ""
	}

	// Wi-Fi uplink.
	wifi := a.wifiUplinkInterface()
	if wifi == "" {
		return nil
	}
	wpaInput := render.WPAInput{Country: a.cfg.Hotspot.Country}
	for _, n := range a.cfg.Uplink.Networks {
		wpaInput.Networks = append(wpaInput.Networks, render.WPANetwork{
			SSID:       n.SSID,
			Security:   n.Security,
			Password:   a.secrets.UplinkPasswords[n.ID],
			Hidden:     n.Hidden,
			RandomMAC:  n.RandomMAC,
			Enterprise: a.wpaEnterprise(n),
		})
	}
	wpaConf, err := render.WPASupplicant(wpaInput)
	if err != nil {
		return err
	}
	// The file holds Wi-Fi passwords, so only root may read it.
	changed, err := writeFileIfChanged(a.paths.WPASupplicantConfig(), wpaConf, 0o600)
	if err != nil {
		return err
	}
	if changed {
		if err := system.ServiceRestart(a.runner, fmt.Sprintf(wpaService, wifi)); err != nil {
			return fmt.Errorf("starting the Wi-Fi uplink: %w", err)
		}
	}
	return nil
}

// applyNetworkdConfig writes RasBeacon's systemd-networkd files and reloads
// networkd if any of them changed.
func (a *Agent) applyNetworkdConfig() error {
	files := map[string]string{}
	unmanaged := []string{render.BridgeName, radio.HotspotInterfaceName, render.GuestBridgeName, render.GuestInterface}

	if a.hasEthernet() {
		if a.cfg.Ethernet.Role == config.EthernetUplink {
			text, err := render.NetworkdUplink(ethernetInterface, render.EthernetRouteMetric)
			if err != nil {
				return err
			}
			files["00-rasbeacon-"+ethernetInterface+".network"] = text
		} else {
			unmanaged = append(unmanaged, ethernetInterface)
		}
	}
	if wifi := a.wifiUplinkInterface(); wifi != "" {
		text, err := render.NetworkdUplink(wifi, render.WiFiRouteMetric)
		if err != nil {
			return err
		}
		files["00-rasbeacon-"+wifi+".network"] = text
	}
	text, err := render.NetworkdUnmanaged(unmanaged)
	if err != nil {
		return err
	}
	files["00-rasbeacon-unmanaged.network"] = text

	dir := a.paths.NetworkdDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	anyChanged := false

	// Delete our files that are no longer wanted (for example Ethernet's
	// after switching it to client port).
	old, _ := filepath.Glob(filepath.Join(dir, "00-rasbeacon-*.network"))
	for _, path := range old {
		if _, wanted := files[filepath.Base(path)]; !wanted {
			if err := os.Remove(path); err != nil {
				return err
			}
			anyChanged = true
		}
	}
	for name, content := range files {
		changed, err := writeFileIfChanged(filepath.Join(dir, name), content, 0o644)
		if err != nil {
			return err
		}
		anyChanged = anyChanged || changed
	}
	if anyChanged {
		return system.ReloadNetworkd(a.runner)
	}
	return nil
}

// writeFileIfChanged writes content to path unless the file already holds
// exactly that. It reports whether the file was written.
func writeFileIfChanged(path, content string, mode os.FileMode) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return false, nil
	}
	return true, config.WriteFileAtomic(path, []byte(content), mode)
}

// enableForwarding lets the kernel pass packets between interfaces. It is
// off when the device boots (see image/.../90-rasbeacon.conf) and rbd only
// turns it on after loading the firewall rules, so packets are never
// forwarded without rules in place.
func (a *Agent) enableForwarding() error {
	if err := writeKernelSetting(a.paths.IPv4Forwarding(), "1"); err != nil {
		return fmt.Errorf("turning on IPv4 forwarding: %w", err)
	}
	if err := writeKernelSetting(a.paths.IPv6Forwarding(), "1"); err != nil {
		// Not fatal: devices still get IPv4.
		slog.Warn("could not turn on IPv6 forwarding", "error", err)
	}
	return nil
}

// writeKernelSetting writes one of the kernel's settings (a file under
// /proc/sys).
func writeKernelSetting(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value+"\n"), 0o644)
}

// --- The regular check ---

// RunUplinkMonitor checks the uplink every UplinkCheckInterval, forever.
// cmd/rbd starts it in the background after Start.
func (a *Agent) RunUplinkMonitor() {
	for {
		time.Sleep(UplinkCheckInterval)
		a.mu.Lock()
		a.checkUplink()
		a.checkHotspot()
		a.renewCertificateIfDue()
		a.sampleReadings()
		a.mu.Unlock()
	}
}

// checkUplink looks at the uplink and reacts to changes. The caller must
// hold a.mu.
func (a *Agent) checkUplink() {
	if a.mode != api.ModeNormal {
		return
	}
	previous := a.uplink.status

	// 1. Ethernet client port: only join the hotspot network if no DHCP
	//    server answers on it.
	a.checkEthernetClientPort()

	// 2. Look at routes, addresses and the Wi-Fi connection.
	status, upstreamIPv4 := a.observeUplink()
	a.uplink.status = status
	if status.Connected != previous.Connected || status.Interface != previous.Interface ||
		status.Address != previous.Address {
		slog.Info("uplink changed", "connected", status.Connected, "interface", status.Interface,
			"address", status.Address, "ipv6", status.IPv6)
		// A new network: look up the VPN server's address again at once.
		a.vpn.lastRefresh = time.Time{}
		a.uplink.connectedSince = a.now()
	}

	// 3. The VPN and its kill switch.
	if err := a.updateTunnel(); err != nil {
		slog.Error("could not update the VPN", "error", err)
	}

	// 4. If the upstream network uses the hotspot's addresses, move the
	//    hotspot network. Otherwise, update the IPv6 announcements if IPv6
	//    internet access came or went. (Moving the network also does that.)
	//    It only moves when it must: each move briefly disconnects every
	//    device (see moveLAN), so it doesn't move back when the clash ends.
	//    If every candidate clashes, it stays put rather than move in place.
	want := render.ChooseLANPrefix(upstreamIPv4)
	if render.LANOverlaps(a.lan.Prefix, upstreamIPv4) && want != a.lan.Prefix {
		a.moveLAN(want)
	} else if status.IPv6 != previous.IPv6 {
		if err := a.applyDHCP(false); err != nil {
			slog.Error("could not update IPv6 announcements", "error", err)
		}
	}
}

// observeUplink collects the uplink status, and the IPv4 networks of all
// uplinks (to detect clashes with the hotspot network).
func (a *Agent) observeUplink() (api.UplinkStatus, []netip.Prefix) {
	uplinks := a.uplinkInterfaces()
	s := api.UplinkStatus{}

	// The first default route through an uplink is the one in use (routes
	// come sorted best first).
	out, _ := a.runner.Run("ip", "-json", "route", "show", "default")
	routes, _ := uplink.ParseDefaultRoutes(out)
	for _, r := range routes {
		if slices.Contains(uplinks, r.Interface) {
			s.Connected, s.Interface, s.Gateway = true, r.Interface, r.Gateway
			break
		}
	}
	if s.Interface == ethernetInterface {
		s.Kind = "ethernet"
	} else if s.Interface != "" {
		s.Kind = "wifi"
	}

	// IPv6 internet access needs an IPv6 default route through an uplink.
	out, _ = a.runner.Run("ip", "-json", "-6", "route", "show", "default")
	routes6, _ := uplink.ParseDefaultRoutes(out)
	for _, r := range routes6 {
		if slices.Contains(uplinks, r.Interface) {
			s.IPv6 = true
		}
	}

	// Addresses on the uplinks.
	var upstreamIPv4 []netip.Prefix
	out, _ = a.runner.Run("ip", "-json", "address", "show")
	if addresses, err := uplink.ParseAddresses(out); err == nil {
		for _, name := range uplinks {
			for _, prefix := range addresses[name] {
				if !prefix.Addr().Is4() {
					continue
				}
				upstreamIPv4 = append(upstreamIPv4, prefix)
				if name == s.Interface && s.Address == "" {
					s.Address = prefix.Addr().String()
					s.SubnetMask = render.Netmask(prefix.Bits())
				}
			}
		}
	}

	if s.Interface != "" {
		s.InterfaceMAC = a.readSysNet(s.Interface, "address")
	}
	if wifi := a.wifiUplinkInterface(); wifi != "" {
		s.WiFi = a.observeWiFi(wifi)
	}
	s.Ethernet = api.EthernetUplinkStatus{
		Present:           a.hasEthernet(),
		Role:              a.cfg.Ethernet.Role,
		CablePlugged:      a.readSysNet(ethernetInterface, "carrier") == "1",
		ForeignDHCPServer: a.uplink.foreignDHCPServer,
	}
	if s.Ethernet.Present {
		s.Ethernet.LinkSpeed, s.Ethernet.Duplex = a.ethernetLink()
	}
	return s, upstreamIPv4
}

// observeWiFi asks wpa_supplicant what the Wi-Fi uplink is doing.
func (a *Agent) observeWiFi(iface string) api.WiFiUplinkStatus {
	s := api.WiFiUplinkStatus{Available: true, Interface: iface}
	out, err := system.WPACommand(a.runner, a.paths.WPAControlDir(), iface, "status")
	if err != nil {
		s.State = "off"
		return s
	}
	wpa := uplink.ParseWPAStatus(out)
	switch {
	case wpa.Connected():
		s.State = "connected"
		s.SSID = wpa.SSID
		s.Band = uplink.BandName(wpa.FrequencyMHz)
		s.BSSID, s.FrequencyMHz, s.Channel = wpa.BSSID, wpa.FrequencyMHz, wifiChannel(wpa.FrequencyMHz)
		poll, _ := system.WPACommand(a.runner, a.paths.WPAControlDir(), iface, "signal_poll")
		s.SignalDBM = uplink.ParseSignalPoll(poll)
	case len(a.cfg.Uplink.Networks) == 0:
		s.State = "not-configured"
	case wpa.LoginFailed:
		s.State = "login-failed"
		s.SSID = wpa.SSID
	case len(a.tempDisabledNetworks(iface)) > 0:
		// A saved network rejected the login and is paused.
		s.State = "login-failed"
		s.SSID = a.tempDisabledNetworks(iface)[0]
	case wpa.State == "SCANNING" || wpa.State == "DISCONNECTED" || wpa.State == "INACTIVE":
		s.State = "searching"
	default:
		s.State = "connecting"
	}
	return s
}

// tempDisabledNetworks lists saved networks wpa_supplicant paused after
// failed logins.
func (a *Agent) tempDisabledNetworks(iface string) []string {
	out, err := system.WPACommand(a.runner, a.paths.WPAControlDir(), iface, "list_networks")
	if err != nil {
		return nil
	}
	return uplink.ParseTempDisabled(out)
}

// checkEthernetClientPort is the safety check for Ethernet in client-port
// mode. Each time a cable is plugged in, the port is kept off the hotspot
// network until we know no DHCP server answers on it. If one answers, the
// cable probably leads to someone else's network (a hotel wall socket), and
// handing out our addresses there would break it.
func (a *Agent) checkEthernetClientPort() {
	if !a.hasEthernet() || a.cfg.Ethernet.Role != config.EthernetClientPort {
		return
	}
	// carrier_changes counts every plug and unplug. If it is unchanged since
	// the last check, the cable has not moved and nothing needs doing.
	changes := a.readSysNet(ethernetInterface, "carrier_changes")
	if changes == a.uplink.ethernetCarrierChanges {
		return
	}
	a.uplink.ethernetCarrierChanges = changes
	a.uplink.foreignDHCPServer = ""
	if a.uplink.ethernetInBridge {
		if err := system.RemoveFromBridge(a.runner, ethernetInterface); err != nil {
			slog.Error("could not take Ethernet off the hotspot network", "error", err)
			return
		}
		a.uplink.ethernetInBridge = false
	}
	if a.readSysNet(ethernetInterface, "carrier") != "1" {
		return // no cable: check again when one is plugged in
	}

	result, err := a.dhcpProbe(ethernetInterface, dhcpProbeTimeout)
	if err != nil {
		slog.Warn("Ethernet safety check failed; keeping the port off the hotspot network", "error", err)
		a.uplink.ethernetCarrierChanges = "" // try again at the next check
		return
	}
	if result.ServerFound {
		slog.Warn("a DHCP server answered on the Ethernet client port; not connecting it",
			"server", result.ServerAddress)
		a.uplink.foreignDHCPServer = result.ServerAddress
		return
	}
	if err := system.AddToBridge(a.runner, ethernetInterface, render.BridgeName); err != nil {
		slog.Error("could not connect Ethernet to the hotspot network", "error", err)
		a.uplink.ethernetCarrierChanges = ""
		return
	}
	a.uplink.ethernetInBridge = true
	slog.Info("Ethernet client port connected to the hotspot network")
}

// readSysNet reads one of an interface's files in /sys/class/net, e.g.
// "carrier". It returns "" if the file can't be read (for example "carrier"
// while the interface is down).
func (a *Agent) readSysNet(iface, file string) string {
	data, err := os.ReadFile(filepath.Join(a.paths.SysClassNet(), iface, file))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// moveLAN switches the hotspot to another network range because the
// upstream network uses the current one. Devices lose their old address and
// ask for a new one; RasBeacon's address changes too (see lanWarning).
func (a *Agent) moveLAN(prefix netip.Prefix) {
	slog.Warn("the upstream network uses the hotspot's addresses; moving the hotspot network",
		"from", a.lan.Prefix, "to", prefix)
	a.lan.Prefix = prefix
	if err := system.FlushAddresses(a.runner, "-4", render.BridgeName); err != nil {
		slog.Error("could not remove the old hotspot address", "error", err)
	}
	if err := system.EnsureBridge(a.runner, render.BridgeName, a.lanAddresses()...); err != nil {
		slog.Error("could not set the new hotspot address", "error", err)
	}
	if a.guestActive() {
		a.applyGuestBridge(true)
	}
	// The plugins' ports are forwarded from RasBeacon's address on the
	// hotspot, which just changed.
	if err := a.applyFirewall(); err != nil {
		slog.Error("could not update the firewall for the new hotspot network", "error", err)
	}
	if err := a.applyDHCP(false); err != nil {
		slog.Error("could not restart DHCP for the new hotspot network", "error", err)
	}
	// Devices still have addresses in the old network, and nothing tells
	// them it moved: they would stay connected to the Wi-Fi but reach
	// nothing, RasBeacon included, until their lease ran out. Restarting the
	// hotspot makes them reconnect and ask for a new address (dnsmasq,
	// being authoritative, refuses their old one at once).
	if err := system.ServiceRestart(a.runner, hostapdService); err != nil {
		slog.Error("could not restart the hotspot after moving its network", "error", err)
	}
}

// lanAddresses returns RasBeacon's addresses on the hotspot bridge.
func (a *Agent) lanAddresses() []string {
	addresses := []string{a.lan.Prefix.String()}
	if a.lan.IPv6Prefix.IsValid() {
		addresses = append(addresses, a.lan.IPv6Prefix.String())
	}
	return addresses
}

// uplinkWarnings explains uplink problems the owner can do something about.
func (a *Agent) uplinkWarnings() []string {
	var warnings []string
	if a.lan.Prefix != render.DefaultLAN.Prefix {
		warnings = append(warnings, fmt.Sprintf("The upstream network uses the same addresses as your hotspot, "+
			"so the hotspot network moved to %s. Devices get new addresses automatically. "+
			"These settings are now at http://%s/.", a.lan.Prefix.Masked(), a.lan.Address()))
	}
	if a.uplink.foreignDHCPServer != "" {
		warnings = append(warnings, "The Ethernet port is set to \"Client port\", but it seems to be plugged into a "+
			"network (a DHCP server answered), so it was not connected. If this cable is your internet "+
			"connection, set the Ethernet port to \"Uplink\" on the Internet page.")
	}
	return warnings
}

// --- Settings and scanning (called through the API) ---

// UplinkSettings returns the Internet page settings (without passwords).
func (a *Agent) UplinkSettings() api.UplinkSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	settings := api.UplinkSettings{
		EthernetRole:        a.cfg.Ethernet.Role,
		Networks:            []api.SavedNetwork{},
		EthernetPresent:     a.hasEthernet(),
		WiFiUplinkAvailable: a.wifiUplinkInterface() != "",
	}
	for _, n := range a.cfg.Uplink.Networks {
		settings.Networks = append(settings.Networks, api.SavedNetwork{
			ID:          n.ID,
			SSID:        n.SSID,
			Security:    n.Security,
			Hidden:      n.Hidden,
			RandomMAC:   n.RandomMAC,
			HasPassword: a.secrets.UplinkPasswords[n.ID] != "",
			Enterprise:  a.enterpriseSettings(n),
		})
	}
	return settings
}

// UpdateUplinkSettings saves the Ethernet role and the list of upstream
// Wi-Fi networks, then applies them. The request holds the complete list in
// order of preference: networks left out are forgotten.
func (a *Agent) UpdateUplinkSettings(req api.UplinkSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	newSecrets := a.secrets.Clone()

	newCfg.Ethernet.Role = req.EthernetRole
	if err := newCfg.Ethernet.Validate(); err != nil {
		return err
	}

	newCfg.Uplink.Networks = nil
	newSecrets.UplinkPasswords = map[string]string{}
	credentials := map[string]config.NetworkCredentials{}
	uploads := map[string]certUploads{}
	for i, n := range req.Networks {
		id := n.ID
		if id == "" {
			id = newNetworkID() // a network being added
		} else if !a.isSavedNetworkID(id) {
			return &config.ValidationError{Field: fmt.Sprintf("networks.%d.id", i), Message: "This network is not saved (reload the page)."}
		}
		if slices.Contains(a.ownSSIDs(), n.SSID) {
			return &config.ValidationError{Field: fmt.Sprintf("networks.%d.ssid", i),
				Message: "This is RasBeacon's own network. Choose the network RasBeacon should connect to."}
		}
		saved := config.SavedNetwork{ID: id, SSID: n.SSID, Security: n.Security, Hidden: n.Hidden, RandomMAC: n.RandomMAC}

		if n.Security == config.UplinkEnterprise && n.Enterprise != nil {
			e := n.Enterprise
			saved.Enterprise = &config.EnterpriseConfig{
				Method:                  e.Method,
				Identity:                e.Identity,
				AnonymousIdentity:       e.AnonymousIdentity,
				ServerName:              e.ServerName,
				AllowNoCertificateCheck: e.AllowNoCertificateCheck,
			}
			parsed, err := parseCertUploads(i, e)
			if err != nil {
				return err
			}
			uploads[id] = parsed
			// A login server trusted at the first connection stays trusted
			// while the network is the same one with no CA or name given.
			for _, old := range a.cfg.Uplink.Networks {
				if old.ID == id && old.SSID == n.SSID && old.Enterprise != nil && e.ServerName == "" && parsed.caCert == "" {
					saved.Enterprise.ServerCertHash = old.Enterprise.ServerCertHash
				}
			}
		}

		if n.Security == config.UplinkPersonal || n.Security == config.UplinkEnterprise {
			password := n.Password
			if password == "" {
				password = a.secrets.UplinkPasswords[id] // keep the saved one
			}
			if password != "" {
				newSecrets.UplinkPasswords[id] = password
			}
		}
		credentials[id] = a.credentialsFor(id, newSecrets.UplinkPasswords[id], uploads[id])
		newCfg.Uplink.Networks = append(newCfg.Uplink.Networks, saved)
	}
	if err := newCfg.Uplink.Validate(credentials); err != nil {
		return err
	}

	// Save certificates first, then secrets, then config.json (which is
	// what makes the new networks "exist"). A power cut in between leaves
	// at worst some unused certificate files.
	for id, u := range uploads {
		if err := a.saveCertUploads(id, u); err != nil {
			return err
		}
	}
	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	a.secrets = newSecrets
	a.removeUnusedCerts()
	slog.Info("uplink settings saved", "ethernetRole", newCfg.Ethernet.Role, "wifiNetworks", len(newCfg.Uplink.Networks))

	// The firewall's list of uplinks changes with the Ethernet role.
	if err := a.applyFirewall(); err != nil {
		return err
	}
	if err := a.applyUplinkConfig(); err != nil {
		return err
	}
	a.endUplinkPause()
	return nil
}

// isSavedNetworkID reports whether id belongs to a saved network.
func (a *Agent) isSavedNetworkID(id string) bool {
	for _, n := range a.cfg.Uplink.Networks {
		if n.ID == id {
			return true
		}
	}
	return false
}

// newNetworkID returns a random ID for a newly saved network.
func newNetworkID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ScanNearby lists the Wi-Fi networks the uplink radio can see. It takes a
// few seconds, during which other requests are not blocked.
func (a *Agent) ScanNearby() (api.ScanResponse, error) {
	a.mu.Lock()
	wifi := a.wifiUplinkInterface()
	own := a.ownSSIDs()
	mode := a.mode
	a.mu.Unlock()

	if mode != api.ModeNormal {
		return api.ScanResponse{}, ErrWrongMode
	}
	if wifi == "" {
		return api.ScanResponse{}, ErrNoWiFiUplink
	}
	// "scan" starts a scan and returns at once ("FAIL-BUSY" if one is
	// already running, which is fine: we just read its results).
	if _, err := system.WPACommand(a.runner, a.paths.WPAControlDir(), wifi, "scan"); err != nil {
		return api.ScanResponse{}, fmt.Errorf("starting a Wi-Fi scan: %w", err)
	}
	a.sleep(scanWait)
	out, err := system.WPACommand(a.runner, a.paths.WPAControlDir(), wifi, "scan_results")
	if err != nil {
		return api.ScanResponse{}, fmt.Errorf("reading scan results: %w", err)
	}
	networks := []uplink.NearbyNetwork{}
	for _, n := range uplink.ParseScanResults(out) {
		if !slices.Contains(own, n.SSID) { // RasBeacon's own networks are not uplinks
			networks = append(networks, n)
		}
	}
	return api.ScanResponse{Networks: networks}, nil
}

// ownSSIDs lists the names of the networks RasBeacon broadcasts itself:
// the hotspot, and the guest network while it is on. Joining one of them
// as the uplink would connect RasBeacon to itself. The caller holds a.mu.
func (a *Agent) ownSSIDs() []string {
	own := []string{a.cfg.Hotspot.SSID}
	if a.guestWanted() {
		own = append(own, a.cfg.Guest.SSID)
	}
	return own
}

// SetDHCPProbe replaces the Ethernet safety check's DHCP probe. The
// simulated device (devsim) uses this so nothing is sent on a real network.
func (a *Agent) SetDHCPProbe(probe func(iface string, timeout time.Duration) (dhcpprobe.Result, error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dhcpProbe = probe
}
