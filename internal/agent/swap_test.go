package agent

import (
	"errors"
	"net/http"
	"os"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

func TestSwap_DefaultSizeChangeAndOff(t *testing.T) {
	e := newClaimedEnv(t, false)
	file := e.paths.SwapFile()

	// Started with the default 2 GB.
	if !e.runner.ran("fallocate -l 2147483648 "+file) || !e.runner.ran("mkswap "+file) || !e.runner.ran("swapon "+file) {
		t.Fatalf("commands: %v", e.runner.commands)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the swap file must be readable by root only: %v %v", info, err)
	}
	// 10 GB free, plus the swap file itself (empty in this test), minus 1 GB.
	if s := e.agent.SwapSettings(); s.SizeGB != 2 || s.MaxGB != 9 {
		t.Errorf("settings = %+v", s)
	}

	// Bigger: swap goes off, the file is made again, swap comes on.
	mustWrite(t, e.paths.ProcSwaps(), "Filename Type Size Used Priority\n"+file+" file 2097148 0 -2\n")
	e.runner.commands = nil
	if err := e.agent.UpdateSwapSettings(api.SwapSettings{SizeGB: 8}); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("swapoff "+file) || !e.runner.ran("fallocate -l 8589934592 "+file) || !e.runner.ran("swapon "+file) {
		t.Errorf("commands: %v", e.runner.commands)
	}

	// Too big, or negative.
	var ve *config.ValidationError
	for _, size := range []int{10, -1} {
		if err := e.agent.UpdateSwapSettings(api.SwapSettings{SizeGB: size}); !errors.As(err, &ve) || ve.Field != "sizeGb" {
			t.Errorf("%d GB: got %v", size, err)
		}
	}

	// Off: the file goes away.
	e.runner.commands = nil
	if err := e.agent.UpdateSwapSettings(api.SwapSettings{SizeGB: 0}); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("swapoff "+file) || e.runner.ran("swapon") {
		t.Errorf("commands: %v", e.runner.commands)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Error("the swap file should be removed")
	}
	if s := e.agent.SwapSettings(); s.SizeGB != 0 {
		t.Errorf("settings = %+v", s)
	}
}

func TestSwap_SmallCardGetsLess(t *testing.T) {
	e := newTestEnv(t)
	// 2.5 GB free: 1 GB stays free, so 1 GB of swap fits.
	e.agent.diskFree = func(string) (int64, error) { return 5 << 29, nil }
	e.start()
	if s := e.agent.SwapSettings(); s.SizeGB != 1 || s.MaxGB != 1 {
		t.Errorf("settings = %+v", s)
	}
	if !e.runner.ran("fallocate -l 1073741824 " + e.paths.SwapFile()) {
		t.Errorf("commands: %v", e.runner.commands)
	}
}

// Memory written to swap can hold Wi-Fi and VPN keys, so a factory reset
// turns swap off and deletes the file; the next start makes a new one.
func TestFactoryReset_DeletesTheSwapFile(t *testing.T) {
	e := newClaimedEnv(t, false)
	file := e.paths.SwapFile()
	mustWrite(t, file, "old memory")
	mustWrite(t, e.paths.ProcSwaps(), "Filename Type Size Used Priority\n"+file+" file 2097148 0 -2\n")
	e.runner.commands = nil

	if status, body := e.call(http.MethodPost, "/system/factory-reset", api.PasswordRequest{Password: validClaim.AdminPassword}); status != http.StatusNoContent {
		t.Fatalf("factory reset = %d %s", status, body)
	}
	if !e.runner.ran("swapoff " + file) {
		t.Errorf("swap should be turned off first: %v", e.runner.commands)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the swap file should be gone: %v", err)
	}
}
