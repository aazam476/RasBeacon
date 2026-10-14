// Package plugin reads RasBeacon plugins: .rba files, their manifest, their
// signature, and the catalogs that list them. It only reads and checks; the
// agent (internal/agent/plugins.go) installs and runs them with Podman.
//
// # The .rba file
//
// A plugin is a zip file renamed to .rba, holding:
//
//	manifest.json   what the plugin is and needs (see Manifest)
//	icon.png        its icon, square, at least 128×128
//	README.md       optional: shown on its page
//	templates/      optional: files filled in from its settings (see Template)
//	image.tar       its container image (`docker save` or `podman save`), or
//	                none when the manifest names an image in a registry
//	signature       optional: the author's signature over all other files
//
// docs/plugins.md describes the format for plugin authors.
package plugin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Manifest is manifest.json.
type Manifest struct {
	// ID names the plugin on every RasBeacon: lowercase letters, digits and
	// dashes, 2 to 32 long. An update must have the same ID.
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	// Boards it runs on (pi5, pi4, pi3, zero2w); empty: all.
	Boards []string `json:"boards,omitempty"`
	// Image is a container image in a registry, pinned by digest. Without
	// it the .rba must hold image.tar.
	Image *RegistryImage `json:"image,omitempty"`
	// Permissions it asks for (see Permissions). Anything not listed is
	// refused.
	Permissions []string `json:"permissions,omitempty"`
	// Ports the container listens on, that devices on the hotspot may
	// reach. The owner chooses the port on RasBeacon for each.
	Ports []Port `json:"ports,omitempty"`
	// MemoryMB and CPUs cap what it may use (defaults 256 MB, half a core).
	MemoryMB int     `json:"memoryMb,omitempty"`
	CPUs     float64 `json:"cpus,omitempty"`
	// MinMemoryMB and MinSwapMB refuse the install on a Pi with less.
	MinMemoryMB int `json:"minMemoryMb,omitempty"`
	MinSwapMB   int `json:"minSwapMb,omitempty"`
	// StorageMB is the size of its data (default 256 MB), kept in one
	// file on the SD card and shared by its Volumes.
	StorageMB int      `json:"storageMb,omitempty"`
	Volumes   []Volume `json:"volumes,omitempty"`
	// USBDrive is where the drive the owner picks appears, with the
	// usb-drive permission.
	USBDrive *USBDrive `json:"usbDrive,omitempty"`
	// Env are fixed environment variables.
	Env map[string]string `json:"env,omitempty"`
	// Command replaces the image's command, if set.
	Command   []string   `json:"command,omitempty"`
	Settings  []Setting  `json:"settings,omitempty"`
	Templates []Template `json:"templates,omitempty"`
	// AfterInstall is shown after installing and on its page, with
	// placeholders (see Expand): what to do next.
	AfterInstall string   `json:"afterInstall,omitempty"`
	Metrics      []Metric `json:"metrics,omitempty"`
	Health       *Health  `json:"health,omitempty"`
}

// RegistryImage is an image to download, e.g. docker.io/adguard/adguardhome
// with sha256:… .
type RegistryImage struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

// Port is a port the container listens on.
type Port struct {
	Name      string `json:"name"`
	Container int    `json:"container"`
	// Protocol is tcp (default) or udp.
	Protocol string `json:"protocol,omitempty"`
	// Default is the port on RasBeacon suggested at install (default: the
	// container's).
	Default int `json:"default,omitempty"`
	// Open puts an Open button on its page: a web page at this port.
	Open bool `json:"open,omitempty"`
	// HTTPS opens it with https:// instead of http://.
	HTTPS bool `json:"https,omitempty"`
}

// Volume is a folder of the plugin's data, mounted at Path.
type Volume struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// USBDrive is where the chosen USB drive is mounted in the container.
type USBDrive struct {
	Path string `json:"path"`
	// Optional lets the owner choose no drive (then a volume at the same
	// path, if any, is used instead).
	Optional bool `json:"optional,omitempty"`
}

// Setting is one field of the plugin's settings form.
type Setting struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Type: text, password, number, switch or select. Passwords are kept
	// with RasBeacon's other secrets.
	Type     string   `json:"type"`
	Help     string   `json:"help,omitempty"`
	Default  string   `json:"default,omitempty"`
	Required bool     `json:"required,omitempty"`
	Options  []Option `json:"options,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	// Env is the environment variable that gets the value, if any.
	Env string `json:"env,omitempty"`
}

// Option is a choice of a select setting.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Template is a file in templates/ written into a volume, filled in with
// the settings (Go text/template: {{.Settings.key}}, {{bcrypt …}}).
type Template struct {
	File string `json:"file"`
	// Path in the container; it must be inside a volume.
	Path string `json:"path"`
	// Once writes it only if it isn't there yet, so changes the plugin
	// makes to it are kept.
	Once bool `json:"once,omitempty"`
}

// Metric is a number shown on the plugin's page, read from its own HTTP API.
type Metric struct {
	Label string `json:"label"`
	// URL with placeholders, e.g. http://{address}:3000/control/stats.
	URL string `json:"url"`
	// Path in the JSON answer, e.g. "num_dns_queries" or "a.b.0".
	Path string `json:"path"`
	// DivideBy is another path; the metric becomes Path/DivideBy.
	DivideBy string `json:"divideBy,omitempty"`
	// Format: number (default), percent (0 to 1), bytes, ms, seconds, text.
	Format string `json:"format,omitempty"`
	// Scale multiplies the number first (e.g. 1000 for seconds to ms).
	Scale    float64 `json:"scale,omitempty"`
	Username string  `json:"username,omitempty"`
	Password string  `json:"password,omitempty"`
}

// Health is how RasBeacon checks the plugin works: an HTTP answer below
// 500 from URL.
type Health struct {
	URL string `json:"url"`
}

// Permission is something a plugin may ask for, in plain words.
type Permission struct {
	ID string `json:"id"`
	// Title and Detail are what the install sheet shows.
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Permissions are all a plugin may ask for. Without "internet" it can't
// reach the internet at all; with it, only through the VPN when the VPN is
// on (and not at all while the kill switch blocks).
var Permissions = []Permission{
	{"internet", "Use the internet", "Through the VPN when it is on, like your devices."},
	{"lan-access", "Reach your devices", "Start connections to devices on your network (not guests)."},
	{"usb-drive", "Use a USB drive", "Only the drive you pick, mounted so nothing on it can run."},
	{"i2c", "Use the I²C bus", "Talk to chips on the board, such as a case's fan controller."},
	{"gpio", "Use the GPIO pins", "Read and set the pins on the Pi's header."},
	{"power-button", "Shut down or restart RasBeacon", "Ask RasBeacon to shut down or restart, for a case's power button."},
	{"temperature", "Read the temperature", "Read the processor's temperature."},
}

// PermissionByID returns a permission's words.
func PermissionByID(id string) (Permission, bool) {
	for _, p := range Permissions {
		if p.ID == id {
			return p, true
		}
	}
	return Permission{}, false
}

// Boards are the board names a manifest may list.
var Boards = []string{"pi5", "pi4", "pi3", "zero2w"}

// Default limits.
const (
	DefaultMemoryMB  = 256
	DefaultCPUs      = 0.5
	DefaultStorageMB = 256
	MaxStorageMB     = 64 * 1024
)

// ReservedPorts are RasBeacon's own ports on the hotspot: SSH, DNS, DHCP,
// the web UI.
var ReservedPorts = []int{22, 53, 67, 80, 443}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$`)
	keyPattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,39}$`)
	envPattern     = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	refPattern     = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(:[0-9]+)?(/[a-z0-9]+([._-][a-z0-9]+)*)+(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?$`)
	volumePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	templateFileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// ParseManifest reads and checks manifest.json. hasImageTar tells whether
// the .rba holds image.tar.
func ParseManifest(data []byte, hasImageTar bool) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	if err := m.check(hasImageTar); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	return &m, nil
}

// ParseInstalledManifest reads an installed plugin's saved manifest (its
// image, from image.tar or a registry, is already installed).
func ParseInstalledManifest(data []byte) (*Manifest, error) {
	var probe struct {
		Image *RegistryImage `json:"image"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	return ParseManifest(data, probe.Image == nil)
}

func (m *Manifest) check(hasImageTar bool) error {
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("id %q must be 2 to 32 lowercase letters, digits and dashes", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 40 {
		return fmt.Errorf("name must be 1 to 40 characters")
	}
	if m.Version == "" || len(m.Version) > 40 || strings.ContainsAny(m.Version, " \t\n/") {
		return fmt.Errorf("version must be 1 to 40 characters without spaces")
	}
	if len(m.Description) > 300 {
		return fmt.Errorf("description must be at most 300 characters")
	}
	if m.Homepage != "" {
		if u, err := url.Parse(m.Homepage); err != nil || u.Scheme != "https" {
			return fmt.Errorf("homepage must be an https:// address")
		}
	}
	for _, b := range m.Boards {
		if !slices.Contains(Boards, b) {
			return fmt.Errorf("unknown board %q (use %s)", b, strings.Join(Boards, ", "))
		}
	}
	switch {
	case m.Image != nil && hasImageTar:
		return fmt.Errorf("has both image.tar and an image to download; use one")
	case m.Image == nil && !hasImageTar:
		return fmt.Errorf("has no image: add image.tar or an image to download")
	case m.Image != nil:
		if !refPattern.MatchString(m.Image.Ref) || strings.Contains(m.Image.Ref, "@") {
			return fmt.Errorf("image ref %q must look like docker.io/owner/name:tag", m.Image.Ref)
		}
		if !digestPattern.MatchString(m.Image.Digest) {
			return fmt.Errorf("image digest must be sha256: followed by 64 hex digits")
		}
	}
	for _, p := range m.Permissions {
		if _, ok := PermissionByID(p); !ok {
			return fmt.Errorf("unknown permission %q", p)
		}
	}
	if slices.Contains(m.Permissions, "usb-drive") != (m.USBDrive != nil) {
		return fmt.Errorf("usbDrive and the usb-drive permission go together")
	}
	seenPorts := map[string]bool{}
	for i := range m.Ports {
		p := &m.Ports[i]
		if p.Protocol == "" {
			p.Protocol = "tcp"
		}
		if p.Protocol != "tcp" && p.Protocol != "udp" {
			return fmt.Errorf("port %d: protocol must be tcp or udp", p.Container)
		}
		if p.Container < 1 || p.Container > 65535 {
			return fmt.Errorf("port %d is not between 1 and 65535", p.Container)
		}
		key := p.Protocol + strconv.Itoa(p.Container)
		if seenPorts[key] {
			return fmt.Errorf("port %d/%s is listed twice", p.Container, p.Protocol)
		}
		seenPorts[key] = true
		if p.Default == 0 {
			p.Default = p.Container
		}
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("port %d needs a name", p.Container)
		}
	}
	if m.MemoryMB < 0 || m.MemoryMB > 8192 || m.CPUs < 0 || m.CPUs > 4 {
		return fmt.Errorf("memoryMb must be at most 8192 and cpus at most 4")
	}
	if m.MinMemoryMB < 0 || m.MinSwapMB < 0 {
		return fmt.Errorf("minMemoryMb and minSwapMb can't be negative")
	}
	if m.StorageMB < 0 || m.StorageMB > MaxStorageMB {
		return fmt.Errorf("storageMb must be at most %d", MaxStorageMB)
	}
	seenVolumes := map[string]bool{}
	for _, v := range m.Volumes {
		if !volumePattern.MatchString(v.Name) || seenVolumes[v.Name] {
			return fmt.Errorf("volume names must be distinct lowercase words (%q)", v.Name)
		}
		seenVolumes[v.Name] = true
		if !absolutePath(v.Path) {
			return fmt.Errorf("volume %s: path must be absolute", v.Name)
		}
	}
	if m.USBDrive != nil && !absolutePath(m.USBDrive.Path) {
		return fmt.Errorf("usbDrive: path must be absolute")
	}
	for k := range m.Env {
		if !envPattern.MatchString(k) {
			return fmt.Errorf("env %q must be uppercase letters, digits and _", k)
		}
	}
	seenKeys := map[string]bool{}
	for i := range m.Settings {
		s := &m.Settings[i]
		if !keyPattern.MatchString(s.Key) || seenKeys[s.Key] {
			return fmt.Errorf("setting keys must be distinct words (%q)", s.Key)
		}
		seenKeys[s.Key] = true
		if strings.TrimSpace(s.Label) == "" {
			return fmt.Errorf("setting %s needs a label", s.Key)
		}
		if s.Env != "" && !envPattern.MatchString(s.Env) {
			return fmt.Errorf("setting %s: env %q must be uppercase letters, digits and _", s.Key, s.Env)
		}
		switch s.Type {
		case "text", "password", "number", "switch":
		case "select":
			if len(s.Options) == 0 {
				return fmt.Errorf("setting %s: a select needs options", s.Key)
			}
		default:
			return fmt.Errorf("setting %s: unknown type %q", s.Key, s.Type)
		}
		if s.Default != "" {
			if err := s.Check(s.Default); err != nil {
				return fmt.Errorf("setting %s: default: %w", s.Key, err)
			}
		}
	}
	for _, t := range m.Templates {
		if !templateFileRe.MatchString(t.File) {
			return fmt.Errorf("template %q: plain file names only", t.File)
		}
		if _, ok := m.VolumeFor(t.Path); !ok {
			return fmt.Errorf("template %s: path %s is not inside a volume", t.File, t.Path)
		}
	}
	if len(m.AfterInstall) > 1000 {
		return fmt.Errorf("afterInstall must be at most 1000 characters")
	}
	if len(m.Metrics) > 4 {
		return fmt.Errorf("at most 4 metrics")
	}
	for _, x := range m.Metrics {
		if strings.TrimSpace(x.Label) == "" || x.Path == "" || !strings.HasPrefix(x.URL, "http://{address}") {
			return fmt.Errorf("metric %q needs a label, a path and a URL starting http://{address}", x.Label)
		}
		switch x.Format {
		case "", "number", "percent", "bytes", "ms", "seconds", "text":
		default:
			return fmt.Errorf("metric %s: unknown format %q", x.Label, x.Format)
		}
	}
	if m.Health != nil && !strings.HasPrefix(m.Health.URL, "http://{address}") {
		return fmt.Errorf("health: the URL must start http://{address}")
	}
	return nil
}

// absolutePath reports whether p is a clean absolute path other than "/".
func absolutePath(p string) bool {
	return strings.HasPrefix(p, "/") && p != "/" && !strings.Contains(p, "..") && !strings.ContainsAny(p, ":,\n")
}

// VolumeFor finds the volume a container path is inside, and the path
// relative to it.
func (m *Manifest) VolumeFor(containerPath string) (Volume, bool) {
	if !absolutePath(containerPath) {
		return Volume{}, false
	}
	for _, v := range m.Volumes {
		if strings.HasPrefix(containerPath, strings.TrimSuffix(v.Path, "/")+"/") {
			return v, true
		}
	}
	return Volume{}, false
}

// Has reports whether the plugin asks for a permission.
func (m *Manifest) Has(permission string) bool { return slices.Contains(m.Permissions, permission) }

// Memory, CPU and storage limits with defaults filled in.
func (m *Manifest) Memory() int {
	if m.MemoryMB == 0 {
		return DefaultMemoryMB
	}
	return m.MemoryMB
}

func (m *Manifest) CPULimit() float64 {
	if m.CPUs == 0 {
		return DefaultCPUs
	}
	return m.CPUs
}

func (m *Manifest) Storage() int {
	if m.StorageMB == 0 {
		return DefaultStorageMB
	}
	return m.StorageMB
}

// RunsOn reports whether the plugin supports a board ("" = unknown: yes).
func (m *Manifest) RunsOn(board string) bool {
	return len(m.Boards) == 0 || board == "" || slices.Contains(m.Boards, board)
}

// SettingByKey finds a setting.
func (m *Manifest) SettingByKey(key string) (Setting, bool) {
	for _, s := range m.Settings {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

// Secret reports whether the setting's value is kept with the secrets.
func (s Setting) Secret() bool { return s.Type == "password" }

// Check checks a value for the setting ("" is allowed unless required).
func (s Setting) Check(value string) error {
	if value == "" {
		if s.Required {
			return fmt.Errorf("required")
		}
		return nil
	}
	if len(value) > 1000 || strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("must be one line of at most 1000 characters")
	}
	switch s.Type {
	case "number":
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("must be a number")
		}
		if s.Min != nil && n < *s.Min || s.Max != nil && n > *s.Max {
			return fmt.Errorf("must be from %s to %s", formatBound(s.Min), formatBound(s.Max))
		}
	case "switch":
		if value != "true" && value != "false" {
			return fmt.Errorf("must be true or false")
		}
	case "select":
		if !slices.ContainsFunc(s.Options, func(o Option) bool { return o.Value == value }) {
			return fmt.Errorf("choose one of the options")
		}
	}
	return nil
}

func formatBound(b *float64) string {
	if b == nil {
		return "any"
	}
	return strconv.FormatFloat(*b, 'f', -1, 64)
}

// Reserved reports whether a port on RasBeacon belongs to RasBeacon itself.
func Reserved(port int) bool { return slices.Contains(ReservedPorts, port) }
