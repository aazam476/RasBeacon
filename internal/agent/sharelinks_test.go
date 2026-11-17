package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// openFolder opens a folder the way rbd opens a share's (see
// sharelinks.go), closed when the test ends.
func openFolder(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

// link makes a link called name that points to target.
func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

// names are the names of what is in dir.
func names(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var list []string
	for _, entry := range entries {
		list = append(list, entry.Name())
	}
	return strings.Join(list, "|")
}

// A share's folder on a drive is a real folder, and so are the folders it
// is in. A link in its place (a drive comes from anywhere) would make
// another place the share: any folder on the Pi.
func TestShareLinks_AShareFolderIsNeverALink(t *testing.T) {
	e := newSharesEnv(t)
	drive := e.paths.DriveMountDir("1234-ABCD")
	elsewhere := t.TempDir()
	mustWrite(t, filepath.Join(elsewhere, "Movies", "private.txt"), "not the share's")
	link(t, filepath.Join(elsewhere, "Movies"), filepath.Join(drive, "Films")) // the folder itself
	link(t, elsewhere, filepath.Join(drive, "Media"))                          // a folder it is in
	mustWrite(t, filepath.Join(drive, "notes.txt"), "a file")

	var invalid *config.ValidationError
	for _, folder := range []string{"Films", "Media/Movies", "Media/New", "notes.txt/Movies"} {
		_, err := e.agent.AddPluginShare(api.PluginShareAddRequest{ID: "adguard-home", Name: "Movies", Drive: "1234-ABCD", Folder: folder, QuotaMB: 256})
		if !errors.As(err, &invalid) || invalid.Field != "folder" || !strings.Contains(invalid.Message, "not a folder") {
			t.Errorf("folder %q: %v", folder, err)
		}
	}
	if len(shares(e)) != 0 {
		t.Errorf("shares = %+v, want none", shares(e))
	}

	// A share's folder that became a link later (the drive was in a
	// computer meanwhile): the plugin doesn't start with it, nothing is
	// made where it points, and no program is given its path.
	addShare(e, api.PluginShareAddRequest{Name: "Photos", Drive: "1234-ABCD", Folder: "Photos", QuotaMB: 256})
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(drive, "Photos")); err != nil {
		t.Fatal(err)
	}
	link(t, elsewhere, filepath.Join(drive, "Photos"))
	quotaLayers := e.runner.count("rbquotafs")
	if err := e.agent.StartPlugin("adguard-home"); err == nil {
		e.runBackground()
	}
	if p := e.plugin("adguard-home"); p.State != api.PluginFailed || !strings.Contains(p.Message, `"Photos" is a link, not a folder`) {
		t.Errorf("state %s %q, want the link refused", p.State, p.Message)
	}
	if e.runner.count("rbquotafs") != quotaLayers {
		t.Error("rbquotafs must not be given a folder that is a link")
	}
	if got := names(t, elsewhere); got != "Movies" {
		t.Errorf("where the link points: %s, want it untouched", got)
	}
}

// Deleting a share's files deletes its folder where it is on the drive,
// never another folder that a link stands for.
func TestShareLinks_DeletingAShareGoesThroughNoLink(t *testing.T) {
	e := newSharesEnv(t)
	drive := e.paths.DriveMountDir("1234-ABCD")
	addShare(e, api.PluginShareAddRequest{Name: "Movies", Drive: "1234-ABCD", Folder: "Media/Movies", QuotaMB: 256})
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	// While the drive was out, the folder Movies is in became a link to a
	// place that has a Movies too.
	elsewhere := t.TempDir()
	mustWrite(t, filepath.Join(elsewhere, "Movies", "keep.txt"), "not the share's")
	if err := os.RemoveAll(filepath.Join(drive, "Media")); err != nil {
		t.Fatal(err)
	}
	link(t, elsewhere, filepath.Join(drive, "Media"))

	var invalid *config.ValidationError
	_, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s1", DeleteFiles: true})
	if !errors.As(err, &invalid) || invalid.Field != "deleteFiles" || !strings.Contains(invalid.Message, `"Media" is a link`) {
		t.Errorf("deleting through a link: %v", err)
	}
	if got := e.readFile(filepath.Join(elsewhere, "Movies", "keep.txt")); got != "not the share's" {
		t.Errorf("keep.txt = %q", got)
	}
	// Its files kept, the share can still be removed.
	if _, err := e.agent.RemovePluginShare(api.PluginShareRemoveRequest{ID: "adguard-home", Share: "s1"}); err != nil {
		t.Fatal(err)
	}
	if len(shares(e)) != 0 {
		t.Errorf("shares = %+v, want none", shares(e))
	}
}

// What RasBeacon writes in a share to encrypt it never lands where a link
// in the share points. On internal storage the plugin itself can put
// links in its share.
func TestShareLinks_EncryptingWritesThroughNoLink(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	files := e.paths.PluginShareMount("adguard-home", "s2")
	elsewhere := t.TempDir()
	settings := filepath.Join(elsewhere, "settings.json")
	mustWrite(t, settings, "RasBeacon's own")
	mustWrite(t, filepath.Join(files, "passport.pdf"), "scan")
	link(t, settings, filepath.Join(files, encryptedReadme))

	if _, err := setEncryption(e, "s2", true, ""); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := encryption(e, "s2"); got.State != "on" {
		t.Fatalf("encryption = %+v, want on", got)
	}
	if got := e.readFile(settings); got != "RasBeacon's own" {
		t.Errorf("the file the link pointed to = %q", got)
	}
	if info, err := os.Lstat(filepath.Join(files, encryptedReadme)); err != nil || !info.Mode().IsRegular() {
		t.Errorf("the README should be a plain file in the share: %v, %v", info, err)
	}
	if got := e.readFile(filepath.Join(e.paths.SharePlainDir("adguard-home", "s2"), "passport.pdf")); got != "scan" {
		t.Errorf("passport.pdf = %q", got)
	}
}

// The folder of a share's encrypted files is a real folder in the share:
// through a link, gocryptfs would put the share's files somewhere else.
func TestShareLinks_TheEncryptedFolderIsNeverALink(t *testing.T) {
	e, _, _ := newEncryptionEnv(t)
	files := e.paths.PluginShareMount("adguard-home", "s2")
	elsewhere := t.TempDir()
	link(t, elsewhere, filepath.Join(files, encryptedFolder))

	if _, err := setEncryption(e, "s2", true, ""); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	p := e.plugin("adguard-home")
	if p.State != api.PluginFailed || !strings.Contains(p.Message, "is a link, not a folder") {
		t.Errorf("state %s %q, want the link refused", p.State, p.Message)
	}
	if e.runner.ran("gocryptfs") {
		t.Error("gocryptfs must not be given a folder that is a link")
	}
	if got := names(t, elsewhere); got != "" {
		t.Errorf("where the link points: %s, want nothing", got)
	}
}

// Decrypting puts a share's files back in its folder, and nowhere else: a
// link left in the folder from before it was encrypted (links stay where
// they are) gives way to a folder of its name made in the share since.
func TestShareLinks_DecryptingWritesThroughNoLink(t *testing.T) {
	e, movies, open := newEncryptionEnv(t)
	elsewhere := t.TempDir()
	link(t, elsewhere, filepath.Join(movies, "photos"))
	if _, err := setEncryption(e, "s1", true, ""); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if info, err := os.Lstat(filepath.Join(movies, "photos")); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("the link should stay where it is while encrypting: %v, %v", info, err)
	}
	mustWrite(t, filepath.Join(open, "photos", "pic.jpg"), "a picture")

	if _, err := setEncryption(e, "s1", false, e.agent.secrets.ShareKeys["1234-ABCD/Movies"]); err != nil {
		t.Fatal(err)
	}
	e.runBackground()
	if got := encryption(e, "s1"); got.State != "off" {
		t.Fatalf("encryption = %+v, want off", got)
	}
	if got := names(t, elsewhere); got != "" {
		t.Errorf("where the link pointed: %s, want nothing", got)
	}
	if info, err := os.Lstat(filepath.Join(movies, "photos")); err != nil || !info.IsDir() {
		t.Errorf("photos should be a real folder now: %v, %v", info, err)
	}
	if got := e.readFile(filepath.Join(movies, "photos", "pic.jpg")); got != "a picture" {
		t.Errorf("pic.jpg = %q", got)
	}
}

// On a drive the kernel follows no link for RasBeacon's programs: not on
// the drive itself, and not in an encrypted share's opened view, where
// rbquotafs works. A share on internal storage is the plugin's own view,
// where links work as before.
func TestShareLinks_DrivesAreMountedWithoutFollowingLinks(t *testing.T) {
	e, _, open := newEncryptionEnv(t)
	if !e.runner.ran("mount -t exfat -o nosuid,nodev,noexec,noatime,nosymfollow,") {
		t.Error("the drive should be mounted with nosymfollow")
	}
	for _, share := range []string{"s1", "s2"} {
		if _, err := setEncryption(e, share, true, ""); err != nil {
			t.Fatal(err)
		}
		e.runBackground()
	}
	const remount = "mount -o remount,bind,nosuid,nodev,nosymfollow "
	if !e.runner.ran(remount + open) {
		t.Error("the opened view of an encrypted share on a drive should follow no links")
	}
	if e.runner.ran(remount + e.paths.SharePlainDir("adguard-home", "s2")) {
		t.Error("the opened view of a share on internal storage is the plugin's own: left as it is")
	}
	// If that can't be done, the plugin doesn't start with the share.
	if err := e.agent.StopPlugin("adguard-home"); err != nil {
		t.Fatal(err)
	}
	e.runner.failing = append(e.runner.failing, remount)
	if err := e.agent.StartPlugin("adguard-home"); err == nil {
		e.runBackground()
	}
	if p := e.plugin("adguard-home"); p.State != api.PluginFailed {
		t.Errorf("state %s %q, want failed", p.State, p.Message)
	}
}

func TestShareLinks_Helpers(t *testing.T) {
	top, elsewhere := t.TempDir(), t.TempDir()
	base := openFolder(t, top)

	// realFolder makes the folders that are missing, and opens the last.
	folder, err := realFolder(base, "Media/Movies", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeNewFile(folder, "film.mkv", "a film"); err != nil {
		t.Fatal(err)
	}
	folder.Close()
	if got, _ := os.ReadFile(filepath.Join(top, "Media", "Movies", "film.mkv")); string(got) != "a film" {
		t.Errorf("film.mkv = %q", got)
	}
	if _, err := realFolder(base, "Media/Series", false); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing folder, not to be made: %v", err)
	}
	whole, err := realFolder(base, "", false)
	if err != nil {
		t.Fatalf("the whole drive: %v", err)
	}
	whole.Close()

	// Links and files are no folders, wherever they point; and nothing
	// but plain names makes a folder.
	link(t, elsewhere, filepath.Join(top, "out"))
	link(t, "Media", filepath.Join(top, "inside"))
	mustWrite(t, filepath.Join(top, "file"), "x")
	for _, bad := range []string{"out", "out/New", "inside", "inside/Movies", "file", "file/New", "..", "Media/..", "Media/../..", "./Media", "Media//Movies", "/Media"} {
		if folder, err := realFolder(base, bad, true); err == nil {
			folder.Close()
			t.Errorf("realFolder(%q) should be refused", bad)
		}
	}
	if got := names(t, elsewhere); got != "" {
		t.Errorf("where the link points: %s, want nothing", got)
	}

	// writeNewFile replaces a link; what it pointed to stays as it was.
	target := filepath.Join(elsewhere, "target.txt")
	mustWrite(t, target, "as it was")
	link(t, target, filepath.Join(top, "README.txt"))
	if err := writeNewFile(base, "README.txt", "new"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "as it was" {
		t.Errorf("the link's target = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(top, "README.txt")); string(got) != "new" {
		t.Errorf("README.txt = %q", got)
	}

	// makeFolder: a link gives way to the folder, a file doesn't.
	if err := makeFolder(base, "out"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(top, "out")); err != nil || !info.IsDir() {
		t.Errorf("out should be a real folder now: %v, %v", info, err)
	}
	if err := makeFolder(base, "file"); err == nil {
		t.Error("a file with the folder's name is a problem")
	}
	if err := makeFolder(base, "Media"); err != nil {
		t.Errorf("a folder that is there: %v", err)
	}

	// walkFolder: folders before what is in them, by name, into no link.
	mustWrite(t, filepath.Join(elsewhere, "secret.txt"), "x")
	link(t, elsewhere, filepath.Join(top, "Media", "link"))
	var seen []string
	err = walkFolder(base, func(rel string, d fs.DirEntry) error {
		seen = append(seen, rel)
		if rel == "out" {
			return fs.SkipDir
		}
		return nil
	})
	want := "Media|Media/Movies|Media/Movies/film.mkv|Media/link|README.txt|file|inside|out"
	if err != nil || strings.Join(seen, "|") != want {
		t.Errorf("walked %v, %v\nwant   %s", seen, err, want)
	}
}
