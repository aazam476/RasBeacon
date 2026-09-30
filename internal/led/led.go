// Package led blinks the board's green activity LED to report problems that
// happen before you can reach the web UI.
//
// Error pattern used by RasBeacon: 3 short blinks, 1 long blink, a pause,
// repeated. It means "setup.txt exists but can't be used". The README's
// troubleshooting section explains how to fix it.
package led

import (
	"os"
	"path/filepath"
	"time"
)

// ErrorPattern is the on/off sequence for the setup.txt error: true means
// the LED is on for that duration.
var ErrorPattern = []Step{
	{On: true, Duration: 200 * time.Millisecond}, {On: false, Duration: 200 * time.Millisecond},
	{On: true, Duration: 200 * time.Millisecond}, {On: false, Duration: 200 * time.Millisecond},
	{On: true, Duration: 200 * time.Millisecond}, {On: false, Duration: 200 * time.Millisecond},
	{On: true, Duration: 900 * time.Millisecond}, {On: false, Duration: 1500 * time.Millisecond},
}

// Step is one part of a blink pattern.
type Step struct {
	On       bool
	Duration time.Duration
}

// FindActivityLED returns the directory of the green activity LED under
// sysClassLeds (normally /sys/class/leds). Newer kernels call it "ACT",
// older ones "led0". Returns "" if neither exists.
func FindActivityLED(sysClassLeds string) string {
	for _, name := range []string{"ACT", "led0"} {
		dir := filepath.Join(sysClassLeds, name)
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	return ""
}

// Blink repeats pattern on the LED in ledDir until stop is closed. It first
// switches off the LED's normal behaviour (flashing on SD card activity) by
// setting its trigger to "none".
func Blink(ledDir string, pattern []Step, stop <-chan struct{}) {
	if ledDir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(ledDir, "trigger"), []byte("none"), 0o644)
	brightness := filepath.Join(ledDir, "brightness")

	for {
		for _, step := range pattern {
			value := "0"
			if step.On {
				value = "1"
			}
			_ = os.WriteFile(brightness, []byte(value), 0o644)
			select {
			case <-stop:
				// Give the LED back its normal SD-card activity behaviour.
				_ = os.WriteFile(filepath.Join(ledDir, "trigger"), []byte("mmc0"), 0o644)
				return
			case <-time.After(step.Duration):
			}
		}
	}
}
