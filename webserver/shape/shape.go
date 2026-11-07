// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package shape

import "math"

// The coupling display modes (/api/set coupling values; measure's coupling).
const (
	CouplingDC  = 0
	CouplingAC  = 1
	CouplingGND = 2
)

// ResampleEnv nearest-resamples an envelope column array to n output columns.
// TRLC-LINKS: REQ-SDS-163
func ResampleEnv(v []uint8, envCols, n int) []int16 {
	out := make([]int16, n)
	for x := range n {
		c := x * envCols / n
		if c < len(v) {
			out[x] = int16(v[c])
		}
	}
	return out
}

// RawInt16 copies a raw code slice to the []int16 wire type without resampling —
// used when there is no trigger to anchor on (free-run). Codes are contiguous.
// TRLC-LINKS: REQ-SDS-163
func RawInt16(codes []uint8) []int16 {
	out := make([]int16, len(codes))
	for i, c := range codes {
		out[i] = int16(c)
	}
	return out
}

// Window maps a record onto n screen columns (spec 04 §5): centre on the
// software edge (or record middle for a flat frame), linear interpolation on
// native-fast bands, nearest sample on decimated. The window is clamped to
// stay within the record so the screen is filled with real data — the edge
// stays centred except near the record ends where there is no data to slide
// into view. Gaps (-1) only occur off the record; the client breaks the
// polyline there.
// TRLC-LINKS: REQ-SDS-163
func Window(sig []uint8, valid, winCols int, edgeX float64, interp bool, n int, posFrac float64) []int16 {
	out := make([]int16, n)
	if valid < 1 {
		for x := range out {
			out[x] = -1
		}
		return out
	}
	win := min(winCols, valid)
	xc := edgeX
	// !(xc >= 0) also catches NaN and -Inf; +Inf is handled by the per-column
	// upper clamp below. A non-finite index (int(NaN) = min-int) panicked the
	// whole serve goroutine on one bad frame (frame-serve fuzz).
	if !(xc >= 0) { // no crossing: the screen centred on the record (lcd traceWindow)
		xc = float64(valid)/2 + (posFrac-0.5)*float64(win)
	}
	// Anchor the crossing at posFrac of the window (0.5 = centre). Do NOT clamp
	// `left` into the record: that would drag the anchor off posFrac whenever the
	// record has no crossing near its middle (sub-period displays), which is what
	// made 50/100 µs jitter. Instead clamp the sample INDEX per column — off-record
	// columns extend the nearest rail (repeat-nearest). This keeps the anchor
	// exactly at posFrac every frame and is periodicity-invariant: a crossing near
	// a record end renders identically to a mid-record one. It is byte-identical
	// wherever the window already fits inside the record.
	left := xc - float64(win)*posFrac
	at := func(x int) float64 { // column x's sample position, clamped
		pos := left + float64(x)*float64(win)/float64(n)
		if !(pos >= 0) { // NaN/-Inf -> first sample (belt-and-suspenders)
			pos = 0
		} else if pos > float64(valid-1) {
			pos = float64(valid - 1)
		}
		return pos
	}
	if !interp { // the interp test once, not a column
		for x := range out {
			out[x] = int16(sig[int(at(x))])
		}
		return out
	}
	for x := range out {
		pos := at(x)
		i := int(pos)
		frac := pos - float64(i)
		v := float64(sig[i])
		if i+1 < valid {
			v = v*(1-frac) + float64(sig[i+1])*frac
		}
		out[x] = int16(math.Round(v))
	}
	return out
}

// CoupleQ8 copies precision words with the coupling display transform
// (Coupling*): a copy taken while the frame lock is held. AC centering uses
// its fractional mean; exported raw records bypass this display operation.
// TRLC-LINKS: REQ-SDS-163
func CoupleQ8(src []uint16, cpl int) []uint16 {
	out := append([]uint16(nil), src...)
	if cpl == CouplingDC {
		return out
	}
	if cpl == CouplingGND {
		for i := range out {
			out[i] = 32768
		}
		return out
	}
	var sum uint64
	for _, v := range src {
		sum += uint64(v)
	}
	mean := float64(sum) / float64(len(src))
	for i, v := range src {
		out[i] = uint16(math.Max(0, math.Min(65535, math.Round(float64(v)-mean+32768))))
	}
	return out
}

// HeldRanges is the [a, b) runs of held samples among the first n; nil when
// there are none.
// TRLC-LINKS: REQ-SDS-071
func HeldRanges(held []bool, n int) [][2]int {
	if len(held) < n {
		return nil
	}
	var out [][2]int
	for i := 0; i < n; i++ {
		if !held[i] {
			continue
		}
		j := i
		for j < n && held[j] {
			j++
		}
		out = append(out, [2]int{i, j})
		i = j
	}
	return out
}
