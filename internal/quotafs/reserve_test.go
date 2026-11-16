//go:build linux

package quotafs

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// smallSizes makes reservations small enough for a test: pieces of 10
// KiB, grown 4 KiB at a time, shrunk with 1 KiB to spare, regrown at once.
func smallSizes(t *testing.T) {
	t.Helper()
	oldPiece, oldStep, oldSlack, oldWait := pieceSize, growStep, slack, regrowAfter
	pieceSize, growStep, slack, regrowAfter = 10<<10, 4<<10, 1<<10, 0
	t.Cleanup(func() { pieceSize, growStep, slack, regrowAfter = oldPiece, oldStep, oldSlack, oldWait })
}

// taken is the space the files in dir really take (their blocks).
func taken(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		var st syscall.Stat_t
		if err := syscall.Stat(filepath.Join(dir, entry.Name()), &st); err == nil && entry.Name() != "README.txt" {
			total += st.Blocks * 512
		}
	}
	return total
}

func TestReservation_GrowsInPiecesAndShrinksFromTheEnd(t *testing.T) {
	smallSizes(t)
	dir := filepath.Join(t.TempDir(), ReservedFolder)
	r := NewReservation(dir, "samba-s1")
	if err := r.GrowTo(25 << 10); err != nil {
		t.Fatal(err)
	}
	if r.Size() != 25<<10 {
		t.Errorf("size = %d, want 25 KiB", r.Size())
	}
	// 10 + 10 + 5 KiB, really taken on the drive.
	for name, want := range map[string]int64{"samba-s1-001.bin": 10 << 10, "samba-s1-002.bin": 10 << 10, "samba-s1-003.bin": 5 << 10} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() != want {
			t.Errorf("%s: %v, %v", name, info, err)
		}
	}
	if got := taken(t, dir); got < 25<<10 {
		t.Errorf("the drive should have the space taken: %d bytes", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.txt")); err != nil {
		t.Error("the folder should say what it is")
	}

	if err := r.AtMost(12 << 10); err != nil {
		t.Fatal(err)
	}
	if r.Size() != 12<<10 {
		t.Errorf("after shrinking: %d", r.Size())
	}
	if _, err := os.Stat(filepath.Join(dir, "samba-s1-003.bin")); !os.IsNotExist(err) {
		t.Error("the last piece goes first")
	}
	// Another share's reservation is its own.
	other := NewReservation(dir, "samba-s2")
	if err := other.GrowTo(3 << 10); err != nil {
		t.Fatal(err)
	}
	if r.Size() != 12<<10 || other.Size() != 3<<10 {
		t.Errorf("sizes %d and %d", r.Size(), other.Size())
	}
	// GrowStep to less than it holds gives the rest back.
	if done, err := r.GrowStep(2 << 10); !done || err != nil || r.Size() != 2<<10 {
		t.Errorf("growing to less: %v %v, size %d", done, err, r.Size())
	}

	if err := r.Remove(); err != nil {
		t.Fatal(err)
	}
	if r.Size() != 0 {
		t.Error("removed, it holds nothing")
	}
	if err := other.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("the folder goes with the last reservation")
	}
}

func TestQuota_HoldsTheRestOfTheQuotaOnTheDrive(t *testing.T) {
	smallSizes(t)
	r := NewReservation(filepath.Join(t.TempDir(), ReservedFolder), "samba-s1")
	// Files added on a computer: the reservation is bigger than the room
	// left, and shrinks to fit at once.
	if err := r.GrowTo(30 << 10); err != nil {
		t.Fatal(err)
	}
	q := NewQuota(20<<10, 40<<10)
	if err := q.HoldWith(r); err != nil {
		t.Fatal(err)
	}
	// Before smallSizes puts the sizes back, which the growing back reads.
	t.Cleanup(q.Close)
	if r.Size() > 20<<10 {
		t.Errorf("held %d, at most the 20 KiB left", r.Size())
	}
	waitFor(t, func() bool { return r.Size() == 20<<10 }, "grows back to the 20 KiB left")

	// Growing files take their room from it first.
	if !q.Reserve(5 << 10) {
		t.Fatal("5 KiB fits")
	}
	if r.Size() > 15<<10 {
		t.Errorf("held %d, at most the 15 KiB left", r.Size())
	}
	// What doesn't fit is refused, and the reservation stays.
	before := r.Size()
	if q.Reserve(16 << 10) {
		t.Error("16 KiB doesn't fit")
	}
	if r.Size() != before {
		t.Errorf("a refused write changed the reservation: %d, was %d", r.Size(), before)
	}
	// Deleting gives the room back to it.
	q.Add(-10 << 10)
	waitFor(t, func() bool { return r.Size() == 25<<10 }, "grows back to the 25 KiB left")
}

func TestMount_AReservedShareMakesRoomAsItFills(t *testing.T) {
	smallSizes(t)
	source, view, q := mounted(t) // 200 KiB used of 1 MiB
	r := NewReservation(filepath.Join(t.TempDir(), ReservedFolder), "samba-s1")
	if err := q.HoldWith(r); err != nil {
		t.Fatal(err)
	}
	// Before smallSizes puts the sizes back, which the growing back reads.
	t.Cleanup(q.Close)
	waitFor(t, func() bool { return r.Size() == 824<<10 }, "holds the 824 KiB left")
	if err := os.WriteFile(filepath.Join(view, "new.bin"), make([]byte, 300<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	if r.Size() > 524<<10 {
		t.Errorf("held %d after 300 KiB more, at most 524 KiB", r.Size())
	}
	if err := os.Remove(filepath.Join(view, "old.bin")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return r.Size() == 724<<10 }, "holds the 724 KiB left after the delete")
	_ = source
}

// waitFor waits up to 5 seconds for ok, failing the test with what it
// waited for.
func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Errorf("it should have: %s", what)
}
