//go:build !linux

package agent

import "errors"

// RasBeacon runs on Linux; elsewhere, only so the package builds, nothing
// is idmapped and no device can be given.

// idmapCapable reports false: idmapped mounts are Linux's.
func idmapCapable(string) bool { return false }

// grantDevice fails: access lists are set the Linux way.
func grantDevice(string, int) error { return errors.New("devices can only be given on Linux") }
