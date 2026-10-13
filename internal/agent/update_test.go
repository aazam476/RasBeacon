package agent

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/config"
	"rasbeacon/internal/update"
)

// sparseImage makes a sparse image holding data (a multiple of 16 bytes)
// as one raw chunk with block size 16.
func sparseImage(data []byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	for _, v := range []any{uint32(0xED26FF3A), uint16(1), uint16(0), uint16(28), uint16(12),
		uint32(16), uint32(len(data) / 16), uint32(1), uint32(0)} {
		binary.Write(&b, le, v)
	}
	for _, v := range []any{uint16(0xCAC1), uint16(0), uint32(len(data) / 16), uint32(12 + len(data))} {
		binary.Write(&b, le, v)
	}
	b.Write(data)
	return b.Bytes()
}

// testBundle is an (uncompressed) bundle with boot and system images.
func testBundle(boot, system string) []byte {
	var b bytes.Buffer
	t := tar.NewWriter(&b)
	for name, content := range map[string]string{"boot": boot, "system": system} {
		image := sparseImage([]byte(content))
		t.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(image))})
		t.Write(image)
	}
	t.Close()
	return b.Bytes()
}

// updateEnv is a claimed device with an update key, a board file, and
// fake "other slot" partitions (64-byte files).
func newUpdateEnv(t *testing.T) (*testEnv, ed25519.PrivateKey) {
	t.Helper()
	e := newClaimedEnv(t, false)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	mustWrite(t, filepath.Join(e.paths.UpdateKeysDir(), "release.pub"), base64.StdEncoding.EncodeToString(public))
	mustWrite(t, e.paths.BoardFile(), "pi4\n")
	for _, part := range []string{"boot", "system"} {
		mustWrite(t, e.paths.SlotDevice("other", part), strings.Repeat("o", 64))
	}
	// The test bundles are not compressed.
	e.agent.decompress = func(r io.Reader) (io.Reader, func() error, error) {
		return r, func() error { return nil }, nil
	}
	return e, private
}

func updateFile(t *testing.T, key ed25519.PrivateKey, board string, bundle []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := update.Pack(&out, bytes.NewReader(bundle), board, "v9.9.9", key); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestUpdate_InstallWritesTheOtherSlotAndTriesIt(t *testing.T) {
	e, key := newUpdateEnv(t)
	boot, system := strings.Repeat("B", 32), strings.Repeat("S", 64)
	if err := e.agent.InstallUpdate(bytes.NewReader(updateFile(t, key, "pi4", testBundle(boot, system)))); err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(e.paths.SlotDevice("other", "system")); got != system {
		t.Errorf("system partition = %q", got)
	}
	if got := e.readFile(e.paths.SlotDevice("other", "boot")); !strings.HasPrefix(got, boot) {
		t.Errorf("boot partition = %q", got)
	}
	e.timers.runAll()
	if !e.runner.ran("systemctl reboot 0 tryboot") {
		t.Error("RasBeacon should restart into the new slot on trial")
	}
	if _, err := os.Stat(e.paths.UpdatePendingFile()); err != nil {
		t.Error("the pending update should be remembered")
	}
}

func TestUpdate_RefusesBadFilesWithoutTouchingTheSlot(t *testing.T) {
	e, key := newUpdateEnv(t)
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	bundle := testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64))

	tests := map[string][]byte{
		"foreign signature": updateFile(t, otherKey, "pi4", bundle),
		"other board":       updateFile(t, key, "pi5", bundle),
		"not an update":     []byte("hello"),
	}
	for name, file := range tests {
		t.Run(name, func(t *testing.T) {
			err := e.agent.InstallUpdate(bytes.NewReader(file))
			var verr *config.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("got %v, want a refusal", err)
			}
			if got := e.readFile(e.paths.SlotDevice("other", "system")); got != strings.Repeat("o", 64) {
				t.Error("nothing may be written for a refused update")
			}
		})
	}
	if e.runner.ran("tryboot") {
		t.Error("a refused update must never be started")
	}
}

func TestUpdate_TamperedBundleIsNotStarted(t *testing.T) {
	e, key := newUpdateEnv(t)
	file := updateFile(t, key, "pi4", testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64)))
	tampered := bytes.Replace(file, []byte(strings.Repeat("S", 64)), []byte(strings.Repeat("X", 64)), 1)
	if err := e.agent.InstallUpdate(bytes.NewReader(tampered)); err == nil {
		t.Fatal("a changed bundle must be refused")
	}
	e.timers.runAll()
	if e.runner.ran("tryboot") {
		t.Error("a changed bundle must never be started")
	}
	if !strings.Contains(e.agent.Status().Update.Message, "checksum") {
		t.Errorf("message = %q", e.agent.Status().Update.Message)
	}
}

func TestUpdate_TrialIsConfirmedAfterANormalStart(t *testing.T) {
	e := newTestEnv(t)
	mustWrite(t, e.paths.TrybootFlag(), "\x00\x00\x00\x01")
	e.runner.outputs["rpi-slot-tryboot"] = "[all]\ntryboot_a_b=1\nboot_partition=3\n[tryboot]\nboot_partition=2\n"
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	// Simulate the reboot into the claimed device on trial.
	restarted := New(e.paths, e.runner)
	timers := &fakeTimers{}
	restarted.afterFunc, restarted.usbRadioWait = timers.afterFunc, 0
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	timers.runAll() // confirmation (after a minute) and the fallback (5 minutes)
	if got := e.readFile(e.paths.AutobootFile()); !strings.Contains(got, "boot_partition=3") {
		t.Errorf("autoboot.txt = %q, want the new slot as default", got)
	}
	for _, c := range e.runner.commands {
		if strings.Contains(c, "reboot") {
			t.Errorf("a confirmed update must not be rolled back (ran %q)", c)
		}
	}
}

func TestUpdate_FailedTrialIsReported(t *testing.T) {
	e := newTestEnv(t)
	// An update was installed, but this start is NOT a trial: the new slot
	// didn't come up, and the bootloader went back to the old one.
	mustWrite(t, e.paths.UpdatePendingFile(), `{"version":"v9.9.9","fromVersion":"dev"}`)
	e.start()
	if msg := e.agent.Status().Update.Message; !strings.Contains(msg, "didn't start correctly") {
		t.Errorf("message = %q", msg)
	}
}

func TestUpdate_RealZstdDecompression(t *testing.T) {
	if _, err := exec.LookPath("zstd"); err != nil {
		t.Skip("zstd is not installed")
	}
	e, key := newUpdateEnv(t)
	e.agent.decompress = zstdDecompress
	cmd := exec.Command("zstd", "-q", "-c")
	cmd.Stdin = bytes.NewReader(testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64)))
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.agent.InstallUpdate(bytes.NewReader(updateFile(t, key, "pi4", compressed))); err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(e.paths.SlotDevice("other", "system")); got != strings.Repeat("S", 64) {
		t.Errorf("system partition = %q", got)
	}
}

func TestFetchLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/rasbeacon/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v1.2.0","html_url":"https://github.com/owner/rasbeacon/releases/tag/v1.2.0"}`))
	}))
	defer server.Close()
	got, err := fetchLatestRelease(server.Client(), server.URL, "owner/rasbeacon")
	if err != nil || got.TagName != "v1.2.0" || !strings.HasSuffix(got.HTMLURL, "v1.2.0") {
		t.Errorf("got %+v, %v", got, err)
	}
	if _, err := fetchLatestRelease(server.Client(), server.URL, "owner/missing"); err == nil {
		t.Error("a missing repository should be an error")
	}
}

func TestUpdate_TheZero2WImageIsFlashedInstead(t *testing.T) {
	e, key := newUpdateEnv(t)
	mustWrite(t, e.paths.BoardFile(), "zero2w\n")
	e.agent.mu.Lock()
	s := e.agent.updateStatus()
	e.agent.mu.Unlock()
	if s.CanInstall || !s.SingleSystem || s.Board != "zero2w" {
		t.Errorf("status = %+v: the Zero 2 W image has one system copy, so it can't install updates", s)
	}
	err := e.agent.InstallUpdate(bytes.NewReader(updateFile(t, key, "zero2w", testBundle("B", "S"))))
	var ve *config.ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Message, "flashed") {
		t.Errorf("got %v", err)
	}
	if e.readFile(e.paths.SlotDevice("other", "system")) != strings.Repeat("o", 64) || e.runner.ran("tryboot") {
		t.Error("nothing may be written or restarted")
	}
}

func TestStorage_TheZero2WImageHasOneSystemPartition(t *testing.T) {
	e, _ := newUpdateEnv(t)
	mustWrite(t, e.paths.BoardFile(), "zero2w\n")
	// /dev/disk/by-slot/{boot,system} point at the card's partitions; the
	// boot partition is 256 MB.
	for part, dev := range map[string]string{"boot": "mmcblk0p1", "system": "mmcblk0p2"} {
		target := filepath.Join(e.paths.Root, "dev", dev)
		mustWrite(t, target, "")
		if err := os.MkdirAll(filepath.Dir(e.paths.SingleSystemDevice(part)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, e.paths.SingleSystemDevice(part)); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(e.paths.SysClassBlock(), "mmcblk0p1", "size"), "524288\n")
	mustWrite(t, filepath.Join(e.paths.PersistentDir(), "rasbeacon", "settings"), strings.Repeat("x", 4096))

	e.agent.mu.Lock()
	s := e.agent.storageInfo()
	e.agent.mu.Unlock()
	if s.Medium != "microSD card" || !strings.HasPrefix(s.Filesystem, "Writable system") {
		t.Errorf("storage = %+v", s)
	}
	if s.FirmwareGB < 0.268 || s.UserGB <= 0 || s.TotalGB < s.FirmwareGB+s.UserGB {
		t.Errorf("storage = %+v: RasBeacon is the boot partition and the system files, settings what /persistent holds", s)
	}
}
