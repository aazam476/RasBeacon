package agent

// This file runs plugins: small programs in Podman containers that the owner
// installs from .rba files or the catalog (see internal/plugin and
// docs/plugins.md). Related files:
//
//	pluginsinstall.go  previewing and installing (and updating) a plugin
//	pluginsrun.go      starting and stopping containers, the crash watch,
//	                   health checks and metrics, the power socket
//	pluginsnet.go      downloads through the VPN, and the catalog
//
// # What a plugin may do
//
// Each plugin has a fixed address on its own network (10.89.0.0/24, the
// bridge rbplug0). The firewall decides what it may reach (see
// render.PluginRule): the internet only with the "internet" permission and
// then like your devices (through the VPN when it is in use), your devices
// only with "lan-access", RasBeacon itself only for DNS. Devices on the
// hotspot reach a plugin's ports at RasBeacon's address and the ports the
// owner chose; guests and the internet never do.
//
// # Long work
//
// Downloading an image or starting a container takes seconds to minutes,
// so it runs without the agent's lock: each plugin is marked busy (so two
// actions on it can't overlap), the work runs, then the lock is taken again
// to record the result.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
	"rasbeacon/internal/readings"
	"rasbeacon/internal/render"
	"rasbeacon/internal/system"
)

// pluginsState is the plugins' state in memory (the settings are in
// a.cfg.Plugins).
type pluginsState struct {
	// live is each installed (or installing) plugin's state, by ID.
	live map[string]*pluginLive
	// staged are plugin files waiting in the install sheet, by staging ID.
	staged map[string]*stagedPlugin
	// runner runs Podman and other slow commands (longer time limit).
	runner system.Runner
	// client makes HTTP requests for plugins: through the VPN (see
	// pluginsnet.go); tests replace it.
	client func() *http.Client
	// catalog is the last catalog fetched.
	catalog catalogCache
	// watching is true once the crash watch runs.
	watching bool
	// proxy is the address of the download proxy for Podman ("" until
	// first used).
	proxy string
	// watchEvery is how often the crash watch runs (0: never, in tests).
	watchEvery time.Duration
}

// pluginLive is one plugin's state in memory.
type pluginLive struct {
	manifest *plugin.Manifest
	icon     string // data: URL
	readme   string
	state    string
	message  string
	// busy is set while a slow action runs on it.
	busy bool
	// crashes counts crashes in a row; startedAt is when it last started.
	crashes   int
	startedAt time.Time
	healthy   *bool
	metrics   []api.PluginMetric
	checkedAt time.Time
}

// errPluginBusy is returned while another action on the plugin runs.
var errPluginBusy = errors.New("RasBeacon is still working on this plugin; try again in a moment")

// errNoPlugin is returned for an ID that isn't installed.
var errNoPlugin = &config.ValidationError{Field: "id", Message: "No such plugin is installed."}

// maxPlugins is how many plugins can be installed (addresses .2 to .254).
const maxPlugins = 50

// Crash handling: a plugin that stops by itself is started again, up to
// maxCrashes times in a row; one that ran for stableAfter counts as fine.
const (
	maxCrashes  = 5
	stableAfter = 10 * time.Minute
)

// loadPlugins reads the installed plugins' manifests at start. A plugin
// whose files are damaged is shown as failed. The caller holds a.mu.
func (a *Agent) loadPlugins() {
	a.plugins.live = map[string]*pluginLive{}
	if a.plugins.staged == nil {
		a.plugins.staged = map[string]*stagedPlugin{}
	}
	// Downloads nobody installed before the restart are gone.
	_ = os.RemoveAll(a.paths.PluginStagingDir())
	for id, p := range a.cfg.Plugins.Installed {
		live := &pluginLive{state: api.PluginStopped}
		a.plugins.live[id] = live
		if err := a.readPluginFiles(id, live); err != nil {
			live.state, live.message = api.PluginFailed, "Its files can't be read: "+err.Error()+". Remove it and install it again."
			continue
		}
		if p.StoppedAfterCrashes {
			live.state, live.message = api.PluginCrashed, "It stopped working several times in a row, so RasBeacon stopped it."
		}
	}
}

// readPluginFiles reads an installed plugin's manifest, icon and readme.
func (a *Agent) readPluginFiles(id string, live *pluginLive) error {
	dir := a.paths.PluginDir(id)
	data, err := os.ReadFile(filepath.Join(dir, plugin.ManifestFile))
	if err != nil {
		return err
	}
	m, err := plugin.ParseInstalledManifest(data)
	if err != nil {
		return err
	}
	live.manifest = m
	if icon, err := os.ReadFile(filepath.Join(dir, plugin.IconFile)); err == nil {
		live.icon = "data:image/png;base64," + base64.StdEncoding.EncodeToString(icon)
	}
	if readme, err := os.ReadFile(filepath.Join(dir, plugin.ReadmeFile)); err == nil {
		live.readme = string(readme)
	}
	return nil
}

// pluginBoard names this Pi as manifests do: pi5, pi4, pi3, zero2w ("" if
// unknown).
func (a *Agent) pluginBoard() string {
	model := a.boardModel()
	switch {
	case strings.Contains(model, "Pi 5"):
		return "pi5"
	case strings.Contains(model, "Pi 4"), strings.Contains(model, "Pi 400"), strings.Contains(model, "Compute Module 4"):
		return "pi4"
	case strings.Contains(model, "Zero 2"):
		return "zero2w"
	case strings.Contains(model, "Pi 3"):
		return "pi3"
	}
	return ""
}

// memoryMB returns the Pi's memory and swap in MB.
func (a *Agent) memoryMB() (memory, swap int) {
	text, err := os.ReadFile(a.paths.ProcMeminfo())
	if err != nil {
		return 0, 0
	}
	m, err := readings.ParseMemory(string(text))
	if err != nil {
		return 0, 0
	}
	return int(m.TotalMB()), int(m.SwapTotalMB())
}

// PluginsState lists the installed plugins (GET /plugins).
func (a *Agent) PluginsState() api.PluginsState {
	a.mu.Lock()
	defer a.mu.Unlock()
	memory, swap := a.memoryMB()
	state := api.PluginsState{
		Plugins:  []api.Plugin{},
		Sources:  a.pluginSources(),
		Board:    a.pluginBoard(),
		MemoryMB: memory,
		SwapMB:   swap,
		Metered:  a.cfg.Hotspot.Metered,
	}
	if !a.plugins.catalog.CheckedAt.IsZero() {
		state.CheckedAt = a.plugins.catalog.CheckedAt.UTC().Format(time.RFC3339)
	}
	state.CheckError = a.plugins.catalog.errorText()
	for _, id := range a.pluginIDs() {
		state.Plugins = append(state.Plugins, a.pluginView(id))
	}
	return state
}

// pluginIDs lists the installed plugins by name. The caller holds a.mu.
func (a *Agent) pluginIDs() []string {
	ids := slices.Collect(maps.Keys(a.cfg.Plugins.Installed))
	slices.SortFunc(ids, func(x, y string) int {
		return strings.Compare(strings.ToLower(a.pluginName(x)), strings.ToLower(a.pluginName(y)))
	})
	return ids
}

func (a *Agent) pluginName(id string) string {
	if live := a.plugins.live[id]; live != nil && live.manifest != nil {
		return live.manifest.Name
	}
	return id
}

// pluginSummaries are the plugins on the status page. The caller holds a.mu.
func (a *Agent) pluginSummaries() []api.PluginSummary {
	summaries := []api.PluginSummary{}
	for _, id := range a.pluginIDs() {
		live := a.plugins.live[id]
		if live == nil {
			continue
		}
		summaries = append(summaries, api.PluginSummary{ID: id, Name: a.pluginName(id), State: live.state, Icon: live.icon})
	}
	return summaries
}

// pluginView is an installed plugin for the API. The caller holds a.mu.
func (a *Agent) pluginView(id string) api.Plugin {
	p := a.cfg.Plugins.Installed[id]
	live := a.plugins.live[id]
	view := api.Plugin{
		ID: id, Name: id, Version: p.Version, Source: p.Source, Signed: p.PublicKey != "",
		State: live.state, Message: live.message, Healthy: live.healthy, Address: p.Address.String(),
		Permissions: []api.PluginPermission{}, Ports: []api.PluginPort{}, Settings: []api.PluginSetting{},
		Metrics: slices.Clone(live.metrics), Drive: p.Drive, Icon: live.icon, Readme: live.readme,
	}
	if view.Metrics == nil {
		view.Metrics = []api.PluginMetric{}
	}
	if p.PublicKey != "" {
		if key, err := base64.StdEncoding.DecodeString(p.PublicKey); err == nil {
			view.KeyID = plugin.KeyID(key)
		}
	}
	m := live.manifest
	if m == nil {
		return view
	}
	view.Name, view.Description, view.Author, view.Homepage = m.Name, m.Description, m.Author, m.Homepage
	view.Permissions = permissionViews(m.Permissions)
	view.Ports = a.portViews(m, p.Ports)
	view.Settings = a.settingViews(m, p.Settings, a.secrets.Plugins[id])
	view.UsesDrive = m.USBDrive != nil
	view.DriveOptional = m.USBDrive != nil && m.USBDrive.Optional
	view.AfterInstall = a.pluginValues(id, m, p).Expand(m.AfterInstall)
	view.MemoryMB, view.CPUs, view.StorageMB = m.Memory(), m.CPULimit(), m.Storage()
	if entry, ok := a.plugins.catalog.find(p.Source, id); ok && plugin.NewerVersion(entry.Version, p.Version) {
		view.UpdateVersion = entry.Version
		view.CanUpdate = p.PublicKey != ""
	}
	return view
}

func permissionViews(ids []string) []api.PluginPermission {
	views := []api.PluginPermission{}
	for _, id := range ids {
		if p, ok := plugin.PermissionByID(id); ok {
			views = append(views, api.PluginPermission{ID: p.ID, Title: p.Title, Detail: p.Detail})
		}
	}
	return views
}

// portKey names a port in requests and settings: "tcp/3000".
func portKey(p plugin.Port) string { return p.Protocol + "/" + strconv.Itoa(p.Container) }

// portViews lists a plugin's ports with the ports on RasBeacon (chosen, or
// suggested when not chosen yet). The caller holds a.mu.
func (a *Agent) portViews(m *plugin.Manifest, chosen map[string]int) []api.PluginPort {
	views := []api.PluginPort{}
	for _, p := range m.Ports {
		host := chosen[portKey(p)]
		if host == 0 {
			host = p.Default
		}
		view := api.PluginPort{Key: portKey(p), Name: p.Name, Protocol: p.Protocol, Container: p.Container, Host: host}
		if p.Open && chosen[portKey(p)] != 0 {
			scheme := "http"
			if p.HTTPS {
				scheme = "https"
			}
			view.URL = fmt.Sprintf("%s://%s:%d/", scheme, a.lan.Address(), host)
		}
		views = append(views, view)
	}
	return views
}

// settingViews lists a plugin's settings with their values; passwords only
// say whether one is saved.
func (a *Agent) settingViews(m *plugin.Manifest, values, secrets map[string]string) []api.PluginSetting {
	views := []api.PluginSetting{}
	for _, s := range m.Settings {
		view := api.PluginSetting{
			Key: s.Key, Label: s.Label, Type: s.Type, Help: s.Help, Unit: s.Unit, Required: s.Required,
			Min: s.Min, Max: s.Max,
		}
		for _, o := range s.Options {
			view.Options = append(view.Options, api.PluginOption{Value: o.Value, Label: o.Label})
		}
		if s.Secret() {
			view.HasValue = secrets[s.Key] != ""
		} else if v, ok := values[s.Key]; ok {
			view.Value = v
		} else {
			view.Value = s.Default
		}
		views = append(views, view)
	}
	return views
}

// pluginValues fills in a manifest's placeholders for an installed plugin.
// withSecrets adds password settings (for rbd's own use, never shown).
func (a *Agent) pluginValues(id string, m *plugin.Manifest, p config.InstalledPlugin, withSecrets ...bool) plugin.Values {
	v := plugin.Values{Address: p.Address.String(), Host: a.lan.Address(), Ports: map[int]int{}, Settings: map[string]string{}}
	for _, port := range m.Ports {
		if host := p.Ports[portKey(port)]; host != 0 {
			v.Ports[port.Container] = host
		}
	}
	for _, s := range m.Settings {
		switch {
		case s.Secret() && len(withSecrets) > 0 && withSecrets[0]:
			v.Settings[s.Key] = a.secrets.Plugins[id][s.Key]
		case s.Secret():
		default:
			value, ok := p.Settings[s.Key]
			if !ok {
				value = s.Default
			}
			v.Settings[s.Key] = value
		}
	}
	return v
}

// checkPorts checks the ports chosen for a plugin: each from 1 to 65535,
// not one of RasBeacon's own, and not taken by another plugin. The caller
// holds a.mu.
func (a *Agent) checkPorts(id string, m *plugin.Manifest, requested map[string]int) (map[string]int, error) {
	chosen := map[string]int{}
	taken := map[string]string{}
	for other, p := range a.cfg.Plugins.Installed {
		if other == id {
			continue
		}
		for key, host := range p.Ports {
			protocol, _, _ := strings.Cut(key, "/")
			taken[protocol+"/"+strconv.Itoa(host)] = a.pluginName(other)
		}
	}
	for _, port := range m.Ports {
		key := portKey(port)
		host, ok := requested[key]
		if !ok {
			host = port.Default
		}
		field := "ports." + key
		switch {
		case host < 1 || host > 65535:
			return nil, &config.ValidationError{Field: field, Message: "Choose a port from 1 to 65535."}
		case plugin.Reserved(host):
			return nil, &config.ValidationError{Field: field, Message: fmt.Sprintf("Port %d is RasBeacon's own. Choose another.", host)}
		case taken[port.Protocol+"/"+strconv.Itoa(host)] != "":
			return nil, &config.ValidationError{Field: field, Message: fmt.Sprintf("%s already uses port %d.", taken[port.Protocol+"/"+strconv.Itoa(host)], host)}
		}
		for otherKey, otherHost := range chosen {
			protocol, _, _ := strings.Cut(otherKey, "/")
			if protocol == port.Protocol && otherHost == host {
				return nil, &config.ValidationError{Field: field, Message: fmt.Sprintf("Port %d is chosen twice.", host)}
			}
		}
		chosen[key] = host
	}
	return chosen, nil
}

// suggestPorts picks ports for a plugin: the ones it has, else its
// defaults, else the next free ones. The caller holds a.mu.
func (a *Agent) suggestPorts(id string, m *plugin.Manifest, current map[string]int) map[string]int {
	taken := map[string]bool{}
	for other, p := range a.cfg.Plugins.Installed {
		if other == id {
			continue
		}
		for key, host := range p.Ports {
			protocol, _, _ := strings.Cut(key, "/")
			taken[protocol+"/"+strconv.Itoa(host)] = true
		}
	}
	suggested := map[string]int{}
	for _, port := range m.Ports {
		host := current[portKey(port)]
		if host == 0 {
			host = port.Default
		}
		for tries := 0; tries < 2000 && (plugin.Reserved(host) || taken[port.Protocol+"/"+strconv.Itoa(host)]); tries++ {
			if host++; host > 65535 {
				host = 1024
			}
		}
		taken[port.Protocol+"/"+strconv.Itoa(host)] = true
		suggested[portKey(port)] = host
	}
	return suggested
}

// checkSettings checks submitted settings and splits them into plain
// values and passwords. An empty password keeps the saved one. The caller
// holds a.mu.
func (a *Agent) checkSettings(id string, m *plugin.Manifest, submitted map[string]string) (values, secrets map[string]string, err error) {
	values, secrets = map[string]string{}, map[string]string{}
	saved := a.secrets.Plugins[id]
	for _, s := range m.Settings {
		value, ok := submitted[s.Key]
		if !ok {
			value = s.Default
			if old, exists := a.cfg.Plugins.Installed[id].Settings[s.Key]; exists && !s.Secret() {
				value = old
			}
		}
		value = strings.TrimSpace(value)
		if s.Secret() && value == "" {
			value = saved[s.Key]
		}
		if err := s.Check(value); err != nil {
			message := err.Error()
			if message == "required" {
				message = "Fill this in."
			} else {
				message = strings.ToUpper(message[:1]) + message[1:] + "."
			}
			return nil, nil, &config.ValidationError{Field: "settings." + s.Key, Message: message}
		}
		if s.Secret() {
			if value != "" {
				secrets[s.Key] = value
			}
		} else {
			values[s.Key] = value
		}
	}
	return values, secrets, nil
}

// checkDrive checks the USB drive chosen for a plugin. The caller holds a.mu.
func (a *Agent) checkDrive(id string, m *plugin.Manifest, uuid string) error {
	if m.USBDrive == nil {
		if uuid != "" {
			return &config.ValidationError{Field: "drive", Message: "This plugin doesn't use a USB drive."}
		}
		return nil
	}
	if uuid == "" {
		if !m.USBDrive.Optional {
			return &config.ValidationError{Field: "drive", Message: "Choose a USB drive."}
		}
		return nil
	}
	for other, p := range a.cfg.Plugins.Installed {
		if other != id && p.Drive == uuid {
			return &config.ValidationError{Field: "drive", Message: a.pluginName(other) + " already uses this drive."}
		}
	}
	return nil
}

// nextPluginAddress picks the lowest free address on the plugins' network.
// The caller holds a.mu.
func (a *Agent) nextPluginAddress() (netip.Addr, error) {
	used := map[netip.Addr]bool{netip.MustParseAddr(render.PluginGateway): true}
	for _, p := range a.cfg.Plugins.Installed {
		used[p.Address] = true
	}
	for addr := netip.MustParseAddr(render.PluginGateway).Next(); render.PluginSubnet.Contains(addr); addr = addr.Next() {
		if !used[addr] && addr != render.PluginSubnet.Masked().Addr() && addr.As4()[3] != 255 {
			return addr, nil
		}
	}
	return netip.Addr{}, errors.New("no address is free for another plugin")
}

// pluginRules are the plugins' firewall rules. The caller holds a.mu.
func (a *Agent) pluginRules() []render.PluginRule {
	var rules []render.PluginRule
	for _, id := range slices.Sorted(maps.Keys(a.cfg.Plugins.Installed)) {
		p := a.cfg.Plugins.Installed[id]
		live := a.plugins.live[id]
		if live == nil || live.manifest == nil || !p.Address.IsValid() {
			continue
		}
		rule := render.PluginRule{
			Address:   p.Address,
			Internet:  live.manifest.Has("internet"),
			LANAccess: live.manifest.Has("lan-access"),
		}
		for _, port := range live.manifest.Ports {
			if host := p.Ports[portKey(port)]; host != 0 {
				rule.Ports = append(rule.Ports, render.PluginPort{Protocol: port.Protocol, Host: host, Container: port.Container})
			}
		}
		rules = append(rules, rule)
	}
	return rules
}

// beginPluginWork marks a plugin busy for a slow action. The caller holds
// a.mu.
func (a *Agent) beginPluginWork(id string) (*pluginLive, error) {
	if a.mode != api.ModeNormal {
		return nil, ErrWrongMode
	}
	live := a.plugins.live[id]
	if _, ok := a.cfg.Plugins.Installed[id]; !ok || live == nil {
		return nil, errNoPlugin
	}
	if live.busy {
		return nil, errPluginBusy
	}
	live.busy = true
	return live, nil
}

// endPluginWork clears the busy mark (takes a.mu).
func (a *Agent) endPluginWork(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if live := a.plugins.live[id]; live != nil {
		live.busy = false
	}
}

// savePluginConfig saves a change to one installed plugin. The caller
// holds a.mu.
func (a *Agent) savePluginConfig(id string, change func(p *config.InstalledPlugin)) error {
	newCfg := a.cfg.Clone()
	p, ok := newCfg.Plugins.Installed[id]
	if !ok {
		return errNoPlugin
	}
	change(&p)
	newCfg.Plugins.Installed[id] = p
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	return nil
}

// StartPlugin starts a stopped plugin (also after it crashed too often).
func (a *Agent) StartPlugin(id string) error {
	a.mu.Lock()
	live, err := a.beginPluginWork(id)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	live.crashes = 0
	if err := a.savePluginConfig(id, func(p *config.InstalledPlugin) { p.Running, p.StoppedAfterCrashes = true, false }); err != nil {
		live.busy = false
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()
	defer a.endPluginWork(id)
	return a.runPlugin(id)
}

// StopPlugin stops a plugin; it stays stopped after a restart.
func (a *Agent) StopPlugin(id string) error {
	a.mu.Lock()
	_, err := a.beginPluginWork(id)
	if err == nil {
		if err = a.savePluginConfig(id, func(p *config.InstalledPlugin) { p.Running = false }); err != nil {
			a.plugins.live[id].busy = false
		}
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	defer a.endPluginWork(id)
	a.stopPluginContainer(id)
	a.mu.Lock()
	a.setPluginState(id, api.PluginStopped, "")
	a.mu.Unlock()
	return nil
}

// UpdatePluginSettings saves a plugin's ports, settings and drive, and
// restarts it if it runs.
func (a *Agent) UpdatePluginSettings(req api.PluginSettingsRequest) (api.Plugin, error) {
	a.mu.Lock()
	live, err := a.beginPluginWork(req.ID)
	if err != nil {
		a.mu.Unlock()
		return api.Plugin{}, err
	}
	fail := func(err error) (api.Plugin, error) {
		live.busy = false
		a.mu.Unlock()
		return api.Plugin{}, err
	}
	m := live.manifest
	if m == nil {
		return fail(errors.New("this plugin's files can't be read; remove it and install it again"))
	}
	ports, err := a.checkPorts(req.ID, m, req.Ports)
	if err != nil {
		return fail(err)
	}
	values, secrets, err := a.checkSettings(req.ID, m, req.Settings)
	if err != nil {
		return fail(err)
	}
	if err := a.checkDrive(req.ID, m, req.Drive); err != nil {
		return fail(err)
	}
	if err := a.savePluginSecrets(req.ID, secrets); err != nil {
		return fail(err)
	}
	if err := a.savePluginConfig(req.ID, func(p *config.InstalledPlugin) {
		p.Ports, p.Settings, p.Drive = ports, values, req.Drive
	}); err != nil {
		return fail(err)
	}
	slog.Info("plugin settings saved", "plugin", req.ID)
	running := a.cfg.Plugins.Installed[req.ID].Running
	if err := a.applyFirewall(); err != nil {
		slog.Error("could not apply the plugin's ports", "plugin", req.ID, "error", err)
	}
	a.mu.Unlock()

	if running {
		_ = a.runPlugin(req.ID) // its state shows any problem
	}
	a.endPluginWork(req.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pluginView(req.ID), nil
}

// savePluginSecrets saves a plugin's passwords (all of them: missing ones
// are removed). The caller holds a.mu.
func (a *Agent) savePluginSecrets(id string, secrets map[string]string) error {
	newSecrets := a.secrets.Clone()
	if newSecrets.Plugins == nil {
		newSecrets.Plugins = map[string]map[string]string{}
	}
	if len(secrets) == 0 {
		delete(newSecrets.Plugins, id)
	} else {
		newSecrets.Plugins[id] = secrets
	}
	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return err
	}
	a.secrets = newSecrets
	return nil
}

// RemovePlugin stops and removes a plugin and its image. With keepData its
// data is kept for a later install of the same plugin.
func (a *Agent) RemovePlugin(req api.PluginRemoveRequest) error {
	a.mu.Lock()
	if _, err := a.beginPluginWork(req.ID); err != nil {
		a.mu.Unlock()
		return err
	}
	image := a.cfg.Plugins.Installed[req.ID].Image
	a.mu.Unlock()

	a.stopPluginContainer(req.ID)
	a.unmountPluginData(req.ID)
	if image != "" {
		_, _ = a.plugins.runner.Run("podman", "rmi", "--ignore", image)
	}
	dir := a.paths.PluginDir(req.ID)
	if req.KeepData {
		if err := os.MkdirAll(a.paths.PluginKeptDir(), 0o700); err == nil {
			_ = os.Rename(filepath.Join(dir, pluginDataImage), filepath.Join(a.paths.PluginKeptDir(), req.ID+".img"))
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("could not delete the plugin's files", "plugin", req.ID, "error", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	newCfg := a.cfg.Clone()
	delete(newCfg.Plugins.Installed, req.ID)
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		a.plugins.live[req.ID].busy = false
		return err
	}
	a.cfg = newCfg
	delete(a.plugins.live, req.ID)
	if err := a.savePluginSecrets(req.ID, nil); err != nil {
		slog.Warn("could not delete the plugin's passwords", "plugin", req.ID, "error", err)
	}
	if err := a.applyFirewall(); err != nil {
		slog.Error("could not update the firewall after removing a plugin", "error", err)
	}
	slog.Info("plugin removed", "plugin", req.ID, "keptData", req.KeepData)
	return nil
}

// PluginLogs returns a plugin's last 500 lines of output (kept in memory,
// in /run, until it restarts or RasBeacon does).
func (a *Agent) PluginLogs(id string) (api.PluginLogs, error) {
	a.mu.Lock()
	_, ok := a.cfg.Plugins.Installed[id]
	a.mu.Unlock()
	if !ok {
		return api.PluginLogs{}, errNoPlugin
	}
	data, err := os.ReadFile(a.pluginLogFile(id))
	if err != nil {
		return api.PluginLogs{Lines: []string{}}, nil
	}
	return api.PluginLogs{Lines: lastLogLines(string(data), 500)}, nil
}

// lastLogLines reads Podman's k8s-file log ("<time> stdout F <text>")
// and keeps the last n lines as "<hh:mm:ss> <text>".
func lastLogLines(text string, n int) []string {
	raw := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(raw) > n {
		raw = raw[len(raw)-n:]
	}
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 4)
		if len(fields) == 4 {
			if t, err := time.Parse(time.RFC3339Nano, fields[0]); err == nil {
				line = t.Local().Format("15:04:05") + " " + fields[3]
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// PluginDrives lists the USB drives plugins may use.
func (a *Agent) PluginDrives() (api.PluginDrives, error) {
	out, err := a.runner.Run("lsblk", plugin.LsblkArgs...)
	if err != nil {
		return api.PluginDrives{}, fmt.Errorf("listing drives: %w", err)
	}
	drives, err := plugin.ParseDrives(out)
	if err != nil {
		return api.PluginDrives{}, fmt.Errorf("listing drives: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	result := api.PluginDrives{Drives: []api.PluginDrive{}}
	for _, d := range drives {
		view := api.PluginDrive{UUID: d.UUID, Label: d.Label, FSType: d.FSType, Size: d.Size}
		for id, p := range a.cfg.Plugins.Installed {
			if p.Drive == d.UUID {
				view.UsedBy = a.pluginName(id)
			}
		}
		result.Drives = append(result.Drives, view)
	}
	return result, nil
}

// pluginSources lists the catalog's sources. The caller holds a.mu.
func (a *Agent) pluginSources() []api.PluginSource {
	sources := []api.PluginSource{{Source: plugin.BuiltInSource, BuiltIn: true}}
	for _, s := range a.cfg.Plugins.Sources {
		sources = append(sources, api.PluginSource{Source: s})
	}
	return sources
}

// SetPluginSources saves the extra catalog sources.
func (a *Agent) SetPluginSources(req api.PluginSourcesRequest) (api.PluginsState, error) {
	var sources []string
	for i, s := range req.Sources {
		checked, err := plugin.CheckSource(s)
		if err != nil {
			return api.PluginsState{}, &config.ValidationError{Field: fmt.Sprintf("sources.%d", i), Message: "Enter a GitHub repository like owner/repo."}
		}
		if strings.EqualFold(checked, plugin.BuiltInSource) || slices.ContainsFunc(sources, func(x string) bool { return strings.EqualFold(x, checked) }) {
			continue
		}
		sources = append(sources, checked)
	}
	if len(sources) > 20 {
		return api.PluginsState{}, &config.ValidationError{Field: "sources", Message: "At most 20 sources."}
	}
	a.mu.Lock()
	if a.mode != api.ModeNormal {
		a.mu.Unlock()
		return api.PluginsState{}, ErrWrongMode
	}
	newCfg := a.cfg.Clone()
	newCfg.Plugins.Sources = sources
	err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644)
	if err == nil {
		a.cfg = newCfg
	}
	a.mu.Unlock()
	if err != nil {
		return api.PluginsState{}, err
	}
	return a.PluginsState(), nil
}

// setPluginState records a plugin's state. The caller holds a.mu.
func (a *Agent) setPluginState(id, state, message string) {
	if live := a.plugins.live[id]; live != nil {
		live.state, live.message = state, message
		if state != api.PluginRunning {
			live.healthy = nil
		}
	}
}

// pluginDiagnostics lists the plugins and their state for the diagnostics
// report (nothing about their settings). The caller holds a.mu.
func (a *Agent) pluginDiagnostics() string {
	if len(a.cfg.Plugins.Installed) == 0 {
		return "Plugins: none\n"
	}
	var b strings.Builder
	b.WriteString("Plugins:\n")
	for _, id := range a.pluginIDs() {
		p := a.cfg.Plugins.Installed[id]
		live := a.plugins.live[id]
		fmt.Fprintf(&b, "  %s %s (%s, %s): %s", id, p.Version, p.Source, map[bool]string{true: "signed", false: "unsigned"}[p.PublicKey != ""], live.state)
		if live.message != "" {
			fmt.Fprintf(&b, " - %s", live.message)
		}
		b.WriteString("\n")
	}
	return b.String()
}
