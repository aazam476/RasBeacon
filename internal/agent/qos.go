package agent

// Priority devices ("quality of service"): the owner's chosen devices go
// first when the internet connection is busy. The firewall sorts their
// traffic into a class (applyFirewall, render.QoSRule) and tc queues send
// each class at the connection's speed (refreshQoS, render.QoS); see
// internal/render/qos.go for how the two fit together.

import (
	"fmt"
	"log/slog"
	"slices"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
)

// qosState is what rbd last did with the queues.
type qosState struct {
	// applied is what the queues were set up for (nothing when off).
	applied render.QoSInput
	// key is applied plus the interfaces' index numbers: an interface
	// that was removed and made again (the VPN tunnel after a restart, a
	// phone plugged back in) has a new number and no queue any more.
	key string
	// problem is why the queues couldn't be set up ("" when they work).
	problem string
}

// qosInput is where and how fast to shape now: nothing before setup or
// without priority devices (priority means nothing without them, and
// nobody's traffic is slowed down for it).
func (a *Agent) qosInput() render.QoSInput {
	if a.mode != api.ModeNormal || len(a.cfg.QoS.Priority) == 0 {
		return render.QoSInput{}
	}
	in := render.QoSInput{UploadKbps: a.cfg.QoS.UploadKbps, DownloadKbps: a.cfg.QoS.DownloadKbps}
	for _, dev := range a.egressInterfaces() {
		// The VPN tunnel may not be there yet; the uplink monitor sets its
		// queue up once it is (refreshQoS).
		if a.readSysNet(dev, "ifindex") != "" {
			in.Upload = append(in.Upload, dev)
		}
	}
	in.LANs = []string{render.BridgeName}
	if a.guestActive() {
		in.LANs = append(in.LANs, render.GuestBridgeName)
	}
	return in
}

// qosRule is the priority devices' part of the firewall, or nil.
func (a *Agent) qosRule() *render.QoSRule {
	in := a.qosInput()
	if !in.Active() {
		return nil
	}
	return &render.QoSRule{
		PriorityMACs: a.cfg.QoS.Priority,
		Upload:       in.UploadKbps > 0,
		Download:     in.DownloadKbps > 0,
	}
}

// qosKey describes in together with its interfaces' index numbers. It is
// "" when nothing is shaped.
func (a *Agent) qosKey(in render.QoSInput) string {
	if !in.Active() {
		return ""
	}
	key := fmt.Sprint(in)
	for _, dev := range append(slices.Clone(in.Upload), in.LANs...) {
		key += " " + dev + "=" + a.readSysNet(dev, "ifindex")
	}
	return key
}

// refreshQoS sets the queues up for the current settings and interfaces,
// when they changed. applyFirewall and the uplink monitor call it; the
// caller holds a.mu.
func (a *Agent) refreshQoS() {
	in := a.qosInput()
	key := a.qosKey(in)
	if key == a.qos.key {
		return
	}
	a.runAll(render.QoSRemove(a.qos.applied))
	a.qos = qosState{key: key}
	for _, c := range render.QoS(in) {
		if _, err := a.runner.Run(c.Args[0], c.Args[1:]...); err != nil && !c.Optional {
			slog.Error("priority devices: could not set up the queues", "command", c.String(), "error", err)
			a.qos.problem = "RasBeacon couldn't set up its queues, so priority devices don't go first. The diagnostics report has the details."
			a.runAll(render.QoSRemove(in))
			return
		}
	}
	a.qos.applied = in
	if in.Active() {
		slog.Info("priority devices: queues set up", "upload", in.Upload, "uploadKbps", in.UploadKbps, "downloadKbps", in.DownloadKbps)
	}
}

// runAll runs commands that may fail (removing queues that may be gone).
func (a *Agent) runAll(commands []render.Command) {
	for _, c := range commands {
		a.runner.Run(c.Args[0], c.Args[1:]...)
	}
}

// SetClientPriority makes a device a priority device, or an ordinary one.
func (a *Agent) SetClientPriority(mac string, priority bool) error {
	mac, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	if slices.Contains(a.cfg.QoS.Priority, mac) == priority {
		return nil
	}
	newCfg := a.cfg.Clone()
	if priority {
		if len(newCfg.QoS.Priority) >= config.MaxPriorityDevices {
			return &config.ValidationError{Field: "priority",
				Message: fmt.Sprintf("At most %d devices can be priority devices. Turn it off for another one first.", config.MaxPriorityDevices)}
		}
		newCfg.QoS.Priority = append(newCfg.QoS.Priority, mac)
		slices.Sort(newCfg.QoS.Priority)
	} else {
		newCfg.QoS.Priority = slices.DeleteFunc(newCfg.QoS.Priority, func(m string) bool { return m == mac })
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("priority device changed", "mac", mac, "priority", priority)
	return a.applyFirewall()
}

// QoSSettings returns the connection's speeds and what priority does now.
func (a *Agent) QoSSettings() api.QoSSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.qosSettings()
}

func (a *Agent) qosSettings() api.QoSSettings {
	return api.QoSSettings{
		UploadKbps:      a.cfg.QoS.UploadKbps,
		DownloadKbps:    a.cfg.QoS.DownloadKbps,
		PriorityDevices: len(a.cfg.QoS.Priority),
		Active:          a.qos.applied.Active(),
		Problem:         a.qos.problem,
	}
}

// UpdateQoSSettings saves the connection's speeds (0: not set).
func (a *Agent) UpdateQoSSettings(req api.QoSSettings) (api.QoSSettings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return api.QoSSettings{}, ErrWrongMode
	}
	if err := config.ValidateSpeed("uploadKbps", req.UploadKbps); err != nil {
		return api.QoSSettings{}, err
	}
	if err := config.ValidateSpeed("downloadKbps", req.DownloadKbps); err != nil {
		return api.QoSSettings{}, err
	}
	newCfg := a.cfg.Clone()
	newCfg.QoS.UploadKbps = req.UploadKbps
	newCfg.QoS.DownloadKbps = req.DownloadKbps
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return api.QoSSettings{}, err
	}
	a.cfg = newCfg
	if err := a.applyFirewall(); err != nil {
		return api.QoSSettings{}, err
	}
	return a.qosSettings(), nil
}
