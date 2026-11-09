package engine

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// coarseBoard is the fake whose edge trigger names its place to a record
// word of two samples; its screen is a raw window about the trigger.
type coarseBoard struct{ *halfake.Board }

func (b coarseBoard) Readout() hal.Readout { return rawScreen{b.Board.Readout()} }

type rawScreen struct{ hal.Readout }

func (rawScreen) ScreenWindow(hal.ScreenRequest) (hal.Window, error) {
	return hal.Window{Role: hal.Main, StartSample: -500, Span: 1000, Want: sample.RawFullRate}, nil
}

func (b coarseBoard) Profile() hal.Profile {
	p := b.Board.Profile()
	p.TriggerIndex = 2
	return p
}

// edgeState is an edge trigger on C1 at level volts, sweep sw.
func edgeState(sw hal.Sweep, level float64) instrument.State {
	s := loopState(sw)
	s.Trigger.Configs[instrument.TrigEdge] = instrument.DefaultTrigConfig(instrument.TrigEdge, 2)
	s.Trigger.Configs[instrument.TrigEdge].Stages[0].Levels.High = level
	return s
}

// On a board whose trigger index is coarse the edge trigger is placed on
// its level crossing in the record; one whose record has no crossing at the
// index is no trigger (AUTO shows it untriggered) and is counted. A board
// whose index is exact keeps it.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-016
func TestHALPlacesCoarseEdgeTriggers(t *testing.T) {
	fake := halfake.New(halfake.Config{})
	inst := instrument.New(edgeState(hal.NormalSweep, 0), nil)
	c := startLoop(coarseBoard{fake}, inst)
	f := c.waitFor(t, 1)[0]
	c.stop()
	if !f.Status.Triggered || f.TriggerAt-math.Floor(f.TriggerAt) != .5 {
		t.Fatalf("trigger at %g (triggered %v), want on the crossing half a sample before the board's index", f.TriggerAt, f.Status.Triggered)
	}
	inst = instrument.New(edgeState(hal.Auto, 3), nil) // above the square's high: never crossed
	c = startLoop(coarseBoard{halfake.New(halfake.Config{})}, inst)
	f = c.waitFor(t, 2)[1]
	c.stop()
	if f.Status.Triggered || f.TrigKind != sample.Forced || f.TriggerAt != -1 {
		t.Fatalf("a trigger with no crossing: triggered %v kind %v at %g", f.Status.Triggered, f.TrigKind, f.TriggerAt)
	}
	if st := inst.Snapshot(); st.FalseTriggers == 0 || !(st.NoiseTriggers > 0) {
		t.Fatalf("false triggers %d share %g", st.FalseTriggers, st.NoiseTriggers)
	}
	inst = instrument.New(edgeState(hal.NormalSweep, 3), nil)
	c = startLoop(exactBoard{coarseBoard{halfake.New(halfake.Config{})}}, inst)
	f = c.waitFor(t, 1)[0]
	c.stop()
	if !f.Status.Triggered || f.TriggerAt != math.Floor(f.TriggerAt) {
		t.Fatalf("exact index: trigger at %g triggered %v, want the board's", f.TriggerAt, f.Status.Triggered)
	}
}

// exactBoard is coarseBoard with an exact trigger index.
type exactBoard struct{ coarseBoard }

func (b exactBoard) Profile() hal.Profile { return b.Board.Profile() }
