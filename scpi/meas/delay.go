package meas

import "math"

// Edges are a channel's interpolated mid-swing crossings, in samples.
type Edges struct{ Rise, Fall []float64 }

// FindEdges slices codes at mid-swing with 10 % hysteresis, so converter
// noise on a flat level is no edge; nil when the codes have no swing (fewer
// than 8 codes of the 8-bit view).
func FindEdges(c []uint8) *Edges {
	if len(c) < 4 {
		return nil
	}
	lo, hi := 255, 0
	for _, v := range c {
		lo, hi = min(lo, int(v)), max(hi, int(v))
	}
	if hi-lo < 8 {
		return nil
	}
	mid := float64(lo+hi) / 2
	hy := float64(hi-lo) / 10
	// The bands as whole codes: a code is past mid+hy exactly when it is at
	// least its ceiling, so the loop compares bytes, not floats.
	up, down := int(math.Ceil(mid+hy)), int(math.Floor(mid-hy))
	e := &Edges{}
	state := 0 // unknown until the first excursion past a hysteresis band
	for i := 1; i < len(c); i++ {
		v := int(c[i])
		switch {
		case v >= up && state <= 0:
			if state < 0 {
				e.Rise = append(e.Rise, crossing(c, i, mid))
			}
			state = 1
		case v <= down && state >= 0:
			if state > 0 {
				e.Fall = append(e.Fall, crossing(c, i, mid))
			}
			state = -1
		}
	}
	return e
}

// crossing walks back from i to the sample pair straddling level and
// interpolates the crossing position.
func crossing(c []uint8, i int, level float64) float64 {
	for j := i; j > 0; j-- {
		a, b := float64(c[j-1]), float64(c[j])
		if (a-level)*(b-level) <= 0 && a != b {
			return float64(j-1) + (level-a)/(b-a)
		}
	}
	return float64(i)
}

// delayParams are the edge-to-edge delays: source A's edge to source B's,
// first or last edge on the record.
var delayParams = map[string]struct{ aRise, bRise, last bool }{
	"FRR": {true, true, false}, "FRF": {true, false, false}, "FFR": {false, true, false}, "FFF": {false, false, false},
	"LRR": {true, true, true}, "LRF": {true, false, true}, "LFR": {false, true, true}, "LFF": {false, false, true},
}

// IsDelay reports whether name is a delay parameter: the eight edge delays,
// PHA and SKEW.
func IsDelay(name string) bool {
	_, ok := delayParams[name]
	return ok || name == "PHA" || name == "SKEW"
}

// Delay measures parameter name between two channels' 8-bit views over a
// record of n samples, sampleS apart, triggered at trig (negative: none).
// PHA is in degrees over A's period; SKEW is between the rising edges
// nearest the trigger (the centre without one); the others are seconds.
// ok is false when the record cannot support it.
func Delay(name string, a, b []uint8, n int, sampleS, trig float64) (value float64, ok bool) {
	if n < 4 || len(a) < n || len(b) < n || !(sampleS > 0) {
		return 0, false
	}
	ea, eb := FindEdges(a[:n]), FindEdges(b[:n])
	if ea == nil || eb == nil {
		return 0, false
	}
	switch name {
	case "PHA":
		if len(ea.Rise) < 2 || len(eb.Rise) == 0 {
			return 0, false
		}
		period := (ea.Rise[len(ea.Rise)-1] - ea.Rise[0]) / float64(len(ea.Rise)-1)
		ta := ea.Rise[0]
		for _, tb := range eb.Rise {
			if tb >= ta-period/2 {
				ph := math.Mod((tb-ta)/period*360, 360)
				if ph > 180 {
					ph -= 360
				} else if ph <= -180 {
					ph += 360
				}
				return ph, true
			}
		}
		return 0, false
	case "SKEW":
		centre := trig
		if centre < 0 {
			centre = float64(n) / 2
		}
		nearest := func(xs []float64) (float64, bool) {
			best, ok := 0.0, false
			for _, x := range xs {
				if !ok || math.Abs(x-centre) < math.Abs(best-centre) {
					best, ok = x, true
				}
			}
			return best, ok
		}
		ta, okA := nearest(ea.Rise)
		tb, okB := nearest(eb.Rise)
		return (tb - ta) * sampleS, okA && okB
	}
	p, known := delayParams[name]
	if !known {
		return 0, false
	}
	pick := func(xs []float64) (float64, bool) {
		if len(xs) == 0 {
			return 0, false
		}
		if p.last {
			return xs[len(xs)-1], true
		}
		return xs[0], true
	}
	xa, xb := ea.Fall, eb.Fall
	if p.aRise {
		xa = ea.Rise
	}
	if p.bRise {
		xb = eb.Rise
	}
	ta, okA := pick(xa)
	tb, okB := pick(xb)
	return (tb - ta) * sampleS, okA && okB
}
