package plugin

import (
	"fmt"
	"slices"
	"strconv"
)

// Podman's network for plugins: a bridge RasBeacon makes itself
// (NetworkInterface, gateway Gateway), without Podman's firewall rules or
// DNS server; RasBeacon's firewall decides what each plugin may reach.
const (
	NetworkName      = "rasbeacon-plugins"
	NetworkInterface = "rbplug0"
	Gateway          = "10.89.0.1"
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
	Env      map[string]string
	// Masked paths are hidden from the container (e.g. the temperature
	// without that permission).
	Masked  []string
	LogFile string
	Command []string
}

// ContainerName is a plugin's container's name.
func ContainerName(id string) string { return ContainerPrefix + id }

// RunArgs are the arguments of `podman` that start a plugin.
func RunArgs(s RunSpec) []string {
	args := []string{
		"run", "--detach", "--replace",
		"--name", ContainerName(s.ID),
		"--label", Label + "=" + s.ID,
		"--network", NetworkName,
		"--ip", s.Address,
		"--dns", Gateway,
		"--hostname", s.ID,
		"--memory", strconv.Itoa(s.MemoryMB) + "m",
		"--cpus", strconv.FormatFloat(s.CPUs, 'f', -1, 64),
		"--pids-limit", "512",
		"--restart", "no",
		"--security-opt", "no-new-privileges",
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
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		args = append(args, "--env", k+"="+s.Env[k])
	}
	args = append(args, s.Image)
	return append(args, s.Command...)
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

// sanitizeTag turns a version into a valid image tag.
func sanitizeTag(v string) string {
	out := []byte{}
	for i := 0; i < len(v) && len(out) < 120; i++ {
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
	return string(out)
}
