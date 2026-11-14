package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

// newEncryptionEnv installs the drive test plugin on the USB stick, with
// some files on it. Mounting and gocryptfs are faked: the drive and the
// opened view are two folders, so files really move between them.
func newEncryptionEnv(t *testing.T) (*testEnv, string, string) {
	t.Helper()
	e := newPluginEnv(t)
	e.runner.outputs["lsblk "+strings.Join(plugin.LsblkArgs, " ")] = usbLsblk
	if _, err := installOnDrive(e, drivePlugin(t, true), "1234-ABCD"); err != nil {
		t.Fatal(err)
	}
	root, plain := e.paths.DriveMountDir("1234-ABCD"), e.paths.DrivePlainDir("1234-ABCD")
	mustWrite(t, filepath.Join(root, "photos", "beach.jpg"), "sand and sea")
	mustWrite(t, filepath.Join(root, "notes.txt"), "pack a charger")
	return e, root, plain
}

// encryption is the plugin's encryption as its page shows it.
func encryption(e *testEnv) api.PluginEncryption {
	return *e.plugin("adguard-home").Encryption
}

func TestDriveEncryption_OnMovesTheFilesInAndOffMovesThemBack(t *testing.T) {
	e, root, plain := newEncryptionEnv(t)
	if got := encryption(e); got.State != "off" {
		t.Fatalf("encryption = %+v, want off", got)
	}

	p, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", On: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.State != api.PluginConverting || p.Encryption.State != "encrypting" {
		t.Errorf("while moving: state %s, encryption %+v", p.State, p.Encryption)
	}
	runs := e.runner.count("podman run")
	e.runBackground()

	// The files are in the opened view, and only the folder and its README
	// are left on the drive itself.
	if got := e.readFile(filepath.Join(plain, "photos", "beach.jpg")); got != "sand and sea" {
		t.Errorf("beach.jpg = %q", got)
	}
	entries, _ := os.ReadDir(root)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, "|") != encryptedFolder+"|"+encryptedReadme {
		t.Errorf("on the drive: %v", names)
	}
	if !e.runner.ran("gocryptfs -init -xchacha") {
		t.Error("the folder should be made with XChaCha20-Poly1305")
	}
	got := encryption(e)
	if got.State != "on" || got.RecoveryKeySaved {
		t.Errorf("encryption = %+v, want on, key not saved yet", got)
	}
	// It starts again, with the opened view as its drive.
	if e.runner.count("podman run") == runs {
		t.Error("it should start again")
	}
	if !strings.Contains(strings.Join(e.runner.commands, "\n"), plain+":/media") {
		t.Error("the plugin should get the opened view")
	}

	// The recovery key, once downloaded, isn't asked for any more.
	file, err := e.agent.PluginRecoveryKey("adguard-home")
	if err != nil {
		t.Fatal(err)
	}
	key := e.agent.secrets.DriveKeys["1234-ABCD"]
	if !strings.Contains(file.Text, key) || file.FileName != "rasbeacon-recovery-key-1234-ABCD.txt" || len(key) != 47 {
		t.Errorf("recovery key file = %+v (key %q)", file, key)
	}
	if !encryption(e).RecoveryKeySaved {
		t.Error("the key should count as saved")
	}

	// Off needs the key; typed in lower case without dashes is fine.
	var invalid *config.ValidationError
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", RecoveryKey: "AAAAA-BBBBB"}); !errors.As(err, &invalid) || invalid.Field != "recoveryKey" {
		t.Errorf("a wrong key: %v", err)
	}
	typed := strings.ToLower(strings.ReplaceAll(key, "-", ""))
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", RecoveryKey: typed}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := e.readFile(filepath.Join(root, "photos", "beach.jpg")); got != "sand and sea" {
		t.Errorf("after decrypting, beach.jpg = %q", got)
	}
	for _, gone := range []string{encryptedFolder, encryptedReadme} {
		if _, err := os.Stat(filepath.Join(root, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s should be gone", gone)
		}
	}
	if got := encryption(e); got.State != "off" || got.RecoveryKeySaved || e.agent.secrets.DriveKeys["1234-ABCD"] != "" {
		t.Errorf("after decrypting: %+v, key %q", got, e.agent.secrets.DriveKeys["1234-ABCD"])
	}
}

func TestDriveEncryption_AFullDriveStopsAndGoesOnLater(t *testing.T) {
	e, root, plain := newEncryptionEnv(t)
	e.agent.diskFree = func(string) (int64, error) { return 1024, nil }
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", On: true}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	p := e.plugin("adguard-home")
	if p.State != api.PluginFailed || !strings.Contains(p.Message, "too full") || p.Encryption.State != "encrypting" {
		t.Errorf("state %s %q, encryption %+v", p.State, p.Message, p.Encryption)
	}
	// Half done, it doesn't start: it would see only some of its files.
	if err := e.agent.StartPlugin("adguard-home"); err == nil {
		e.runBackground()
	}
	if p := e.plugin("adguard-home"); !strings.Contains(p.Message, "partly encrypted") {
		t.Errorf("starting half encrypted: %q", p.Message)
	}

	// With room again, RasBeacon goes on by itself when it starts.
	e.agent.diskFree = func(string) (int64, error) { return 10 << 30, nil }
	e.agent.startPlugins()
	e.runBackground()
	if got := encryption(e); got.State != "on" {
		t.Errorf("encryption = %+v, want on", got)
	}
	if got := e.readFile(filepath.Join(plain, "notes.txt")); got != "pack a charger" {
		t.Errorf("notes.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old copy should be gone")
	}
}

func TestDriveEncryption_TurnedOffBeforeTheMoveStarts(t *testing.T) {
	e, root, _ := newEncryptionEnv(t)
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", On: true}); err != nil {
		t.Fatal(err)
	}
	key := e.agent.secrets.DriveKeys["1234-ABCD"]
	// While it waits to move, it can be turned the other way.
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", RecoveryKey: key}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := encryption(e); got.State != "off" {
		t.Errorf("encryption = %+v, want off", got)
	}
	if got := e.readFile(filepath.Join(root, "notes.txt")); got != "pack a charger" {
		t.Errorf("notes.txt = %q", got)
	}
}

func TestDriveEncryption_Rules(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	var invalid *config.ValidationError
	// No recovery key before the files are encrypted.
	if _, err := e.agent.PluginRecoveryKey("adguard-home"); !errors.As(err, &invalid) {
		t.Errorf("recovery key before encrypting: %v", err)
	}
	if _, err := e.agent.PluginRecoveryKey("nope"); err == nil {
		t.Error("an unknown plugin has no key")
	}
	// Off when it is off, and on twice, change nothing.
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home"}); err != nil {
		t.Errorf("off when off: %v", err)
	}
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", On: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", On: true}); err != nil {
		t.Errorf("on while encrypting: %v", err)
	}
	e.runBackground()
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", On: true}); err != nil {
		t.Errorf("on when on: %v", err)
	}
	// Another drive only after turning it off.
	_, err := e.agent.UpdatePluginSettings(api.PluginSettingsRequest{ID: "adguard-home", Drive: ""})
	if !errors.As(err, &invalid) || invalid.Field != "drive" {
		t.Errorf("changing the drive while encrypted: %v", err)
	}

	// A plugin without a drive has nothing to encrypt.
	other := newPluginEnv(t)
	if _, err := other.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	if other.plugin("adguard-home").Encryption != nil {
		t.Error("no drive, no encryption")
	}
	if _, err := other.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", On: true}); !errors.As(err, &invalid) || invalid.Field != "drive" {
		t.Errorf("without a drive: %v", err)
	}
}

func TestDriveEncryption_AnEncryptedDriveChosenAgainOpens(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	if _, err := e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", On: true}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	// Removed (the key stays), then installed again on the same drive.
	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home"}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if _, err := installOnDrive(e, drivePlugin(t, true), "1234-ABCD"); err != nil {
		t.Fatal(err)
	}
	if got := encryption(e); got.State != "on" || !got.RecoveryKeySaved {
		t.Errorf("encryption = %+v, want on (the key was given before)", got)
	}
}

func TestDriveEncryption_Helpers(t *testing.T) {
	key := newRecoveryKey()
	if len(key) != 47 || strings.Count(key, "-") != 7 || formatRecoveryKey(strings.ToLower(key)) != key {
		t.Errorf("key %q", key)
	}
	if newRecoveryKey() == key {
		t.Error("each key is new")
	}
	if got := formatRecoveryKey(" abcde fghij\n"); got != "ABCDE-FGHIJ" {
		t.Errorf("formatted = %q", got)
	}
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 3010: "3,010", 1234567: "1,234,567"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q", n, got)
		}
	}
	if got := conversionMessage(encryptionDecrypting, 0, 0); got != "Decrypting the drive's files…" {
		t.Errorf("message = %q", got)
	}
}

// After a restart in the middle of a move: a file already copied only has
// its old copy removed, and a copy that wasn't finished is made again.
func TestDriveEncryption_AMoveGoesOnWhereItStopped(t *testing.T) {
	e := newTestEnv(t)
	from, to := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(from, "done.txt"), "copied before")
	mustWrite(t, filepath.Join(to, "done.txt"), "copied before")
	mustWrite(t, filepath.Join(from, "half.txt"), "the whole file")
	mustWrite(t, filepath.Join(to, "half.txt"+partSuffix), "the wh")
	mustWrite(t, filepath.Join(from, "old.txt"+partSuffix), "left from going the other way")
	var steps []int
	err := e.agent.moveFiles(from, to, func(string) bool { return false }, func(done, total int) error {
		steps = append(steps, done*10+total)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(filepath.Join(to, "half.txt")); got != "the whole file" {
		t.Errorf("half.txt = %q", got)
	}
	for _, gone := range []string{filepath.Join(from, "done.txt"), filepath.Join(from, "half.txt"),
		filepath.Join(to, "half.txt"+partSuffix), filepath.Join(from, "old.txt"+partSuffix)} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s should be gone", gone)
		}
	}
	if len(steps) != 2 || steps[1] != 22 {
		t.Errorf("progress = %v, want 2 files", steps)
	}

	// The progress callback can stop it (the owner turned it around).
	mustWrite(t, filepath.Join(from, "a.txt"), "a")
	mustWrite(t, filepath.Join(from, "b.txt"), "b")
	err = e.agent.moveFiles(from, to, func(string) bool { return false }, func(int, int) error { return errTurnedAround })
	if !errors.Is(err, errTurnedAround) {
		t.Errorf("err = %v, want turned around", err)
	}
	// A file that can't be read stops it, naming the file.
	if err := e.agent.moveFile(filepath.Join(from, "missing"), filepath.Join(to, "missing")); err == nil {
		t.Error("a missing file can't be moved")
	}
}

func TestDriveEncryption_Routes(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	if status, body := e.call("POST", "/plugins/encryption", api.PluginEncryptionRequest{ID: "adguard-home", On: true}); status != 200 {
		t.Errorf("encryption = %d %s", status, body)
	}
	e.runBackground()
	if status, body := e.call("POST", "/plugins/recovery-key", api.PluginIDRequest{ID: "adguard-home"}); status != 200 || !strings.Contains(string(body), "fileName") {
		t.Errorf("recovery key = %d %s", status, body)
	}
}
