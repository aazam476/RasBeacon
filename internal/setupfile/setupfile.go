// Package setupfile reads the optional setup.txt file.
//
// # What setup.txt is for
//
// On first boot, RasBeacon broadcasts a "setup network" so you can reach the
// setup page from your phone. By default that network is open (no password):
// the first person to choose an admin password claims the device.
//
// If you want the setup network protected, create a file named setup.txt on
// the SD card's small FAT partition (it shows up as a drive on your computer
// after flashing) containing one line:
//
//	setup_password=your-password-here
//
// The password must be 8 to 63 characters (a Wi-Fi rule). Blank lines and
// lines starting with # are ignored.
//
// # Safety rule
//
// If setup.txt exists but cannot be used, RasBeacon does NOT fall back to an
// open network: you asked for protection, so silently removing it would be
// worse than not starting. The caller (rbd) keeps the setup network off and
// blinks an error code on the board's LED instead.
package setupfile

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"rasbeacon/internal/config"
)

// Result describes what was found.
type Result struct {
	// Present is true if setup.txt exists.
	Present bool
	// Password is the setup network password. Only valid when Present is true
	// and the returned error is nil.
	Password string
}

// Read loads and checks setup.txt at path.
//
//   - File missing: returns Result{Present: false} and no error.
//   - File present and valid: returns the password.
//   - File present but unusable: returns Present: true and an error that
//     explains what to fix.
func Read(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Result{Present: false}, nil
	}
	if err != nil {
		return Result{Present: true}, fmt.Errorf("setup.txt exists but could not be read: %w", err)
	}

	password, err := parse(string(data))
	if err != nil {
		return Result{Present: true}, err
	}
	return Result{Present: true, Password: password}, nil
}

// parse extracts setup_password from the file's text.
func parse(text string) (string, error) {
	// Some editors (Windows Notepad) add an invisible "byte order mark" at the
	// start of the file. Remove it so the first line is read correctly.
	text = strings.TrimPrefix(text, "\uFEFF")

	var password string
	found := false

	scanner := bufio.NewScanner(strings.NewReader(text))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		// TrimRight removes Windows line endings ("\r") and trailing spaces.
		line := strings.TrimRight(scanner.Text(), "\r \t")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue // blank line or comment
		}

		key, value, hasEquals := strings.Cut(trimmed, "=")
		if !hasEquals {
			return "", fmt.Errorf("setup.txt line %d: expected setup_password=..., got %q", lineNumber, trimmed)
		}
		key = strings.TrimSpace(key)
		if key != "setup_password" {
			return "", fmt.Errorf("setup.txt line %d: unknown setting %q (only setup_password is supported)", lineNumber, key)
		}
		if found {
			return "", fmt.Errorf("setup.txt line %d: setup_password appears more than once", lineNumber)
		}
		// Keep the value exactly as typed after "=", except the line-ending
		// whitespace removed above. Spaces inside a password are allowed.
		password = value
		found = true
	}

	if !found {
		return "", errors.New("setup.txt has no setup_password=... line")
	}
	if err := config.ValidateWiFiPassword(password); err != nil {
		return "", fmt.Errorf("setup.txt: setup_password: %w", err)
	}
	return password, nil
}

// Remove deletes setup.txt after the device has been claimed, so the setup
// password does not stay readable on the SD card. It overwrites the contents
// first; on a flash card this is best effort (the card may keep old copies
// internally), but it stops the file being read casually.
func Remove(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	zeros := make([]byte, info.Size())
	if err := os.WriteFile(path, zeros, 0o600); err != nil {
		return fmt.Errorf("overwriting setup.txt: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting setup.txt: %w", err)
	}
	return nil
}
