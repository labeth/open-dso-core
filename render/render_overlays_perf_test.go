// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render

import (
	"fmt"
	"math"
	"testing"
)

// mathCodesRef is the per-sample math the tabled mathCodes must match.
// TRLC-LINKS: REQ-DSO-021
func mathCodesRef(c1, c2 []uint8, hud HUD) []uint8 {
	scale := mathScale(hud) / 25
	v1 := hud.Ch[0].VdivV * float64(max(hud.Ch[0].Zoom, 1)) / 25 * math.Max(hud.Ch[0].Probe, 1)
	v2 := hud.Ch[1].VdivV * float64(max(hud.Ch[1].Zoom, 1)) / 25 * math.Max(hud.Ch[1].Probe, 1)
	if v1 <= 0 {
		v1 = 1.0 / 25
	}
	if v2 <= 0 {
		v2 = 1.0 / 25
	}
	off1, off2 := hud.Ch[0].OffV*math.Max(hud.Ch[0].Probe, 1), hud.Ch[1].OffV*math.Max(hud.Ch[1].Probe, 1)
	if hud.Ch[0].Cpl != 0 {
		off1 = 0
	}
	if hud.Ch[1].Cpl != 0 {
		off2 = 0
	}
	m := make([]uint8, min(len(c1), len(c2)))
	for i := range m {
		a, b := (float64(c1[i])-128)*v1-off1, (float64(c2[i])-128)*v2-off2
		var v float64
		switch hud.MathMode {
		case 1:
			v = a + b
		case 2:
			v = a - b
		case 3:
			v = b - a
		case 4:
			v = a * b
		}
		m[i] = uint8(math.Round(math.Max(0, math.Min(255, 128+v/scale))))
	}
	return m
}

// Every code pair, every mode, odd scales, offsets, probes and coupling.
// TRLC-LINKS: REQ-DSO-021
func TestMathCodesMatchesRef(t *testing.T) {
	c1, c2 := make([]uint8, 65536+7), make([]uint8, 65536)
	for i := range c2 {
		c1[i], c2[i] = uint8(i), uint8(i>>8)
	}
	for _, h := range []HUD{
		HUD{Ch: [MaxChans]Channel{{VdivV: 0.2}, {VdivV: 0.5}}},
		HUD{Ch: [MaxChans]Channel{{VdivV: 0.002, Probe: 10, OffV: 0.013}, {VdivV: 5, Zoom: 4, OffV: -1.7}}},
		HUD{Ch: [MaxChans]Channel{{VdivV: 0.1, OffV: 0.3, Cpl: 1}, {VdivV: 0.1, OffV: 0.3}}},
		HUD{Ch: [MaxChans]Channel{{VdivV: 0}, {VdivV: -1, Probe: 100, OffV: 1e-3}}},
		HUD{Ch: [MaxChans]Channel{{VdivV: 1, OffV: math.Inf(1)}, {VdivV: 1, OffV: math.Inf(1)}}}, // NaN sums
	} {
		for mode := range 6 {
			h.MathMode = mode
			got, want := mathCodes(c1, c2, h, defaultCodesPerDiv), mathCodesRef(c1, c2, h)
			if string(got) != string(want) {
				t.Fatalf("mode %d hud %+v: mathCodes differs from the reference", mode, h)
			}
		}
	}
}

// The tuner's cents text reads as fmt's "%+.1f c"; si3 as "%.3g".
// TRLC-LINKS: REQ-DSO-273
func TestSoundTextMatchesFmt(t *testing.T) {
	for _, c := range []float64{0, math.Copysign(0, -1), 3.14, -3.14, -0.04, 0.04, 49.96, -50, 1e-7, 123456, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got, want := centsText(c), fmt.Sprintf("%+.1f c", c); got != want {
			t.Errorf("centsText(%v) = %q, fmt %q", c, got, want)
		}
		if got, want := si3(c), fmt.Sprintf("%.3g", c); got != want {
			t.Errorf("si3(%v) = %q, fmt %q", c, got, want)
		}
	}
}
