// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"math"
	"testing"
)

// The take times stay inside the ring: pre at most 10 s, post at least a
// tenth, the two at most 16 s.
// TRLC-LINKS: REQ-SDS-275
func TestSoundTakeTimes(t *testing.T) {
	for _, c := range []struct{ pre, post, wp, wq float64 }{
		{1, 4, 1, 4}, {-1, 0, 1, 4}, {math.NaN(), math.Inf(1), 1, 4}, {12, 3, 10, 3}, {8, 15, 1, 15}, {0, 0.01, 0, 4},
	} {
		if p, q := SoundTakeTimes(c.pre, c.post); p != c.wp || q != c.wq {
			t.Errorf("%v %v: %v %v, want %v %v", c.pre, c.post, p, q, c.wp, c.wq)
		}
	}
}

// The comparator's first crossing: armed at or below the level less the
// hysteresis, firing at or above it; falling mirrored; a crossing before
// start disarms as in the fabric.
// TRLC-LINKS: REQ-SDS-275
func TestFirstCrossing(t *testing.T) {
	codes := []int{130, 130, 120, 125, 129, 131, 140, 120, 135, 150, 110}
	q := make([]uint16, len(codes))
	for i, c := range codes {
		q[i] = uint16(c << 8)
	}
	if i, ok := FirstCrossingFrom(q, 130, 4, true, 0); !ok || i != 5 {
		t.Errorf("rising: %d %v", i, ok)
	}
	if i, ok := FirstCrossingFrom(q, 130, 4, true, 6); !ok || i != 8 {
		t.Errorf("rising from 6: %d %v", i, ok)
	}
	if i, ok := FirstCrossingFrom(q, 130, 4, false, 0); !ok || i != 7 {
		t.Errorf("falling: %d %v", i, ok)
	}
	if _, ok := FirstCrossingFrom(q[:2], 130, 4, true, 0); ok {
		t.Error("a crossing without arming")
	}
}
