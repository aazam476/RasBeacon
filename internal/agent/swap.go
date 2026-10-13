package agent

// The swap file: room on the SD card for memory the Pi isn't using, so a
// program that briefly needs more memory than the Pi has doesn't get
// stopped. Its size is set on the System page, 4 GB by default, from 0
// (off) up to the free space on the card minus 1 GB, which stays free for
// settings and updates. Swapping wears the card, so the kernel is told to
// swap only when memory is really short (vm.swappiness in 90-rasbeacon.conf).

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"syscall"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

const gigabyte = int64(1) << 30

// swapReserve is kept free on the data partition whatever the swap size.
const swapReserve = gigabyte

// diskFreeBytes returns the free space on the file system holding path.
func diskFreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// swapFileSize returns the swap file's size (0 if there is none).
func (a *Agent) swapFileSize() int64 {
	info, err := os.Stat(a.paths.SwapFile())
	if err != nil {
		return 0
	}
	return info.Size()
}

// swapMaxGB is the largest swap file that fits.
func (a *Agent) swapMaxGB() int {
	free, err := a.diskFree(a.paths.DataDir())
	if err != nil {
		return 0
	}
	room := free + a.swapFileSize() - swapReserve
	return int(max(0, room/gigabyte))
}

// swapWantedGB is the size to use: the owner's choice or the default,
// never more than fits.
func (a *Agent) swapWantedGB() int {
	wanted := config.DefaultSwapGB
	if a.cfg.Swap.SizeGB != nil {
		wanted = *a.cfg.Swap.SizeGB
	}
	return min(wanted, a.swapMaxGB())
}

// swapActive reports whether the swap file is in use (/proc/swaps).
func (a *Agent) swapActive() bool {
	data, _ := os.ReadFile(a.paths.ProcSwaps())
	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == a.paths.SwapFile() {
			return true
		}
	}
	return false
}

// applySwap makes the swap file the wanted size and turns it on (or off
// and removes it, for 0). The caller holds a.mu.
func (a *Agent) applySwap() error {
	file := a.paths.SwapFile()
	want := int64(a.swapWantedGB()) * gigabyte
	active := a.swapActive()
	size := a.swapFileSize()
	if want > 0 && active && size == want {
		return nil // already as wanted
	}
	if active {
		if _, err := a.runner.Run("swapoff", file); err != nil {
			return fmt.Errorf("turning off swap: %w", err)
		}
	}
	if want == 0 {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		slog.Info("swap off")
		return nil
	}
	if size != want {
		// A new file: fallocate reserves the space at once (no holes, as
		// swap needs), and only root may read it (memory ends up there).
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		f.Close()
		if _, err := a.runner.Run("fallocate", "-l", strconv.FormatInt(want, 10), file); err != nil {
			_ = os.Remove(file)
			return fmt.Errorf("making the swap file: %w", err)
		}
		if _, err := a.runner.Run("mkswap", file); err != nil {
			return fmt.Errorf("making the swap file: %w", err)
		}
	}
	if _, err := a.runner.Run("swapon", file); err != nil {
		// An old file from before a crash may need formatting again.
		if _, err := a.runner.Run("mkswap", file); err != nil {
			return fmt.Errorf("making the swap file: %w", err)
		}
		if _, err := a.runner.Run("swapon", file); err != nil {
			return fmt.Errorf("turning on swap: %w", err)
		}
	}
	slog.Info("swap on", "sizeGB", want/gigabyte)
	return nil
}

// SwapSettings returns the swap size and the largest that fits.
func (a *Agent) SwapSettings() api.SwapSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return api.SwapSettings{SizeGB: a.swapWantedGB(), MaxGB: a.swapMaxGB()}
}

// UpdateSwapSettings saves a new swap size and applies it.
func (a *Agent) UpdateSwapSettings(req api.SwapSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if maxGB := a.swapMaxGB(); req.SizeGB < 0 || req.SizeGB > maxGB {
		return &config.ValidationError{Field: "sizeGb", Message: fmt.Sprintf("Must be from 0 (off) to %d GB: 1 GB of the SD card stays free.", maxGB)}
	}
	newCfg := a.cfg.Clone()
	size := req.SizeGB
	newCfg.Swap.SizeGB = &size
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	return a.applySwap()
}
