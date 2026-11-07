// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render

import (
	"math"

	"github.com/labeth/open-dso-core/dsp"
	"github.com/labeth/open-dso-core/sample"
)

// Traces of a converter finer than eight bits are drawn from their Q8.8
// words (the 8-bit view's precision, sample.View8Frame), so a screen with
// more rows a division than the view has codes shows every row the
// converter resolves: a 12-bit trace at 30 codes a division on a 600-row
// screen steps one row, not two. An 8-bit scope's traces are drawn from
// their codes as they always were.

// fineQ is channel ch's first valid Q8.8 words as the display shows them
// (coupling and invert applied), nil when the frame was not of wider words
// or the trace has no words for them.
// TRLC-LINKS: REQ-DSO-021
func fineQ(p plotArea, f *sample.Frame, c Channel, ch, valid int) []uint16 {
	if !p.fine {
		return nil
	}
	q := f.Precision(ch)
	if len(q) < valid || valid == 0 {
		return nil
	}
	out := coupleQ8(q[:valid], c.Cpl)
	if c.Inv {
		for i, v := range out {
			out[i] = 0xFFFF - v
		}
	}
	return out
}

// coupleQ8 is q under the display coupling, as dsp.CoupleDisplay does it to
// codes: DC as it is, GND the centre, AC about its own mean. It copies.
// TRLC-LINKS: REQ-DSO-021
func coupleQ8(q []uint16, cpl int) []uint16 {
	out := append([]uint16(nil), q...)
	switch cpl {
	case dsp.CoupleGND:
		for i := range out {
			out[i] = sample.Mid8 << 8
		}
	case dsp.CoupleAC:
		var sum uint64
		for _, v := range q {
			sum += uint64(v)
		}
		mean := float64(sum) / float64(len(q))
		for i, v := range q {
			out[i] = uint16(math.Max(0, math.Min(0xFFFF, math.Round(float64(v)-mean+sample.Mid8*256))))
		}
	}
	return out
}

// qCode is Q8.8 word w as a code of the 8-bit view.
// TRLC-LINKS: REQ-DSO-021
func qCode(w uint16) float64 { return float64(w) / 256 }

// drawFineTrace is drawTraceHeld for Q8.8 words: each sample's row from its
// word, not its rounded code. A record with more samples than columns is
// drawn as each column's extent joined to the last column's end, as
// drawDenseTrace draws codes.
// TRLC-LINKS: REQ-DSO-021
func drawFineTrace(sf Canvas, p plotArea, q []uint16, held []bool, win int, xc float64, interp bool, col Color, posFrac float64, zoom int) {
	n := len(q)
	if n == 0 {
		return
	}
	if len(held) != n {
		held = nil
	}
	z := 1.0
	if zoom > 1 {
		z = float64(zoom)
	}
	win = min(win, n)
	if !(posFrac >= 0 && posFrac <= 1) {
		posFrac = 0.5
	}
	left := xc - float64(win)*posFrac
	row := func(v float64) int { return p.sy(sample.Mid8 + (v-sample.Mid8)*z) }
	colour := func(i int) Color {
		if held != nil && held[min(max(i, 0), n-1)] {
			return heldColor(sf.Format(), col)
		}
		return col
	}
	if !interp && float64(win) > 1.5*float64(p.w) {
		prevY := -1
		for x := range p.w {
			a := int(math.Floor(left + float64(x)*float64(win)/float64(p.w)))
			b := int(math.Floor(left+float64(x+1)*float64(win)/float64(p.w))) - 1
			a, b = min(max(a, 0), n-1), min(max(b, a), n-1)
			lo, hi := q[a], q[a]
			for _, v := range q[a+1 : b+1] {
				lo, hi = min(lo, v), max(hi, v)
			}
			yTop, yBot := row(qCode(hi)), row(qCode(lo))
			if prevY >= 0 {
				yTop, yBot = min(yTop, prevY), max(yBot, prevY)
			}
			vline(sf, x, yTop, yBot, colour(a))
			prevY = row(qCode(q[b]))
		}
		return
	}
	prevX, prevY := -1, 0
	for x := range p.w {
		pos := left + float64(x)*float64(win)/float64(p.w)
		y := row(fineAt(q, pos, interp))
		c := colour(int(pos))
		if prevX >= 0 {
			drawLine(sf, prevX, prevY, x, y, c)
		} else {
			sf.SetPixel(x, y, c)
		}
		prevX, prevY = x, y
	}
}

// fineAt is codeAt for Q8.8 words: the view's code at record position pos.
// TRLC-LINKS: REQ-DSO-021
func fineAt(q []uint16, pos float64, interp bool) float64 {
	n := len(q)
	pos = min(max(pos, 0), float64(n-1))
	i := int(pos)
	v := qCode(q[i])
	if interp && i+1 < n {
		v += (qCode(q[i+1]) - v) * (pos - float64(i))
	}
	return v
}
