package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// newEncryptionEnv is the shares test plugin with a share on the USB stick
// (Movies) and one on internal storage (Files), each with some files.
// Mounting and gocryptfs are faked: a share's folder and its opened view
// are two folders, so files really move between them.
func newEncryptionEnv(t *testing.T) (e *testEnv, movies, moviesOpen string) {
	t.Helper()
	e = newSharesEnv(t)
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 1024})
	addShare(e, api.PluginShareAddRequest{Name: "Files", QuotaMB: 512})
	movies = filepath.Join(e.paths.DriveMountDir("1234-ABCD"), "Movies")
	mustWrite(t, filepath.Join(movies, "trips", "beach.mkv"), "sand and sea")
	mustWrite(t, filepath.Join(movies, "notes.txt"), "pack a charger")
	return e, movies, e.paths.SharePlainDir("adguard-home", "s1")
}

// encryption is a share's encryption as the plugin's page shows it.
func encryption(e *testEnv, shareID string) api.PluginEncryption {
	e.t.Helper()
	for _, share := range shares(e) {
		if share.ID == shareID {
			return share.Encryption
		}
	}
	e.t.Fatalf("no share %s", shareID)
	return api.PluginEncryption{}
}

// setEncryption turns a share's encryption on or off.
func setEncryption(e *testEnv, shareID string, on bool, key string) (api.Plugin, error) {
	return e.agent.SetPluginEncryption(api.PluginEncryptionRequest{ID: "adguard-home", Share: shareID, On: on, RecoveryKey: key})
}

func TestShareEncryption_OnMovesTheFilesInAndOffMovesThemBack(t *testing.T) {
	e, movies, open := newEncryptionEnv(t)
	if got := encryption(e, "s1"); got.State != "off" {
		t.Fatalf("encryption = %+v, want off", got)
	}
	p, err := setEncryption(e, "s1", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.State != api.PluginConverting || !strings.Contains(p.Message, "Encrypting the share Movies") {
		t.Errorf("while moving: %s %q", p.State, p.Message)
	}
	runs := e.runner.count("podman run")
	e.runBackground()

	// The files are in the opened view; only the encrypted folder and its
	// README are left in the share's folder. The other share isn't touched.
	if got := e.readFile(filepath.Join(open, "trips", "beach.mkv")); got != "sand and sea" {
		t.Errorf("beach.mkv = %q", got)
	}
	entries, _ := os.ReadDir(movies)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, "|") != encryptedFolder+"|"+encryptedReadme {
		t.Errorf("in the share's folder: %v", names)
	}
	if !e.runner.ran("gocryptfs -init -xchacha") {
		t.Error("the folder should be made with XChaCha20-Poly1305")
	}
	if got := encryption(e, "s1"); got.State != "on" || got.RecoveryKeySaved {
		t.Errorf("Movies = %+v, want on, key not saved yet", got)
	}
	if got := encryption(e, "s2"); got.State != "off" {
		t.Errorf("Files = %+v, want off", got)
	}
	if e.runner.count("podman run") == runs {
		t.Error("the plugin should start again")
	}
	// The plugin gets the opened view, held to the share's quota.
	if !e.runner.ran("rbquotafs -limit 1073741824 " + open + " " + e.paths.ShareQuotaDir("adguard-home", "s1")) {
		t.Error("the plugin should get the opened view, held to its quota")
	}

	file, err := e.agent.PluginRecoveryKey(api.PluginShareRequest{ID: "adguard-home", Share: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	key := e.agent.secrets.ShareKeys["1234-ABCD/Movies"]
	if !strings.Contains(file.Text, key) || file.FileName != "rasbeacon-recovery-key-Movies.txt" || len(key) != 47 {
		t.Errorf("recovery key file = %+v (key %q)", file, key)
	}
	if !encryption(e, "s1").RecoveryKeySaved {
		t.Error("the key should count as saved")
	}

	// Off needs the key; typed in lower case without dashes is fine.
	var invalid *config.ValidationError
	if _, err := setEncryption(e, "s1", false, "AAAAA-BBBBB"); !errors.As(err, &invalid) || invalid.Field != "recoveryKey" {
		t.Errorf("a wrong key: %v", err)
	}
	if _, err := setEncryption(e, "s1", false, strings.ToLower(strings.ReplaceAll(key, "-", ""))); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := e.readFile(filepath.Join(movies, "trips", "beach.mkv")); got != "sand and sea" {
		t.Errorf("after decrypting, beach.mkv = %q", got)
	}
	for _, gone := range []string{encryptedFolder, encryptedReadme} {
		if _, err := os.Stat(filepath.Join(movies, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s should be gone", gone)
		}
	}
	if got := encryption(e, "s1"); got.State != "off" || e.agent.secrets.ShareKeys["1234-ABCD/Movies"] != "" {
		t.Errorf("after decrypting: %+v", got)
	}
}

func TestShareEncryption_OnInternalStorage(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	files := e.paths.PluginShareMount("adguard-home", "s2")
	mustWrite(t, filepath.Join(files, "passport.pdf"), "scan")
	mustWrite(t, filepath.Join(files, "lost+found", "x"), "ext4's own")
	if _, err := setEncryption(e, "s2", true, ""); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := e.readFile(filepath.Join(e.paths.SharePlainDir("adguard-home", "s2"), "passport.pdf")); got != "scan" {
		t.Errorf("passport.pdf = %q", got)
	}
	if _, err := os.Stat(filepath.Join(files, "lost+found", "x")); err != nil {
		t.Error("ext4's lost+found stays where it is")
	}
	if e.agent.secrets.ShareKeys["adguard-home/s2"] == "" {
		t.Error("the key is kept by plugin and share")
	}
}

func TestShareEncryption_AFullDriveStopsAndGoesOnLater(t *testing.T) {
	e, movies, open := newEncryptionEnv(t)
	e.agent.diskFree = func(string) (int64, error) { return 1024, nil }
	if _, err := setEncryption(e, "s1", true, ""); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	p := e.plugin("adguard-home")
	if p.State != api.PluginFailed || !strings.Contains(p.Message, "too full") || encryption(e, "s1").State != "encrypting" {
		t.Errorf("state %s %q", p.State, p.Message)
	}
	// Half done, it doesn't start: it would see only some of the files.
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
	if got := encryption(e, "s1"); got.State != "on" {
		t.Errorf("encryption = %+v, want on", got)
	}
	if got := e.readFile(filepath.Join(open, "notes.txt")); got != "pack a charger" {
		t.Errorf("notes.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(movies, "notes.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old copy should be gone")
	}
}

func TestShareEncryption_TurnedOffBeforeTheMoveStarts(t *testing.T) {
	e, movies, _ := newEncryptionEnv(t)
	if _, err := setEncryption(e, "s1", true, ""); err != nil {
		t.Fatal(err)
	}
	key := e.agent.secrets.ShareKeys["1234-ABCD/Movies"]
	// While it waits to move, it can be turned the other way.
	if _, err := setEncryption(e, "s1", false, key); err != nil {
		t.Fatal(err)
	}
	// Another share can't change meanwhile: one move at a time.
	if _, err := setEncryption(e, "s2", true, ""); err == nil {
		t.Error("another share's encryption must wait for the move")
	}
	e.runBackground()
	if got := encryption(e, "s1"); got.State != "off" {
		t.Errorf("encryption = %+v, want off", got)
	}
	if got := e.readFile(filepath.Join(movies, "notes.txt")); got != "pack a charger" {
		t.Errorf("notes.txt = %q", got)
	}
}

func TestShareEncryption_Rules(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	var invalid *config.ValidationError
	if _, err := e.agent.PluginRecoveryKey(api.PluginShareRequest{ID: "adguard-home", Share: "s1"}); !errors.As(err, &invalid) {
		t.Errorf("recovery key before encrypting: %v", err)
	}
	if _, err := e.agent.PluginRecoveryKey(api.PluginShareRequest{ID: "nope", Share: "s1"}); err == nil {
		t.Error("an unknown plugin has no key")
	}
	if _, err := e.agent.PluginRecoveryKey(api.PluginShareRequest{ID: "adguard-home", Share: "s9"}); err == nil {
		t.Error("an unknown share has no key")
	}
	if _, err := setEncryption(e, "s9", true, ""); !errors.As(err, &invalid) || invalid.Field != "share" {
		t.Errorf("an unknown share: %v", err)
	}
	if _, err := setEncryption(e, "s1", false, ""); err != nil {
		t.Errorf("off when off: %v", err)
	}
	if _, err := setEncryption(e, "s1", true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := setEncryption(e, "s1", true, ""); err != nil {
		t.Errorf("on while encrypting: %v", err)
	}
	e.runBackground()
	if _, err := setEncryption(e, "s1", true, ""); err != nil {
		t.Errorf("on when on: %v", err)
	}
}

// Removing an encrypted share always deletes its files; a share made in
// the same folder later finds the key it was encrypted with.
func TestShareEncryption_RemovingAnEncryptedShareDeletesItsFiles(t *testing.T) {
	e, movies, _ := newEncryptionEnv(t)
	if _, err := setEncryption(e, "s1", true, ""); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(movies); !errors.Is(err, os.ErrNotExist) {
		t.Error("an encrypted share's folder goes with it")
	}
	if e.agent.secrets.ShareKeys["1234-ABCD/Movies"] != "" {
		t.Error("its key goes too")
	}
}

func TestShareEncryption_AFolderEncryptedBeforeOpensAgain(t *testing.T) {
	e, movies, _ := newEncryptionEnv(t)
	if _, err := setEncryption(e, "s1", true, ""); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	// The plugin is removed (the drive's keys stay), the folder is still
	// encrypted, and a share is made there again.
	mustWrite(t, filepath.Join(movies, encryptedFolder, "gocryptfs.conf"), "{}")
	if err := e.agent.RemovePlugin(api.PluginRemoveRequest{ID: "adguard-home"}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if _, err := e.installTestPlugin(sharesPlugin(t), nil); err != nil {
		t.Fatal(err)
	}
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 1024})
	if got := encryption(e, "s1"); got.State != "on" || !got.RecoveryKeySaved {
		t.Errorf("encryption = %+v, want on (its key was given before)", got)
	}
}

func TestShareEncryption_Helpers(t *testing.T) {
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
	if got := conversionMessage("Files", encryptionDecrypting, 0, 0); got != "Decrypting the share Files…" {
		t.Errorf("message = %q", got)
	}
	if got := conversionMessage("Files", encryptionEncrypting, 3, 3010); got != "Encrypting the share Files: 3 of 3,010 files" {
		t.Errorf("message = %q", got)
	}
}

// After a restart in the middle of a move: a file already copied only has
// its old copy removed, and a copy that wasn't finished is made again.
func TestShareEncryption_AMoveGoesOnWhereItStopped(t *testing.T) {
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

func TestShareEncryption_Routes(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	if status, body := e.call("POST", "/plugins/encryption", api.PluginEncryptionRequest{ID: "adguard-home", Share: "s1", On: true}); status != 200 {
		t.Errorf("encryption = %d %s", status, body)
	}
	e.runBackground()
	if status, body := e.call("POST", "/plugins/recovery-key", api.PluginShareRequest{ID: "adguard-home", Share: "s1"}); status != 200 || !strings.Contains(string(body), "fileName") {
		t.Errorf("recovery key = %d %s", status, body)
	}
}

func TestShareEncryption_FromTheStart(t *testing.T) {
	e := newSharesEnv(t)
	// Files already in the drive's folder are moved into the encrypted folder.
	movies := filepath.Join(e.paths.DriveMountDir("1234-ABCD"), "Movies")
	mustWrite(t, filepath.Join(movies, "beach.mkv"), "sand and sea")
	p, err := e.agent.AddPluginShare(api.PluginShareAddRequest{
		ID: "adguard-home", Name: "Movies", Drive: "1234-ABCD", Folder: "Movies", QuotaMB: 1024, Encrypt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.State != api.PluginConverting || !strings.Contains(p.Message, "Encrypting the share Movies") {
		t.Errorf("after adding: %s %q", p.State, p.Message)
	}
	e.runBackground()
	if got := e.readFile(filepath.Join(e.paths.SharePlainDir("adguard-home", "s1"), "beach.mkv")); got != "sand and sea" {
		t.Errorf("beach.mkv = %q", got)
	}
	if got := encryption(e, "s1"); got.State != "on" || got.RecoveryKeySaved {
		t.Errorf("Movies = %+v, want on, key not saved yet", got)
	}

	// On internal storage, an empty share; the plugin is free again after.
	if _, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Files", QuotaMB: 512, Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := encryption(e, "s2"); got.State != "on" || e.agent.secrets.ShareKeys["adguard-home/s2"] == "" {
		t.Errorf("Files = %+v, want on with a key", got)
	}
	addShare(e, api.PluginShareAddRequest{Name: "Plain", QuotaMB: 256})
	if got := encryption(e, "s3"); got.State != "off" {
		t.Errorf("Plain = %+v, want off", got)
	}
}
