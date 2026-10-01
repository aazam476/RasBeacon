// Package button watches the optional factory-reset button: a push button
// between GPIO 17 (pin 11) and ground (pin 9) on the Raspberry Pi header.
//
// The image's config.txt loads the standard "gpio-key" overlay, which makes
// the Linux kernel report the button as a key ("KEY_RESTART") on an input
// device, like a keyboard. This package reads those key events and calls a
// function when the button has been held down long enough.
package button

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// KeyRestart is the key code the overlay reports (Linux KEY_RESTART).
const KeyRestart = 408

// Linux input event: a timestamp (16 bytes on 64-bit systems), then type,
// code and value. Type 1 is a key; value 1 is "pressed", 0 "released", 2
// "still held" (auto-repeat).
const (
	eventSize = 24
	evKey     = 1
)

// FindDevices returns the input devices (/dev/input/eventN) that can send
// key code. sysClassInput is /sys/class/input; devInput is /dev/input.
func FindDevices(sysClassInput, devInput string, code int) []string {
	var devices []string
	entries, _ := filepath.Glob(filepath.Join(sysClassInput, "event*"))
	for _, entry := range entries {
		capabilities, err := os.ReadFile(filepath.Join(entry, "device", "capabilities", "key"))
		if err == nil && HasKey(string(capabilities), code) {
			devices = append(devices, filepath.Join(devInput, filepath.Base(entry)))
		}
	}
	return devices
}

// HasKey reads a capabilities bitmap from sysfs: hexadecimal 64-bit words,
// most significant first, e.g. "1000000 0 0 0 0 0 0" means only key 408.
func HasKey(bitmap string, code int) bool {
	words := strings.Fields(bitmap)
	index := len(words) - 1 - code/64 // the word that holds this key's bit
	if index < 0 {
		return false
	}
	word, err := strconv.ParseUint(words[index], 16, 64)
	return err == nil && word&(1<<(code%64)) != 0
}

// Watch reads key events from events and calls onHold if key code stays
// pressed for hold. It returns when events ends (or fails).
func Watch(events io.Reader, code int, hold time.Duration, onHold func()) error {
	var timer *time.Timer
	buffer := make([]byte, eventSize)
	for {
		if _, err := io.ReadFull(events, buffer); err != nil {
			if timer != nil {
				timer.Stop()
			}
			return err
		}
		eventType := binary.LittleEndian.Uint16(buffer[16:18])
		eventCode := binary.LittleEndian.Uint16(buffer[18:20])
		value := int32(binary.LittleEndian.Uint32(buffer[20:24]))
		if eventType != evKey || int(eventCode) != code {
			continue
		}
		switch value {
		case 1: // pressed: start counting
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(hold, onHold)
		case 0: // released too early: forget it
			if timer != nil {
				timer.Stop()
				timer = nil
			}
		}
	}
}
