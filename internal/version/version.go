// Package version holds the RasBeacon version string.
//
// The release build sets it with a linker flag:
//
//	go build -ldflags "-X rasbeacon/internal/version.Version=v0.1.0" ...
//
// Local builds show "dev".
package version

import (
	"cmp"
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

// Compare orders two versions as Semantic Versioning does: -1 when a is
// older than b, 0 when they're the same, 1 when a is newer. Their numbers
// count first (a missing one is 0: "v1.2" is "v1.2.0"); with the same
// numbers, a version with a label comes before the one without
// ("v0.2.0-beta.1" before "v0.2.0"), and two labels compare part by part:
// numbers as numbers ("beta.2" before "beta.10"), words as text, a number
// before a word, and fewer parts first. Build data after "+" doesn't
// count. ok is false when either one has no numbers (a development build),
// and then there is nothing to compare.
func Compare(a, b string) (result int, ok bool) {
	result, ok = CompareNumbers(a, b)
	if !ok || result != 0 {
		return result, ok
	}
	return compareLabels(label(a), label(b)), true
}

// CompareNumbers compares only the numbers of two versions, as Compare does
// first: a beta of 0.2.0 is the same as 0.2.0. It is for minimums (the
// oldest version an update installs over, the RasBeacon a plugin needs, the
// newest a backup may come from): a beta is on its way to its release.
func CompareNumbers(a, b string) (result int, ok bool) {
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

// label is a version's label: "beta.1" for "v0.2.0-beta.1+build.5", "" for
// none.
func label(v string) string {
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "+")
	_, l, _ := strings.Cut(v, "-")
	return l
}

// compareLabels compares two labels (see Compare); "" is no label, which
// comes after any.
func compareLabels(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(x), len(y)) {
		if c := compareLabelPart(x[i], y[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(x), len(y))
}

// compareLabelPart compares one part of two labels: numbers as numbers,
// words as text, a number before a word.
func compareLabelPart(a, b string) int {
	m, errA := strconv.ParseUint(a, 10, 64)
	n, errB := strconv.ParseUint(b, 10, 64)
	switch {
	case errA == nil && errB == nil:
		return cmp.Compare(m, n)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}
