// ENGMODEL-OWNER-UNIT: FU-DSO-DSP

package dsp

import "math"

// The coupling modes CoupleDisplay knows, hal.Coupling's values: a scope
// whose relay cannot block DC or ground its input models AC and GND on the
// samples instead.
const (
	CoupleDC  = 0
	CoupleAC  = 1
	CoupleGND = 2
)

// CoupleDisplay applies a coupling mode's software transform to a copy of sig
// for the display and measurements: DC passes through unchanged; AC removes the
// DC component (mean → mid-scale); GND shows a flat trace at mid-scale (the
// ground reference). The input is never mutated (it is shared with
// trigger/decode). Callers zero the channel's offset for AC/GND so the ground
// marker sits at the centred baseline.
// TRLC-LINKS: REQ-DSO-096
func CoupleDisplay(sig []uint8, mode int) []uint8 {
	switch mode {
	case CoupleAC:
		return RemoveDC(sig)
	case CoupleGND:
		out := make([]uint8, len(sig))
		for i := range out {
			out[i] = 128
		}
		return out
	default:
		return sig
	}
}

// CoupleDisplayBy is CoupleDisplay for part, a stretch of sig drawn on its own
// (a zoom's detail): AC moves it by what takes sig's DC away, so it sits where
// the whole trace does. part is never mutated.
// TRLC-LINKS: REQ-DSO-096
func CoupleDisplayBy(part, sig []uint8, mode int) []uint8 {
	switch mode {
	case CoupleAC:
		if len(sig) == 0 {
			return part
		}
		return shifted(part, dcShift(sig))
	case CoupleGND:
		return CoupleDisplay(part, CoupleGND)
	default:
		return part
	}
}

// RemoveDC returns a copy of sig with its DC component removed: the record mean
// is shifted to mid-scale (code 128), clamped to [0,255]. The input is never
// mutated.
// TRLC-LINKS: REQ-DSO-096
func RemoveDC(sig []uint8) []uint8 {
	if len(sig) == 0 {
		return sig
	}
	return shifted(sig, dcShift(sig))
}

// dcShift is what moves sig's mean to mid-scale (code 128).
// TRLC-LINKS: REQ-DSO-096
func dcShift(sig []uint8) int {
	var sum int
	for _, v := range sig {
		sum += int(v)
	}
	return 128 - int(math.Round(float64(sum)/float64(len(sig))))
}

// shifted is a copy of sig moved by shift codes, clamped to [0,255]: through
// a table of the 256 codes, a lookup a sample in place of a clamp.
// TRLC-LINKS: REQ-DSO-096
func shifted(sig []uint8, shift int) []uint8 {
	var lut [256]uint8
	for v := range lut {
		lut[v] = uint8(min(max(v+shift, 0), 255))
	}
	out := make([]uint8, len(sig))
	for i, v := range sig {
		out[i] = lut[v]
	}
	return out
}
