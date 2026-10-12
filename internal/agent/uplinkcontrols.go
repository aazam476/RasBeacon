package agent

// This file has the buttons on the Internet page:
//
//   - Disconnect: leave the network that carries the internet now. RasBeacon
//     stays away from it for uplinkPauseTime and uses another saved network
//     (or the other uplink) meanwhile. Saving the Wi-Fi networks, or Rescan
//     port for the cable, ends the pause early.
//   - Renew lease: ask the uplink's DHCP server for the address again.
//   - Rescan port: check the Ethernet port again (as if the cable had just
//     been plugged in).

import (
	"fmt"
	"log/slog"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/system"
	"rasbeacon/internal/uplink"
)

// uplinkPauseTime is how long Disconnect keeps RasBeacon off a network.
const uplinkPauseTime = 10 * time.Minute

// errNoUplink answers buttons that need an internet connection.
var errNoUplink = &config.ValidationError{Message: "RasBeacon isn't connected to the internet right now."}

// DisconnectUplink leaves the network that carries the internet now.
func (a *Agent) DisconnectUplink() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	status := a.uplink.status
	if !status.Connected {
		return errNoUplink
	}
	switch status.Kind {
	case "wifi":
		iface := status.Interface
		out, err := system.WPACommand(a.runner, a.paths.WPAControlDir(), iface, "status")
		id := uplink.ParseWPAStatus(out).NetworkID
		if err != nil || id == "" {
			return fmt.Errorf("asking wpa_supplicant which network is in use: %v", err)
		}
		if _, err := system.WPACommand(a.runner, a.paths.WPAControlDir(), iface, "disable_network", id); err != nil {
			return fmt.Errorf("leaving the Wi-Fi network: %w", err)
		}
		a.afterFunc(uplinkPauseTime, func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			// If wpa_supplicant restarted meanwhile (the networks were
			// saved), every network is enabled already; this does no harm.
			_, _ = system.WPACommand(a.runner, a.paths.WPAControlDir(), iface, "enable_network", id)
		})
	case "ethernet":
		if _, err := a.runner.Run("networkctl", "down", ethernetInterface); err != nil {
			return fmt.Errorf("disconnecting the cable: %w", err)
		}
		a.afterFunc(uplinkPauseTime, func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.cfg.Ethernet.Role == config.EthernetUplink {
				_, _ = a.runner.Run("networkctl", "up", ethernetInterface)
			}
		})
	default:
		return errNoUplink
	}
	slog.Info("uplink disconnected by the owner", "interface", status.Interface, "for", uplinkPauseTime)
	a.checkUplink()
	return nil
}

// RenewDHCPLease asks the uplink's DHCP server for the address again.
func (a *Agent) RenewDHCPLease() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	if !a.uplink.status.Connected || a.uplink.status.Interface == "" {
		return errNoUplink
	}
	if _, err := a.runner.Run("networkctl", "renew", a.uplink.status.Interface); err != nil {
		return fmt.Errorf("renewing the address: %w", err)
	}
	slog.Info("DHCP lease renewed", "interface", a.uplink.status.Interface)
	a.checkUplink()
	return nil
}

// RescanEthernetPort checks the Ethernet port again. As an uplink, the port
// is brought up (ending a Disconnect pause) and asks for an address again;
// as a client port, the safety check runs again.
func (a *Agent) RescanEthernetPort() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	if !a.hasEthernet() {
		return &config.ValidationError{Field: "ethernetRole", Message: "This device has no Ethernet port."}
	}
	if a.cfg.Ethernet.Role == config.EthernetUplink {
		if _, err := a.runner.Run("networkctl", "up", ethernetInterface); err != nil {
			return fmt.Errorf("bringing up the port: %w", err)
		}
		if _, err := a.runner.Run("networkctl", "reconfigure", ethernetInterface); err != nil {
			return fmt.Errorf("checking the port again: %w", err)
		}
	} else {
		a.uplink.ethernetCarrierChanges = "" // makes the safety check run now
	}
	slog.Info("Ethernet port checked again", "role", a.cfg.Ethernet.Role)
	a.checkUplink()
	return nil
}
