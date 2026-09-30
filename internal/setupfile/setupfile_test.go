package setupfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSetupTxt creates a setup.txt with the given contents in a temporary
// directory and returns its path.
func writeSetupTxt(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "setup.txt")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRead_MissingFileMeansOpenSetupNetwork(t *testing.T) {
	result, err := Read(filepath.Join(t.TempDir(), "setup.txt"))
	if err != nil {
		t.Fatalf("missing file should not be an error, got %v", err)
	}
	if result.Present {
		t.Error("Present should be false when the file does not exist")
	}
}

func TestRead_ValidPassword(t *testing.T) {
	path := writeSetupTxt(t, "# my setup password\nsetup_password=correct horse battery\n")
	result, err := Read(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Present || result.Password != "correct horse battery" {
		t.Errorf("got %+v, want password %q", result, "correct horse battery")
	}
}

func TestRead_HandlesWindowsNotepadFile(t *testing.T) {
	// Byte order mark + CRLF line endings, as Windows Notepad may save.
	path := writeSetupTxt(t, "\uFEFFsetup_password=abcdefgh123\r\n")
	result, err := Read(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Password != "abcdefgh123" {
		t.Errorf("password = %q, want abcdefgh123", result.Password)
	}
}

func TestRead_UnusableFilesAreErrorsNotOpenNetworks(t *testing.T) {
	tests := []struct {
		name          string
		contents      string
		errorMentions string
	}{
		{"empty file", "", "no setup_password"},
		{"only comments", "# nothing here\n", "no setup_password"},
		{"password too short", "setup_password=short\n", "8 to 63"},
		{"password too long", "setup_password=" + strings.Repeat("a", 64) + "\n", "8 to 63"},
		{"typo in key", "setup_pasword=abcdefgh123\n", "unknown setting"},
		{"missing equals sign", "abcdefgh123\n", "expected setup_password"},
		{"duplicate key", "setup_password=abcdefgh1\nsetup_password=abcdefgh2\n", "more than once"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Read(writeSetupTxt(t, tc.contents))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !result.Present {
				t.Error("Present must be true so the caller knows NOT to open the network")
			}
			if !strings.Contains(err.Error(), tc.errorMentions) {
				t.Errorf("error %q should mention %q", err, tc.errorMentions)
			}
		})
	}
}

func TestRemove_DeletesFileAndIgnoresMissing(t *testing.T) {
	path := writeSetupTxt(t, "setup_password=abcdefgh123\n")
	if err := Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("setup.txt should be gone")
	}
	if err := Remove(path); err != nil {
		t.Errorf("removing a missing file should be fine, got %v", err)
	}
}
