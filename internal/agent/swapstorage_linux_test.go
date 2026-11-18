//go:build linux

package agent

import (
	"os"
	"syscall"
	"testing"

	"rasbeacon/internal/api"
)

// On the Pi 4/5 image the swap file is on the data partition, shown on its
// own: not in "Settings and logs".
func TestStorage_TheSwapFileIsShownOnItsOwn(t *testing.T) {
	e, _ := newUpdateEnv(t)
	storage := func() api.StorageInfo {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		return e.agent.storageInfo()
	}
	before := storage()
	if before.SwapGB != 0 {
		t.Errorf("without a swap file: %+v", before)
	}
	// 256 MB, set aside like the real one (fallocate).
	f, err := os.Create(e.paths.SwapFile())
	if err != nil {
		t.Fatal(err)
	}
	err = syscall.Fallocate(int(f.Fd()), 0, 0, 256<<20)
	f.Close()
	if err != nil {
		t.Skip("this filesystem can't set space aside:", err)
	}
	after := storage()
	if after.SwapGB*1e9 < 256<<20 {
		t.Errorf("swap = %.3f GB, want the 256 MB file", after.SwapGB)
	}
	// "Settings and logs" didn't grow by it. (Other tests write to the same
	// disk meanwhile, so roughly.)
	if grew := after.UserGB - before.UserGB; grew > 0.128 {
		t.Errorf("settings went from %.3f to %.3f GB: the swap file was counted in them", before.UserGB, after.UserGB)
	}
}
