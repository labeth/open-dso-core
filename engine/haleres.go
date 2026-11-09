// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"fmt"
	"math"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// The enhanced-resolution mode of a board that filters no records itself
// is math on the frames it reads (the board refuses the mode and says so),
// as the SDS1000CML+'s is: the owner asks for normal records, read raw (an
// envelope holds no samples to filter), and filters each trace with the
// mode's centred boxcar of EresLen points before anything else reads it.
// Precision words (State.Precision) are the board's own: a board without
// them refuses that mode. A stopped record shown again keeps its filtered
// copy while the length stays, and is filtered again from the record read
// when it changes, without reading the board.

// eresed is the request r with the enhanced resolution the board refused
// taken over: a normal record and the boxcar's length when r asks for a
// mode the board does not filter, 0 otherwise.
// TRLC-LINKS: REQ-DSO-012
func (c *HALCycle) eresed(r hal.Request, s instrument.State) (hal.Request, int) {
	if r.Mode != hal.HighRes || s.Precision {
		return r, 0
	}
	if _, err := c.b.Planner().Plan(r); err == nil {
		return r, 0 // the board filters itself
	}
	r.Mode = hal.Normal
	return r, ClampEresLen(s.EresLen)
}

// condition filters raw frame f with the committed boxcar (none: f is left
// as read).
// TRLC-LINKS: REQ-DSO-012
func (c *HALCycle) condition(f *sample.Frame) {
	length := c.eresLen
	if length <= 1 || f.Content == sample.PeakPairs || f.Flags.Has(sample.Envelope) {
		return
	}
	n := f.Len()
	if n == 0 {
		return
	}
	if len(f.Traces) > 0 && f.Traces[0].Codes16 == nil {
		Eres(f, length, &c.scratch) // 8-bit codes: through Q8.8, the codes rounded from it
		return
	}
	c.scratch = QBuffer(c.scratch, n)
	for i := range f.Traces {
		t := &f.Traces[i]
		if m := min(t.Valid, len(t.Codes16)); m > 0 {
			EresQ8(t.Codes16[:m], c.scratch, length) // a centred mean of the words, rounded
		}
		t.Precision = nil
	}
	f.Cond.Filter += fmt.Sprintf("; ERES %d-point boxcar", length)
	f.Cond.Guard += length / 2
	f.Cond.NoiseGain = math.Sqrt(float64(length))
	if bw := .443 / (float64(length) * f.SampleS); f.Cond.BandwidthHz == 0 || bw < f.Cond.BandwidthHz {
		f.Cond.BandwidthHz = bw
	}
}

// heldCond is the stopped record's filtered copy and what filtered it.
type heldCond struct {
	raw    *sample.Frame // the record as read
	length int           // the boxcar the shown copy has (0: none)
}

// reconditioned is the held record r as the committed filter shows it:
// the copy held while the length stays, else the record read filtered
// again (a new content).
// TRLC-LINKS: REQ-DSO-012
func (c *HALCycle) reconditioned(r *stoppedRecord) *sample.Frame {
	h := &r.cond
	if h.raw == nil || h.length == c.eresLen {
		return r.held
	}
	g := &sample.Frame{}
	g.CopyFrom(h.raw)
	c.condition(g)
	g.ContentID = r.newContent()
	h.length = c.eresLen
	r.held = g
	return g
}
