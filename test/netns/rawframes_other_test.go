//go:build !linux

package netns

import "testing"

// sendFromManyAddresses is a stub: these tests only run on Linux (see
// rawframes_linux_test.go), but the package must still build elsewhere.
func sendFromManyAddresses(t *testing.T, _, _ string, _ int) {
	t.Skip("needs Linux")
}
