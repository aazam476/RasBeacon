package plugin

// This file builds the Podman commands that run plugins: their shared
// network, their container's ports, folders, devices and settings.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Podman's network for plugins: a bridge RasBeacon makes itself
// (NetworkInterface, gateway Gateway), without Podman's firewall rules or
// DNS server; RasBeacon's firewall decides what each plugin may reach.
const (
	NetworkName      = "rasbeacon-plugins"
	NetworkInterface = "rbplug0"
	Gateway          = "10.89.0.1"
	// HostPortPrefix starts the name of each plugin's port on the bridge.
	HostPortPrefix = "rbport"
	// ContainerPrefix starts every plugin container's name.
	ContainerPrefix = "rasbeacon-"
	// Label marks plugin containers, with the plugin ID as its value.
	Label = "rasbeacon.plugin"
)

// Mount is a folder or device given to a container.
type Mount struct {
	Host      string
	Container string
	ReadOnly  bool
}

// RunSpec is everything needed to start a plugin's container.
type RunSpec struct {
	ID       string
	Image    string
	Address  string
	MemoryMB int
	CPUs     float64
	Mounts   []Mount
	Devices  []string
	// Env are the container's environment variables. They aren't put on
	// the command line, where an error message or log would show them
	// (plugin passwords are among them), but in EnvFile.
	Env map[string]string
	// EnvFile is where the caller writes EnvFileContents(Env) before
	// running podman, in a folder only root can read.
	EnvFile string
	// Masked paths are hidden from the container (e.g. the temperature
	// without that permission).
	Masked  []string
	LogFile string
	Command []string
}

// HostPortName is the name of the port that plugs the plugin with this
// address into the bridge: "rbport" and the address's last number, such
// as "rbport3" for 10.89.0.3. Each plugin has its own address, so each
// port has its own name.
func HostPortName(address string) string {
	return HostPortPrefix + address[strings.LastIndex(address, ".")+1:]
}

// ContainerName is a plugin's container's name.
func ContainerName(id string) string { return ContainerPrefix + id }

// RunArgs are the arguments of `podman` that start a plugin.
func RunArgs(s RunSpec) []string {
	args := []string{
		// Run in the background, replacing an old container of the same name.
		"run", "--detach", "--replace",
		"--name", ContainerName(s.ID),
		// The label finds RasBeacon's containers (podman ps --filter label=…).
		"--label", Label + "=" + s.ID,
		// Its network, its fixed address on it, the name of its port on
		// the bridge (the firewall drops what plugins send each other by
		// it), and RasBeacon's DNS server.
		"--network", NetworkName + ":ip=" + s.Address + ",host_interface_name=" + HostPortName(s.Address),
		"--dns", Gateway,
		"--hostname", s.ID,
		// What it may use: memory, processor, and at most 512 processes (so
		// a runaway program can't use up the Pi's).
		"--memory", strconv.Itoa(s.MemoryMB) + "m",
		"--cpus", strconv.FormatFloat(s.CPUs, 'f', -1, 64),
		"--pids-limit", "512",
		// RasBeacon restarts it itself (with a crash count); Podman doesn't.
		"--restart", "no",
		// No program in it can gain more privileges (like set-user-ID ones).
		"--security-opt", "no-new-privileges",
		// Its output goes to a file in memory, at most 256 KB.
		"--log-driver", "k8s-file",
		"--log-opt", "path=" + s.LogFile,
		"--log-opt", "max-size=256k",
	}
	for _, m := range s.Mounts {
		volume := m.Host + ":" + m.Container
		if m.ReadOnly {
			volume += ":ro"
		}
		args = append(args, "--volume", volume)
	}
	for _, d := range s.Devices {
		args = append(args, "--device", d)
	}
	for _, path := range s.Masked {
		args = append(args, "--security-opt", "mask="+path)
	}
	if s.EnvFile != "" {
		args = append(args, "--env-file", s.EnvFile)
	}
	args = append(args, s.Image)
	return append(args, s.Command...)
}

// EnvFileContents is Podman's --env-file format for env: one KEY=value line
// per variable, sorted, so the same settings always give the same file (a
// map's order changes from one loop to the next). Values are one line each
// (the manifest and settings checks refuse line breaks), and Podman takes
// everything after the first "=" as the value, as it is.
func EnvFileContents(env map[string]string) []byte {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + env[k] + "\n")
	}
	return []byte(b.String())
}

// NetworkCreateArgs make Podman's network for plugins.
func NetworkCreateArgs() []string {
	return []string{
		"network", "create", "--driver", "bridge",
		"--interface-name", NetworkInterface,
		"--subnet", "10.89.0.0/24", "--gateway", Gateway,
		"--disable-dns", "--ignore",
		NetworkName,
	}
}

// LocalImage is the name RasBeacon gives a plugin's image.
func LocalImage(id, version string) string {
	return fmt.Sprintf("localhost/rasbeacon-plugin/%s:%s", id, sanitizeTag(version))
}

// sanitizeTag turns a version into a valid image tag: letters, digits, '.',
// '-' and '_', at most 128 long, not starting with '.' or '-'. A version
// that had to change also gets the start of its SHA-256, so two versions
// that change into the same text ("1.0+a" and "1.0_a") still get two tags:
// otherwise an update between them would replace the old image, and going
// back to it would fail.
func sanitizeTag(v string) string {
	out := []byte{}
	for i := 0; i < len(v) && len(out) < 110; i++ {
		c := v[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 || out[0] == '.' || out[0] == '-' {
		out = append([]byte{'v'}, out...)
	}
	if string(out) != v {
		sum := sha256.Sum256([]byte(v))
		out = append(out, "-"+hex.EncodeToString(sum[:])[:8]...)
	}
	return string(out)
}
