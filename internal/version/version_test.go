package version

import (
	"slices"
	"testing"
)

func TestNumbers(t *testing.T) {
	for v, want := range map[string][]int{
		"v0.2.0":        {0, 2, 0},
		"0.2.0":         {0, 2, 0},
		"v0.2.0-beta.1": {0, 2, 0},
		"v1.2":          {1, 2},
		"v1.10.3":       {1, 10, 3},
		"dev":           nil,
		"dev-abc1234":   nil,
		"":              nil,
	} {
		if got := Numbers(v); !slices.Equal(got, want) {
			t.Errorf("Numbers(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.2.0", "v0.3.0", -1, true},
		{"v0.3.0", "v0.2.0", 1, true},
		{"v0.10.0", "v0.9.0", 1, true}, // numbers, not text
		{"v1.2", "v1.2.0", 0, true},    // a missing number is 0
		{"v1.2.1", "v1.2", 1, true},
		// A beta comes before its release, and labels in order.
		{"v0.2.0-beta.1", "v0.2.0", -1, true},
		{"v0.2.0", "v0.2.0-beta.1", 1, true},
		{"v0.2.0-beta.2", "v0.2.0-beta.10", -1, true}, // numbers, not text
		{"v0.2.0-alpha", "v0.2.0-beta", -1, true},
		{"v0.2.0-beta", "v0.2.0-beta.1", -1, true}, // fewer parts first
		{"v0.2.0-1", "v0.2.0-beta", -1, true},      // a number before a word
		{"v0.2.0-beta.1", "v0.2.0-beta.1", 0, true},
		{"v0.2.0+build.5", "v0.2.0", 0, true}, // build data doesn't count
		{"v0.3.0-beta.1", "v0.2.9", 1, true},  // the numbers first
		{"dev", "v0.2.0", 0, false},           // a development build has nothing to compare
		{"v0.2.0", "dev-abc", 0, false},
	} {
		got, ok := Compare(tc.a, tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", tc.a, tc.b, got, ok, tc.want, tc.ok)
		}
	}
}

// Minimums compare the numbers only: a beta is on its way to its release.
func TestCompareNumbers(t *testing.T) {
	if got, ok := CompareNumbers("v0.2.0-beta.1", "v0.2.0"); got != 0 || !ok {
		t.Errorf("a beta and its release: %d, %v", got, ok)
	}
	if got, ok := CompareNumbers("v0.1.9", "v0.2.0-beta.1"); got != -1 || !ok {
		t.Errorf("an older version: %d, %v", got, ok)
	}
	if _, ok := CompareNumbers("dev-abc", "v0.2.0"); ok {
		t.Error("a development build has nothing to compare")
	}
}
