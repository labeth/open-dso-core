// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"github.com/labeth/open-dso-core/dsp"
	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/sample"
)

// codesOf returns a channel's valid samples as codes (Q8.8 when the frame
// carries them), with the display coupling applied.
// TRLC-LINKS: REQ-DSO-215
func codesOf(f *sample.Frame, ch, lo, hi, cpl int) []float64 {
	valid := framecalc.Valid(f)
	lo, hi = max(lo, 0), min(hi, valid)
	if hi <= lo {
		return nil
	}
	sig := framecalc.Channel(f, ch, valid)
	if cpl != dsp.CoupleDC {
		sig = dsp.CoupleDisplay(sig, cpl)
	}
	q := f.Precision(0)
	if ch == 1 {
		q = f.Precision(1)
	}
	out := make([]float64, hi-lo)
	if cpl == dsp.CoupleDC && len(q) >= valid {
		for i, v := range q[lo:hi] {
			out[i] = float64(v) / 256
		}
		return out
	}
	for i, v := range sig[lo:hi] {
		out[i] = float64(v)
	}
	return out
}
