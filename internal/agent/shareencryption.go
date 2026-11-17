package agent

// Encrypting a share's files (a plugin's share, Samba's for example), so
// someone who pulls out the USB drive or the SD card and plugs it into a
// computer can't read them. They are read through the plugin only.
//
// The files are encrypted with gocryptfs (XChaCha20-Poly1305), each file
// on its own, in a folder called "RasBeacon encrypted files" in the share
// (its folder on the drive, or its storage file on internal storage), with
// a README next to it that says what it is. RasBeacon opens the folder
// with the share's recovery key, kept with its other secrets on the SD
// card, and gives the plugin the opened view instead.
//
// Turning encryption on moves every file of the share into the folder,
// one at a time: copied, written for sure (fsync), and only then removed.
// Turning it off (which needs the recovery key) moves them back. The
// plugin is paused meanwhile, and starts again when all the files are
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

// The states of a share's encryption (PluginShare.Encryption).
const (
	encryptionOff        = ""
	encryptionEncrypting = "encrypting"
	encryptionOn         = "on"
	encryptionDecrypting = "decrypting"
)

const (
	// encryptedFolder holds a share's encrypted files.
	encryptedFolder = "RasBeacon encrypted files"
	// encryptedReadme says next to it what the folder is.
	encryptedReadme = "RasBeacon encrypted files - README.txt"
	// partSuffix marks a file being copied, until it is complete.
	partSuffix = ".rasbeacon-part"
)

// encryptedReadmeText is the README written next to the encrypted folder.
const encryptedReadmeText = `This share's files are encrypted by RasBeacon.

They are in the folder "RasBeacon encrypted files", each file encrypted on
its own with gocryptfs (XChaCha20-Poly1305). On RasBeacon, the plugin that
offers this share (such as Samba) shows the files as usual.

To read them on a computer, you need the share's recovery key, which you
can download from the plugin's page on RasBeacon. With gocryptfs installed
(https://nuetzlich.net/gocryptfs/), open a terminal in this folder and run:

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

// --- Opening the encrypted folder ---

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

// openEncryptedFolder makes the encrypted folder in a share if it isn't
// there yet, and opens it at plain. store is the share's folder, storeDir
// its path (which the caller has checked, see openShareStore). Called
// without a.mu.
func (a *Agent) openEncryptedFolder(store *os.Root, storeDir, plain, key string) error {
	r := a.plugins.runner
	if _, err := r.Run("mountpoint", "-q", plain); err == nil {
		return nil // already open
	}
	if err := os.MkdirAll(plain, 0o755); err != nil {
		return err
	}
	// A real folder of the share's, never a link to another place.
	ready, err := encryptedFolderIn(store)
	if err != nil {
		return fmt.Errorf("the encrypted folder can't be made: %w", err)
	}
	cipher := filepath.Join(storeDir, encryptedFolder)
	return a.withKeyFile(key, func(keyFile string) error {
		if !ready {
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

// releaseDrive unmounts a plugin's drive, so it can be pulled out (its
// encrypted shares are closed first, by releaseSharesOf). Called without
// a.mu.
func (a *Agent) releaseDrive(uuid string) {
	if !plugin.ValidDriveUUID(uuid) {
		return
	}
	_, _ = a.plugins.runner.Run("umount", a.paths.DriveMountDir(uuid))
}

// --- Moving the files ---

// errDriveFull is returned when a file doesn't fit on the drive twice (it
// is copied before the old copy is removed).
var errDriveFull = errors.New("the drive is too full")

// moveFiles moves every file in the folder from into the folder to,
// keeping the folders they are in, one file at a time (see moveFile). skip
// leaves out paths (relative to from) and what is in them. progress is
// told after each file; an error from it stops the move there. Empty
// folders left behind are removed.
//
// Both folders are opened ones (see sharelinks.go): nothing is read or
// written outside them, whatever links they hold. A link in to with the
// name of a folder that comes from from gives way to that folder.
func (a *Agent) moveFiles(from, to *os.Root, skip func(rel string) bool, progress func(done, total int) error) error {
	var files, dirs []string
	err := walkFolder(from, func(rel string, d fs.DirEntry) error {
		switch {
		case strings.HasSuffix(rel, partSuffix) && !d.IsDir():
			// A copy that wasn't finished before a restart: made again.
			_ = from.Remove(rel)
		case skip(rel):
			return fs.SkipDir
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
		if err := makeFolder(to, rel); err != nil {
			return err
		}
	}
	for i, rel := range files {
		if err := a.moveFile(from, to, rel); err != nil {
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
		_ = from.Remove(rel)
	}
	return nil
}

// moveFile moves the file rel from the folder from to the same place in
// the folder to: copied to a part file next to where it goes, written to
// the drive for sure, renamed into place, and only then removed from where
// it was. If RasBeacon stopped in the middle last time, a part file is
// made again, and a file already in place (the old copy wasn't removed
// yet) only needs the old copy removed.
func (a *Agent) moveFile(from, to *os.Root, rel string) error {
	info, err := from.Lstat(rel)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("it is not a plain file any more")
	}
	part := rel + partSuffix
	_ = to.Remove(part)
	// (A link with the file's name isn't the file: the file replaces it.)
	if done, err := to.Lstat(rel); err == nil && done.Mode().IsRegular() && done.Size() == info.Size() {
		return from.Remove(rel)
	}
	// Both copies are on the same drive for a moment: the new one (a bit
	// bigger when encrypted) must fit next to the old.
	if free, err := a.diskFree(to.Name()); err == nil && free < info.Size()+info.Size()/64+1<<20 {
		return fmt.Errorf("%w: it needs about %s free", errDriveFull, sizeText(int(info.Size()>>20)+1))
	}
	if err := copyFileSynced(from, to, rel, part, info); err != nil {
		_ = to.Remove(part)
		return err
	}
	if err := to.Rename(part, rel); err != nil {
		return err
	}
	return from.Remove(rel)
}

// copyFileSynced copies the file rel in the folder from to dst in the
// folder to (which must not exist), waits until it is written to the
// drive, and keeps its modification time.
func copyFileSynced(from, to *os.Root, rel, dst string, info fs.FileInfo) error {
	in, err := from.Open(rel)
	if err != nil {
		return err
	}
	defer in.Close()
	// O_EXCL: a new file, never one a link with its name points to.
	out, err := to.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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
	return to.Chtimes(dst, info.ModTime(), info.ModTime())
}

// --- Turning encryption on and off ---

// SetPluginEncryption turns the encryption of one share on or off. Turning
// it off needs the share's recovery key. The files are moved in the
// background (convertShare) while the plugin is paused; the answer comes
// at once, and the plugin's state shows the progress. It can be turned
// the other way while the files are moving: the move turns around.
func (a *Agent) SetPluginEncryption(req api.PluginEncryptionRequest) (api.Plugin, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	live := a.plugins.live[req.ID]
	// Only the share being moved can be turned around meanwhile.
	converting := live != nil && live.converting && live.convertShare == req.Share
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
	index := findShare(p.Shares, req.Share)
	if index < 0 {
		return done(&config.ValidationError{Field: "share", Message: "No such share."})
	}
	share := p.Shares[index]
	keyName := shareKeyName(req.ID, share)
	key := a.secrets.ShareKeys[keyName]
	var want string
	switch {
	case req.On && (share.Encryption == encryptionOn || share.Encryption == encryptionEncrypting && converting):
		return done(nil) // on, or already on its way
	case req.On:
		want = encryptionEncrypting
	case share.Encryption == encryptionOff:
		return done(nil)
	case key == "" || formatRecoveryKey(req.RecoveryKey) != key:
		return done(&config.ValidationError{Field: "recoveryKey", Message: "This isn't the share's recovery key."})
	case share.Encryption == encryptionDecrypting && converting:
		return done(nil)
	default:
		want = encryptionDecrypting
	}
	if converting {
		// The move going on picks up the new direction.
		if err := a.saveShare(req.ID, req.Share, func(s *config.PluginShare) { s.Encryption = want }); err != nil {
			return done(err)
		}
		slog.Info("share encryption changing", "plugin", req.ID, "share", req.Share, "to", want)
		return a.pluginView(req.ID), nil
	}
	if err := a.startConversion(live, req.ID, share, want); err != nil {
		return done(err)
	}
	return a.pluginView(req.ID), nil
}

// startConversion starts moving a share's files into its encrypted folder
// (want encrypting) or out of it (decrypting), in the background: it makes
// the share's key if it has none yet, saves the direction, and marks the
// plugin as moving. The plugin must be marked busy (beginPluginWork); the
// move ends that when it is done. The caller holds a.mu.
func (a *Agent) startConversion(live *pluginLive, id string, share config.PluginShare, want string) error {
	keyName := shareKeyName(id, share)
	if a.secrets.ShareKeys[keyName] == "" {
		newSecrets := a.secrets.Clone()
		if newSecrets.ShareKeys == nil {
			newSecrets.ShareKeys = map[string]string{}
		}
		newSecrets.ShareKeys[keyName] = newRecoveryKey()
		if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
			return err
		}
		a.secrets = newSecrets
	}
	if err := a.saveShare(id, share.ID, func(s *config.PluginShare) { s.Encryption = want }); err != nil {
		return err
	}
	slog.Info("share encryption changing", "plugin", id, "share", share.ID, "to", want)
	live.converting, live.convertShare = true, share.ID
	a.setPluginState(id, api.PluginConverting, conversionMessage(share.Name, want, 0, 0))
	a.background(func() { a.convertShare(id, share.ID) })
	return nil
}

// saveShare saves a change to one share. The caller holds a.mu.
func (a *Agent) saveShare(id, shareID string, change func(s *config.PluginShare)) error {
	return a.savePluginConfig(id, func(p *config.InstalledPlugin) {
		if i := findShare(p.Shares, shareID); i >= 0 {
			change(&p.Shares[i])
		}
	})
}

// errTurnedAround stops a move whose direction the owner changed.
var errTurnedAround = errors.New("turned around")

// convertShare moves a share's files into its encrypted folder or out of
// it, as its Encryption says, with the plugin paused, then starts it again
// if it was running. The plugin is busy until it ends (set by
// SetPluginEncryption, or resumeConversions). Called without a.mu.
func (a *Agent) convertShare(id, shareID string) {
	defer a.endPluginWork(id)
	_, _ = a.plugins.runner.Run("podman", "rm", "--force", "--ignore", "--time", "10", plugin.ContainerName(id))
	for {
		a.mu.Lock()
		p := a.cfg.Plugins.Installed[id]
		index := findShare(p.Shares, shareID)
		if index < 0 {
			a.mu.Unlock()
			return
		}
		share := p.Shares[index]
		key := a.secrets.ShareKeys[shareKeyName(id, share)]
		a.mu.Unlock()
		// Its quota view sits on top of its files: let go of it first. Its
		// reserved room on the drive is given back while files move (each
		// is copied before the old one goes); it is held again after.
		_, _ = a.plugins.runner.Run("umount", a.paths.ShareQuotaDir(id, share.ID))
		if r := a.reservation(id, share); r != nil {
			_ = r.AtMost(0)
		}
		err := a.convertOnce(id, share, key)
		if errors.Is(err, errTurnedAround) {
			continue
		}
		a.mu.Lock()
		live := a.plugins.live[id]
		if live != nil {
			live.converting, live.convertShare = false, ""
		}
		if err != nil {
			slog.Error("could not move a share's files", "plugin", id, "share", shareID, "error", err)
			action, again := "Encrypting", "on"
			if share.Encryption == encryptionDecrypting {
				action, again = "Decrypting", "off"
			}
			a.setPluginState(id, api.PluginFailed, fmt.Sprintf(
				"%s the share %s stopped: %v. Nothing is lost; turn its encryption %s again to go on.", action, share.Name, err, again))
			a.mu.Unlock()
			return
		}
		if err := a.finishConversion(id, share); err != nil {
			a.setPluginState(id, api.PluginFailed, "The share's files were moved, but the settings can't be saved: "+err.Error())
			a.mu.Unlock()
			return
		}
		running := a.cfg.Plugins.Installed[id].Running
		a.reserveWhileStopped(id, share, a.cachedShareUsage(share))
		a.setPluginState(id, api.PluginStopped, "")
		a.mu.Unlock()
		if running {
			_ = a.runPlugin(id) // its state shows any problem
		}
		return
	}
}

// convertOnce moves a share's files one way: into the encrypted folder
// (encrypting) or out of it (decrypting). It stops with errTurnedAround
// when the owner changed the direction. Called without a.mu.
func (a *Agent) convertOnce(id string, share config.PluginShare, key string) error {
	storeDir, plainDir := a.shareStoreDir(id, share), a.paths.SharePlainDir(id, share.ID)
	if share.Drive == "" {
		if err := a.mountImage(a.paths.PluginShareImage(id, share.ID), storeDir, share.QuotaMB); err != nil {
			return err
		}
	} else if err := a.mountDrive(share.Drive); err != nil {
		return err
	}
	// The share's folder and its opened view, as opened folders: the files
	// move between these two and nowhere else (see sharelinks.go).
	store, err := a.openShareStore(id, share, true)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := a.openEncryptedFolder(store, storeDir, plainDir, key); err != nil {
		return err
	}
	plain, err := os.OpenRoot(plainDir)
	if err != nil {
		return err
	}
	defer plain.Close()
	direction := share.Encryption
	progress := func(done, total int) error {
		a.mu.Lock()
		defer a.mu.Unlock()
		p := a.cfg.Plugins.Installed[id]
		if i := findShare(p.Shares, share.ID); i < 0 || p.Shares[i].Encryption != direction {
			return errTurnedAround
		}
		if live := a.plugins.live[id]; live != nil {
			live.convertDone, live.convertTotal = done, total
		}
		a.setPluginState(id, api.PluginConverting, conversionMessage(share.Name, direction, done, total))
		return nil
	}
	if direction == encryptionEncrypting {
		if err := writeNewFile(store, encryptedReadme, encryptedReadmeText); err != nil {
			return err
		}
		// ext4 keeps lost+found at the top of a share on internal
		// storage: it stays where it is.
		ours := func(rel string) bool { return rel == encryptedFolder || rel == encryptedReadme || rel == "lost+found" }
		return a.moveFiles(store, plain, ours, progress)
	}
	if err := a.moveFiles(plain, store, func(string) bool { return false }, progress); err != nil {
		return err
	}
	// Everything is out: the encrypted folder and its README go. (The view
	// can only be closed once nothing has it open.)
	plain.Close()
	if _, err := a.plugins.runner.Run("umount", plainDir); err != nil {
		return fmt.Errorf("the encrypted folder can't be closed: %w", err)
	}
	if err := store.RemoveAll(encryptedFolder); err != nil {
		return err
	}
	// (No README when it was turned off before the first file moved.)
	if err := store.Remove(encryptedReadme); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// finishConversion saves that the move is done: encryption on, or off
// (then the recovery key goes too, as nothing is encrypted with it any
// more). The caller holds a.mu.
func (a *Agent) finishConversion(id string, share config.PluginShare) error {
	if live := a.plugins.live[id]; live != nil {
		live.convertDone, live.convertTotal = 0, 0
	}
	if share.Encryption == encryptionEncrypting {
		slog.Info("share encrypted", "plugin", id, "share", share.ID)
		return a.saveShare(id, share.ID, func(s *config.PluginShare) { s.Encryption = encryptionOn })
	}
	a.forgetShareKey(shareKeyName(id, share))
	slog.Info("share decrypted", "plugin", id, "share", share.ID)
	return a.saveShare(id, share.ID, func(s *config.PluginShare) {
		s.Encryption, s.RecoveryKeySaved = encryptionOff, false
	})
}

// conversionMessage is the plugin's state message while a share's files
// move: "Encrypting the share Movies: 120 of 3,010 files".
func conversionMessage(name, direction string, done, total int) string {
	action := "Encrypting"
	if direction == encryptionDecrypting {
		action = "Decrypting"
	}
	if total == 0 {
		return action + " the share " + name + "…"
	}
	return fmt.Sprintf("%s the share %s: %s of %s files", action, name, thousands(done), thousands(total))
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
// middle of; startPlugins calls it first and leaves these plugins to it
// (one share at a time per plugin; the others go on when it starts
// again). The caller holds a.mu.
func (a *Agent) resumeConversions() map[string]bool {
	resumed := map[string]bool{}
	for _, id := range a.pluginIDs() {
		live := a.plugins.live[id]
		if live == nil || live.busy {
			continue
		}
		for _, share := range a.cfg.Plugins.Installed[id].Shares {
			if share.Encryption != encryptionEncrypting && share.Encryption != encryptionDecrypting {
				continue
			}
			live.busy, live.converting, live.convertShare = true, true, share.ID
			resumed[id] = true
			a.setPluginState(id, api.PluginConverting, conversionMessage(share.Name, share.Encryption, 0, 0))
			shareID := share.ID
			a.background(func() { a.convertShare(id, shareID) })
			break
		}
	}
	return resumed
}

// --- The recovery key ---

// PluginRecoveryKey is a share's recovery key as a text file to save. It
// is given once the files are encrypted; after that, the page stops asking
// to save it.
func (a *Agent) PluginRecoveryKey(req api.PluginShareRequest) (api.PluginRecoveryKey, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.cfg.Plugins.Installed[req.ID]
	if !ok {
		return api.PluginRecoveryKey{}, errNoPlugin
	}
	index := findShare(p.Shares, req.Share)
	if index < 0 {
		return api.PluginRecoveryKey{}, &config.ValidationError{Field: "share", Message: "No such share."}
	}
	share := p.Shares[index]
	key := a.secrets.ShareKeys[shareKeyName(req.ID, share)]
	if share.Encryption != encryptionOn || key == "" {
		return api.PluginRecoveryKey{}, &config.ValidationError{Field: "share", Message: "The share's files aren't encrypted."}
	}
	if !share.RecoveryKeySaved {
		if err := a.saveShare(req.ID, req.Share, func(s *config.PluginShare) { s.RecoveryKeySaved = true }); err != nil {
			return api.PluginRecoveryKey{}, err
		}
	}
	where := "on RasBeacon's internal storage"
	if share.Drive != "" {
		where = fmt.Sprintf("in the folder %s on the USB drive %s", share.Folder, share.Drive)
	}
	text := fmt.Sprintf(`RasBeacon recovery key

For the share %s of %s, %s. Saved %s.

    %s

Keep it somewhere safe, away from the drive: with it, anyone can read the
share's files, and without it, nobody can (RasBeacon too, if its SD card
is lost or erased). It is also needed to turn the encryption off.

To read the files on a computer, see "RasBeacon encrypted files -
README.txt" in the share.
`, share.Name, a.pluginName(req.ID), where, a.now().Format("2 January 2006"), key)
	safe := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, share.Name)
	return api.PluginRecoveryKey{FileName: "rasbeacon-recovery-key-" + safe + ".txt", Text: text}, nil
}
