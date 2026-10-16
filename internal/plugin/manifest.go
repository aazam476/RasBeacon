// Package plugin reads RasBeacon plugins: .rba files, their manifest, their
// signature, and the catalogs that list them. It only reads and checks; the
// agent (internal/agent/plugins.go) installs and runs them with Podman.
//
// # The .rba file
//
// A plugin is a zip file renamed to .rba, holding:
//
//	manifest.json   what the plugin is and needs (see Manifest)
//	icon.png        its icon: a square PNG, 128×128 to 1024×1024
//	                (256×256 is best), at most 512 KB
//	README.md       optional: shown on its page
//	templates/      optional: files filled in from its settings (see Template)
//	image.tar       its container image (`docker save` or `podman save`), or
//	                none when the manifest names an image in a registry
//	signature       optional: the author's signature over all other files
//
// docs/plugins.md describes the format for plugin authors.
//
// # Why so many checks
//
// A plugin file can come from anyone. Everything in it is checked here,
// before RasBeacon shows it to the owner, so the rest of RasBeacon can trust
// that IDs are safe as folder names, paths stay inside the plugin's own
// folders, and URLs only lead to the plugin itself.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"rasbeacon/internal/version"
)

// Manifest is manifest.json. The `json:"…"` after each field is the name
// the field has in the file (Go's encoding/json reads it).
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
	// MinRasBeacon is the oldest RasBeacon it works with, e.g. "0.2.0"
	// (empty: any). An older RasBeacon refuses to install it.
	MinRasBeacon string `json:"minRasBeacon,omitempty"`
	// Image is a container image in a registry, pinned by digest. Without
	// it the .rba must hold image.tar. (A pointer, so "not there" is nil.)
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
	// Min and Max limit a number. They are pointers so that "no limit"
	// (nil) is different from a limit of 0.
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Unit string   `json:"unit,omitempty"`
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

// PermissionByID returns a permission's words. The second result ("ok")
// is false for an unknown ID, a common Go pattern for lookups.
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

// The longest texts a manifest may have, in characters. They keep the
// screens readable (a 300-character button label would break any layout).
const (
	maxNameLength         = 40
	maxDescriptionLength  = 300
	maxAuthorLength       = 100
	maxPortNameLength     = 40
	maxLabelLength        = 40
	maxHelpLength         = 300
	maxUnitLength         = 20
	maxOptionLabelLength  = 60
	maxMetricLabelLength  = 30
	maxAfterInstallLength = 1000
	// MaxValueLength is the longest setting value.
	MaxValueLength = 1000
)

// Patterns (regular expressions) for names. ^ and $ make a pattern match
// the whole text, not just part of it.
var (
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$`)
	keyPattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,39}$`)
	envPattern     = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	refPattern     = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(:[0-9]+)?(/[a-z0-9]+([._-][a-z0-9]+)*)+(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?$`)
	volumePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	templateFileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	// pluginURLPattern is a URL RasBeacon itself asks (metrics, health): only
	// the plugin's own address, an optional port, and a path. Nothing may
	// come between "{address}" and the port or path, so a URL can't be
	// bent to lead elsewhere (like http://{address}@other-host/).
	pluginURLPattern = regexp.MustCompile(`^http://\{address\}(:[0-9]{1,5})?(/[^\s]*)?$`)
	// minVersionPattern is a RasBeacon version a plugin needs: 1 to 3
	// numbers ("0.2", "0.2.1").
	minVersionPattern = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,4}){0,2}$`)
	// driveUUIDPattern is a filesystem's UUID as lsblk shows it: ext4's
	// 8-4-4-4-12 hex digits, FAT's 1234-ABCD, NTFS's 16 hex digits.
	driveUUIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
)

// ValidID reports whether id is a valid plugin ID. IDs become folder names,
// so anything else (like "../x") must never get that far.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// ValidDriveUUID reports whether uuid looks like a USB drive's UUID. It
// becomes a folder name too.
func ValidDriveUUID(uuid string) bool { return driveUUIDPattern.MatchString(uuid) }

// ParseManifest reads and checks manifest.json. hasImageTar tells whether
// the .rba holds image.tar.
func ParseManifest(data []byte, hasImageTar bool) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	// A field RasBeacon doesn't know is most likely a typo ("memoryMB"
	// for "memoryMb"), which would otherwise be silently ignored.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("in manifest.json: %w", err)
	}
	// One JSON object, and nothing after it.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("in manifest.json: there is more after the manifest's closing }")
	}
	if err := m.check(hasImageTar); err != nil {
		return nil, fmt.Errorf("in manifest.json: %w", err)
	}
	return &m, nil
}

// ParseInstalledManifest reads an installed plugin's saved manifest (its
// image, from image.tar or a registry, is already installed).
func ParseInstalledManifest(data []byte) (*Manifest, error) {
	// Peek at the manifest first: without a registry image, the image came
	// from image.tar, which isn't kept after installing.
	var probe struct {
		Image *RegistryImage `json:"image"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("in manifest.json: %w", err)
	}
	return ParseManifest(data, probe.Image == nil)
}

// check checks every part of the manifest, and fills in defaults (a port's
// protocol and default port). The first problem found is returned.
func (m *Manifest) check(hasImageTar bool) error {
	// Each step checks one part; the first error stops the checking.
	steps := []func() error{
		m.checkAbout,
		func() error { return m.checkImage(hasImageTar) },
		m.checkPermissions,
		m.checkPorts,
		m.checkLimits,
		m.checkStorage,
		m.checkEnv,
		m.checkSettings,
		m.checkTemplates,
		m.checkMetrics,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// checkAbout checks what the plugin says about itself.
func (m *Manifest) checkAbout() error {
	if !ValidID(m.ID) {
		return fmt.Errorf("id %q must be 2 to 32 lowercase letters, digits and dashes", m.ID)
	}
	if err := checkLine("name", m.Name, maxNameLength, true); err != nil {
		return err
	}
	if m.Version == "" || len(m.Version) > 40 || strings.ContainsAny(m.Version, " \t\r\n/") {
		return errors.New("version must be 1 to 40 characters, without spaces or /")
	}
	if err := checkParagraph("description", m.Description, maxDescriptionLength); err != nil {
		return err
	}
	if err := checkLine("author", m.Author, maxAuthorLength, false); err != nil {
		return err
	}
	if m.Homepage != "" {
		if u, err := url.Parse(m.Homepage); err != nil || u.Scheme != "https" || u.Host == "" {
			return errors.New("homepage must be an https:// address")
		}
	}
	for _, b := range m.Boards {
		if !slices.Contains(Boards, b) {
			return fmt.Errorf("unknown board %q (use %s)", b, strings.Join(Boards, ", "))
		}
	}
	if m.MinRasBeacon != "" && !minVersionPattern.MatchString(m.MinRasBeacon) {
		return fmt.Errorf("minRasBeacon %q must be a version like 0.2.0", m.MinRasBeacon)
	}
	return checkParagraph("afterInstall", m.AfterInstall, maxAfterInstallLength)
}

// checkImage checks where the container image comes from: image.tar in the
// file, or a registry (then pinned by its digest, so the download is
// exactly the image the author tested).
func (m *Manifest) checkImage(hasImageTar bool) error {
	switch {
	case m.Image != nil && hasImageTar:
		return errors.New("has both image.tar and an image to download; use one")
	case m.Image == nil && !hasImageTar:
		return errors.New("has no image: add image.tar or an image to download")
	case m.Image == nil:
		return nil
	}
	ref := m.Image.Ref
	registry, _, _ := strings.Cut(ref, "/")
	// The registry must be named ("docker.io/…", not just "owner/name"),
	// or Podman would have to guess which registry is meant.
	if !refPattern.MatchString(ref) || strings.Contains(ref, "@") || !strings.ContainsAny(registry, ".:") {
		return fmt.Errorf("image ref %q must look like docker.io/owner/name:tag", ref)
	}
	if !digestPattern.MatchString(m.Image.Digest) {
		return errors.New("image digest must be sha256: followed by 64 hex digits")
	}
	return nil
}

// checkPermissions checks that every permission is one RasBeacon knows.
func (m *Manifest) checkPermissions() error {
	for _, p := range m.Permissions {
		if _, ok := PermissionByID(p); !ok {
			return fmt.Errorf("unknown permission %q", p)
		}
	}
	// "!=" between two bools is true when exactly one of them is true.
	if m.Has("usb-drive") != (m.USBDrive != nil) {
		return errors.New("usbDrive and the usb-drive permission go together")
	}
	return nil
}

// checkPorts checks the ports and fills in their defaults.
func (m *Manifest) checkPorts() error {
	seen := map[string]bool{}
	for i := range m.Ports {
		// A pointer into the list, so the defaults filled in below stay.
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
		key := p.Protocol + "/" + strconv.Itoa(p.Container)
		if seen[key] {
			return fmt.Errorf("port %s is listed twice", key)
		}
		seen[key] = true
		if p.Default < 0 || p.Default > 65535 {
			return fmt.Errorf("port %s: default must be from 1 to 65535", key)
		}
		if p.Default == 0 {
			p.Default = p.Container
		}
		if err := checkLine("port "+key+": name", p.Name, maxPortNameLength, true); err != nil {
			return err
		}
	}
	return nil
}

// checkLimits checks the memory and processor limits.
func (m *Manifest) checkLimits() error {
	if m.MemoryMB < 0 || m.MemoryMB > 8192 || m.CPUs < 0 || m.CPUs > 4 {
		return errors.New("memoryMb must be at most 8192 and cpus at most 4")
	}
	if m.MinMemoryMB < 0 || m.MinSwapMB < 0 {
		return errors.New("minMemoryMb and minSwapMb can't be negative")
	}
	return nil
}

// checkStorage checks the data size, the volumes and the USB drive's path.
func (m *Manifest) checkStorage() error {
	if m.StorageMB < 0 || m.StorageMB > MaxStorageMB {
		return fmt.Errorf("storageMb must be at most %d", MaxStorageMB)
	}
	names, paths := map[string]bool{}, map[string]bool{}
	for _, v := range m.Volumes {
		if !volumePattern.MatchString(v.Name) || names[v.Name] {
			return fmt.Errorf("volume names must be distinct lowercase words (%q)", v.Name)
		}
		names[v.Name] = true
		if !absolutePath(v.Path) {
			return fmt.Errorf("volume %s: path must be absolute", v.Name)
		}
		if paths[v.Path] {
			return fmt.Errorf("volume %s: another volume has the same path", v.Name)
		}
		paths[v.Path] = true
	}
	if m.USBDrive != nil && !absolutePath(m.USBDrive.Path) {
		return errors.New("usbDrive: path must be absolute")
	}
	return nil
}

// checkEnv checks the fixed environment variables.
func (m *Manifest) checkEnv() error {
	for name, value := range m.Env {
		if !envPattern.MatchString(name) {
			return fmt.Errorf("env %q must be uppercase letters, digits and _", name)
		}
		if !oneLine(value) {
			return fmt.Errorf("env %s must be one line", name)
		}
	}
	return nil
}

// checkSettings checks the settings form: keys, labels, types, choices,
// limits and defaults.
func (m *Manifest) checkSettings() error {
	seen := map[string]bool{}
	for _, s := range m.Settings {
		if !keyPattern.MatchString(s.Key) || seen[s.Key] {
			return fmt.Errorf("setting keys must be distinct words (%q)", s.Key)
		}
		seen[s.Key] = true
		field := "setting " + s.Key
		if err := checkLine(field+": label", s.Label, maxLabelLength, true); err != nil {
			return err
		}
		if err := checkParagraph(field+": help", s.Help, maxHelpLength); err != nil {
			return err
		}
		if err := checkLine(field+": unit", s.Unit, maxUnitLength, false); err != nil {
			return err
		}
		if s.Env != "" && !envPattern.MatchString(s.Env) {
			return fmt.Errorf("%s: env %q must be uppercase letters, digits and _", field, s.Env)
		}
		switch s.Type {
		case "text", "password", "switch":
		case "number":
			if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
				return fmt.Errorf("%s: min is more than max", field)
			}
		case "select":
			if err := checkOptions(s); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: unknown type %q", field, s.Type)
		}
		if s.Default != "" {
			if err := s.Check(s.Default); err != nil {
				return fmt.Errorf("%s: default: %w", field, err)
			}
		}
	}
	return nil
}

// checkOptions checks a select setting's choices.
func checkOptions(s Setting) error {
	if len(s.Options) == 0 {
		return fmt.Errorf("setting %s: a select needs options", s.Key)
	}
	seen := map[string]bool{}
	for _, o := range s.Options {
		if seen[o.Value] || !oneLine(o.Value) || utf8.RuneCountInString(o.Value) > MaxValueLength {
			return fmt.Errorf("setting %s: option values must be distinct, one line each", s.Key)
		}
		seen[o.Value] = true
		if err := checkLine("setting "+s.Key+": option label", o.Label, maxOptionLabelLength, true); err != nil {
			return err
		}
	}
	return nil
}

// checkTemplates checks that every template has a plain file name and is
// written inside one of the plugin's volumes.
func (m *Manifest) checkTemplates() error {
	for _, t := range m.Templates {
		if !templateFileRe.MatchString(t.File) {
			return fmt.Errorf("template %q: plain file names only", t.File)
		}
		if strings.HasSuffix(t.Path, "/") {
			return fmt.Errorf("template %s: path %s names a folder, not a file", t.File, t.Path)
		}
		if _, ok := m.VolumeFor(t.Path); !ok {
			return fmt.Errorf("template %s: path %s is not inside a volume", t.File, t.Path)
		}
	}
	return nil
}

// checkMetrics checks the metrics and the health check: at most 4 metrics,
// and URLs that only reach the plugin itself.
func (m *Manifest) checkMetrics() error {
	if len(m.Metrics) > 4 {
		return errors.New("at most 4 metrics")
	}
	for _, x := range m.Metrics {
		if err := checkLine("metric label", x.Label, maxMetricLabelLength, true); err != nil {
			return err
		}
		if x.Path == "" || !pluginURLPattern.MatchString(x.URL) {
			return fmt.Errorf("metric %q needs a path, and a URL like http://{address}:3000/stats", x.Label)
		}
		switch x.Format {
		case "", "number", "percent", "bytes", "ms", "seconds", "text":
		default:
			return fmt.Errorf("metric %s: unknown format %q", x.Label, x.Format)
		}
	}
	if m.Health != nil && !pluginURLPattern.MatchString(m.Health.URL) {
		return errors.New("health: the URL must look like http://{address}:3000/")
	}
	return nil
}

// checkLine checks a one-line text shown on RasBeacon's screens (a name, a
// label): at most limit characters, no control characters such as line
// breaks, and not empty when required. Characters are counted, not bytes:
// "é" is one character but two bytes.
func checkLine(field, text string, limit int, required bool) error {
	switch {
	case required && strings.TrimSpace(text) == "":
		return fmt.Errorf("%s is missing", field)
	case utf8.RuneCountInString(text) > limit:
		return fmt.Errorf("%s must be at most %d characters", field, limit)
	case strings.ContainsFunc(text, unicode.IsControl):
		return fmt.Errorf("%s must be one line", field)
	}
	return nil
}

// checkParagraph checks a longer text (a description, help): at most limit
// characters; line breaks are fine, other control characters aren't.
func checkParagraph(field, text string, limit int) error {
	if utf8.RuneCountInString(text) > limit {
		return fmt.Errorf("%s must be at most %d characters", field, limit)
	}
	if strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) {
		return fmt.Errorf("%s has a character that can't be shown", field)
	}
	return nil
}

// oneLine reports whether a value has no line breaks or NUL characters.
func oneLine(value string) bool { return !strings.ContainsAny(value, "\x00\r\n") }

// absolutePath reports whether p is a clean absolute path other than "/".
// ".." could climb out of a folder, and ":" and "," mean something else to
// Podman's --volume option, so none of them may be in it.
func absolutePath(p string) bool {
	return strings.HasPrefix(p, "/") && p != "/" && !strings.Contains(p, "..") && !strings.ContainsAny(p, ":,\x00\r\n")
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

// Memory is the plugin's memory limit in MB (the default if it set none).
func (m *Manifest) Memory() int {
	if m.MemoryMB == 0 {
		return DefaultMemoryMB
	}
	return m.MemoryMB
}

// CPULimit is how many processor cores it may use (the default if none set).
func (m *Manifest) CPULimit() float64 {
	if m.CPUs == 0 {
		return DefaultCPUs
	}
	return m.CPUs
}

// Storage is the size of its data in MB (the default if none set).
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
// The error's text is shown under the field, e.g. "must be a number".
func (s Setting) Check(value string) error {
	if value == "" {
		if s.Required {
			return errors.New("required")
		}
		return nil
	}
	if utf8.RuneCountInString(value) > MaxValueLength || !oneLine(value) {
		return fmt.Errorf("must be one line of at most %d characters", MaxValueLength)
	}
	switch s.Type {
	case "number":
		n, err := strconv.ParseFloat(value, 64)
		// ParseFloat also reads "NaN" and "Inf", which aren't numbers anyone
		// means to type.
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return errors.New("must be a number")
		}
		if (s.Min != nil && n < *s.Min) || (s.Max != nil && n > *s.Max) {
			return errors.New(rangeText(s.Min, s.Max))
		}
	case "switch":
		if value != "true" && value != "false" {
			return errors.New("must be true or false")
		}
	case "select":
		if !slices.ContainsFunc(s.Options, func(o Option) bool { return o.Value == value }) {
			return errors.New("choose one of the options")
		}
	}
	return nil
}

// rangeText says which numbers a setting takes: "must be from 1 to 10",
// "must be at least 1" or "must be at most 10".
func rangeText(low, high *float64) string {
	format := func(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
	switch {
	case low != nil && high != nil:
		return "must be from " + format(*low) + " to " + format(*high)
	case low != nil:
		return "must be at least " + format(*low)
	default:
		return "must be at most " + format(*high)
	}
}

// SupportedBy reports whether a plugin needing RasBeacon min works on the
// RasBeacon version running ("v0.1.0", "v0.2.0-beta.1"). Only the numbers
// count: a beta of 0.2.0 is enough for a plugin needing 0.2.0. Development
// builds ("dev", "dev-abc1234") take every plugin.
func SupportedBy(min, running string) bool {
	if min == "" {
		return true
	}
	result, ok := version.Compare(running, min)
	return !ok || result >= 0
}

// Reserved reports whether a port on RasBeacon belongs to RasBeacon itself.
func Reserved(port int) bool { return slices.Contains(ReservedPorts, port) }
