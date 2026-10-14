package agent

// Starting and stopping plugins' containers, their data and drives, the
// crash watch, health checks and metrics, and the power socket. See
// plugins.go.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"golang.org/x/crypto/bcrypt"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
	"rasbeacon/internal/render"
	"rasbeacon/internal/system"
)

// pluginDataImage is the file holding a plugin's data, in its folder.
const pluginDataImage = "data.img"

// pluginWatchEvery is how often the crash watch looks at the containers;
// health and metrics are read every other time.
const pluginWatchEvery = 15 * time.Second

// pluginLogFile is where a plugin's output goes (in /run: memory).
func (a *Agent) pluginLogFile(id string) string {
	return filepath.Join(a.paths.PluginRunDir(id), "output.log")
}

func (a *Agent) pluginDataMount(id string) string {
	return filepath.Join(a.paths.PluginRunDir(id), "data")
}

func (a *Agent) pluginSocketDir(id string) string {
	return filepath.Join(a.paths.PluginRunDir(id), "socket")
}

// pluginRunPlan is everything needed to start a plugin, taken under the
// lock so the slow part can run without it.
type pluginRunPlan struct {
	id        string
	manifest  *plugin.Manifest
	storageMB int
	templates []renderedTemplate
	drive     string
	spec      plugin.RunSpec
	power     bool
}

type renderedTemplate struct {
	volume, relative string
	content          []byte
	once             bool
}

// planPluginRun prepares a plugin's start. The caller holds a.mu.
func (a *Agent) planPluginRun(id string) (*pluginRunPlan, error) {
	p, ok := a.cfg.Plugins.Installed[id]
	live := a.plugins.live[id]
	if !ok || live == nil {
		return nil, errNoPlugin
	}
	m := live.manifest
	if m == nil {
		return nil, errors.New("its files can't be read")
	}
	plan := &pluginRunPlan{id: id, manifest: m, storageMB: m.Storage(), drive: p.Drive, power: m.Has("power-button")}
	values := a.pluginValues(id, m, p, true)

	for _, t := range m.Templates {
		text, err := os.ReadFile(filepath.Join(a.paths.PluginDir(id), "templates", t.File))
		if err != nil {
			return nil, fmt.Errorf("its template %s can't be read", t.File)
		}
		content, err := fillTemplate(t.File, text, values)
		if err != nil {
			return nil, err
		}
		volume, _ := m.VolumeFor(t.Path)
		relative := strings.TrimPrefix(t.Path, strings.TrimSuffix(volume.Path, "/")+"/")
		plan.templates = append(plan.templates, renderedTemplate{volume: volume.Name, relative: relative, content: content, once: t.Once})
	}

	env := map[string]string{}
	for k, v := range m.Env {
		env[k] = v
	}
	for _, s := range m.Settings {
		if s.Env != "" {
			env[s.Env] = values.Settings[s.Key]
		}
	}
	spec := plugin.RunSpec{
		ID: id, Image: p.Image, Address: p.Address.String(), MemoryMB: m.Memory(), CPUs: m.CPULimit(),
		Env: env, LogFile: a.pluginLogFile(id), Command: m.Command,
	}
	dataMount := a.pluginDataMount(id)
	for _, v := range m.Volumes {
		// A chosen drive replaces a volume at the same path.
		if p.Drive != "" && m.USBDrive != nil && m.USBDrive.Path == v.Path {
			continue
		}
		spec.Mounts = append(spec.Mounts, plugin.Mount{Host: filepath.Join(dataMount, v.Name), Container: v.Path})
	}
	if p.Drive != "" && m.USBDrive != nil {
		spec.Mounts = append(spec.Mounts, plugin.Mount{Host: a.paths.DriveMountDir(p.Drive), Container: m.USBDrive.Path})
	}
	if plan.power {
		env["RASBEACON_SOCKET"] = "/run/rasbeacon/rbd.sock"
		spec.Mounts = append(spec.Mounts, plugin.Mount{Host: a.pluginSocketDir(id), Container: "/run/rasbeacon"})
	}
	dev := a.paths.DevDir()
	if m.Has("i2c") {
		matches, _ := filepath.Glob(filepath.Join(dev, "i2c-*"))
		spec.Devices = append(spec.Devices, matches...)
	}
	if m.Has("gpio") {
		matches, _ := filepath.Glob(filepath.Join(dev, "gpiochip*"))
		spec.Devices = append(spec.Devices, matches...)
	}
	if !m.Has("temperature") {
		spec.Masked = []string{"/sys/class/thermal", "/sys/devices/virtual/thermal", "/sys/class/hwmon"}
	}
	plan.spec = spec
	return plan, nil
}

// fillTemplate fills in a template with the plugin's settings:
// {{.Settings.key}}, {{.Address}}, {{.Host}}, {{port 3000}}, and
// {{bcrypt .Settings.password}} for programs that keep password hashes.
func fillTemplate(name string, text []byte, values plugin.Values) ([]byte, error) {
	funcs := template.FuncMap{
		"bcrypt": func(password string) (string, error) {
			hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
			return string(hash), err
		},
		"port": func(container int) int { return values.Ports[container] },
		"json": func(v any) (string, error) {
			data, err := json.Marshal(v)
			return string(data), err
		},
	}
	t, err := template.New(name).Funcs(funcs).Option("missingkey=zero").Parse(string(text))
	if err != nil {
		return nil, fmt.Errorf("its template %s is broken: %w", name, err)
	}
	var out bytes.Buffer
	if err := t.Execute(&out, values); err != nil {
		return nil, fmt.Errorf("its template %s can't be filled in: %w", name, err)
	}
	return out.Bytes(), nil
}

// runPlugin (re)starts a plugin's container with its current settings.
// It must be marked busy by the caller, and is called without a.mu.
func (a *Agent) runPlugin(id string) error {
	a.mu.Lock()
	plan, err := a.planPluginRun(id)
	if err == nil {
		a.setPluginState(id, api.PluginStarting, "")
	}
	a.mu.Unlock()
	if err == nil {
		err = a.preparePluginRun(plan)
	}
	if err == nil {
		if _, err = a.plugins.runner.Run("podman", plugin.RunArgs(plan.spec)...); err != nil {
			err = fmt.Errorf("podman couldn't start it: %w", err)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		slog.Error("plugin couldn't start", "plugin", id, "error", err)
		a.setPluginState(id, api.PluginFailed, "It couldn't start: "+err.Error())
		return err
	}
	if live := a.plugins.live[id]; live != nil {
		live.startedAt = a.now()
	}
	a.setPluginState(id, api.PluginRunning, "")
	slog.Info("plugin started", "plugin", id)
	return nil
}

// preparePluginRun mounts the plugin's data and drive, writes its
// templates and opens its power socket. Called without a.mu.
func (a *Agent) preparePluginRun(plan *pluginRunPlan) error {
	if err := os.MkdirAll(a.paths.PluginRunDir(plan.id), 0o700); err != nil {
		return err
	}
	if err := a.mountPluginData(plan.id, plan.storageMB); err != nil {
		return fmt.Errorf("its data can't be opened: %w", err)
	}
	dataMount := a.pluginDataMount(plan.id)
	for _, v := range plan.manifest.Volumes {
		if err := os.MkdirAll(filepath.Join(dataMount, v.Name), 0o755); err != nil {
			return err
		}
	}
	for _, t := range plan.templates {
		target := filepath.Join(dataMount, t.volume, filepath.FromSlash(t.relative))
		if _, err := os.Stat(target); err == nil && t.once {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := config.WriteFileAtomic(target, t.content, 0o644); err != nil {
			return err
		}
	}
	if plan.drive != "" {
		if err := a.mountDrive(plan.drive); err != nil {
			if plan.manifest.USBDrive == nil || !plan.manifest.USBDrive.Optional {
				return err
			}
			slog.Warn("the plugin's USB drive isn't there; starting without it", "plugin", plan.id, "error", err)
			plan.spec.Mounts = removeMount(plan.spec.Mounts, a.paths.DriveMountDir(plan.drive))
		}
	}
	if plan.power {
		if err := a.openPowerSocket(plan.id); err != nil {
			return fmt.Errorf("its connection to RasBeacon can't be opened: %w", err)
		}
	}
	return os.MkdirAll(filepath.Dir(plan.spec.LogFile), 0o700)
}

func removeMount(mounts []plugin.Mount, host string) []plugin.Mount {
	var kept []plugin.Mount
	for _, m := range mounts {
		if m.Host != host {
			kept = append(kept, m)
		}
	}
	return kept
}

// mountPluginData makes (or grows) the plugin's data file and mounts it.
// The file has a fixed size, so a plugin can't fill the SD card.
func (a *Agent) mountPluginData(id string, storageMB int) error {
	r := a.plugins.runner
	mount := a.pluginDataMount(id)
	if _, err := r.Run("mountpoint", "-q", mount); err == nil {
		return nil // already mounted
	}
	if err := os.MkdirAll(mount, 0o755); err != nil {
		return err
	}
	file := filepath.Join(a.paths.PluginDir(id), pluginDataImage)
	size := strconv.Itoa(storageMB) + "M"
	info, err := os.Stat(file)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if _, err := r.Run("fallocate", "--length", size, file); err != nil {
			return fmt.Errorf("no room for its %d MB of data: %w", storageMB, err)
		}
		if _, err := r.Run("mkfs.ext4", "-q", "-F", "-m", "0", "-L", "plugin", file); err != nil {
			_ = os.Remove(file)
			return err
		}
	case err != nil:
		return err
	case info.Size() < int64(storageMB)<<20:
		// A newer version wants more room: grow the file and the filesystem.
		_, _ = r.Run("e2fsck", "-f", "-p", file)
		if _, err := r.Run("fallocate", "--length", size, file); err != nil {
			return fmt.Errorf("no room to grow its data to %d MB: %w", storageMB, err)
		}
		if _, err := r.Run("resize2fs", file); err != nil {
			return err
		}
	}
	_, err = r.Run("mount", "-o", "loop,nosuid,nodev", file, mount)
	return err
}

// unmountPluginData unmounts a plugin's data (errors: it wasn't mounted).
func (a *Agent) unmountPluginData(id string) {
	_, _ = a.plugins.runner.Run("umount", a.pluginDataMount(id))
}

// mountDrive mounts a USB drive for a plugin, so nothing on it can run.
func (a *Agent) mountDrive(uuid string) error {
	r := a.plugins.runner
	dir := a.paths.DriveMountDir(uuid)
	if _, err := r.Run("mountpoint", "-q", dir); err == nil {
		return nil
	}
	out, err := a.runner.Run("lsblk", plugin.LsblkArgs...)
	if err != nil {
		return fmt.Errorf("the USB drives can't be listed: %w", err)
	}
	drives, _ := plugin.ParseDrives(out)
	for _, d := range drives {
		if d.UUID != uuid {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		kind, options := plugin.MountOptions(d.FSType)
		if _, err := r.Run("mount", "-t", kind, "-o", options, d.Device, dir); err != nil {
			return fmt.Errorf("the USB drive can't be opened: %w", err)
		}
		return nil
	}
	return errors.New("its USB drive isn't plugged in")
}

// stopPluginContainer stops and removes a plugin's container, unmounts its
// drive and closes its power socket. Called without a.mu.
func (a *Agent) stopPluginContainer(id string) {
	_, _ = a.plugins.runner.Run("podman", "rm", "--force", "--ignore", "--time", "10", plugin.ContainerName(id))
	a.mu.Lock()
	drive := a.cfg.Plugins.Installed[id].Drive
	a.mu.Unlock()
	if drive != "" {
		_, _ = a.plugins.runner.Run("umount", a.paths.DriveMountDir(drive))
	}
	a.closePowerSocket(id)
}

// ensurePluginNetwork makes the plugins' bridge (before dnsmasq starts, so
// it can answer plugins) and Podman's network on it. The caller holds a.mu.
func (a *Agent) ensurePluginNetwork() bool {
	// Podman unpacks images here (see the image's containers.conf).
	if err := os.MkdirAll(filepath.Join(a.paths.ContainersDir(), "tmp"), 0o700); err != nil {
		slog.Warn("could not make Podman's folder", "error", err)
	}
	if err := system.EnsureBridge(a.runner, render.PluginBridge, render.PluginGateway+"/24"); err != nil {
		slog.Error("could not make the plugins' network", "error", err)
		return false
	}
	return true
}

// startPlugins starts the plugins that were running before RasBeacon
// restarted, and the crash watch. Runs in the background.
func (a *Agent) startPlugins() {
	if _, err := a.plugins.runner.Run("podman", plugin.NetworkCreateArgs()...); err != nil {
		slog.Error("could not make Podman's network for plugins", "error", err)
	}
	a.mu.Lock()
	var ids []string
	for _, id := range a.pluginIDs() {
		p := a.cfg.Plugins.Installed[id]
		live := a.plugins.live[id]
		if p.Running && live != nil && live.manifest != nil && !live.busy {
			live.busy = true
			ids = append(ids, id)
		}
	}
	watching := a.plugins.watching
	a.plugins.watching = true
	a.mu.Unlock()
	for _, id := range ids {
		_ = a.runPlugin(id)
		a.endPluginWork(id)
	}
	if !watching && a.plugins.watchEvery > 0 {
		go a.watchPlugins()
	}
}

// watchPlugins restarts crashed plugins, reads health and metrics, and
// checks the catalog daily. It runs for as long as rbd does.
func (a *Agent) watchPlugins() {
	for round := 0; ; round++ {
		time.Sleep(a.plugins.watchEvery)
		a.checkPluginContainers()
		if round%2 == 0 {
			a.readPluginHealth()
		}
		a.dailyCatalogCheck()
	}
}

// podmanContainer is what `podman ps --format json` says about one.
type podmanContainer struct {
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

// checkPluginContainers finds plugins that should run but don't, and
// starts them again; after maxCrashes in a row it stops trying.
func (a *Agent) checkPluginContainers() {
	out, err := a.plugins.runner.Run("podman", "ps", "--all", "--format", "json", "--filter", "label="+plugin.Label)
	if err != nil {
		return
	}
	var containers []podmanContainer
	if err := json.Unmarshal([]byte(out), &containers); err != nil {
		return
	}
	running := map[string]bool{}
	for _, c := range containers {
		if c.State == "running" {
			running[c.Labels[plugin.Label]] = true
		}
	}

	a.mu.Lock()
	var restart []string
	for id, p := range a.cfg.Plugins.Installed {
		live := a.plugins.live[id]
		if live == nil || live.busy || !p.Running || live.state != api.PluginRunning || running[id] {
			continue
		}
		if a.now().Sub(live.startedAt) > stableAfter {
			live.crashes = 0
		}
		live.crashes++
		slog.Warn("plugin stopped by itself", "plugin", id, "times", live.crashes)
		if live.crashes >= maxCrashes {
			_ = a.savePluginConfig(id, func(p *config.InstalledPlugin) { p.Running, p.StoppedAfterCrashes = false, true })
			a.setPluginState(id, api.PluginCrashed, "It stopped working several times in a row, so RasBeacon stopped it. Check its log, then start it again.")
			continue
		}
		live.busy = true
		restart = append(restart, id)
	}
	a.mu.Unlock()
	for _, id := range restart {
		_ = a.runPlugin(id)
		a.endPluginWork(id)
	}
}

// pluginCheck is one health or metric request.
type pluginCheck struct {
	id      string
	health  string
	metrics []plugin.Metric
	values  plugin.Values
}

// readPluginHealth asks running plugins' health URL and metrics.
func (a *Agent) readPluginHealth() {
	a.mu.Lock()
	var checks []pluginCheck
	for id, p := range a.cfg.Plugins.Installed {
		live := a.plugins.live[id]
		if live == nil || live.manifest == nil || live.state != api.PluginRunning {
			continue
		}
		c := pluginCheck{id: id, metrics: live.manifest.Metrics, values: a.pluginValues(id, live.manifest, p, true)}
		if live.manifest.Health != nil {
			c.health = live.manifest.Health.URL
		}
		checks = append(checks, c)
	}
	a.mu.Unlock()

	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	for _, c := range checks {
		var healthy *bool
		if c.health != "" {
			ok := false
			if resp, err := client.Get(c.values.Expand(c.health)); err == nil {
				ok = resp.StatusCode < 500
				resp.Body.Close()
			}
			healthy = &ok
		}
		metrics := []api.PluginMetric{}
		for _, m := range c.metrics {
			metrics = append(metrics, api.PluginMetric{Label: m.Label, Value: readOneMetric(client, m, c.values)})
		}
		a.mu.Lock()
		if live := a.plugins.live[c.id]; live != nil && live.state == api.PluginRunning {
			live.healthy, live.metrics, live.checkedAt = healthy, metrics, a.now()
		}
		a.mu.Unlock()
	}
}

// readOneMetric asks the plugin for one metric ("" if it can't be read).
func readOneMetric(client *http.Client, m plugin.Metric, values plugin.Values) string {
	req, err := http.NewRequest(http.MethodGet, values.Expand(m.URL), nil)
	if err != nil {
		return ""
	}
	if m.Username != "" || m.Password != "" {
		req.SetBasicAuth(values.Expand(m.Username), values.Expand(m.Password))
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	value, err := plugin.ReadMetric(m, body)
	if err != nil {
		return ""
	}
	return value
}

// --- The power socket -------------------------------------------------

// openPowerSocket lets a plugin with the power-button permission ask
// RasBeacon to shut down or restart: a socket in a folder only that
// plugin's container gets.
func (a *Agent) openPowerSocket(id string) error {
	a.closePowerSocket(id)
	dir := a.pluginSocketDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "rbd.sock")
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /power", func(w http.ResponseWriter, r *http.Request) {
		var req api.PluginPowerRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var command string
		switch req.Action {
		case "shutdown":
			command = "poweroff"
		case "reboot":
			command = "reboot"
		default:
			http.Error(w, "action must be shutdown or reboot", http.StatusBadRequest)
			return
		}
		slog.Warn("a plugin asked RasBeacon to "+req.Action, "plugin", id)
		w.WriteHeader(http.StatusNoContent)
		a.afterFunc(time.Second, func() { _, _ = a.runner.Run("systemctl", command) })
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	a.mu.Lock()
	if a.powerSockets == nil {
		a.powerSockets = map[string]*http.Server{}
	}
	a.powerSockets[id] = server
	a.mu.Unlock()
	go func() { _ = server.Serve(listener) }()
	return nil
}

func (a *Agent) closePowerSocket(id string) {
	a.mu.Lock()
	server := a.powerSockets[id]
	delete(a.powerSockets, id)
	a.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
}

// stopAllPlugins stops every plugin's container and unmounts their data,
// before a factory reset. The caller holds a.mu.
func (a *Agent) stopAllPlugins() {
	_, _ = a.plugins.runner.Run("podman", "rm", "--all", "--force", "--time", "5")
	for id := range a.cfg.Plugins.Installed {
		_, _ = a.plugins.runner.Run("umount", a.pluginDataMount(id))
		if drive := a.cfg.Plugins.Installed[id].Drive; drive != "" {
			_, _ = a.plugins.runner.Run("umount", a.paths.DriveMountDir(drive))
		}
	}
}
