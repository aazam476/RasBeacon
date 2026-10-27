package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// When the browser can't start, Start says why and leaves no profile
// folder behind.
func TestStart_Failures(t *testing.T) {
	profiles := t.TempDir()
	// A browser that prints a reason and exits at once, like Chromium
	// missing a library.
	quitter := filepath.Join(t.TempDir(), "quitter")
	os.WriteFile(quitter, []byte("#!/bin/sh\necho 'missing libnss3' >&2\nexit 1\n"), 0o755)

	for name, tc := range map[string]struct {
		opts Options
		want string
	}{
		"unknown user":       {Options{Binary: quitter, ProfileParent: profiles, User: "no-such-user-rasbeacon"}, "browser user"},
		"no browser":         {Options{Binary: filepath.Join(profiles, "missing"), ProfileParent: profiles}, "starting the browser"},
		"browser that quits": {Options{Binary: quitter, ProfileParent: profiles}, "missing libnss3"},
	} {
		_, err := Start(tc.opts)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
		if left, _ := filepath.Glob(filepath.Join(profiles, "profile-*")); len(left) != 0 {
			t.Errorf("%s: left %v behind", name, left)
		}
	}

	// A profile folder that can't be made.
	file := filepath.Join(t.TempDir(), "a-file")
	os.WriteFile(file, nil, 0o644)
	if _, err := Start(Options{Binary: quitter, ProfileParent: filepath.Join(file, "profiles")}); err == nil {
		t.Error("a profile folder inside a file can't be made")
	}
}
