// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"math"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// A live envelope frame's detail windows (sample.Detail) are stretches of
// the same capture read again at the record's rate, beside the envelope:
//   - the view windows (RoleView(s)): the stretch a surface shows magnified
//     (its zoom), leased per surface (instrument.Model.LeaseView), read as
//     an envelope of its own buckets, or raw where it is short: the screen's
//     envelope spreads its buckets over the whole screen, so a zoom of it
//     showed a bucket's (min, max) as a slope;
//   - the edge windows (Edge, EdgeOpp): the trigger edge and the first edge
//     the other way after it, raw, for rise, fall and over/preshoot, which
//     an envelope cannot hold; read every EdgeDetailEvery under one
//     committed state and carried to the frames between reads (Detail.Read
//     numbers the reads, so statistics count each once).
// A window's Detail.From, To are where it lies among the frame's samples.
// A detail read the board refuses or fails costs the frame nothing.

// edgeWindowSamples is an edge window's acquired samples: 2048 at the
// record's rate (4 us at 2 ns), the least a converter calibration finds its
// rotation in.
const edgeWindowSamples = 2048

// edgeCarry is the last edge windows read, carried to the frames until the
// next read; a commit drops them.
type edgeCarry struct {
	gen       uint64 // the committed generation they were read under
	need      instrument.RecordNeed
	at        time.Time
	edge, opp *sample.Frame
	read      uint64
}

// details reads frame f's detail windows (a live envelope's), or drops the
// edge windows carried on a frame of another kind. triggered: the frame's
// trigger is the board's, not a forced one.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-021
func (c *HALCycle) details(ctx context.Context, f *sample.Frame, triggered bool) {
	if f.Content != sample.PeakPairs && !f.Flags.Has(sample.Envelope) {
		c.edges = edgeCarry{read: c.edges.read}
		return
	}
	for s := range instrument.ViewSurfaces {
		if v, ok := c.inst.ViewLease(s); ok {
			c.viewWindow(ctx, f, sample.RoleView(s), v)
		}
	}
	if !triggered || f.TriggerAt < 0 {
		c.edges = edgeCarry{read: c.edges.read}
		return
	}
	e := &c.edges
	if e.gen != c.gen || e.need != c.need {
		*e = edgeCarry{gen: c.gen, need: c.need, read: e.read}
	}
	if now := time.Now(); e.at.IsZero() || now.Sub(e.at) >= EdgeDetailEvery {
		at, opp, hasOpp := c.edgesOf(f)
		e.edge = c.rawWindow(ctx, sample.Edge, at)
		e.opp = nil
		if hasOpp {
			e.opp = c.rawWindow(ctx, sample.EdgeOpp, opp)
		}
		e.at = now
		e.read++
	}
	for _, d := range [...]struct {
		r sample.Role
		f *sample.Frame
	}{{sample.Edge, e.edge}, {sample.EdgeOpp, e.opp}} {
		if d.f != nil {
			f.Details = append(f.Details, sample.Detail{Role: d.r, Read: e.read, Frame: d.f})
		}
	}
}

// screenSamples is the acquired samples across the screen in the
// committed plan (the frame's own when the plan gives none).
// TRLC-LINKS: REQ-DSO-021
func (c *HALCycle) screenSamples(f *sample.Frame) float64 {
	if c.plan.WinSamples > 0 {
		return float64(c.plan.WinSamples)
	}
	if f.CaptureS > 0 && f.SampleS > 0 {
		return float64(f.WinPoints) * f.SampleS / f.CaptureS
	}
	return 0
}

// anchor is where a view window's screen widths count from, in acquired
// samples from the trigger: the trigger on a triggered frame, else the
// screen's middle (the screen centre sits DelayS after the trigger).
// TRLC-LINKS: REQ-DSO-021
func (c *HALCycle) anchor(f *sample.Frame) float64 {
	if f.TriggerAt >= 0 {
		return 0
	}
	return (.5 - c.posFrac()) * c.screenSamples(f)
}

// indexOf is frame f's sample index of the acquired sample o samples from
// the trigger.
// TRLC-LINKS: REQ-DSO-021
func (c *HALCycle) indexOf(f *sample.Frame, o float64) float64 {
	per := 1.0
	if f.CaptureS > 0 && f.SampleS > 0 {
		per = f.CaptureS / f.SampleS
	}
	if f.TriggerAt >= 0 {
		return f.TriggerAt + o*per
	}
	return float64(f.Len())/2 + (o-c.anchor(f))*per
}

// viewWindow reads surface role's view window v of frame f's capture.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-021
func (c *HALCycle) viewWindow(ctx context.Context, f *sample.Frame, role sample.Role, v instrument.ViewWindow) {
	screen := c.screenSamples(f)
	if !(screen > 0) {
		return
	}
	buckets := ViewBuckets(v.A, v.B, v.Cols, uint32(max(f.WinPoints/2, 0))) //nolint:gosec // G115: a screen's buckets
	if buckets == 0 {
		return // the screen's envelope resolves the stretch already
	}
	from := math.Floor(c.anchor(f) + v.A*screen)
	span := math.Ceil((v.B - v.A) * screen)
	w := hal.Window{Role: role, StartSample: int64(from), Span: int64(span), Points: int(buckets), Want: sample.PeakPairs}
	if span <= 2*float64(buckets) {
		w.Points, w.Want = 0, c.rawContent()
	}
	g, err := c.read(ctx, w)
	if err != nil || g.Len() < 2 {
		return
	}
	f.Details = append(f.Details, sample.Detail{Role: role, From: c.indexOf(f, from), To: c.indexOf(f, from+span), Frame: g})
}

// rawWindow is an edge window of the capture, raw, centred o acquired
// samples from the trigger; nil when the board does not read it.
// TRLC-LINKS: REQ-DSO-021
func (c *HALCycle) rawWindow(ctx context.Context, role sample.Role, o float64) *sample.Frame {
	start := math.Floor(o) - edgeWindowSamples/2
	w := hal.Window{Role: role, StartSample: int64(start), Span: edgeWindowSamples, Want: c.rawContent()}
	g, err := c.read(ctx, w)
	if err != nil || g.Len() < 2 {
		return nil
	}
	return g
}

// rawContent is the raw content the committed plan's records hold.
// TRLC-LINKS: REQ-DSO-021
func (c *HALCycle) rawContent() sample.Content {
	if c.plan.AcqDecim > 1 {
		return sample.RawDecimated
	}
	return sample.RawFullRate
}

// edgesOf is where frame f's edge windows go, in acquired samples from the
// trigger: the trigger edge, and the first edge after it where the trigger
// channel comes back from the level the edge went to (hasOpp false: its
// envelope shows none). A serial, pulse, slope or video trigger points at
// no edge of the trigger's own direction (a UART match ends in its stop
// bit): the edge windows go to the edge nearest it, either way.
// TRLC-LINKS: REQ-DSO-021
func (c *HALCycle) edgesOf(f *sample.Frame) (at, opp float64, hasOpp bool) {
	spec := c.cur.TriggerSpec()
	if len(spec.Stages) == 0 {
		return 0, 0, false
	}
	st := spec.Stages[0]
	var t *sample.Trace
	for i := range f.Traces {
		if f.Traces[i].Ch == st.Source.Index {
			t = &f.Traces[i]
		}
	}
	if t == nil || t.Valid < 4 {
		return 0, 0, false
	}
	pairs := make([]uint8, t.Valid&^1)
	for k := range pairs {
		pairs[k] = t.Format.Code8(t.Word(k))
	}
	per := 1.0 // acquired samples a frame value
	if f.CaptureS > 0 && f.SampleS > 0 {
		per = f.SampleS / f.CaptureS
	}
	offset := func(b int) float64 { return (float64(2*b) + 1 - f.TriggerAt) * per } // bucket b's middle
	trig, rising := int(f.TriggerAt)/2, st.Levels.Slope != hal.Falling
	if c.cur.SerialMode != 0 || c.cur.Trigger.Type != instrument.TrigEdge {
		if b, r := NearestEdgeBucket(pairs, trig); b >= 0 {
			at, trig, rising = offset(b), b, r
		}
	}
	if b := OppositeEdgeBucket(pairs, trig, rising); b >= 0 {
		return at, offset(b), true
	}
	return at, 0, false
}
