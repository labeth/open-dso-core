// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import "math"

// The clamps a controller's trigger and acquisition settings go through,
// whichever scope and controller (ADR-INSTRUMENT-CLAMPS): the SDS1000CML+'s.

// The holdoff's and the average's ranges.
const (
	MaxHoldoffS = 10
	MaxAverages = 256
	MaxEresLen  = 64
)

// OrderPair reads a qualifier's two limits as a range whichever way round
// they came: an inverted pair (W max under W min, slope High under Low, from
// the web or SCPI) would never trigger. A zero limit is "unset"
// (e.g. T max under "<min") and is left where it is.
// TRLC-LINKS: REQ-DSO-011
func OrderPair(lo, hi float64) (float64, float64) {
	if lo > hi && hi > 0 {
		return hi, lo
	}
	return lo, hi
}

// ClampFrac clamps a level fraction to [0, 1].
// TRLC-LINKS: REQ-DSO-011
func ClampFrac(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// ClampEresLen clamps a boxcar length to [1, MaxEresLen] and makes it odd
// (an even length steps down by one).
// TRLC-LINKS: REQ-DSO-012
func ClampEresLen(l int) int {
	if l < 1 {
		l = 1
	}
	if l > MaxEresLen {
		l = MaxEresLen
	}
	if l > 1 && l%2 == 0 {
		l--
	}
	return l
}

// ClampHoldoff clamps a holdoff to [0, MaxHoldoffS] seconds; one that is
// not a number is off.
// TRLC-LINKS: REQ-DSO-011
func ClampHoldoff(sec float64) float64 {
	if !(sec > 0) {
		return 0
	}
	return math.Min(sec, MaxHoldoffS)
}

// ClampAverages clamps an average's frame count to [1, MaxAverages].
// TRLC-LINKS: REQ-DSO-012
func ClampAverages(n int) int { return min(max(n, 1), MaxAverages) }

// ClampCond is a qualifier's condition number in its two bits.
// TRLC-LINKS: REQ-DSO-011
func ClampCond(c int) int { return c & 3 }
