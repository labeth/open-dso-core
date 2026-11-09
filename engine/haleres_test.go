package engine

import (
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// eresBoard is the whole-record fake whose planner filters nothing: ERES
// is refused, as on a board that leaves it to math on the frames.
type eresBoard struct{ wholeBoard }

func (b eresBoard) Planner() hal.Planner { return noEresPlanner{b.wholeBoard.Planner()} }

type noEresPlanner struct{ hal.Planner }

func (p noEresPlanner) Plan(r hal.Request) (hal.Plan, error) {
	if r.Mode == hal.HighRes {
		return hal.Plan{}, hal.Refuse("acquisition mode", "no ERES in the board")
	}
	return p.Planner.Plan(r)
}

// ERES on a board that filters nothing is the boxcar on the frames read,
// on 8-bit and on 16-bit codes; stopped, a new length filters the held
// record again without reading the board, and the same length shows the
// same filtered copy.
// TRLC-LINKS: REQ-DSO-012
func TestHALEresInSoftware(t *testing.T) {
	for _, fm := range []sample.CodeFormat{{}, {Bits: 12, Storage: sample.U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000}} {
		b := eresBoard{wholeBoard{Board: halfake.New(halfake.Config{Format: fm})}}
		s := loopState(hal.Single)
		s.Mode, s.EresLen = hal.HighRes, 5
		inst := instrument.New(s, nil)
		c := startLoop(b, inst)
		got := c.waitFor(t, 2)
		for _, f := range got[:2] {
			if !strings.Contains(f.Cond.Filter, "ERES 5-point boxcar") || f.Content == sample.PeakPairs {
				t.Fatalf("%v frame %v filtered %q, want the 5-point boxcar on samples", fm.Storage, f.Content, f.Cond.Filter)
			}
		}
		if inst.State().Mode != hal.HighRes {
			t.Fatal("the mode was refused")
		}
		whole := got[1]
		reads := strings.Count(strings.Join(b.Calls(), " "), "Read")
		_ = inst.Change(func(s *instrument.State) error { s.EresLen = 9; return nil })
		c.waitFor(t, 3)
		_ = inst.Change(func(s *instrument.State) error { s.Channels[0].VoltsDiv = 2; return nil })
		got = c.waitFor(t, 4)
		c.stop()
		if n := strings.Count(strings.Join(b.Calls(), " "), "Read"); n != reads {
			t.Fatalf("%d board reads while stopped", n-reads)
		}
		again, same := got[2], got[3]
		if !strings.Contains(again.Cond.Filter, "ERES 9-point") || again.ContentID == whole.ContentID {
			t.Fatalf("new length: %q content %d (was %d)", again.Cond.Filter, again.ContentID, whole.ContentID)
		}
		if same.ContentID != again.ContentID {
			t.Fatal("the same filter filtered the held record again")
		}
	}
}
