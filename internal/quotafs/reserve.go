package quotafs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// A share on a USB drive is a folder, and a folder can't have space set
// aside for it the way a file can. So the room its quota promises, and its
// files don't use yet, is held by files of its own: its "reservation", in
// the drive's ReservedFolder. A computer then sees that space as taken,
// and nothing else on the drive (another share, or a computer copying
// files while the drive is out) can use it. As the share's files grow, its
// reservation shrinks by as much first; as they shrink, it grows back.
//
// The reservation is split into pieces of at most pieceSize (a FAT32 file
// can't be 4 GB), named "<name>-001.bin", "<name>-002.bin", and so on.

// ReservedFolder is the folder at the top of a drive that holds its
// shares' reservations, with a README saying what it is.
const ReservedFolder = "RasBeacon reserved space"

// pieceSize is the most one reservation file holds (variables, so tests
// can use small ones). growStep is how much a reservation grows at a
// time, so a share writing meanwhile never waits long for it to shrink
// again.
var (
	pieceSize int64 = 1 << 30
	growStep  int64 = 64 << 20
)

// reservedReadme says what the folder is to someone who finds it on a
// computer.
const reservedReadme = `These files hold the space of RasBeacon's shares on this drive.

Each share has a quota: the most its files may take. The part of it they
don't take yet is kept here, so that nothing else on the drive can use it,
and the drive shows as full as its shares may become.

Deleting these files frees the space until the drive is next used by
RasBeacon, which then makes them again. Removing a share on RasBeacon
deletes its files here.
`

// Reservation is one share's reservation, in dir (the drive's
// ReservedFolder), named name. Its methods may be called from several
// goroutines and processes (rbd and rbquotafs): each takes a lock on a
// file of its own.
//
// A drive comes from anywhere, and what is on it can't be trusted: where
// the folder should be there may be a link to another place, and in the
// folder links with its files' names. The reservation's files are as big
// as a share's quota, and they are made, cut short and deleted by root,
// so none of that may happen where a link points. The folder must
// therefore be a real one, and it is opened as an os.Root (see open),
// through which nothing outside it can be reached.
type Reservation struct {
	dir, name string
	mu        sync.Mutex
}

// NewReservation is the reservation called name in dir.
func NewReservation(dir, name string) *Reservation {
	return &Reservation{dir: dir, name: name}
}

// Name is the reservation's name (its plugin and share).
func (r *Reservation) Name() string { return r.name }

// open opens the reservations' folder, which must be a real folder at the
// top of the drive, not a link. With create, it is made if it isn't
// there, with its README. The caller closes it.
func (r *Reservation) open(create bool) (*os.Root, error) {
	top, err := os.OpenRoot(filepath.Dir(r.dir))
	if err != nil {
		return nil, err
	}
	defer top.Close()
	folder := filepath.Base(r.dir)
	info, err := top.Lstat(folder)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := top.Mkdir(folder, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err = top.Lstat(folder)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q on the drive is not a folder", folder)
	}
	dir, err := top.OpenRoot(folder)
	if err != nil {
		return nil, err
	}
	// What was opened is the folder that was looked at, not a link put in
	// its place since.
	if opened, err := dir.Stat("."); err != nil || !os.SameFile(info, opened) {
		dir.Close()
		return nil, fmt.Errorf("%q on the drive changed while it was being opened", folder)
	}
	if _, err := dir.Lstat("README.txt"); errors.Is(err, os.ErrNotExist) && create {
		// O_EXCL: a new file, or none.
		if f, err := dir.OpenFile("README.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
			_, _ = f.WriteString(reservedReadme)
			f.Close()
		}
	}
	return dir, nil
}

// lockName is the name of the reservation's lock file in the folder.
func (r *Reservation) lockName() string { return "." + r.name + ".lock" }

// lock takes the reservation's locks (this process's, and the file's for
// other processes), making the folder if it isn't there. It returns the
// opened folder, and a function that lets go of the locks and closes it.
func (r *Reservation) lock() (*os.Root, func(), error) {
	r.mu.Lock()
	dir, err := r.open(true)
	if err != nil {
		r.mu.Unlock()
		return nil, nil, err
	}
	f, err := dir.OpenFile(r.lockName(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		dir.Close()
		r.mu.Unlock()
		return nil, nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		dir.Close()
		r.mu.Unlock()
		return nil, nil, err
	}
	return dir, func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
		dir.Close()
		r.mu.Unlock()
	}, nil
}

// entries are the names of what is in the folder.
func entries(dir *os.Root) []string {
	f, err := dir.Open(".")
	if err != nil {
		return nil
	}
	defer f.Close()
	names, _ := f.Readdirnames(-1)
	return names
}

// pieces are the reservation's files in the folder, in order, with their
// sizes. Only plain files are: something else with a piece's name (a
// link, say) holds no room, and counts as 0 bytes.
func (r *Reservation) pieces(dir *os.Root) ([]string, []int64) {
	var names []string
	for _, name := range entries(dir) {
		if strings.HasPrefix(name, r.name+"-") && strings.HasSuffix(name, ".bin") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	sizes := make([]int64, len(names))
	for i, name := range names {
		if info, err := dir.Lstat(name); err == nil && info.Mode().IsRegular() {
			sizes[i] = info.Size()
		}
	}
	return names, sizes
}

// Size is how much the reservation holds now.
func (r *Reservation) Size() int64 {
	dir, err := r.open(false)
	if err != nil {
		return 0
	}
	defer dir.Close()
	_, sizes := r.pieces(dir)
	var total int64
	for _, size := range sizes {
		total += size
	}
	return total
}

// AtMost makes the reservation hold at most bytes, giving back the rest at
// once (its last pieces first). It is how room is made before files grow.
func (r *Reservation) AtMost(bytes int64) error {
	dir, unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return r.shrinkTo(dir, max(0, bytes))
}

// shrinkTo gives back all but bytes. The caller holds the locks.
func (r *Reservation) shrinkTo(dir *os.Root, bytes int64) error {
	names, sizes := r.pieces(dir)
	var total int64
	for _, size := range sizes {
		total += size
	}
	for i := len(names) - 1; i >= 0 && total > bytes; i-- {
		keep := sizes[i] - (total - bytes)
		if keep <= 0 {
			if err := dir.Remove(names[i]); err != nil {
				return err
			}
			total -= sizes[i]
			continue
		}
		if err := truncate(dir, names[i], keep); err != nil {
			return err
		}
		total = bytes
	}
	return nil
}

// truncate cuts the file name in dir down to size bytes.
func truncate(dir *os.Root, name string, size int64) error {
	f, err := dir.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(size)
}

// GrowTo makes the reservation hold bytes, growStep at a time. It stops
// early, without an error, when the drive is full: it holds as much as
// fits.
func (r *Reservation) GrowTo(bytes int64) error {
	for {
		done, err := r.GrowStep(bytes)
		if done || err != nil {
			return err
		}
	}
}

// GrowStep grows the reservation by at most growStep toward want (or
// shrinks it to want, if it holds more). It reports whether it is done:
// it holds want, or the drive is full.
func (r *Reservation) GrowStep(want int64) (bool, error) {
	dir, unlock, err := r.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	done, err := r.growStepTo(dir, want)
	if errors.Is(err, syscall.ENOSPC) {
		return true, nil // the drive is full: as much as fits
	}
	return done, err
}

// growStepTo is GrowStep with the locks held.
func (r *Reservation) growStepTo(dir *os.Root, want int64) (bool, error) {
	names, sizes := r.pieces(dir)
	var total int64
	for _, size := range sizes {
		total += size
	}
	if total >= want {
		return true, r.shrinkTo(dir, max(0, want))
	}
	// Into the last piece, or a new one when it is full.
	name := fmt.Sprintf("%s-%03d.bin", r.name, len(names)+1)
	size := int64(0)
	if len(names) > 0 && sizes[len(sizes)-1] < pieceSize {
		name, size = names[len(names)-1], sizes[len(sizes)-1]
	}
	grown := min(size+growStep, pieceSize, size+want-total)
	if err := extend(dir, name, size, grown); err != nil {
		return false, err
	}
	return false, nil
}

// Remove deletes the reservation, giving all of its space back.
func (r *Reservation) Remove() error {
	dir, unlock, err := r.lock()
	if err != nil {
		return err
	}
	err = r.shrinkTo(dir, 0)
	_ = dir.Remove(r.lockName())
	// The folder goes once it holds nothing else.
	left := entries(dir)
	if len(left) == 1 && left[0] == "README.txt" {
		_ = dir.Remove("README.txt")
	}
	unlock()
	if len(left) <= 1 {
		_ = os.Remove(r.dir) // only when it is empty
	}
	return err
}
