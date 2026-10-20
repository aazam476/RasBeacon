package led

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindActivityLED(t *testing.T) {
	for _, name := range []string{"ACT", "led0"} {
		leds := t.TempDir()
		if err := os.Mkdir(filepath.Join(leds, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := FindActivityLED(leds); got != filepath.Join(leds, name) {
			t.Errorf("with %s: %q", name, got)
		}
	}
	if got := FindActivityLED(t.TempDir()); got != "" {
		t.Errorf("without an LED: %q, want none", got)
	}
}

func TestBlink_TakesTheLEDAndGivesItBack(t *testing.T) {
	dir := t.TempDir()
	read := func(name string) string {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		return string(data)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		Blink(dir, []Step{{On: true, Duration: time.Hour}}, stop)
		close(done)
	}()
	// While blinking: the SD card activity no longer drives it, and it is on.
	deadline := time.Now().Add(5 * time.Second)
	for read("brightness") != "1" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if read("trigger") != "none" || read("brightness") != "1" {
		t.Fatalf("trigger = %q, brightness = %q", read("trigger"), read("brightness"))
	}
	close(stop)
	<-done
	if read("trigger") != "mmc0" {
		t.Errorf("after stopping, trigger = %q, want the SD card's activity back", read("trigger"))
	}
}

func TestBlink_NoLEDDoesNothing(t *testing.T) {
	Blink("", ErrorPattern, nil) // returns at once
}
