package plugin

import "testing"

// A setting about the USB drive (onlyWithDrive) needs a drive to be about,
// and can't be required, since without a drive it isn't shown.
func TestParseManifest_OnlyWithDrive(t *testing.T) {
	withSetting := func(setting map[string]any, drive bool) map[string]any {
		m := validManifest()
		m["settings"] = []map[string]any{setting}
		if drive {
			m["permissions"] = []string{"usb-drive"}
			m["usbDrive"] = map[string]any{"path": "/share", "optional": true}
		}
		return m
	}
	limit := map[string]any{"key": "limit", "label": "Size limit", "type": "number", "onlyWithDrive": true}
	if m, err := ParseManifest([]byte(manifestJSON(withSetting(limit, true))), false); err != nil || !m.Settings[0].OnlyWithDrive {
		t.Errorf("a drive setting with a drive: %v", err)
	}
	if _, err := ParseManifest([]byte(manifestJSON(withSetting(limit, false))), false); err == nil {
		t.Error("a drive setting without usbDrive must be refused")
	}
	required := map[string]any{"key": "limit", "label": "Size limit", "type": "number", "onlyWithDrive": true, "required": true}
	if _, err := ParseManifest([]byte(manifestJSON(withSetting(required, true))), false); err == nil {
		t.Error("a required drive setting must be refused")
	}
}
