//go:build !linux

package dhcpprobe

import (
	"errors"
	"time"
)

// probe is only available on Linux. On other systems (for example when
// running the simulated device on a Mac) it reports an error.
func probe(iface string, timeout time.Duration) (Result, error) {
	return Result{}, errors.New("DHCP probing is only supported on Linux")
}
