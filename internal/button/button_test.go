package button

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHasKey(t *testing.T) {
	// Only key 408 (word 6 from the end, bit 24).
	if !HasKey("1000000 0 0 0 0 0 0", KeyRestart) {
		t.Error("key 408 should be found")
	}
	if HasKey("0 0 0 0 0 0 0", KeyRestart) || HasKey("1000000", KeyRestart) || HasKey("", KeyRestart) {
		t.Error("key 408 should not be found")
	}
}

func TestFindDevices(t *testing.T) {
	root := t.TempDir()
	write := func(event, caps string) {
		dir := filepath.Join(root, "sys", event, "device", "capabilities")
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "key"), []byte(caps+"\n"), 0o644)
	}
	write("event0", "0 0 0 0 0 0 0") // a keyboard without the key
	write("event3", "1000000 0 0 0 0 0 0")
	got := FindDevices(filepath.Join(root, "sys"), "/dev/input", KeyRestart)
	if len(got) != 1 || got[0] != "/dev/input/event3" {
		t.Errorf("got %v", got)
	}
}

// keyEvent encodes one input event.
func keyEvent(code uint16, value int32) []byte {
	b := make([]byte, eventSize)
	binary.LittleEndian.PutUint16(b[16:], evKey)
	binary.LittleEndian.PutUint16(b[18:], code)
	binary.LittleEndian.PutUint32(b[20:], uint32(value))
	return b
}

func TestWatch_OnlyALongPressCounts(t *testing.T) {
	reader, writer := io.Pipe()
	fired := make(chan struct{}, 1)
	go Watch(reader, KeyRestart, 100*time.Millisecond, func() { fired <- struct{}{} })

	// A short press: nothing happens.
	writer.Write(keyEvent(KeyRestart, 1))
	time.Sleep(20 * time.Millisecond)
	writer.Write(keyEvent(KeyRestart, 0))
	select {
	case <-fired:
		t.Fatal("a short press must not trigger")
	case <-time.After(200 * time.Millisecond):
	}

	// Another key held: nothing happens.
	writer.Write(keyEvent(30, 1))
	select {
	case <-fired:
		t.Fatal("other keys must not trigger")
	case <-time.After(200 * time.Millisecond):
	}

	// A long press triggers.
	writer.Write(keyEvent(KeyRestart, 1))
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("a long press should trigger")
	}
	writer.Close()
}
