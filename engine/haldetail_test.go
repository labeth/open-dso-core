package engine

import (
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// envBoard is the fake whose readout places the screen as an envelope of
// 100 buckets, as a scope's live view reads a deep record.
type envBoard struct{ *halfake.Board }

func (b envBoard) Readout() hal.Readout { return envReadout{b.Board.Readout()} }

type envReadout struct{ hal.Readout }

func (envReadout) ScreenWindow(r hal.ScreenRequest) (hal.Window, error) {
	if r.Record || !r.Envelope {
		return hal.Window{Role: hal.Main, StartSample: -1000, Span: 2000, Want: sample.RawFullRate}, nil
	}
	return hal.Window{Role: hal.Main, StartSample: -1000, Span: 2000, Points: 100, Want: sample.PeakPairs}, nil
}

// A live envelope carries its edge windows, raw at the record's rate, read
// once and carried to the frames between reads under one numbered read;
// a surface's view lease adds its stretch read at full detail, where it
// lies among the envelope's values.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-021
func TestHALDetailWindows(t *testing.T) {
	b := envBoard{halfake.New(halfake.Config{PeriodSamples: 400})}
	s := loopState(hal.NormalSweep)
	s.Trigger.Configs[instrument.TrigEdge] = instrument.DefaultTrigConfig(instrument.TrigEdge, 2)
	inst := instrument.New(s, nil)
	c := startLoop(b, inst)
	got := c.waitFor(t, 3)
	var reads []uint64
	for _, f := range got[:3] {
		if f.Content != sample.PeakPairs {
			t.Fatalf("live frame %v, want the envelope", f.Content)
		}
		e := f.Detail(sample.Edge)
		if e == nil || e.Frame.Content != sample.RawFullRate || e.Frame.Len() != edgeWindowSamples {
			t.Fatalf("edge window %+v, want %d raw samples", e, edgeWindowSamples)
		}
		if f.Detail(sample.EdgeOpp) == nil {
			t.Fatal("no window on the opposite edge")
		}
		reads = append(reads, e.Read)
	}
	if reads[0] != reads[2] {
		t.Fatalf("edge reads %v within %v, want one read carried", reads, EdgeDetailEvery)
	}
	inst.LeaseView(int(ViewWeb), instrument.ViewWindow{A: -.05, B: .05, Cols: 400}, time.Second)
	var f *sample.Frame
	var v *sample.Detail
	for deadline := time.Now().Add(5 * time.Second); v == nil && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		c.mu.Lock()
		f = c.frames[len(c.frames)-1]
		c.mu.Unlock()
		v = ViewDetail(f, ViewWeb)
	}
	c.stop()
	if v == nil {
		t.Fatal("no view window under the lease")
	}
	if v.Frame.Len() < 2 || !(v.To > v.From) {
		t.Fatalf("view window %d values at %g..%g", v.Frame.Len(), v.From, v.To)
	}
	// the stretch is a tenth of the screen about the trigger
	if mid := (v.From + v.To) / 2; mid < f.TriggerAt-2 || mid > f.TriggerAt+2 {
		t.Fatalf("view window about %g, trigger at %g", mid, f.TriggerAt)
	}
}
