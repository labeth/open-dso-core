package render

import "testing"

// A dense fine trace panned past the record's start draws its columns left
// of the record on the first sample, as the code trace does, and does not
// panic: each column's end is clamped against its clamped start.
// TRLC-LINKS: REQ-DSO-021
func TestFineTracePannedPastTheStart(_ *testing.T) {
	sf := NewImage565(m800)
	p := plotArea{top: 20, bot: 400, y0: 14, h: 400, w: 700, m: m800, cpd: defaultCodesPerDiv, span: 8 * defaultCodesPerDiv}
	q := make([]uint16, 4096)
	for i := range q {
		q[i] = uint16(100+i%50) << 8
	}
	for _, xc := range []float64{-5000, -1500, 0, 6000} {
		drawFineTrace(sf, p, q, nil, 4096, xc, false, 0xffff, 0.5, 1)
	}
}
