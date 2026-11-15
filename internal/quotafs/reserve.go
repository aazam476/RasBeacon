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

// lock takes the reservation's locks (this process's, and the file's for
// other processes), making the folder if it isn't there. The returned
// function lets go of them.
func (r *Reservation) lock() (func(), error) {
	r.mu.Lock()
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		r.mu.Unlock()
		return nil, err
	}
	readme := filepath.Join(r.dir, "README.txt")
	if _, err := os.Stat(readme); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(readme, []byte(reservedReadme), 0o644)
	}
	f, err := os.OpenFile(filepath.Join(r.dir, "."+r.name+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		r.mu.Unlock()
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		r.mu.Unlock()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
		r.mu.Unlock()
	}, nil
}

// pieces are the reservation's files, in order, with their sizes.
func (r *Reservation) pieces() ([]string, []int64) {
	entries, _ := os.ReadDir(r.dir)
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), r.name+"-") && strings.HasSuffix(entry.Name(), ".bin") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	sizes := make([]int64, len(names))
	for i, name := range names {
		if info, err := os.Stat(filepath.Join(r.dir, name)); err == nil {
			sizes[i] = info.Size()
		}
	}
	return names, sizes
}

// Size is how much the reservation holds now.
func (r *Reservation) Size() int64 {
	_, sizes := r.pieces()
	var total int64
	for _, size := range sizes {
		total += size
	}
	return total
}

// AtMost makes the reservation hold at most bytes, giving back the rest at
// once (its last pieces first). It is how room is made before files grow.
func (r *Reservation) AtMost(bytes int64) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return r.shrinkTo(max(0, bytes))
}

// shrinkTo gives back all but bytes. The caller holds the locks.
func (r *Reservation) shrinkTo(bytes int64) error {
	names, sizes := r.pieces()
	var total int64
	for _, size := range sizes {
		total += size
	}
	for i := len(names) - 1; i >= 0 && total > bytes; i-- {
		path := filepath.Join(r.dir, names[i])
		keep := sizes[i] - (total - bytes)
		if keep <= 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
			total -= sizes[i]
			continue
		}
		if err := os.Truncate(path, keep); err != nil {
			return err
		}
		total = bytes
	}
	return nil
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
	unlock, err := r.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	done, err := r.growStepTo(want)
	if errors.Is(err, syscall.ENOSPC) {
		return true, nil // the drive is full: as much as fits
	}
	return done, err
}

// growStepTo is GrowStep with the locks held.
func (r *Reservation) growStepTo(want int64) (bool, error) {
	names, sizes := r.pieces()
	var total int64
	for _, size := range sizes {
		total += size
	}
	if total >= want {
		return true, r.shrinkTo(max(0, want))
	}
	// Into the last piece, or a new one when it is full.
	name := fmt.Sprintf("%s-%03d.bin", r.name, len(names)+1)
	size := int64(0)
	if len(names) > 0 && sizes[len(sizes)-1] < pieceSize {
		name, size = names[len(names)-1], sizes[len(sizes)-1]
	}
	grown := min(size+growStep, pieceSize, size+want-total)
	if err := extend(filepath.Join(r.dir, name), size, grown); err != nil {
		return false, err
	}
	return false, nil
}

// Remove deletes the reservation, giving all of its space back.
func (r *Reservation) Remove() error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	err = r.shrinkTo(0)
	_ = os.Remove(filepath.Join(r.dir, "."+r.name+".lock"))
	unlock()
	// The folder goes once it holds nothing else.
	if entries, _ := os.ReadDir(r.dir); len(entries) == 1 && entries[0].Name() == "README.txt" {
		_ = os.RemoveAll(r.dir)
	}
	return err
}
