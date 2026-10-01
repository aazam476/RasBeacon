// Package paths lists every file and directory RasBeacon reads or writes on
// the device, in one place.
//
// Keeping all locations here means a reader can answer "where does RasBeacon
// keep X?" by reading one short file, and tests can point the whole program
// at a temporary directory by changing a single value (see Paths.Root).
//
// The device's disk layout (from the image build, see image/README.md):
//
//	/                read-only system (A/B slot, replaced by updates)
//	/bootfs          small FAT partition you can see from any computer after
//	                 flashing; holds setup.txt and factory-reset
//	/persistent      writable data partition that survives updates
//	/run             in-memory files, cleared at every boot
package paths

import "path/filepath"

// Paths holds the locations RasBeacon uses. Production code uses Default();
// tests build their own with every path under a temporary directory.
type Paths struct {
	// Root is prepended to every path below. It is "/" on the device and a
	// temporary directory in tests.
	Root string
}

// Default returns the real on-device locations.
func Default() Paths {
	return Paths{Root: "/"}
}

// join builds an absolute path under Root.
func (p Paths) join(parts ...string) string {
	return filepath.Join(append([]string{p.Root}, parts...)...)
}

// --- Persistent data (survives reboots and updates) ---

// DataDir is RasBeacon's directory on the persistent partition. A factory
// reset deletes everything inside it.
func (p Paths) DataDir() string { return p.join("persistent", "rasbeacon") }

// ConfigFile holds the non-secret settings (hotspot name, band, ...).
func (p Paths) ConfigFile() string { return filepath.Join(p.DataDir(), "config.json") }

// SecretsFile holds passwords and keys. Only root can read it (mode 0600).
func (p Paths) SecretsFile() string { return filepath.Join(p.DataDir(), "secrets.json") }

// IdentityFile holds values generated once on first boot (device ID, ...).
func (p Paths) IdentityFile() string { return filepath.Join(p.DataDir(), "identity.json") }

// SSHAuthorizedKeysFile holds the public keys allowed to log in over SSH
// when SSH is turned on in the web UI.
func (p Paths) SSHAuthorizedKeysFile() string {
	return filepath.Join(p.DataDir(), "ssh", "authorized_keys")
}

// --- Files the owner can drop onto the SD card from a computer ---

// BootConfigDir is the FAT partition visible on a Mac or PC after flashing.
func (p Paths) BootConfigDir() string { return p.join("bootfs") }

// SetupTxtFile optionally protects the first-boot setup network with a
// password. See internal/setupfile for the format.
func (p Paths) SetupTxtFile() string { return filepath.Join(p.BootConfigDir(), "setup.txt") }

// FactoryResetFile, if present at boot, wipes all settings.
func (p Paths) FactoryResetFile() string {
	return filepath.Join(p.BootConfigDir(), "factory-reset")
}

// ClockFile holds the last known time (seconds since 1970), saved
// regularly so the clock never goes backwards across a reboot.
func (p Paths) ClockFile() string { return filepath.Join(p.DataDir(), "clock") }

// CertDir holds the certificates and keys uploaded for WPA-Enterprise
// networks. Each file name starts with the network's ID.
func (p Paths) CertDir() string { return filepath.Join(p.DataDir(), "certs") }

// CACertFile is the CA certificate that a network's login server must be
// signed by.
func (p Paths) CACertFile(networkID string) string {
	return filepath.Join(p.CertDir(), networkID+"-ca.pem")
}

// ClientCertFile is the owner's personal certificate for EAP-TLS.
func (p Paths) ClientCertFile(networkID string) string {
	return filepath.Join(p.CertDir(), networkID+"-client.pem")
}

// ClientKeyFile is the private key for ClientCertFile (root-only).
func (p Paths) ClientKeyFile(networkID string) string {
	return filepath.Join(p.CertDir(), networkID+"-key.pem")
}

// --- Runtime files (in memory, rebuilt on every boot) ---

// RunDir holds generated configs and the API socket.
func (p Paths) RunDir() string { return p.join("run", "rbd") }

// APISocket is the local socket rbweb uses to talk to rbd.
func (p Paths) APISocket() string { return filepath.Join(p.RunDir(), "api.sock") }

// HostapdConfig is the generated hostapd (hotspot) configuration.
func (p Paths) HostapdConfig() string { return filepath.Join(p.RunDir(), "hostapd.conf") }

// DnsmasqConfig is the generated DHCP/DNS server configuration.
func (p Paths) DnsmasqConfig() string { return filepath.Join(p.RunDir(), "dnsmasq.conf") }

// DnsmasqLeases lists the addresses handed out to connected devices.
func (p Paths) DnsmasqLeases() string { return filepath.Join(p.RunDir(), "dnsmasq.leases") }

// FirewallRules is the generated nftables ruleset.
func (p Paths) FirewallRules() string { return filepath.Join(p.RunDir(), "firewall.nft") }

// WPASupplicantConfig is the generated configuration for joining upstream
// Wi-Fi networks. It holds their passwords, so it is root-only.
func (p Paths) WPASupplicantConfig() string { return filepath.Join(p.RunDir(), "wpa_supplicant.conf") }

// WPAControlDir holds wpa_supplicant's control sockets (used by wpa_cli).
func (p Paths) WPAControlDir() string { return p.join("run", "wpa_supplicant") }

// WireGuardConfig is the generated WireGuard tunnel configuration. It holds
// the private key, so it is root-only.
func (p Paths) WireGuardConfig() string { return filepath.Join(p.RunDir(), "wg0.conf") }

// NetworkdDir holds generated systemd-networkd settings. Files here only
// last until the next reboot, which is what we want: rbd writes them again
// at every start.
func (p Paths) NetworkdDir() string { return p.join("run", "systemd", "network") }

// --- Kernel settings ---

// IPv4Forwarding switches routing of IPv4 packets between interfaces on
// ("1") or off ("0").
func (p Paths) IPv4Forwarding() string { return p.join("proc", "sys", "net", "ipv4", "ip_forward") }

// IPv6Forwarding is the same for IPv6.
func (p Paths) IPv6Forwarding() string {
	return p.join("proc", "sys", "net", "ipv6", "conf", "all", "forwarding")
}

// --- Read-only system information ---

// SysClassNet lists network interfaces (Linux sysfs).
func (p Paths) SysClassNet() string { return p.join("sys", "class", "net") }

// SysClassLeds lists the board's LEDs (used to blink error codes).
func (p Paths) SysClassLeds() string { return p.join("sys", "class", "leds") }

// DeviceTreeModel names the board, e.g. "Raspberry Pi 4 Model B Rev 1.5".
func (p Paths) DeviceTreeModel() string { return p.join("proc", "device-tree", "model") }

// ProcUptime holds the seconds since the device booted.
func (p Paths) ProcUptime() string { return p.join("proc", "uptime") }
