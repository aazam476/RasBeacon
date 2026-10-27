package browser

// This file says how the login browser's process runs on Linux (see
// procattr_other.go for other systems).

import "syscall"

// processAttributes says how the browser process runs: as cred's user, in
// its own process group (its helper processes are stopped together), and
// killed by the kernel if rbd itself dies (Pdeathsig, Linux only), so no
// browser is left running.
func processAttributes(cred *syscall.Credential) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Credential: cred,
		Setpgid:    true,
		Pdeathsig:  syscall.SIGKILL,
	}
}
