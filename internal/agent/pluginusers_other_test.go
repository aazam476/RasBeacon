//go:build !linux

package agent

import "testing"

// checkDeviceGiven checks nothing: access lists are set the Linux way, and
// RasBeacon runs on Linux (see pluginusers_linux_test.go).
func checkDeviceGiven(*testing.T, string, int) {}
