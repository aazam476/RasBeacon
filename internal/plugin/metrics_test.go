package plugin

import (
	"math"
	"testing"
)

func TestGroupThousands(t *testing.T) {
	for n, want := range map[float64]string{
		0:             "0",
		999:           "999",
		12345:         "12,345",
		-1234567.6:    "-1,234,568",
		1e19:          "10,000,000,000,000,000,000", // past int64's largest
		math.Inf(1):   "+Inf",
		math.MaxInt64: "9,223,372,036,854,775,808",
	} {
		if got := groupThousands(n); got != want {
			t.Errorf("groupThousands(%v) = %q, want %q", n, got, want)
		}
	}
	if got := groupThousands(math.NaN()); got != "NaN" {
		t.Errorf("groupThousands(NaN) = %q", got)
	}
}
