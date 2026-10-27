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
		{"v0.2.0-beta.1", "v0.2.0", 0, true},
		{"v1.2.1", "v1.2", 1, true},
		{"dev", "v0.2.0", 0, false}, // a development build has nothing to compare
		{"v0.2.0", "dev-abc", 0, false},
	} {
		got, ok := Compare(tc.a, tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", tc.a, tc.b, got, ok, tc.want, tc.ok)
		}
	}
}
