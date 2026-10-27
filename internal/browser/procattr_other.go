//go:build !linux

package browser

// This file is processAttributes for systems other than Linux (see
// procattr_linux.go).

import "syscall"

// processAttributes is the Linux version (procattr_linux.go) without
// Pdeathsig, which other systems don't have. It lets rbd -dev build and run
// on a Mac; there, a browser can outlive a crashed rbd.
func processAttributes(cred *syscall.Credential) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Credential: cred, Setpgid: true}
}
