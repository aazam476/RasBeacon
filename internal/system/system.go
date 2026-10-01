// Package system runs the Linux commands RasBeacon needs (ip, iw, nft,
// systemctl, ...) and logs each one.
//
// All commands go through the Runner interface. On the device, the real
// implementation (ExecRunner) runs them. In tests, a fake Runner records
// which commands would have run, so the logic can be tested on any computer
// without changing its network settings.
package system

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// Runner runs a command and returns its standard output.
type Runner interface {
	Run(name string, args ...string) (string, error)
}

// ExecRunner runs real commands.
type ExecRunner struct {
	// Timeout stops a command that hangs. Zero means 30 seconds.
	Timeout time.Duration
}

// Run executes name with args. If the command fails, the error includes what
// it printed to standard error, which usually explains the problem.
func (r ExecRunner) Run(name string, args ...string) (string, error) {
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	slog.Debug("running command", "command", name, "args", strings.Join(args, " "))
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("%s %s: %w: %s",
			name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// --- Helpers for common tasks, built on Runner ---

// ServiceRestart restarts a systemd service, e.g. "rasbeacon-hostapd.service".
func ServiceRestart(r Runner, unit string) error {
	_, err := r.Run("systemctl", "restart", unit)
	return err
}

// ServiceStop stops a systemd service.
func ServiceStop(r Runner, unit string) error {
	_, err := r.Run("systemctl", "stop", unit)
	return err
}

// ApplyFirewall loads an nftables ruleset file in one transaction.
func ApplyFirewall(r Runner, rulesFile string) error {
	_, err := r.Run("nft", "-f", rulesFile)
	return err
}

// LinkExists reports whether a network interface exists.
func LinkExists(r Runner, name string) bool {
	_, err := r.Run("ip", "link", "show", "dev", name)
	return err == nil
}

// RenameLink renames a network interface. The interface must be down while
// it is renamed.
func RenameLink(r Runner, from, to string) error {
	if _, err := r.Run("ip", "link", "set", "dev", from, "down"); err != nil {
		return err
	}
	_, err := r.Run("ip", "link", "set", "dev", from, "name", to)
	return err
}

// EnsureBridge creates the bridge (if missing), gives it the addresses (like
// "10.87.0.1/24") and brings it up. Running it again is harmless.
func EnsureBridge(r Runner, bridge string, addresses ...string) error {
	if !LinkExists(r, bridge) {
		if _, err := r.Run("ip", "link", "add", "name", bridge, "type", "bridge"); err != nil {
			return err
		}
	}
	// "replace" adds the address, or does nothing if it is already there.
	for _, address := range addresses {
		if _, err := r.Run("ip", "address", "replace", address, "dev", bridge); err != nil {
			return err
		}
	}
	_, err := r.Run("ip", "link", "set", "dev", bridge, "up")
	return err
}

// SetRegulatoryCountry tells the Wi-Fi drivers which country's rules apply.
func SetRegulatoryCountry(r Runner, country string) error {
	_, err := r.Run("iw", "reg", "set", country)
	return err
}

// PhyInfo returns `iw phy <phy> info` output.
func PhyInfo(r Runner, phy string) (string, error) {
	return r.Run("iw", "phy", phy, "info")
}

// StationDump returns `iw dev <iface> station dump` output.
func StationDump(r Runner, iface string) (string, error) {
	return r.Run("iw", "dev", iface, "station", "dump")
}

// SetLinkUp brings a network interface up.
func SetLinkUp(r Runner, name string) error {
	_, err := r.Run("ip", "link", "set", "dev", name, "up")
	return err
}

// AddToBridge makes an interface a port of a bridge, so devices on it share
// the bridge's network.
func AddToBridge(r Runner, iface, bridge string) error {
	_, err := r.Run("ip", "link", "set", "dev", iface, "master", bridge)
	return err
}

// RemoveFromBridge takes an interface out of whatever bridge it is in.
func RemoveFromBridge(r Runner, iface string) error {
	_, err := r.Run("ip", "link", "set", "dev", iface, "nomaster")
	return err
}

// FlushAddresses removes the IP addresses of one family ("-4" or "-6") from
// an interface.
func FlushAddresses(r Runner, family, iface string) error {
	_, err := r.Run("ip", family, "address", "flush", "dev", iface)
	return err
}

// ReloadNetworkd makes systemd-networkd re-read its .network files and apply
// any changes.
func ReloadNetworkd(r Runner) error {
	_, err := r.Run("networkctl", "reload")
	return err
}

// WPACommand runs a wpa_cli command against the wpa_supplicant controlling
// iface, for example WPACommand(r, dir, "wlan0", "status").
func WPACommand(r Runner, controlDir, iface string, args ...string) (string, error) {
	return r.Run("wpa_cli", append([]string{"-p", controlDir, "-i", iface}, args...)...)
}
