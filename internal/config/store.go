package config

// This file reads and writes the settings files safely.
//
// The device can lose power at any moment (someone unplugs the power bank).
// If we overwrote config.json in place and power died halfway, the file would
// be half old, half new, and the device might not boot into a working state.
//
// So every write follows the "write to a temporary file, then rename" pattern:
//
//  1. Write the new contents to config.json.tmp.
//  2. Flush it to the SD card (fsync), so the bytes are really stored.
//  3. Rename config.json.tmp to config.json. On Linux a rename within one
//     filesystem is atomic: after a crash you see either the old file or the
//     new one, never a mix.
//  4. Flush the directory too, so the rename itself is stored.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotFound is returned when a settings file does not exist yet (for
// example before first boot has finished).
var ErrNotFound = errors.New("settings file not found")

// ReadJSON loads a JSON file into target. It returns ErrNotFound if the file
// does not exist.
func ReadJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}

// WriteJSON saves value as indented JSON using the crash-safe steps described
// at the top of this file. mode sets the file permissions, for example 0600
// for secrets (owner read/write only) or 0644 for normal settings.
func WriteJSON(path string, value any, mode fs.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	data = append(data, '\n')
	return WriteFileAtomic(path, data, mode)
}

// WriteFileAtomic replaces the file at path with data, crash-safely.
func WriteFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	// Step 1: write the temporary file.
	tmpPath := path + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmpPath, err)
	}
	// If anything below fails, remove the temporary file so it doesn't
	// linger. (After a successful rename this does nothing.)
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	// OpenFile only applies mode when creating the file, and the process
	// umask can remove bits, so set the permissions explicitly.
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("setting permissions on %s: %w", tmpPath, err)
	}

	// Step 2: make sure the data is physically written.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("flushing %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpPath, err)
	}

	// Step 3: atomically swap the new file into place.
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}

	// Step 4: flush the directory so the rename survives a power cut.
	return syncDir(dir)
}

// syncDir flushes a directory's entries to disk.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening directory %s: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("flushing directory %s: %w", dir, err)
	}
	return nil
}
