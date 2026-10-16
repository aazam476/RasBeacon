// Package version holds the RasBeacon version string.
//
// The release build sets it with a linker flag:
//
//	go build -ldflags "-X rasbeacon/internal/version.Version=v0.1.0" ...
//
// Local builds show "dev".
package version

import (
	"strconv"
	"strings"
)

// Version is the running RasBeacon version.
var Version = "dev"

// Repository is the GitHub repository ("owner/name") whose releases the
// optional update check looks at. Release builds set it; when it is empty
// the check is not offered.
var Repository = ""

// Numbers reads the leading numbers of a version: "v0.2.0-beta.1" gives
// 0, 2, 0. A version that doesn't start with a number ("dev",
// "dev-abc1234") gives none.
func Numbers(v string) []int {
	var out []int
	for _, part := range strings.Split(strings.TrimPrefix(v, "v"), ".") {
		digits := part
		if i := strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
			digits = part[:i] // "0-beta" gives "0"
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			break
		}
		out = append(out, n)
		if len(digits) < len(part) {
			break // the rest is a label like "-beta"
		}
	}
	return out
}

// Compare compares the numbers of two versions: -1 when a is older than b,
// 0 when they're the same, 1 when a is newer. Only the numbers count, so a
// beta of 0.2.0 is the same as 0.2.0, and a missing number counts as 0
// ("v1.2" is "v1.2.0"). ok is false when either one has no numbers (a
// development build), and then there is nothing to compare.
func Compare(a, b string) (result int, ok bool) {
	x, y := Numbers(a), Numbers(b)
	if len(x) == 0 || len(y) == 0 {
		return 0, false
	}
	for i := 0; i < max(len(x), len(y)); i++ {
		var m, n int
		if i < len(x) {
			m = x[i]
		}
		if i < len(y) {
			n = y[i]
		}
		if m != n {
			if m < n {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}
