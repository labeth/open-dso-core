// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"fmt"
	"math"
	"strings"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// The average of a board that averages no records itself is math on the
// frames it reads (the board refuses the mode and says so): the owner asks
// the board for normal records and averages each channel's samples over the
// last N records of one geometry, as a scope's average does. A change of
// geometry (the timebase, the record, a channel's scale) starts it again.

// average is the running average of the frames read.
type average struct {
	on    bool
	n     int         // the captures it averages
	k     int         // the captures it holds
	key   string      // the geometry it holds them at
	sum   [][]float64 // per trace, the sum of the captures' words
	queue [][][]float64
}

// geometry is a frame's geometry for the average: its records add sample
// for sample only at one.
// TRLC-LINKS: REQ-DSO-012
func geometryKey(f *sample.Frame) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%g|%g|%d", f.Len(), f.SampleS, f.Snapshot.TdivS, len(f.Traces))
	for i := range f.Traces {
		t := &f.Traces[i]
		fmt.Fprintf(&b, "|%d:%g:%g:%g", t.Ch, t.Scale.PerCode, t.Scale.OffsetV, t.Scale.Zero)
	}
	return b.String()
}

// reset empties the average.
// TRLC-LINKS: REQ-DSO-012
func (a *average) reset() {
	a.k, a.key, a.sum, a.queue = 0, "", nil, nil
}

// add takes frame f into the average and writes the average into f's words
// (rounded to the words a trace stores), naming it in f's conditioning.
// TRLC-LINKS: REQ-DSO-012
func (a *average) add(f *sample.Frame) {
	n := f.Len()
	if n == 0 {
		return
	}
	if key := geometryKey(f); key != a.key {
		a.reset()
		a.key = key
		a.sum = make([][]float64, len(f.Traces))
		for i := range a.sum {
			a.sum[i] = make([]float64, n)
		}
	}
	words := make([][]float64, len(f.Traces))
	for i := range f.Traces {
		t := &f.Traces[i]
		w := make([]float64, n)
		for j := range min(n, t.Valid) {
			w[j] = float64(t.Word(j))
		}
		words[i] = w
		for j := range n {
			a.sum[i][j] += w[j]
		}
	}
	a.queue = append(a.queue, words)
	a.k++
	for a.k > a.n {
		old := a.queue[0]
		a.queue = a.queue[1:]
		for i := range old {
			for j := range old[i] {
				a.sum[i][j] -= old[i][j]
			}
		}
		a.k--
	}
	for i := range f.Traces {
		t := &f.Traces[i]
		for j := range min(n, t.Valid) {
			v := math.Round(a.sum[i][j] / float64(a.k))
			if t.Format.Storage == sample.U16 {
				t.Codes16[j] = uint16(min(max(v, 0), math.MaxUint16))
			} else {
				t.Codes8[j] = uint8(min(max(v, 0), math.MaxUint8))
			}
		}
		t.Precision = nil // the words now hold the average
	}
	f.Cond.Filter = fmt.Sprintf("average %d/%d", a.k, a.n)
}

// averaged is the request r with the average the board refused taken over:
// a normal record when r asks for an average the board does not run.
// TRLC-LINKS: REQ-DSO-012
func (c *HALCycle) averaged(r hal.Request, n int) (hal.Request, bool) {
	if r.Mode != hal.Average {
		return r, false
	}
	if _, err := c.b.Planner().Plan(r); err == nil {
		return r, false // the board averages itself
	}
	r.Mode = hal.Normal
	c.avg.n = max(n, 1)
	return r, true
}
