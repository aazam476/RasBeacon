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

// SwapFile is the swap file, on the data partition but outside DataDir
// (see agent/swap.go).
func (p Paths) SwapFile() string { return p.join("persistent", "swapfile") }

// ProcSwaps lists the swap areas in use.
func (p Paths) ProcSwaps() string { return p.join("proc", "swaps") }

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

// RememberedLoginsFile lists the browsers that chose "Stay logged in" (see
// agent/remember.go).
func (p Paths) RememberedLoginsFile() string {
	return filepath.Join(p.DataDir(), "remembered-logins.json")
}

// ClockFile holds the last known time (seconds since 1970), saved
// regularly so the clock never goes backwards across a reboot.
func (p Paths) ClockFile() string { return filepath.Join(p.DataDir(), "clock") }

// TLSDir holds the web UI's HTTPS certificate and key (cert.pem, key.pem).
func (p Paths) TLSDir() string { return filepath.Join(p.DataDir(), "tls") }

// RunTLSDir is where rbd puts a copy of the certificate and key for rbweb,
// readable by rbweb's group. rbweb serves HTTPS only if they exist.
func (p Paths) RunTLSDir() string { return filepath.Join(p.RunDir(), "tls") }

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

// BrowserDir holds the login browser's temporary profiles (see
// internal/browser), deleted when it closes.
func (p Paths) BrowserDir() string { return filepath.Join(p.RunDir(), "browser") }

// APISocket is the local socket rbweb uses to talk to rbd.
func (p Paths) APISocket() string { return filepath.Join(p.RunDir(), "api.sock") }

// HostapdConfig is the generated hostapd (hotspot) configuration.
func (p Paths) HostapdConfig() string { return filepath.Join(p.RunDir(), "hostapd.conf") }

// DnsmasqConfig is the generated DHCP/DNS server configuration.
func (p Paths) DnsmasqConfig() string { return filepath.Join(p.RunDir(), "dnsmasq.conf") }

// DoHEnv is the DoH forwarder's settings (see cmd/rbdoh).
func (p Paths) DoHEnv() string { return filepath.Join(p.RunDir(), "doh.env") }

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

// ProcSysNetIPv6 exists only when the kernel has IPv6 switched on.
func (p Paths) ProcSysNetIPv6() string { return p.join("proc", "sys", "net", "ipv6") }

// IPv6Forwarding is the same for IPv6.
func (p Paths) IPv6Forwarding() string {
	return p.join("proc", "sys", "net", "ipv6", "conf", "all", "forwarding")
}

// --- Updates (A/B slots) ---

// UpdateKeysDir holds the public keys (*.pub) that update files must be
// signed with. They are part of the image.
func (p Paths) UpdateKeysDir() string { return p.join("usr", "lib", "rasbeacon", "update-keys") }

// BoardFile names the image's board ("rpi64" or "rpi3"), written at build
// time.
func (p Paths) BoardFile() string { return p.join("usr", "lib", "rasbeacon", "board") }

// SlotDevice is a partition of the running ("active") or the other A/B
// slot, e.g. SlotDevice("other", "system").
func (p Paths) SlotDevice(slot, part string) string {
	return p.join("dev", "disk", "by-slot", slot, part)
}

// SingleSystemDevice is the boot or system partition of an image with one
// copy of the system (the Pi Zero 2 W/3B/3B+ image), e.g.
// SingleSystemDevice("system").
func (p Paths) SingleSystemDevice(part string) string {
	return p.join("dev", "disk", "by-slot", part)
}

// PersistentDir holds everything RasBeacon writes: its own partition on the
// Pi 4/5 image, a folder on the system partition on the Pi Zero 2 W/3B/3B+ image.
func (p Paths) PersistentDir() string { return p.join("persistent") }

// TrybootFlag is non-zero when the bootloader started the system "on
// trial" (tryboot), after an update.
func (p Paths) TrybootFlag() string {
	return p.join("proc", "device-tree", "chosen", "bootloader", "tryboot")
}

// AutobootFile tells the bootloader which slot to start.
func (p Paths) AutobootFile() string { return filepath.Join(p.BootConfigDir(), "autoboot.txt") }

// UpdatePendingFile remembers an installed update until it is confirmed.
func (p Paths) UpdatePendingFile() string { return filepath.Join(p.DataDir(), "update-pending.json") }

// --- Read-only system information ---

// SysClassNet lists network interfaces (Linux sysfs).
func (p Paths) SysClassNet() string { return p.join("sys", "class", "net") }

// SysClassInput lists input devices (used to find the reset button).
func (p Paths) SysClassInput() string { return p.join("sys", "class", "input") }

// DevInput holds the input devices' event files.
func (p Paths) DevInput() string { return p.join("dev", "input") }

// SysClassLeds lists the board's LEDs (used to blink error codes).
func (p Paths) SysClassLeds() string { return p.join("sys", "class", "leds") }

// DeviceTreeModel names the board, e.g. "Raspberry Pi 4 Model B Rev 1.5".
func (p Paths) DeviceTreeModel() string { return p.join("proc", "device-tree", "model") }

// ProcUptime holds the seconds since the device booted.
func (p Paths) ProcUptime() string { return p.join("proc", "uptime") }

// --- Hardware readings (see internal/readings) ---

// ProcStat holds the processor's time counters.
func (p Paths) ProcStat() string { return p.join("proc", "stat") }

// ProcMeminfo describes memory and swap.
func (p Paths) ProcMeminfo() string { return p.join("proc", "meminfo") }

// ProcCPUInfo describes the processor cores.
func (p Paths) ProcCPUInfo() string { return p.join("proc", "cpuinfo") }

// ProcMounts lists mounted filesystems.
func (p Paths) ProcMounts() string { return p.join("proc", "mounts") }

// KernelRelease holds the kernel version, e.g. "6.12.34+rpt-rpi-v8".
func (p Paths) KernelRelease() string { return p.join("proc", "sys", "kernel", "osrelease") }

// SysClassThermal holds the temperature sensors and cooling devices.
func (p Paths) SysClassThermal() string { return p.join("sys", "class", "thermal") }

// ThrottledFlags holds the Raspberry Pi firmware's throttling flags.
func (p Paths) ThrottledFlags() string {
	return p.join("sys", "devices", "platform", "soc", "soc:firmware", "get_throttled")
}

// SysClassBlock describes disks and partitions (sizes in 512-byte sectors).
func (p Paths) SysClassBlock() string { return p.join("sys", "class", "block") }

// NetworkdLeases holds systemd-networkd's DHCP leases, one file per
// interface number.
func (p Paths) NetworkdLeases() string { return p.join("run", "systemd", "netif", "leases") }
