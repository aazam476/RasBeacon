package agent

// This file keeps rbd's own work on a share's files inside the share.
//
// What is in a share is not RasBeacon's: its plugin writes there, and on a
// USB drive so does anyone who had the drive in a computer. Either can
// leave a link there, a name that stands for another place ("Movies" for
// "/etc"). rbd is root, and a program that opens "Movies/film.mkv" by its
// path opens whatever a link named Movies points to. So if rbd made a
// share's folder, moved its files to encrypt them, or deleted them, by
// their paths, a link could send that work to any file on the Pi.
//
// rbd therefore never uses such a path. It opens the top of the drive, or
// of a share on internal storage, as an os.Root, which reaches nothing
// outside that folder whatever links are in it. And the folders RasBeacon
// itself decides on (a share's folder on its drive, the folder of its
// encrypted files) must be real folders, every part of them: a link with
// one of their names is refused, even one that stays on the drive, where
// it would put the share in another folder than the owner chose.
//
// The programs rbd starts on a share (rbquotafs, gocryptfs) still get its
// path. On a drive the kernel keeps them from following a link: drives
// are mounted with nosymfollow (see plugin.MountOptions), and so is the
// opened view of an encrypted share there (see refuseLinksIn).

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"rasbeacon/internal/config"
)

// notFolder is the problem of a folder's name being taken by something
// else.
func notFolder(name string, info fs.FileInfo) error {
	what := "a file"
	if info.Mode()&fs.ModeSymlink != 0 {
		what = "a link"
	}
	return fmt.Errorf("%q is %s, not a folder (RasBeacon follows no links in a share)", name, what)
}

// folderParts splits a folder as a share has it ("Media/Movies") into its
// parts. "" (the whole drive) has none. Anything but plain names is
// refused: the folder was checked when the share was made (see
// cleanShareFolder), but settings can also come from a backup.
func folderParts(folder string) ([]string, error) {
	if folder == "" {
		return nil, nil
	}
	parts := strings.Split(folder, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("%q isn't a folder on the drive", folder)
		}
	}
	return parts, nil
}

// realFolder opens folder ("Media/Movies") below base, following no link:
// every part of it must be a real folder. With create, the parts that are
// missing are made. The caller closes it.
func realFolder(base *os.Root, folder string, create bool) (*os.Root, error) {
	parts, err := folderParts(folder)
	if err != nil {
		return nil, err
	}
	dir, err := base.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, part := range parts {
		next, err := realSubfolder(dir, part, create, 0o755)
		dir.Close()
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

// realSubfolder opens the folder called name in dir, which must be a real
// folder there, not a link or a file. With create, it is made (with mode)
// if it is missing.
func realSubfolder(dir *os.Root, name string, create bool, mode os.FileMode) (*os.Root, error) {
	info, err := dir.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) && create {
		if err := dir.Mkdir(name, mode); err != nil {
			return nil, err
		}
		info, err = dir.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, notFolder(name, info)
	}
	sub, err := dir.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	// What was opened is the folder that was looked at, not a link put in
	// its place since.
	if opened, err := sub.Stat("."); err != nil || !os.SameFile(info, opened) {
		sub.Close()
		return nil, fmt.Errorf("%q changed while it was being opened", name)
	}
	return sub, nil
}

// openShareStore opens the folder a share's files are kept in (see
// shareStoreDir), which must be mounted. On a drive it is reached from the
// drive's top through real folders only (see realFolder); with create, it
// is made there if it is missing. The caller closes it.
func (a *Agent) openShareStore(id string, share config.PluginShare, create bool) (*os.Root, error) {
	if share.Drive == "" {
		return os.OpenRoot(a.paths.PluginShareMount(id, share.ID))
	}
	drive, err := os.OpenRoot(a.paths.DriveMountDir(share.Drive))
	if err != nil {
		return nil, err
	}
	defer drive.Close()
	return realFolder(drive, share.Folder, create)
}

// encryptedFolderIn makes sure the folder for a share's encrypted files
// (encryptedFolder, in store) is a real folder, making it if it is
// missing, and reports whether gocryptfs has set it up already (its
// gocryptfs.conf is there). A link with the folder's name is refused:
// gocryptfs would put the share's files where it points.
func encryptedFolderIn(store *os.Root) (ready bool, err error) {
	dir, err := realSubfolder(store, encryptedFolder, true, 0o700)
	if err != nil {
		return false, err
	}
	defer dir.Close()
	info, err := dir.Lstat("gocryptfs.conf")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("gocryptfs.conf in %q is not a plain file", encryptedFolder)
	}
	return true, nil
}

// hasEncryptedFolder reports whether a share's folder already holds
// encrypted files that gocryptfs set up (for a share made again where an
// encrypted one was). Nothing is made, and no link is followed.
func (a *Agent) hasEncryptedFolder(id string, share config.PluginShare) bool {
	store, err := a.openShareStore(id, share, false)
	if err != nil {
		return false
	}
	defer store.Close()
	dir, err := realSubfolder(store, encryptedFolder, false, 0)
	if err != nil {
		return false
	}
	defer dir.Close()
	info, err := dir.Lstat("gocryptfs.conf")
	return err == nil && info.Mode().IsRegular()
}

// refuseLinksIn makes the kernel follow no link in what is mounted at
// mount (nosymfollow), as it does on a drive: for the opened view of an
// encrypted share on a drive, which rbquotafs works in by its paths as
// root, for a plugin that can put links there. Reading a link, and making
// one, still work, and the plugin's own view (rbquotafs's) follows them as
// before.
func (a *Agent) refuseLinksIn(mount string) error {
	// The other two are what a FUSE mount has anyway; a remount sets all
	// of them anew.
	_, err := a.plugins.runner.Run("mount", "-o", "remount,bind,nosuid,nodev,nosymfollow", mount)
	return err
}

// writeNewFile writes a file of RasBeacon's own in a share's folder,
// replacing what has its name: a link there is removed, never followed.
func writeNewFile(dir *os.Root, name, text string) error {
	if err := dir.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// O_EXCL: only ever a new file, whatever turned up meanwhile.
	f, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// walkFolder calls visit for everything in root's folder, a folder before
// what is in it, in the order of the names. rel is the path from the
// folder. Like filepath.WalkDir it follows no link; when visit returns
// fs.SkipDir for a folder, what is in it is left out.
func walkFolder(root *os.Root, visit func(rel string, d fs.DirEntry) error) error {
	var walk func(dir string) error
	walk = func(dir string) error {
		f, err := root.Open(dir)
		if err != nil {
			return err
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return err
		}
		slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		for _, entry := range entries {
			rel := filepath.Join(dir, entry.Name())
			err := visit(rel, entry)
			if errors.Is(err, fs.SkipDir) {
				continue
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if err := walk(rel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(".")
}

// makeFolder makes the folder rel in root if it isn't there (the folders
// it is in are). A link with its name gives way to it: the folder comes
// with files to put in it, and the link would send them elsewhere. A file
// with its name stays, and is a problem.
func makeFolder(root *os.Root, rel string) error {
	info, err := root.Lstat(rel)
	if err == nil && info.Mode()&fs.ModeSymlink != 0 {
		if err := root.Remove(rel); err != nil {
			return err
		}
		return root.Mkdir(rel, 0o755)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return root.Mkdir(rel, 0o755)
	case err != nil:
		return err
	case !info.IsDir():
		return notFolder(rel, info)
	}
	return nil
}
