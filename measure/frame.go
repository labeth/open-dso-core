// ENGMODEL-OWNER-UNIT: FU-DSO-MEASURE

package measure

import "github.com/labeth/open-dso-core/sample"

// EdgeWindows is channel ch's samples of frame f's edge windows, for
// Result.WithEdgeDetail; none on a frame without them. The codes are the
// calibrated ones rounded: the Q8.8 values would cost each measurement a
// 65536-bin histogram (512 KB), on every display repaint - half the frame
// rate.
// TRLC-LINKS: REQ-DSO-021
func EdgeWindows(f *sample.Frame, ch int) []EdgeWindow {
	var out []EdgeWindow // nil without windows
	for _, r := range [2]sample.Role{sample.Edge, sample.EdgeOpp} {
		d := f.Detail(r)
		if d == nil {
			continue
		}
		if out == nil {
			out = make([]EdgeWindow, 0, 2) // both, one allocation
		}
		w := d.Frame
		out = append(out, EdgeWindow{Sig: w.Codes8(ch), SampleS: w.SampleS, Shape: w.Content == sample.RawFullRate})
	}
	return out
}

// Range is the stretch of frame f's record measurements read: a roll view's
// longest run of captured samples - its held stretches are no signal (a 1 kHz
// square measured across them read 769 Hz) - else the whole record.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-021
func Range(f *sample.Frame) (int, int) {
	held, n := f.Held(0), f.Len()
	if len(held) != n {
		return 0, n
	}
	best, bestAt, at := 0, 0, -1
	for i := 0; i <= n; i++ {
		if i < n && !held[i] {
			if at < 0 {
				at = i
			}
			continue
		}
		if at >= 0 && i-at > best {
			best, bestAt = i-at, at
		}
		at = -1
	}
	return bestAt, bestAt + best
}
