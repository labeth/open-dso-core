// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"testing"
)

// A view is read only where the screen's envelope is coarser than the display
// draws it: a bucket every second column or fewer.
// TRLC-LINKS: REQ-DSO-021
func TestViewBuckets(t *testing.T) {
	for _, c := range []struct {
		name         string
		a, b         float64
		cols         int
		screen, want uint32
	}{
		{"LCD zoom 2: 1024 buckets over 770 columns", -0.25, 0.25, 770, 2048, 0},
		{"LCD zoom 5: 410 buckets, about one every second column", -0.1, 0.1, 770, 2048, 0},
		{"LCD zoom 10", -0.05, 0.05, 770, 2048, 770},
		{"LCD zoom 50, odd columns", -0.01, 0.01, 771, 2048, 772},
		{"canvas zoom 2", -0.25, 0.25, 1900, 2048, 0},
		{"canvas zoom 5: capped", -0.1, 0.1, 1900, 2048, viewMaxBuckets},
		{"Q8 screen, canvas zoom 2", -0.25, 0.25, 1900, 1024, viewMaxBuckets},
		{"empty", 0.1, 0.1, 770, 2048, 0},
		{"no columns", -0.05, 0.05, 1, 2048, 0},
	} {
		if got := ViewBuckets(c.a, c.b, c.cols, c.screen); got != c.want {
			t.Errorf("%s: %d buckets, want %d", c.name, got, c.want)
		}
	}
}
