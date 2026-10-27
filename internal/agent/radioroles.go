package agent

// Which Wi-Fi radio does what. RasBeacon picks by itself (radio.AssignRoles),
// but the owner can choose on the System page: which radio runs the
// hotspot, which joins the internet, and whether the guest network gets a
// radio of its own. Radios are remembered by their own MAC address.
//
// A change restarts rbd, which detects the radios again and renames them
// (nameRadios). Picking the wrong radio could leave the owner without a
// hotspot to reach the settings from, so the change is pending until the
// owner keeps it: RasBeacon goes back to the previous roles by itself when
// nobody does within RadioRolesConfirmTime.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/radio"
	"rasbeacon/internal/render"
	"rasbeacon/internal/system"
)

// RadioRolesConfirmTime is how long the owner has to keep new radio roles
// after rbd restarted with them.
const RadioRolesConfirmTime = 3 * time.Minute

// radioChoices is the owner's saved choices, by radio MAC address.
func (a *Agent) radioChoices() map[string]radio.Role {
	choices := map[string]radio.Role{}
	for mac, role := range a.cfg.Radios.Roles {
		choices[mac] = radio.Role(role)
	}
	return choices
}

// radioWithRole returns the radio with role, or nil.
func (a *Agent) radioWithRole(role radio.Role) *radio.Radio {
	for i := range a.radios {
		if a.radios[i].Role == role {
			return &a.radios[i]
		}
	}
	return nil
}

// guestRadio is the radio that runs the guest network on its own, or nil
// (the guest network then shares the hotspot's radio).
func (a *Agent) guestRadio() *radio.Radio {
	return a.radioWithRole(radio.RoleGuest)
}

// nameRadios gives the hotspot radio the name "ap0" and a guest radio
// "guest0", as the rest of RasBeacon expects. A radio that still has one
// of these names from before a change of roles is renamed first
// ("wlanp1" for phy1), and hostapd, which uses them, is stopped.
func (a *Agent) nameRadios() error {
	wanted := map[string]radio.Role{
		radio.HotspotInterfaceName: radio.RoleHotspot,
		radio.GuestInterfaceName:   radio.RoleGuest,
	}
	rename := func(r *radio.Radio, to string) error {
		slog.Info("renaming radio", "from", r.Interface, "to", to, "role", r.Role, "driver", r.Driver, "usbId", r.USBID)
		if err := system.RenameLink(a.runner, r.Interface, to); err != nil {
			return err
		}
		r.Interface = to
		return nil
	}
	stopped := false
	stopHostapd := func() {
		if !stopped {
			_ = system.ServiceStop(a.runner, hostapdService)
			stopped = true
		}
	}
	// First out of the way: radios called ap0 or guest0 that now do
	// something else.
	for i := range a.radios {
		r := &a.radios[i]
		if role, special := wanted[r.Interface]; special && r.Role != role {
			stopHostapd()
			if err := rename(r, "wlanp"+strings.TrimPrefix(r.Phy, "phy")); err != nil {
				return err
			}
		}
	}
	for name, role := range wanted {
		r := a.radioWithRole(role)
		if r == nil || r.Interface == name {
			continue
		}
		stopHostapd()
		// A guest network hostapd ran on the hotspot's radio has this
		// name too; it is made again when needed.
		if name == radio.GuestInterfaceName && system.LinkExists(a.runner, name) {
			_, _ = a.runner.Run("iw", "dev", name, "del")
		}
		if err := rename(r, name); err != nil {
			return err
		}
	}
	return nil
}

// guestRadioHotspot describes the guest network on its own radio for
// hostapd, next to the main hotspot, or nil when it runs on the hotspot's
// radio (or is off). It uses 2.4 GHz, on a channel away from the
// hotspot's.
func (a *Agent) guestRadioHotspot(main render.HotspotInput) *render.HotspotInput {
	r := a.guestRadio()
	if r == nil || !a.guestWanted() || main.Open {
		return nil
	}
	guest := render.HotspotInput{
		Interface:  radio.GuestInterfaceName,
		SSID:       a.cfg.Guest.SSID,
		Country:    main.Country,
		Security:   main.Security,
		Password:   a.secrets.GuestPassword,
		GuestRadio: true,
	}
	// 2.4 GHz, which every device and country has, on a channel that
	// doesn't overlap the hotspot's.
	guest.Band, guest.Channel = config.Band2GHz, 6
	if main.Band == config.Band2GHz {
		guest.Channel = 11
		if main.Channel > 6 {
			guest.Channel = 1
		}
	}
	if guest.Security == config.SecurityWPA3 && !r.HasWPA3 {
		guest.Security = config.SecurityWPA2WPA3
	}
	if r.HasWiFi6 {
		guest.Standard = config.StandardWiFi6
	}
	return &guest
}

// writeGuestRadioConfig writes the guest radio's hostapd file and tells the
// hostapd service whether to run it (hostapd.env). The caller restarts
// hostapd afterwards.
func (a *Agent) writeGuestRadioConfig(main render.HotspotInput) error {
	env := "GUEST_CONF=\n"
	if guest := a.guestRadioHotspot(main); guest != nil {
		conf, err := render.Hostapd(*guest)
		if err != nil {
			return fmt.Errorf("guest network: %w", err)
		}
		// It holds the guests' password, so only root may read it.
		if err := config.WriteFileAtomic(a.paths.HostapdGuestConfig(), []byte(conf), 0o600); err != nil {
			return err
		}
		env = "GUEST_CONF=" + a.paths.HostapdGuestConfig() + "\n"
	}
	return config.WriteFileAtomic(a.paths.HostapdEnv(), []byte(env), 0o644)
}

// --- Changing the roles (System page) ---

// radioRolesPending is the file kept while new roles wait to be kept.
type radioRolesPending struct {
	// Previous are the roles to go back to.
	Previous map[string]string `json:"previous"`
}

// readRadioRolesPending returns the pending change, if there is one.
func (a *Agent) readRadioRolesPending() (radioRolesPending, bool) {
	var pending radioRolesPending
	data, err := os.ReadFile(a.paths.RadioRolesPendingFile())
	if err != nil || json.Unmarshal(data, &pending) != nil {
		return pending, false
	}
	return pending, true
}

// UpdateRadioRoles saves the owner's roles (radio MAC address to "hotspot",
// "uplink", "guest" or "unused"; radios left out are automatic) and
// restarts rbd with them. The change waits to be kept (KeepRadioRoles).
func (a *Agent) UpdateRadioRoles(roles map[string]string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mode != api.ModeNormal {
		return ErrWrongMode
	}
	choices := map[string]radio.Role{}
	counts := map[radio.Role]int{}
	for mac, role := range roles {
		r := a.radioByMAC(strings.ToLower(mac))
		if r == nil {
			return &config.ValidationError{Field: "roles", Message: "One of these radios isn't plugged in any more. Reload the page."}
		}
		switch radio.Role(role) {
		case radio.RoleHotspot, radio.RoleGuest:
			if !r.SupportsAP {
				return &config.ValidationError{Field: "roles", Message: fmt.Sprintf("The radio %s can't run a hotspot.", r.Interface)}
			}
		case radio.RoleUplink, radio.RoleUnused:
		default:
			return &config.ValidationError{Field: "roles", Message: fmt.Sprintf("%q isn't a job for a radio.", role)}
		}
		counts[radio.Role(role)]++
		choices[r.MAC] = radio.Role(role)
	}
	for _, role := range []radio.Role{radio.RoleHotspot, radio.RoleUplink, radio.RoleGuest} {
		if counts[role] > 1 {
			return &config.ValidationError{Field: "roles", Message: "Give each job to one radio only."}
		}
	}
	if assigned, _ := radio.AssignRoles(a.radios, choices); !hasRole(assigned, radio.RoleHotspot) {
		return &config.ValidationError{Field: "roles", Message: "One radio must run the hotspot, or you couldn't reach RasBeacon."}
	}

	newCfg := a.cfg.Clone()
	newCfg.Radios.Roles = map[string]string{}
	for mac, role := range choices {
		newCfg.Radios.Roles[mac] = string(role)
	}
	// The roles to go back to are the ones from before the first change
	// that wasn't kept yet.
	if _, already := a.readRadioRolesPending(); !already {
		pending, _ := json.Marshal(radioRolesPending{Previous: a.cfg.Radios.Roles})
		if err := config.WriteFileAtomic(a.paths.RadioRolesPendingFile(), pending, 0o644); err != nil {
			return err
		}
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	slog.Info("radio roles changed; restarting", "roles", newCfg.Radios.Roles)
	a.restartRBD()
	return nil
}

// KeepRadioRoles keeps the new roles: RasBeacon no longer goes back.
func (a *Agent) KeepRadioRoles() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.Remove(a.paths.RadioRolesPendingFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	a.radioRolesDeadline = time.Time{}
	slog.Info("new radio roles kept")
	return nil
}

// RevertRadioRoles goes back to the roles from before the change now (the
// owner pressed Go back, or nobody kept them in time) and restarts rbd.
func (a *Agent) RevertRadioRoles() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.revertRadioRoles()
}

func (a *Agent) revertRadioRoles() error {
	pending, ok := a.readRadioRolesPending()
	if !ok {
		return nil // kept in the meantime
	}
	newCfg := a.cfg.Clone()
	newCfg.Radios.Roles = pending.Previous
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	if err := os.Remove(a.paths.RadioRolesPendingFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	a.radioRolesDeadline = time.Time{}
	slog.Info("radio roles went back to the previous ones; restarting", "roles", newCfg.Radios.Roles)
	a.restartRBD()
	return nil
}

// watchRadioRoles starts the countdown when rbd started with roles that
// weren't kept yet. Start calls it.
func (a *Agent) watchRadioRoles() {
	if _, pending := a.readRadioRolesPending(); !pending {
		return
	}
	a.radioRolesDeadline = a.now().Add(RadioRolesConfirmTime)
	a.afterFunc(RadioRolesConfirmTime, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if err := a.revertRadioRoles(); err != nil {
			slog.Error("could not go back to the previous radio roles", "error", err)
		}
	})
}

// radioRolesSecondsLeft is how long the owner has left to keep new roles
// (0: nothing waits).
func (a *Agent) radioRolesSecondsLeft() int {
	if a.radioRolesDeadline.IsZero() {
		return 0
	}
	return max(1, int(a.radioRolesDeadline.Sub(a.now()).Seconds()))
}

// restartRBD restarts rbd shortly after answering, so it detects and
// renames the radios again.
func (a *Agent) restartRBD() {
	a.afterFunc(switchDelay, func() { _, _ = a.runner.Run("systemctl", "restart", "rbd.service") })
}

// radioByMAC returns the radio with this own address, or nil.
func (a *Agent) radioByMAC(mac string) *radio.Radio {
	for i := range a.radios {
		if a.radios[i].MAC == mac {
			return &a.radios[i]
		}
	}
	return nil
}

func hasRole(radios []radio.Radio, role radio.Role) bool {
	for _, r := range radios {
		if r.Role == role {
			return true
		}
	}
	return false
}
