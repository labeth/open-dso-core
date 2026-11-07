// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"sync/atomic"
)

// TrigType selects the discrimination pipeline.
// TRLC-LINKS: REQ-DSO-011
type TrigType int

// The trigger types (SetTrigType).
const (
	TrigEdge  TrigType = 0 // the hardware edge alone
	TrigPulse TrigType = 1 // GLIT
	TrigSlope TrigType = 2 // SLEW
	TrigVideo TrigType = 3 // TV
)

// Condition semantics for width/time windows: any=0, less=1, greater=2,
// inside=3. Literal: with min=max=0, greater passes everything > 0 and less
// passes nothing.
const (
	CondAny     = 0
	CondLess    = 1
	CondGreater = 2
	CondInside  = 3
)

// TrigParams is the staged qualifier parameter set (command mutex).
// TRLC-LINKS: REQ-DSO-011
type TrigParams struct {
	Typ TrigType

	PulseLvlFrac float64
	PulseWMinNs  float64
	PulseWMaxNs  float64
	PulseCond    int

	SlopeLoFrac float64
	SlopeHiFrac float64
	SlopeTMinNs float64
	SlopeTMaxNs float64
	SlopeCond   int

	VideoStd  int // 0=PAL (≤625 lines), 1=NTSC (≤525)
	VideoLine int // 0 = any line, else 1-based line N
	VideoNeg  bool
}

// TRLC-LINKS: REQ-DSO-011
func DefaultTrigParams() TrigParams {
	return TrigParams{
		Typ:          TrigEdge,
		PulseLvlFrac: 0.5, PulseCond: CondAny,
		SlopeLoFrac: 0.2, SlopeHiFrac: 0.8, SlopeCond: CondAny,
		VideoStd: 0, VideoLine: 0, VideoNeg: true,
	}
}

// TRLC-LINKS: REQ-DSO-011
func condOK(m, lo, hi float64, cond int) bool {
	switch cond {
	case CondLess:
		return m < lo
	case CondGreater:
		return m > hi
	case CondInside:
		return lo <= m && m <= hi
	default:
		return true
	}
}

// AnchorSpan says where a qualifier may anchor: the candidate nearest
// target wins, and only candidates in [lo, hi] count - the indices around
// which the recalled window still fills a whole screen. Anchoring outside it
// shifts the trace by up to a screen and pads the rest flat (a pulse trigger
// at trigger position 0.2/0.8).
type AnchorSpan struct{ Target, Lo, Hi int }

// centreAnchor is the band pipeline's rule: nearest the frame centre, anywhere.
// TRLC-LINKS: REQ-DSO-011
func centreAnchor(n int) AnchorSpan { return AnchorSpan{Target: n / 2, Lo: 0, Hi: n} }

// TRLC-LINKS: REQ-DSO-011
func (a AnchorSpan) holds(i int) bool { return i >= a.Lo && i <= a.Hi }

// closer reports whether i is an allowed anchor nearer the target than best,
// and if so records its distance in best.
// TRLC-LINKS: REQ-DSO-011
func (a AnchorSpan) closer(i int, best *int) bool {
	if !a.holds(i) {
		return false
	}
	d := i - a.Target
	if d < 0 {
		d = -d
	}
	if d >= *best {
		return false
	}
	*best = d
	return true
}

// flatReject is the qualifier preamble's no-event gate: a span under 40
// codes is a rail — never fabricate an event from noise crossings.
const FlatRejectSpan = 40

// CrossFrac interpolates the sub-sample position of a crossing at index c.
// TRLC-LINKS: REQ-DSO-011
func CrossFrac(disc []uint8, c, lvl int) float64 {
	a, b := int(disc[c-1]), int(disc[c])
	frac := 0.0
	if b != a {
		frac = float64(lvl-a) / float64(b-a)
		if frac < 0 || frac >= 1 {
			frac = 0
		}
	}
	return float64(c-1) + frac
}

// QualifyPulse (spec 05: GLIT). rising=true = high pulse (region above the
// level between a rising entry and the next falling exit); false mirrored.
// The anchor is the COMPLETING edge of the qualifying pulse nearest the
// frame centre — only when the pulse completes is its width known.
// TRLC-LINKS: REQ-DSO-011
func QualifyPulse(disc []uint8, intervalNs float64, p TrigParams, rising bool) float64 {
	return QualifyPulseAt(disc, intervalNs, p, rising, centreAnchor(len(disc)))
}

// TRLC-LINKS: REQ-DSO-011
func QualifyPulseAt(disc []uint8, intervalNs float64, p TrigParams, rising bool, at AnchorSpan) float64 {
	n := len(disc)
	mn, _, span := Ptp(disc)
	if span < FlatRejectSpan {
		return -1
	}
	lvl := mn + int(p.PulseLvlFrac*float64(span))

	bestX, bestDist := -1.0, n+1
	enter := -1
	// A low pulse is a high one of the inverted codes: one loop, no
	// direction test a sample (a > lvl is 255-a < 255-lvl).
	flip, l := 0, lvl
	if !rising {
		flip, l = 255, 255-lvl
	}
	prev := int(disc[0]) ^ flip
	for c := 1; c < n; c++ {
		a, b := prev, int(disc[c])^flip
		prev = b
		entering := a < l && b >= l // into the pulse
		exiting := a > l && b <= l  // out of it
		if entering {
			enter = c
			continue
		}
		if exiting && enter >= 0 {
			widthNs := float64(c-enter) * intervalNs
			if condOK(widthNs, p.PulseWMinNs, p.PulseWMaxNs, p.PulseCond) {
				if at.closer(c, &bestDist) {
					bestX = CrossFrac(disc, c, lvl)
				}
			}
			enter = -1
		}
	}
	return bestX
}

// QualifySlope (spec 05: SLEW): a monotone lo→hi (rising) or hi→lo
// (falling) traversal whose time qualifies. Anchor = the second-threshold
// crossing nearest the frame centre.
// TRLC-LINKS: REQ-DSO-011
func QualifySlope(disc []uint8, intervalNs float64, p TrigParams, rising bool) float64 {
	return qualifySlopeAt(disc, intervalNs, p, rising, centreAnchor(len(disc)))
}

// TRLC-LINKS: REQ-DSO-011
func qualifySlopeAt(disc []uint8, intervalNs float64, p TrigParams, rising bool, at AnchorSpan) float64 {
	n := len(disc)
	mn, _, span := Ptp(disc)
	if span < FlatRejectSpan {
		return -1
	}
	lo := mn + int(p.SlopeLoFrac*float64(span))
	hi := mn + int(p.SlopeHiFrac*float64(span))
	bail := span / 10

	first, second, sg := lo, hi, 1 // rising: up through lo, then up through hi
	if !rising {
		first, second, sg = hi, lo, -1
	}

	bestX, bestDist := -1.0, n+1
	for c := 1; c < n; c++ {
		a, b := int(disc[c-1]), int(disc[c])
		var firstCross bool
		if rising {
			firstCross = a < first && b >= first
		} else {
			firstCross = a > first && b <= first
		}
		if !firstCross {
			continue
		}
		// Walk toward the second threshold; bail if the level reverses back
		// past the first threshold by more than span/10 (not monotone). Start
		// at k=c so a single sample step that spans BOTH thresholds (a fast
		// edge at any decimated band) is detected as a zero-time traversal at
		// index c — otherwise disc[k-1] is already past `second` for the whole
		// plateau and the crossing is never found.
		// In the slope's own orientation (codes negated for a falling edge)
		// the walk runs upward.
		for k := c; k < n; k++ {
			v, prev := sg*int(disc[k]), sg*int(disc[k-1])
			if v < sg*first-bail {
				break
			}
			if prev < sg*second && v >= sg*second {
				tNs := slopeTimeNs(disc, c, first, k, second, p, intervalNs)
				if condOK(tNs, p.SlopeTMinNs, p.SlopeTMaxNs, p.SlopeCond) && at.closer(k, &bestDist) {
					bestX = CrossFrac(disc, k, second)
				}
				break
			}
		}
	}
	return bestX
}

// Line sync pulses (4.7 us PAL/NTSC; equalizing 2.35 us) and the limits a
// sync must fall inside, give or take a sample interval: without them any
// square wave's edges count as line syncs (VIDEO fires on a 5 kHz square).
const (
	videoSyncMinNs = 1500
	videoSyncMaxNs = 12000
)

// videoSyncWidthOK reports whether the sync pulse entered at c leaves the
// sync region after a line sync's width.
// TRLC-LINKS: REQ-DSO-011
func videoSyncWidthOK(disc []uint8, c, syncLvl int, neg bool, intervalNs float64) bool {
	for k := c + 1; k < len(disc); k++ {
		a, b := int(disc[k-1]), int(disc[k])
		out := a <= syncLvl && b > syncLvl // negative sync: back up out of it
		if !neg {
			out = a >= syncLvl && b < syncLvl
		}
		if out {
			w := (CrossFrac(disc, k, syncLvl) - CrossFrac(disc, c, syncLvl)) * intervalNs
			return w+intervalNs >= videoSyncMinNs && w-intervalNs <= videoSyncMaxNs
		}
	}
	return false // the record ends inside the pulse: its width is unknown
}

// slopeTimeNs estimates a traversal's time from the interpolated threshold
// crossings, less what sample averaging alone makes of a step: the sample
// holding the edge averages it into an intermediate value, so interpolated
// between the plateaus a step spans up to two intervals and its threshold
// crossings sit up to 2*(hi-lo) intervals apart. Counting samples times every
// fast edge at 0 or one interval (SLOPE > 1 us would fire on ns edges at
// 500 us/div, 4.86 us samples). A slow ramp reads short by that allowance.
// TRLC-LINKS: REQ-DSO-011
func slopeTimeNs(disc []uint8, c, first, k, second int, p TrigParams, intervalNs float64) float64 {
	d := math.Abs(CrossFrac(disc, k, second) - CrossFrac(disc, c, first))
	return math.Max(0, d-2*math.Abs(p.SlopeHiFrac-p.SlopeLoFrac)) * intervalNs
}

// QualifyVideo (spec 05: TV): sync-separate at 30% up from the sync tip,
// collect crossings INTO the sync region as line boundaries, and anchor on
// the selected line's sync edge. Only all-lines (line=0) and line-N exist;
// odd/even field discrimination is NOT implementable here (needs a full
// video frame in the record) and must never silently mis-trigger.
// TRLC-LINKS: REQ-DSO-011
func QualifyVideo(disc []uint8, intervalNs float64, p TrigParams) float64 {
	return qualifyVideoAt(disc, intervalNs, p, centreAnchor(len(disc)))
}

// TRLC-LINKS: REQ-DSO-011
func qualifyVideoAt(disc []uint8, intervalNs float64, p TrigParams, at AnchorSpan) float64 {
	n := len(disc)
	mn, mx, span := Ptp(disc)
	if span < FlatRejectSpan {
		return -1
	}
	var syncLvl int
	if p.VideoNeg {
		syncLvl = mn + int(0.30*float64(span))
	} else {
		syncLvl = mx - int(0.30*float64(span))
	}

	maxLine := 625 // PAL
	if p.VideoStd == 1 {
		maxLine = 525 // NTSC
	}
	line := min(p.VideoLine, maxLine)

	count := 0
	bestX, bestDist := -1.0, n+1
	for c := 1; c < n; c++ {
		a, b := int(disc[c-1]), int(disc[c])
		var syncEdge bool
		if p.VideoNeg {
			syncEdge = a > syncLvl && b <= syncLvl // falling into sync
		} else {
			syncEdge = a < syncLvl && b >= syncLvl // rising into sync
		}
		if !syncEdge || !videoSyncWidthOK(disc, c, syncLvl, p.VideoNeg, intervalNs) {
			continue
		}
		count++
		if line == 0 {
			if at.closer(c, &bestDist) {
				bestX = CrossFrac(disc, c, syncLvl)
			}
		} else if count == line {
			if !at.holds(c) {
				return -1 // the line's sync falls where a screen cannot be filled
			}
			return CrossFrac(disc, c, syncLvl)
		}
	}
	if line != 0 {
		return -1 // fewer than N sync edges in the record
	}
	return bestX
}

// TrigQual reports the pulse and slope qualifiers (levels as fractions of the
// span, widths and times in ns, conditions any/<min/>max/in), so every
// surface shows and edits the values the engine uses.
type TrigQual struct {
	VideoStd   int     `json:"video_std"`
	VideoLine  int     `json:"video_line"`
	VideoNeg   bool    `json:"video_neg"`
	PulseLvl   float64 `json:"pulse_lvl"`
	PulseMinNs float64 `json:"pulse_min_ns"`
	PulseMaxNs float64 `json:"pulse_max_ns"`
	PulseCond  int     `json:"pulse_cond"`
	SlopeLo    float64 `json:"slope_lo"`
	SlopeHi    float64 `json:"slope_hi"`
	SlopeMinNs float64 `json:"slope_min_ns"`
	SlopeMaxNs float64 `json:"slope_max_ns"`
	SlopeCond  int     `json:"slope_cond"`
}

// QualOf is the reported view of trigger parameters.
// TRLC-LINKS: REQ-DSO-127
func QualOf(p TrigParams) TrigQual {
	return TrigQual{VideoStd: p.VideoStd, VideoLine: p.VideoLine, VideoNeg: p.VideoNeg, PulseLvl: p.PulseLvlFrac, PulseMinNs: p.PulseWMinNs, PulseMaxNs: p.PulseWMaxNs, PulseCond: p.PulseCond, SlopeLo: p.SlopeLoFrac, SlopeHi: p.SlopeHiFrac, SlopeMinNs: p.SlopeTMinNs, SlopeMaxNs: p.SlopeTMaxNs, SlopeCond: p.SlopeCond}
}

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

// QualExtent is what the captures taught about each channel's code range.
type QualExtent struct {
	Lo, Hi [2]atomic.Uint64 // the extent's ends in volts at the BNC (float64 bits)
	Have   [2]atomic.Bool
}
