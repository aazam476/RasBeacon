package agent

// Checking a backup's settings before they are restored.
//
// A backup is a file, and it may have been made elsewhere or changed since.
// What it holds goes into the files rbd writes for other programs:
// hostapd's and wpa_supplicant's configuration, dnsmasq's, the firewall's.
// Each settings page checks what it saves; a restore saves every setting
// at once, so it checks them all the same way first. Plugins are checked
// by checkRestoredPlugins.

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"rasbeacon/internal/auth"
	"rasbeacon/internal/config"
	"rasbeacon/internal/notify"
	"rasbeacon/internal/radio"
)

// checkRestoredSettings checks a backup's settings (cfg, with FillDefaults
// applied, as rbd reads them after the restore) and passwords. staging is
// where the backup was unpacked: its Enterprise networks' certificates are
// there. The problem is named in words for the owner.
func (a *Agent) checkRestoredSettings(cfg config.Config, secrets config.Secrets, staging string) error {
	// A hash with settings argon2 can't use would lock the owner out.
	if _, err := auth.VerifyPassword("", secrets.AdminPasswordHash); err != nil {
		return errors.New("its admin password can't be used")
	}
	if cfg.Hostname != "" {
		if err := config.ValidateHostname(cfg.Hostname); err != nil {
			return fmt.Errorf("RasBeacon's name: %w", err)
		}
	}
	if err := config.ValidateWiFiPassword(secrets.HotspotPassword); err != nil {
		return fmt.Errorf("the hotspot's password: %w", err)
	}
	checks := []error{
		cfg.Hotspot.Validate(),
		cfg.Access.Validate(),
		cfg.Guest.Validate(secrets.GuestPassword, cfg.Hotspot.SSID),
		cfg.Ethernet.Validate(),
		cfg.Uplink.Validate(restoredCredentials(a, cfg, secrets, staging)),
		cfg.DNS.Validate(),
		cfg.Welcome.Validate(),
		cfg.Modem.Validate(),
		checkRestoredMAC(cfg.Uplink.MAC),
		checkRestoredVPN(cfg.VPN, secrets.WireGuardKeys),
		checkRestoredClients(cfg.Clients),
		checkRestoredQoS(cfg.QoS),
		checkRestoredRadios(cfg.Radios),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	if cfg.Updates.CheckHours != 0 && !slices.Contains(config.UpdateCheckHours, cfg.Updates.CheckHours) {
		return errors.New("how often it checks for updates isn't one of the choices")
	}
	if secrets.UpdateNotifyURL != "" {
		if err := notify.CheckURL(secrets.UpdateNotifyURL); err != nil {
			return fmt.Errorf("the address new versions are announced at: %w", err)
		}
	}
	if cfg.Swap.SizeGB != nil && *cfg.Swap.SizeGB < 0 {
		return errors.New("the swap file's size is below 0")
	}
	return nil
}

// restoredCredentials are the saved networks' passwords and certificates
// as the backup has them, for UplinkConfig.Validate.
func restoredCredentials(a *Agent, cfg config.Config, secrets config.Secrets, staging string) map[string]config.NetworkCredentials {
	// A certificate's place in the backup is its place under the data
	// folder (backupDirs).
	staged := func(path string) bool {
		rel, err := filepath.Rel(a.paths.DataDir(), path)
		if err != nil {
			return false
		}
		info, err := os.Lstat(filepath.Join(staging, rel))
		return err == nil && info.Mode().IsRegular()
	}
	credentials := map[string]config.NetworkCredentials{}
	for _, n := range cfg.Uplink.Networks {
		credentials[n.ID] = config.NetworkCredentials{
			Password:      secrets.UplinkPasswords[n.ID],
			HasCACert:     staged(a.paths.CACertFile(n.ID)),
			HasClientCert: staged(a.paths.ClientCertFile(n.ID)),
			HasClientKey:  staged(a.paths.ClientKeyFile(n.ID)),
		}
	}
	return credentials
}

// checkRestoredMAC checks the address the uplinks show their networks.
func checkRestoredMAC(choice config.MACChoice) error {
	if err := choice.Validate("mac"); err != nil {
		return fmt.Errorf("the address shown to networks: %w", err)
	}
	return nil
}

// checkRestoredVPN checks the VPN's profiles as the VPN page does when one
// is imported or changed (checkProfile), and that the one in use is one of
// them.
func checkRestoredVPN(vpn config.VPNConfig, keys map[string]config.WireGuardKeys) error {
	names := map[string]bool{}
	for _, p := range vpn.Profiles {
		if err := config.ValidateProfileName(p.Name); err != nil {
			return fmt.Errorf("a VPN profile's name: %w", err)
		}
		if names[p.Name] {
			return fmt.Errorf("two VPN profiles are called %q", p.Name)
		}
		names[p.Name] = true
		if _, err := checkProfile(p, keys[p.Name]); err != nil {
			return fmt.Errorf("the VPN profile %q: %w", p.Name, err)
		}
	}
	if vpn.Active != "" && !names[vpn.Active] {
		return errors.New("the VPN profile in use isn't one of its profiles")
	}
	return nil
}

// checkDeviceName checks a name the owner gave a device or a radio, by
// the rules of SetClientName and SetRadioName.
func checkDeviceName(name string) error {
	if utf8.RuneCountInString(name) > config.MaxDeviceNameLength || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("the device name %q can't be used", name)
	}
	return nil
}

// checkRestoredClients checks the devices the owner named, paused or gave
// a fixed address: each by its MAC address.
func checkRestoredClients(c config.ClientsConfig) error {
	for mac := range c.Paused {
		if _, err := config.NormalizeMAC(mac); err != nil {
			return fmt.Errorf("a paused device's address %q isn't a MAC address", mac)
		}
	}
	for mac, name := range c.Names {
		if _, err := config.NormalizeMAC(mac); err != nil {
			return fmt.Errorf("a named device's address %q isn't a MAC address", mac)
		}
		if err := checkDeviceName(name); err != nil {
			return err
		}
	}
	for mac, address := range c.FixedAddresses {
		if _, err := config.NormalizeMAC(mac); err != nil {
			return fmt.Errorf("a device with a fixed address has the address %q, which isn't a MAC address", mac)
		}
		if ip, err := netip.ParseAddr(address); err != nil || !ip.Is4() || ip.String() != address {
			return fmt.Errorf("the fixed address %q isn't an IPv4 address", address)
		}
	}
	if c.UsageLimitMB < 0 {
		return errors.New("the devices' data limit is below 0")
	}
	return nil
}

// checkRestoredQoS checks the priority devices and the connection's speeds.
func checkRestoredQoS(q config.QoSConfig) error {
	if len(q.Priority) > config.MaxPriorityDevices {
		return fmt.Errorf("at most %d devices can have priority", config.MaxPriorityDevices)
	}
	for _, mac := range q.Priority {
		if _, err := config.NormalizeMAC(mac); err != nil {
			return fmt.Errorf("a priority device's address %q isn't a MAC address", mac)
		}
	}
	for field, kbps := range map[string]int{"uploadKbps": q.UploadKbps, "downloadKbps": q.DownloadKbps} {
		if kbps != 0 {
			if err := config.ValidateSpeed(field, kbps); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkRestoredRadios checks the jobs and names given to Wi-Fi radios, by
// their MAC addresses. (Whether a radio can do its job is only known
// when it is plugged in; rbd checks that when it starts the radios.)
func checkRestoredRadios(r config.RadiosConfig) error {
	for mac, role := range r.Roles {
		if _, err := config.NormalizeMAC(mac); err != nil {
			return fmt.Errorf("a radio's address %q isn't a MAC address", mac)
		}
		switch radio.Role(role) {
		case radio.RoleHotspot, radio.RoleGuest, radio.RoleUplink, radio.RoleUnused:
		default:
			return fmt.Errorf("%q isn't a job for a radio", role)
		}
	}
	for mac, name := range r.Names {
		if _, err := config.NormalizeMAC(mac); err != nil {
			return fmt.Errorf("a radio's address %q isn't a MAC address", mac)
		}
		if err := checkDeviceName(name); err != nil {
			return err
		}
	}
	return nil
}
