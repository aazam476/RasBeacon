package agent

// Mobile broadband modems that need a modem manager (QMI and MBIM: most
// M.2 and many 5G USB modems). ModemManager talks to the modem; rbd tells
// it what to do through mmcli, once per uplink check:
//
//   - a SIM locked with a PIN gets the saved PIN, once. If the SIM refuses
//     it, RasBeacon never tries it again until the owner saves another, so
//     it can't use up the SIM's attempts (three wrong PINs block the SIM);
//   - a modem that is off is turned on;
//   - a modem on a network is connected with the saved APN;
//   - a connected modem's data port gets the address its network gave
//     (systemd-networkd), and becomes an uplink used after Ethernet, Wi-Fi
//     and phones.

import (
	"log/slog"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/modem"
	"rasbeacon/internal/render"
)

// modemConnectEvery is how often a modem that is on a network but not
// connected is asked to connect again.
const modemConnectEvery = 30 * time.Second

// modemState is what rbd knows about the modem between checks.
type modemState struct {
	// status is the last check's result for the pages (nil: no modem).
	status *api.ModemStatus
	// pinTried is true once the saved PIN was sent; pinRefused when the
	// SIM refused it. Both reset only when the owner saves a PIN.
	pinTried, pinRefused bool
	// lastConnect is when the modem was last asked to connect.
	lastConnect time.Time
	// connectError is the last connection attempt's problem.
	connectError string
	// iface and network are the data port in use and its networkd file
	// ("" while it isn't connected).
	iface, network string
}

// ModemSettings is the modem's settings, without its secrets.
func (a *Agent) ModemSettings() api.ModemSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return api.ModemSettings{
		APN: a.cfg.Modem.APN, Username: a.cfg.Modem.Username,
		HasPassword: a.secrets.ModemPassword != "", HasPIN: a.secrets.ModemPIN != "",
	}
}

// UpdateModemSettings saves the modem's settings. A new PIN may be tried
// (once) at the next check.
func (a *Agent) UpdateModemSettings(req api.ModemSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	m := config.ModemConfig{APN: strings.TrimSpace(req.APN), Username: strings.TrimSpace(req.Username)}
	if err := m.Validate(); err != nil {
		return err
	}
	pin := strings.TrimSpace(req.PIN)
	if pin != "" && (len(pin) < 4 || len(pin) > 8 || strings.Trim(pin, "0123456789") != "") {
		return &config.ValidationError{Field: "pin", Message: "A SIM PIN is 4 to 8 digits."}
	}
	if strings.ContainsAny(req.Password, ",=\"'\n\r") || len(req.Password) > 64 {
		return &config.ValidationError{Field: "password", Message: "Use up to 64 characters, without commas, quotes or =."}
	}
	newCfg := a.cfg.Clone()
	newCfg.Modem = m
	newSecrets := a.secrets.Clone()
	switch {
	case req.ForgetPassword:
		newSecrets.ModemPassword = ""
	case req.Password != "":
		newSecrets.ModemPassword = req.Password
	}
	switch {
	case req.ForgetPIN:
		newSecrets.ModemPIN = ""
	case pin != "":
		newSecrets.ModemPIN = pin
		// A new PIN may be tried, once.
		a.modem.pinTried, a.modem.pinRefused = false, false
	}
	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg, a.secrets = newCfg, newSecrets
	a.modem.lastConnect = time.Time{} // connect with the new settings at once
	slog.Info("modem settings saved", "apn", m.APN)
	return nil
}

// checkModem looks at the modem and takes the next step (see the top of
// this file). The uplink monitor calls it; the caller holds a.mu.
func (a *Agent) checkModem() {
	out, err := a.runner.Run("mmcli", "-L", "-J")
	indexes, _ := modem.List(out)
	if err != nil || len(indexes) == 0 {
		a.modem.status = nil
		a.setModemUplink("", "")
		return
	}
	out, err = a.runner.Run("mmcli", "-m", indexes[0], "-J")
	m, perr := modem.Parse(out)
	if err != nil || perr != nil {
		a.modem.status = &api.ModemStatus{Name: "Mobile modem", State: "starting", PINTries: -1}
		return
	}
	status := &api.ModemStatus{
		Name: m.Name(), Operator: m.Operator, Network: m.Network(), SignalPercent: m.SignalPercent,
		PINTries: m.PINTries, Error: a.modem.connectError,
	}
	a.modem.status = status
	connected := ""
	network := ""

	switch m.State {
	case "locked":
		status.State = a.unlockModem(m)
	case "failed":
		status.State = "failed"
		if m.FailedReason == "sim-missing" {
			status.State = "no-sim"
		}
	case "disabled":
		status.State = "starting"
		if _, err := a.runner.Run("mmcli", "-m", m.Index, "-e"); err != nil {
			slog.Warn("could not turn the modem on", "error", err)
		}
	case "registered":
		status.State = "connecting"
		a.connectModem(m)
	case "connecting":
		status.State = "connecting"
	case "connected":
		status.State = "connected"
		connected, network = a.modemNetwork(m)
		if connected == "" {
			status.State = "connecting"
		}
	case "searching", "enabled", "enabling":
		status.State = "searching"
	default:
		status.State = "starting"
	}
	a.setModemUplink(connected, network)
}

// unlockModem sends the saved PIN to a locked SIM, once, and says what the
// page shows.
func (a *Agent) unlockModem(m modem.Modem) string {
	switch {
	case m.UnlockRequired == "sim-puk":
		return "puk-needed"
	case m.UnlockRequired != "sim-pin":
		return "starting"
	case a.modem.pinRefused:
		return "pin-refused"
	case a.secrets.ModemPIN == "" || a.modem.pinTried:
		return "pin-needed"
	}
	a.modem.pinTried = true
	if _, err := a.runner.Run("mmcli", "-i", m.SIM, "--pin="+a.secrets.ModemPIN); err != nil {
		// Never again with this PIN: each wrong one uses up an attempt.
		a.modem.pinRefused = true
		slog.Warn("the SIM refused the saved PIN; waiting for a new one", "triesLeft", m.PINTries-1)
		return "pin-refused"
	}
	return "starting"
}

// connectModem asks a modem on a network to connect, at most every
// modemConnectEvery.
func (a *Agent) connectModem(m modem.Modem) {
	if a.now().Sub(a.modem.lastConnect) < modemConnectEvery {
		return
	}
	a.modem.lastConnect = a.now()
	settings := []string{"ip-type=ipv4"}
	if a.cfg.Modem.APN != "" {
		settings = append(settings, "apn="+a.cfg.Modem.APN)
	}
	if a.cfg.Modem.Username != "" {
		settings = append(settings, "user="+a.cfg.Modem.Username)
	}
	if a.secrets.ModemPassword != "" {
		settings = append(settings, "password="+a.secrets.ModemPassword)
	}
	out, err := a.runner.Run("mmcli", "-m", m.Index, "--simple-connect="+strings.Join(settings, ","))
	if err != nil {
		a.modem.connectError = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "error:"))
		if a.modem.connectError == "" {
			a.modem.connectError = err.Error()
		}
		slog.Warn("the modem could not connect", "error", a.modem.connectError)
		return
	}
	a.modem.connectError = ""
}

// modemNetwork reads a connected modem's data connection and returns its
// port and the networkd file that sets it up ("" while it isn't ready).
func (a *Agent) modemNetwork(m modem.Modem) (iface, network string) {
	for _, index := range m.Bearers {
		out, err := a.runner.Run("mmcli", "-b", index, "-J")
		if err != nil {
			continue
		}
		b, err := modem.ParseBearer(out)
		if err != nil || !b.Connected || b.Interface == "" {
			continue
		}
		var text string
		if b.Method == "dhcp" {
			text, err = render.NetworkdUplink(b.Interface, render.ModemRouteMetric)
		} else {
			text, err = render.NetworkdStaticUplink(render.StaticUplink{
				Interface: b.Interface, Address: b.Address, Gateway: b.Gateway, DNS: b.DNS, MTU: b.MTU,
				RouteMetric: render.ModemRouteMetric,
			})
		}
		if err != nil {
			slog.Warn("could not use the modem's connection", "error", err)
			continue
		}
		return b.Interface, text
	}
	return "", ""
}

// setModemUplink makes iface (with its networkd file) the modem's uplink,
// or none (""), and updates networkd and the firewall when that changes.
func (a *Agent) setModemUplink(iface, network string) {
	if iface == a.modem.iface && network == a.modem.network {
		return
	}
	a.modem.iface, a.modem.network = iface, network
	slog.Info("modem connection changed", "interface", iface)
	if err := a.applyNetworkdConfig(); err != nil {
		slog.Error("could not set up the modem's connection", "error", err)
	}
	if err := a.applyFirewall(); err != nil {
		slog.Error("could not update the firewall for the modem", "error", err)
	}
}
