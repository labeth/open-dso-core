package instrument

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// runOf is the run control of m: running, the trigger mode and a shot armed.
func runOf(m *Model) (running bool, sweep hal.Sweep, single bool) {
	s := m.State()
	return s.Running, s.Sweep, s.Single
}

// The run control keeps the rules the SDS1000CML+'s engine has always had
// (ADR-RUN-CONTROL): SINGLE arms a shot in NORMAL and the next RUN brings
// back the mode it replaced; AUTO or NORMAL picked during the shot keeps it
// armed and is the user's own (RUN brings nothing back); STOP and RUN cancel
// the shot; a second SINGLE keeps the mode the first replaced.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func TestRunControlRules(t *testing.T) {
	m := New(twoChannels(), nil) // running, AUTO
	m.SetSingle()
	if r, sw, one := runOf(m); !r || sw != hal.NormalSweep || !one {
		t.Fatalf("SINGLE: running %v sweep %v single %v", r, sw, one)
	}
	if st := m.Snapshot(); !st.Single || !st.Norm || !st.Running {
		t.Fatalf("SINGLE's snapshot: %+v", st)
	}
	m.SetSingle() // again: the mode to bring back is still AUTO
	m.SetRunning(true)
	if r, sw, one := runOf(m); !r || sw != hal.Auto || one {
		t.Fatalf("RUN after SINGLE: running %v sweep %v single %v", r, sw, one)
	}

	m.SetSingle()
	m.SetNorm(false) // AUTO during the shot: still armed, waits in AUTO
	if r, sw, one := runOf(m); !r || sw != hal.Auto || !one {
		t.Fatalf("AUTO during SINGLE: running %v sweep %v single %v", r, sw, one)
	}
	if got := m.State().BoardSweep(); got != hal.Auto {
		t.Fatalf("a shot armed in AUTO runs the board in %v", got)
	}
	m.SetNorm(true)
	if got := m.State().BoardSweep(); got != hal.Single {
		t.Fatalf("a shot armed in NORMAL runs the board in %v", got)
	}
	m.SetRunning(true)
	if r, sw, one := runOf(m); !r || sw != hal.NormalSweep || one {
		t.Fatalf("RUN after a mode picked during SINGLE: running %v sweep %v single %v", r, sw, one)
	}

	m.SetNorm(false)
	m.SetSingle()
	m.SetRunning(false)
	if r, _, one := runOf(m); r || one {
		t.Fatalf("STOP cancels the shot: running %v single %v", r, one)
	}
	m.SetRunning(true)
	if _, sw, _ := runOf(m); sw != hal.Auto {
		t.Fatalf("RUN after STOP brings back the mode the shot replaced: %v", sw)
	}
}

// A controller that edits the state (SCPI, the web) gets the same rules:
// writing the single sweep arms a shot, and a stop or start cancels it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func TestRunControlThroughChange(t *testing.T) {
	m := New(twoChannels(), nil)
	if err := m.Change(func(s *State) error { s.Sweep, s.Running = hal.Single, true; return nil }); err != nil {
		t.Fatal(err)
	}
	if r, sw, one := runOf(m); !r || sw != hal.NormalSweep || !one {
		t.Fatalf("a single sweep written: running %v sweep %v single %v", r, sw, one)
	}
	if err := m.Change(func(s *State) error { s.Running = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := m.Change(func(s *State) error { s.Running = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if r, sw, one := runOf(m); !r || sw != hal.Auto || one {
		t.Fatalf("STOP then RUN written: running %v sweep %v single %v", r, sw, one)
	}
	if m := New(State{Sweep: hal.Single, Running: true}, nil); !m.State().Single || m.State().Sweep != hal.NormalSweep {
		t.Fatalf("a model made armed: %+v", m.State())
	}
}

// The setters stage what the snapshot then reports, the timebase snaps to
// the ladder, keeps the trigger's place across the screen and refuses a
// timebase off it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-010, REQ-DSO-011, REQ-DSO-012
func TestModelControls(t *testing.T) {
	m := New(twoChannels(), nil)
	m.SetTimebases([]float64{1e-6, 2e-6, 5e-6})
	m.SetTdiv(1e-6)
	m.SetTrigPosFrac(.25)
	if tb, ok := m.SetTdiv(2e-6 * (1 + 1e-9)); !ok || tb.TdivS != 2e-6 {
		t.Fatalf("SetTdiv: %+v %v", tb, ok)
	}
	if _, ok := m.SetTdiv(3e-6); ok {
		t.Fatal("a timebase off the ladder")
	}
	m.SetTrigType(2) // each type keeps its own source and slope
	m.SetTrigSource(1)
	m.SetTrigSource(7) // no such channel: kept
	m.SetTrigSlope(false)
	m.SetSlopeParams(.2, .8, 10, 20, 1)
	m.SetAcqMode(1)
	m.SetAvgCount(64)
	m.SetEresLen(3)
	m.SetETS(true)
	if got := m.SetHoldoff(-1); got != 0 {
		t.Fatalf("a negative holdoff: %g", got)
	}
	m.SetMemDepth(10000)
	st := m.Snapshot()
	if st.TdivS != 2e-6 || st.TrigPosFrac < .2499 || st.TrigPosFrac > .2501 || st.TrigSource != 1 || st.TrigRising ||
		st.TrigType != 2 || math.Abs(st.TrigQual.SlopeHi-.8) > 1e-12 || st.AcqMode != 1 || st.AvgCount != 64 || st.EresLen != 3 ||
		!st.ETS || st.MemDepth != 10000 {
		t.Fatalf("snapshot %+v", st)
	}
}
