package agent

// Encrypting a plugin's USB drive (the Samba plugin's, for example), so
// someone who pulls the drive out and plugs it into a computer can't read
// the files. They are read through the plugin only.
//
// The files are encrypted with gocryptfs (XChaCha20-Poly1305), each file
// on its own, in a folder on the drive called "RasBeacon encrypted files",
// with a README.txt next to it that says what it is. RasBeacon opens the
// folder with the drive's recovery key, kept with its other secrets on the
// SD card (by the drive's ID), and gives the plugin the opened view instead
// of the drive itself.
//
// Turning encryption on moves every file on the drive into the folder, one
// at a time: copied, written to the drive for sure (fsync), and only then
// removed. Turning it off (which needs the recovery key) moves them back.
// The plugin is paused meanwhile, and starts again when all the files are
// moved. If RasBeacon restarts or the drive is pulled out in the middle,
// the move goes on where it stopped the next time (a file is never in only
// half of its copies: a copy that wasn't finished is made again).
//
// The recovery key can be downloaded once the files are encrypted. With
// it, the files can be read on a computer that has gocryptfs, and without
// it nobody can read them, RasBeacon included if its SD card is lost.

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

// The states of a plugin's drive encryption (InstalledPlugin.Encryption).
const (
	encryptionOff        = ""
	encryptionEncrypting = "encrypting"
	encryptionOn         = "on"
	encryptionDecrypting = "decrypting"
)

const (
	// encryptedFolder holds the encrypted files on the drive.
	encryptedFolder = "RasBeacon encrypted files"
	// encryptedReadme says on the drive what the folder is.
	encryptedReadme = "RasBeacon encrypted files - README.txt"
	// partSuffix marks a file being copied, until it is complete.
	partSuffix = ".rasbeacon-part"
)

// encryptedReadmeText is the README written next to the encrypted folder.
const encryptedReadmeText = `This USB drive's files are encrypted by RasBeacon.

They are in the folder "RasBeacon encrypted files", each file encrypted on
its own with gocryptfs (XChaCha20-Poly1305). Plugged into RasBeacon, the
plugin that uses this drive (such as Samba) shows the files as usual.

To read them on a computer, you need the drive's recovery key, which you
can download from the plugin's page on RasBeacon. With gocryptfs installed
(https://nuetzlich.net/gocryptfs/), open a terminal and run:

    gocryptfs "RasBeacon encrypted files" ~/rasbeacon-files

and type the recovery key, with its dashes, when it asks for a password.
The files then appear in the folder rasbeacon-files.

Don't rename, move or change the files in "RasBeacon encrypted files":
they can't be read any more if you do.
`

// newRecoveryKey makes a recovery key: 200 random bits, as 40 letters and
// digits in groups of five ("ABCDE-FGHIJ-…"), easy to read and type.
func newRecoveryKey() string {
	b := make([]byte, 25)
	_, _ = rand.Read(b) // never fails (crypto/rand panics instead)
	return formatRecoveryKey(base32.StdEncoding.EncodeToString(b))
}

// formatRecoveryKey writes a key the way it is shown: upper case, in
// groups of five with dashes. A key typed with spaces, without dashes or in
// lower case comes out the same.
func formatRecoveryKey(typed string) string {
	plain := strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, strings.ToUpper(typed))
	var groups []string
	for len(plain) > 5 {
		groups = append(groups, plain[:5])
		plain = plain[5:]
	}
	return strings.Join(append(groups, plain), "-")
}

// --- Opening and closing the encrypted folder ---

// driveHostDir is the folder the plugin gets as its drive: the opened view
// of the encrypted files while encryption is on, else the drive itself.
func (a *Agent) driveHostDir(p config.InstalledPlugin) string {
	if p.Encryption == encryptionOn {
		return a.paths.DrivePlainDir(p.Drive)
	}
	return a.paths.DriveMountDir(p.Drive)
}

// withKeyFile writes the recovery key to a file only root can read, in
// RasBeacon's run folder (in memory, never on the SD card), for gocryptfs's
// -passfile, and removes it afterwards.
func (a *Agent) withKeyFile(key string, run func(file string) error) error {
	dir := a.paths.RunDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "drive-key-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(key + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return run(f.Name())
}

// openEncryptedFolder makes the encrypted folder on a mounted drive if it
// isn't there yet, and opens it at DrivePlainDir. Called without a.mu.
func (a *Agent) openEncryptedFolder(uuid, key string) error {
	r := a.plugins.runner
	plain := a.paths.DrivePlainDir(uuid)
	if _, err := r.Run("mountpoint", "-q", plain); err == nil {
		return nil // already open
	}
	cipher := filepath.Join(a.paths.DriveMountDir(uuid), encryptedFolder)
	if err := os.MkdirAll(plain, 0o755); err != nil {
		return err
	}
	return a.withKeyFile(key, func(keyFile string) error {
		if _, err := os.Stat(filepath.Join(cipher, "gocryptfs.conf")); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(cipher, 0o700); err != nil {
				return err
			}
			// -xchacha: XChaCha20-Poly1305, fast on a Pi without AES
			// instructions (the Pi 4 and older).
			if _, err := r.Run("gocryptfs", "-init", "-xchacha", "-q", "-passfile", keyFile, cipher); err != nil {
				return fmt.Errorf("the encrypted folder can't be made: %w", err)
			}
		}
		// -allow_other: the plugin's container reaches it too.
		if _, err := r.Run("gocryptfs", "-q", "-allow_other", "-passfile", keyFile, cipher, plain); err != nil {
			return fmt.Errorf("the encrypted folder can't be opened: %w", err)
		}
		return nil
	})
}

// knownDriveEncryption is the encryption of a drive the owner chooses: on
// when RasBeacon has its recovery key (it encrypted the drive for a plugin
// removed since, whose files are still there), else off. The key was given
// then, so it isn't asked for again. The caller holds a.mu.
func (a *Agent) knownDriveEncryption(uuid string) (state string, keySaved bool) {
	if uuid != "" && a.secrets.DriveKeys[uuid] != "" {
		return encryptionOn, true
	}
	return encryptionOff, false
}

// releaseDrive closes a drive's encrypted folder (if open) and unmounts
// the drive, so it can be pulled out. Called without a.mu.
func (a *Agent) releaseDrive(uuid string) {
	if !plugin.ValidDriveUUID(uuid) {
		return
	}
	_, _ = a.plugins.runner.Run("umount", a.paths.DrivePlainDir(uuid))
	_, _ = a.plugins.runner.Run("umount", a.paths.DriveMountDir(uuid))
}

// --- Moving the files ---

// errDriveFull is returned when a file doesn't fit on the drive twice (it
// is copied before the old copy is removed).
var errDriveFull = errors.New("the drive is too full")

// moveFiles moves every file under from into to, keeping the folders they
// are in, one file at a time (see moveFile). skip leaves out paths (relative
// to from) and what is in them. progress is told after each file; an
// error from it stops the move there. Empty folders left behind are
// removed.
func (a *Agent) moveFiles(from, to string, skip func(rel string) bool, progress func(done, total int) error) error {
	var files, dirs []string
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		switch {
		case rel == ".":
		case strings.HasSuffix(rel, partSuffix) && !d.IsDir():
			// A copy that wasn't finished before a restart: made again.
			_ = os.Remove(path)
		case skip(rel):
			if d.IsDir() {
				return filepath.SkipDir
			}
		case d.IsDir():
			dirs = append(dirs, rel)
		case d.Type().IsRegular():
			files = append(files, rel)
		}
		// Links and other special files are left where they are: a drive
		// for sharing files rarely has them, and FAT and exFAT can't.
		return nil
	})
	if err != nil {
		return err
	}
	for _, rel := range dirs {
		if err := os.MkdirAll(filepath.Join(to, rel), 0o755); err != nil {
			return err
		}
	}
	for i, rel := range files {
		if err := a.moveFile(filepath.Join(from, rel), filepath.Join(to, rel)); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if err := progress(i+1, len(files)); err != nil {
			return err
		}
	}
	// The deepest folders first, so each is empty when its turn comes. One
	// that still holds something (a link, say) stays.
	slices.Reverse(dirs)
	for _, rel := range dirs {
		_ = os.Remove(filepath.Join(from, rel))
	}
	return nil
}

// moveFile moves one file: copied to a part file next to where it goes,
// written to the drive for sure, renamed into place, and only then removed
// from where it was. If RasBeacon stopped in the middle last time, a part
// file is made again, and a file already in place (the old copy wasn't
// removed yet) only needs the old copy removed.
func (a *Agent) moveFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	part := dst + partSuffix
	_ = os.Remove(part)
	if done, err := os.Stat(dst); err == nil && done.Size() == info.Size() {
		return os.Remove(src)
	}
	// Both copies are on the same drive for a moment: the new one (a bit
	// bigger when encrypted) must fit next to the old.
	if free, err := a.diskFree(filepath.Dir(dst)); err == nil && free < info.Size()+info.Size()/64+1<<20 {
		return fmt.Errorf("%w: it needs about %s free", errDriveFull, sizeText(int(info.Size()>>20)+1))
	}
	if err := copyFileSynced(src, part, info); err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := os.Rename(part, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// copyFileSynced copies src to dst (which must not exist), waits until it
// is written to the drive, and keeps its modification time.
func copyFileSynced(src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// --- Turning encryption on and off ---

// SetPluginEncryption turns the encryption of a plugin's drive on or off.
// Turning it off needs the drive's recovery key. The files are moved in
// the background (convertDrive) while the plugin is paused; the answer
// comes at once, and the plugin's state shows the progress. It can be
// turned the other way while the files are moving: the move turns around.
func (a *Agent) SetPluginEncryption(req api.PluginEncryptionRequest) (api.Plugin, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	live := a.plugins.live[req.ID]
	converting := live != nil && live.converting
	if !converting {
		var err error
		if live, err = a.beginPluginWork(req.ID); err != nil {
			return api.Plugin{}, err
		}
	}
	// done clears the busy mark (unless a move goes on) for each return
	// below that starts nothing.
	done := func(err error) (api.Plugin, error) {
		if !converting {
			live.busy = false
		}
		if err != nil {
			return api.Plugin{}, err
		}
		return a.pluginView(req.ID), nil
	}
	p := a.cfg.Plugins.Installed[req.ID]
	if p.Drive == "" || !plugin.ValidDriveUUID(p.Drive) {
		return done(&config.ValidationError{Field: "drive", Message: "Choose a USB drive first."})
	}
	key := a.secrets.DriveKeys[p.Drive]
	var want string
	switch {
	case req.On && (p.Encryption == encryptionOn || p.Encryption == encryptionEncrypting) && converting:
		return done(nil) // already on its way
	case req.On && p.Encryption == encryptionOn:
		return done(nil)
	case req.On:
		want = encryptionEncrypting
	case p.Encryption == encryptionOff:
		return done(nil)
	case key == "" || formatRecoveryKey(req.RecoveryKey) != key:
		return done(&config.ValidationError{Field: "recoveryKey", Message: "This isn't the drive's recovery key."})
	case p.Encryption == encryptionDecrypting && converting:
		return done(nil)
	default:
		want = encryptionDecrypting
	}
	if key == "" {
		newSecrets := a.secrets.Clone()
		if newSecrets.DriveKeys == nil {
			newSecrets.DriveKeys = map[string]string{}
		}
		newSecrets.DriveKeys[p.Drive] = newRecoveryKey()
		if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
			return done(err)
		}
		a.secrets = newSecrets
	}
	if err := a.savePluginConfig(req.ID, func(p *config.InstalledPlugin) { p.Encryption = want }); err != nil {
		return done(err)
	}
	slog.Info("drive encryption changing", "plugin", req.ID, "to", want)
	if !converting {
		live.converting = true
		a.setPluginState(req.ID, api.PluginConverting, conversionMessage(want, 0, 0))
		a.background(func() { a.convertDrive(req.ID) })
	}
	return a.pluginView(req.ID), nil
}

// errTurnedAround stops a move whose direction the owner changed.
var errTurnedAround = errors.New("turned around")

// convertDrive moves a plugin's drive files into the encrypted folder or
// out of it, as its Encryption says, with the plugin paused, then starts
// it again if it was running. The plugin is busy until it ends (set by
// SetPluginEncryption, or resumeConversions). Called without a.mu.
func (a *Agent) convertDrive(id string) {
	defer a.endPluginWork(id)
	_, _ = a.plugins.runner.Run("podman", "rm", "--force", "--ignore", "--time", "10", plugin.ContainerName(id))
	for {
		a.mu.Lock()
		p := a.cfg.Plugins.Installed[id]
		key := a.secrets.DriveKeys[p.Drive]
		a.mu.Unlock()
		err := a.convertOnce(id, p.Drive, key, p.Encryption)
		if errors.Is(err, errTurnedAround) {
			continue
		}
		a.mu.Lock()
		live := a.plugins.live[id]
		if live != nil {
			live.converting = false
		}
		if err != nil {
			slog.Error("could not move the drive's files", "plugin", id, "error", err)
			action := "Encrypting"
			if p.Encryption == encryptionDecrypting {
				action = "Decrypting"
			}
			a.setPluginState(id, api.PluginFailed, fmt.Sprintf(
				"%s the drive's files stopped: %v. Nothing is lost; turn encryption %s again to go on.",
				action, err, map[bool]string{true: "on", false: "off"}[action == "Encrypting"]))
			a.mu.Unlock()
			return
		}
		if err := a.finishConversion(id, p); err != nil {
			a.setPluginState(id, api.PluginFailed, "The drive's files were moved, but the settings can't be saved: "+err.Error())
			a.mu.Unlock()
			return
		}
		running := a.cfg.Plugins.Installed[id].Running
		a.setPluginState(id, api.PluginStopped, "")
		a.mu.Unlock()
		if running {
			_ = a.runPlugin(id) // its state shows any problem
		}
		return
	}
}

// convertOnce moves the files one way: into the encrypted folder
// (encrypting) or out of it (decrypting). It stops with errTurnedAround
// when the owner changed the direction. Called without a.mu.
func (a *Agent) convertOnce(id, uuid, key, direction string) error {
	if err := a.mountDrive(uuid); err != nil {
		return err
	}
	if err := a.openEncryptedFolder(uuid, key); err != nil {
		return err
	}
	root, plain := a.paths.DriveMountDir(uuid), a.paths.DrivePlainDir(uuid)
	progress := func(done, total int) error {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.cfg.Plugins.Installed[id].Encryption != direction {
			return errTurnedAround
		}
		if live := a.plugins.live[id]; live != nil {
			live.convertDone, live.convertTotal = done, total
		}
		a.setPluginState(id, api.PluginConverting, conversionMessage(direction, done, total))
		return nil
	}
	if direction == encryptionEncrypting {
		readme := filepath.Join(root, encryptedReadme)
		if err := os.WriteFile(readme, []byte(encryptedReadmeText), 0o644); err != nil {
			return err
		}
		ours := func(rel string) bool { return rel == encryptedFolder || rel == encryptedReadme }
		return a.moveFiles(root, plain, ours, progress)
	}
	if err := a.moveFiles(plain, root, func(string) bool { return false }, progress); err != nil {
		return err
	}
	// Everything is out: the encrypted folder and its README go.
	if _, err := a.plugins.runner.Run("umount", plain); err != nil {
		return fmt.Errorf("the encrypted folder can't be closed: %w", err)
	}
	if err := os.RemoveAll(filepath.Join(root, encryptedFolder)); err != nil {
		return err
	}
	// (No README when it was turned off before the first file moved.)
	if err := os.Remove(filepath.Join(root, encryptedReadme)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// finishConversion saves that the move is done: encryption on, or off
// (then the recovery key goes too, as nothing is encrypted with it any
// more). The caller holds a.mu.
func (a *Agent) finishConversion(id string, p config.InstalledPlugin) error {
	if live := a.plugins.live[id]; live != nil {
		live.convertDone, live.convertTotal = 0, 0
	}
	if p.Encryption == encryptionEncrypting {
		slog.Info("drive encrypted", "plugin", id)
		return a.savePluginConfig(id, func(p *config.InstalledPlugin) { p.Encryption = encryptionOn })
	}
	newSecrets := a.secrets.Clone()
	delete(newSecrets.DriveKeys, p.Drive)
	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return err
	}
	a.secrets = newSecrets
	slog.Info("drive decrypted", "plugin", id)
	return a.savePluginConfig(id, func(p *config.InstalledPlugin) {
		p.Encryption, p.RecoveryKeySaved = encryptionOff, false
	})
}

// conversionMessage is the plugin's state message while its files move:
// "Encrypting the drive's files: 120 of 3,010".
func conversionMessage(direction string, done, total int) string {
	action := "Encrypting"
	if direction == encryptionDecrypting {
		action = "Decrypting"
	}
	if total == 0 {
		return action + " the drive's files…"
	}
	return fmt.Sprintf("%s the drive's files: %s of %s", action, thousands(done), thousands(total))
}

// thousands writes a count with commas: 3010 → "3,010".
func thousands(n int) string {
	digits := strconv.Itoa(n)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}

// resumeConversions goes on with moves that RasBeacon restarted in the
// middle of; startPlugins calls it first and leaves these plugins to it.
// The caller holds a.mu.
func (a *Agent) resumeConversions() map[string]bool {
	resumed := map[string]bool{}
	for _, id := range a.pluginIDs() {
		p := a.cfg.Plugins.Installed[id]
		live := a.plugins.live[id]
		if live == nil || live.busy || (p.Encryption != encryptionEncrypting && p.Encryption != encryptionDecrypting) {
			continue
		}
		live.busy, live.converting = true, true
		resumed[id] = true
		a.setPluginState(id, api.PluginConverting, conversionMessage(p.Encryption, 0, 0))
		a.background(func() { a.convertDrive(id) })
	}
	return resumed
}

// --- The recovery key ---

// PluginRecoveryKey is the drive's recovery key as a text file to save.
// It is given once the files are encrypted; after that, the page stops
// asking to save it.
func (a *Agent) PluginRecoveryKey(id string) (api.PluginRecoveryKey, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.cfg.Plugins.Installed[id]
	if !ok {
		return api.PluginRecoveryKey{}, errNoPlugin
	}
	key := a.secrets.DriveKeys[p.Drive]
	if p.Encryption != encryptionOn || key == "" {
		return api.PluginRecoveryKey{}, &config.ValidationError{Field: "id", Message: "The drive's files aren't encrypted."}
	}
	if !p.RecoveryKeySaved {
		if err := a.savePluginConfig(id, func(p *config.InstalledPlugin) { p.RecoveryKeySaved = true }); err != nil {
			return api.PluginRecoveryKey{}, err
		}
	}
	name := a.pluginName(id)
	text := fmt.Sprintf(`RasBeacon recovery key

For the USB drive %s, used by %s. Saved %s.

    %s

Keep it somewhere safe, away from the drive: with it, anyone can read the
drive's files, and without it, nobody can (RasBeacon too, if its SD card
is lost or erased). It is also needed to turn the encryption off.

To read the files on a computer, see "RasBeacon encrypted files -
README.txt" on the drive.
`, p.Drive, name, a.now().Format("2 January 2006"), key)
	return api.PluginRecoveryKey{FileName: "rasbeacon-recovery-key-" + p.Drive + ".txt", Text: text}, nil
}
