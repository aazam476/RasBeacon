package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

// Each manifest rule, broken one at a time, is refused with a message that
// tells the plugin's author what to fix. validManifest (plugin_test.go)
// downloads its image, so these parse without image.tar.
func TestParseManifest_RefusesEachBrokenRule(t *testing.T) {
	setting := func(s map[string]any) func(m map[string]any) {
		return func(m map[string]any) {
			base := map[string]any{"key": "x", "label": "X", "type": "text"}
			for k, v := range s {
				base[k] = v
			}
			m["settings"] = []map[string]any{base}
		}
	}
	set := func(field string, value any) func(m map[string]any) {
		return func(m map[string]any) { m[field] = value }
	}
	cases := []struct {
		change func(m map[string]any)
		want   string
	}{
		{set("version", ""), "version must be"},
		{set("version", "1 0"), "version must be"},
		{set("description", strings.Repeat("x", 5000)), "description must be at most"},
		{set("description", "bell\a"), "description has a character"},
		{set("author", "A\nB"), "author must be one line"},
		{set("name", ""), "name is missing"},
		{set("name", strings.Repeat("n", 200)), "name must be at most"},
		{set("homepage", "http://example.com"), "homepage must be an https:// address"},
		{set("boards", []string{"rpi1"}), `unknown board "rpi1"`},
		{set("image", nil), "has no image"},
		{set("image", map[string]any{"ref": "docker.io/a/b@sha256:x", "digest": "sha256:" + strings.Repeat("ab", 32)}), "must look like docker.io"},
		{set("ports", []map[string]any{{"name": "W", "container": 80, "protocol": "sctp"}}), "protocol must be tcp or udp"},
		{set("ports", []map[string]any{{"name": "W", "container": 0}}), "is not between 1 and 65535"},
		{set("ports", []map[string]any{{"name": "W", "container": 80}, {"name": "X", "container": 80}}), "listed twice"},
		{set("memoryMb", 9000), "memoryMb must be at most 8192"},
		{set("cpus", 5), "cpus at most 4"},
		{set("minMemoryMb", -1), "can't be negative"},
		{set("storageMb", 1<<30), "storageMb must be at most"},
		{set("volumes", []map[string]any{{"name": "Conf", "path": "/conf"}}), "volume names must be distinct"},
		{set("volumes", []map[string]any{{"name": "conf", "path": "conf"}}), "path must be absolute"},
		{func(m map[string]any) {
			m["permissions"] = []string{"internet", "usb-drive"}
			m["usbDrive"] = map[string]any{"path": "share"}
		}, "usbDrive: path must be absolute"},
		{set("env", map[string]string{"lower": "x"}), "must be uppercase letters"},
		{setting(map[string]any{"key": "Bad Key"}), "setting keys must be distinct"},
		{func(m map[string]any) {
			m["settings"] = []map[string]any{{"key": "x", "label": "X", "type": "text"}, {"key": "x", "label": "Y", "type": "text"}}
		}, "setting keys must be distinct"},
		{setting(map[string]any{"label": ""}), "label is missing"},
		{setting(map[string]any{"help": "a\x00b"}), "help has a character"},
		{setting(map[string]any{"unit": "m\ns"}), "unit must be one line"},
		{setting(map[string]any{"env": "lower"}), `env "lower" must be uppercase`},
		{setting(map[string]any{"type": "number", "min": 5, "max": 1}), "min is more than max"},
		{setting(map[string]any{"type": "colour"}), `unknown type "colour"`},
		{setting(map[string]any{"type": "number", "default": "lots"}), "default:"},
		{setting(map[string]any{"type": "select"}), "a select needs options"},
		{setting(map[string]any{"type": "select", "options": []map[string]any{{"value": "a", "label": ""}}}), "option label is missing"},
		{set("templates", []map[string]any{{"file": "../x", "path": "/opt/adguardhome/conf/x"}}), "plain file names only"},
		{set("templates", []map[string]any{{"file": "x.yaml", "path": "/opt/adguardhome/conf/"}}), "names a folder"},
		{set("metrics", []map[string]any{
			{"label": "a", "url": "http://{address}:1/", "path": "a"}, {"label": "b", "url": "http://{address}:1/", "path": "a"},
			{"label": "c", "url": "http://{address}:1/", "path": "a"}, {"label": "d", "url": "http://{address}:1/", "path": "a"},
			{"label": "e", "url": "http://{address}:1/", "path": "a"},
		}), "at most 4 metrics"},
		{set("metrics", []map[string]any{{"label": "a", "url": "http://{address}:1/", "path": ""}}), "needs a path"},
		{set("metrics", []map[string]any{{"label": "a", "url": "http://{address}:1/", "path": "a", "format": "roman"}}), `unknown format "roman"`},
		{set("metrics", []map[string]any{{"label": "", "url": "http://{address}:1/", "path": "a"}}), "metric label is missing"},
	}
	for _, tc := range cases {
		m := validManifest()
		tc.change(m)
		data, _ := json.Marshal(m)
		_, err := ParseManifest(data, false)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", data, err, tc.want)
		}
	}
}

// A plugin that carries its image in image.tar can't also name one to
// download.
func TestParseManifest_ImageTarOrDownloadNotBoth(t *testing.T) {
	data, _ := json.Marshal(validManifest())
	if _, err := ParseManifest(data, true); err == nil || !strings.Contains(err.Error(), "use one") {
		t.Errorf("got %v", err)
	}
}

// An installed plugin's saved manifest is read back, whether its image
// came from a registry or from image.tar (which isn't kept).
func TestParseInstalledManifest(t *testing.T) {
	withRegistry, _ := json.Marshal(validManifest())
	if _, err := ParseInstalledManifest(withRegistry); err != nil {
		t.Errorf("registry image: %v", err)
	}
	m := validManifest()
	delete(m, "image")
	fromTar, _ := json.Marshal(m)
	if _, err := ParseInstalledManifest(fromTar); err != nil {
		t.Errorf("image.tar image: %v", err)
	}
	if _, err := ParseInstalledManifest([]byte("{")); err == nil {
		t.Error("broken JSON must be refused")
	}
}
