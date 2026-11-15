package plugin

import "testing"

// The shares part of a manifest: where they go, how many, their settings,
// and the usb-drive permission that lets them be on drives.
func TestParseManifest_Shares(t *testing.T) {
	parse := func(change func(m map[string]any)) error {
		m := validManifest()
		m["shares"] = map[string]any{"path": "/shares", "max": 4,
			"settings": []map[string]any{{"key": "readOnly", "label": "Read-only", "type": "switch", "default": "false"}}}
		change(m)
		_, err := ParseManifest([]byte(manifestJSON(m)), false)
		return err
	}
	if err := parse(func(map[string]any) {}); err != nil {
		t.Errorf("valid shares: %v", err)
	}
	if err := parse(func(m map[string]any) { m["permissions"] = []string{"usb-drive"} }); err != nil {
		t.Errorf("shares on drives (usb-drive without usbDrive): %v", err)
	}
	bad := map[string]func(m map[string]any){
		"relative path": func(m map[string]any) { m["shares"].(map[string]any)["path"] = "shares" },
		"too many":      func(m map[string]any) { m["shares"].(map[string]any)["max"] = 99 },
		"with usbDrive": func(m map[string]any) {
			m["permissions"] = []string{"usb-drive"}
			m["usbDrive"] = map[string]any{"path": "/media"}
		},
		"a password setting": func(m map[string]any) {
			m["shares"].(map[string]any)["settings"] = []map[string]any{{"key": "pin", "label": "PIN", "type": "password"}}
		},
		"a setting with env": func(m map[string]any) {
			m["shares"].(map[string]any)["settings"] = []map[string]any{{"key": "x", "label": "X", "type": "text", "env": "X"}}
		},
		"a broken setting": func(m map[string]any) {
			m["shares"].(map[string]any)["settings"] = []map[string]any{{"key": "x", "label": "X", "type": "colour"}}
		},
		"usb-drive for nothing": func(m map[string]any) {
			delete(m, "shares")
			m["permissions"] = []string{"usb-drive"}
		},
	}
	for name, change := range bad {
		if err := parse(change); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
	if got := (&Shares{}).MaxShareCount(); got != DefaultMaxShares {
		t.Errorf("default max = %d", got)
	}
}
