// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"math"

	"github.com/labeth/open-dso-core/hal"
)

var _ Controls = (*Model)(nil)

// runMemory is the trigger mode a single shot replaced, which the next RUN
// brings back unless the user picks a mode meanwhile.
type runMemory struct {
	sweep hal.Sweep
	set   bool
}

// The acquisition modes by the number Controls and Stats name them with.
var acqModes = [...]hal.AcqMode{hal.Normal, hal.Average, hal.HighRes, hal.PeakDetect, hal.HighRes}

// SetTimebases gives the timebase ladder SetTdiv snaps to (the profile's);
// without one any positive timebase is taken.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-010
func (m *Model) SetTimebases(tdivs []float64) {
	m.mu.Lock()
	m.tdivs = append([]float64(nil), tdivs...)
	m.mu.Unlock()
}

// Snapshot is the wanted state as the instrument's snapshot, with the run
// state and the newest frame's number.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-009
func (m *Model) Snapshot() Stats {
	m.mu.Lock()
	s, run, last := m.want, m.run, m.last
	m.mu.Unlock()
	st := Stats{
		Running: s.Running, Norm: s.Sweep == hal.NormalSweep || s.Sweep == hal.Single, Single: s.Single,
		TdivS: s.TimeDivS, DisplayedS: s.TimeDivS, TrigPosFrac: posFrac(s),
		TrigRising: s.Trigger.Slope != hal.Falling, TrigSource: s.Trigger.Source.Index,
		TrigType: s.TrigType, TrigQual: s.Qual, HoldoffS: s.Trigger.HoldoffS,
		AvgCount: s.Averages, EresLen: s.EresLen, ETS: s.ETS, MemDepth: s.Depth,
		Roll: run == Rolling,
	}
	for i, md := range acqModes[:4] {
		if s.Mode == md {
			st.AcqMode = i
		}
	}
	if last != nil {
		st.Seq, st.Published = last.Seq, last.Seq
	}
	return st
}

// posFrac is the trigger's place across the screen in state s.
// TRLC-LINKS: REQ-DSO-008
func posFrac(s State) float64 {
	if s.TimeDivS <= 0 {
		return .5
	}
	return .5 + s.DelayS/(10*s.TimeDivS)
}

// set stages fn's edit, refusals dropped: a setter has no error to give,
// and the board's refusal at the commit reaches the OnRefused listeners.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) set(fn func(*State)) {
	_ = m.Change(func(s *State) error { fn(s); return nil })
}

// SetTdiv stages the ladder's timebase at tdivS; the trigger keeps its
// place across the screen.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-010
func (m *Model) SetTdiv(tdivS float64) (Timebase, bool) {
	m.mu.Lock()
	tdivs := m.tdivs
	m.mu.Unlock()
	if !(tdivS > 0) || math.IsInf(tdivS, 0) {
		return Timebase{}, false
	}
	if len(tdivs) > 0 {
		found := false
		for _, t := range tdivs {
			if math.Abs(t-tdivS) <= t*1e-6 {
				tdivS, found = t, true
				break
			}
		}
		if !found {
			return Timebase{}, false
		}
	}
	m.set(func(s *State) {
		f := posFrac(*s)
		s.TimeDivS = tdivS
		s.DelayS = (f - .5) * 10 * tdivS
	})
	return Timebase{TdivS: tdivS}, true
}

// SetTrigPosFrac places the trigger across the screen.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) SetTrigPosFrac(frac float64) {
	frac = math.Min(1, math.Max(0, frac))
	m.set(func(s *State) { s.DelayS = (frac - .5) * 10 * s.TimeDivS })
}

// SetMemDepth asks for a record depth; the scope plans what it can.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) SetMemDepth(samples int) int {
	samples = max(samples, 0)
	m.set(func(s *State) { s.Depth = samples })
	return samples
}

// SetRunning starts or stops the run; either way an armed single shot is
// cancelled, and RUN brings back the mode a shot replaced.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetRunning(on bool) {
	m.runChange(func(s *State, r *runMemory) {
		s.Running, s.Single = on, false
		if on && r.set {
			s.Sweep, r.set = r.sweep, false
		}
	})
}

// SetNorm picks the trigger mode; an armed shot stays armed and waits in it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetNorm(on bool) {
	m.runChange(func(s *State, r *runMemory) {
		r.set = false
		s.Sweep = hal.Auto
		if on {
			s.Sweep = hal.NormalSweep
		}
	})
}

// SetSingle arms a single shot in NORMAL, keeping the mode it replaced.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetSingle() {
	m.runChange(func(s *State, r *runMemory) { arm(s, s.Sweep, r) })
}

// arm arms a single shot in s; replaced is the trigger mode it replaces.
// TRLC-LINKS: REQ-DSO-011
func arm(s *State, replaced hal.Sweep, r *runMemory) {
	if !r.set {
		r.sweep, r.set = replaced, true
		if replaced == hal.Single {
			r.sweep = hal.NormalSweep
		}
	}
	s.Sweep, s.Single, s.Running = hal.NormalSweep, true, true
}

// runChange stages a run-control edit with the memory of the replaced mode.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) runChange(fn func(*State, *runMemory)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, r := m.want.Clone(), m.runMem
	fn(&s, &r)
	_ = m.commitLocked(s, r) // a refusal leaves the run as it was
}

// runRules gives an edit made through Change the run control's meaning:
// writing Sweep Single (or Single) arms a shot, a new trigger mode is the
// user's own, and a run started or stopped cancels an armed shot, a start
// bringing back the mode the shot replaced.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func runRules(old State, s *State, r *runMemory) {
	switch {
	case s.Sweep == hal.Single || s.Single && !old.Single:
		replaced := old.Sweep
		if s.Sweep != hal.Single && s.Sweep != old.Sweep {
			replaced = s.Sweep
		}
		arm(s, replaced, r)
	default:
		if s.Sweep != old.Sweep {
			r.set = false
		}
		if s.Running != old.Running {
			s.Single = false
			if s.Running && r.set {
				s.Sweep, r.set = r.sweep, false
			}
		}
	}
}

// SetTrigSource takes the edge's source among the analog channels.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetTrigSource(ch int) {
	m.set(func(s *State) {
		if ch >= 0 && ch < len(s.Channels) {
			s.Trigger.Source = hal.Source{Kind: hal.Analog, Index: ch}
		}
	})
}

// SetTrigSlope picks the rising or the falling edge.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetTrigSlope(rising bool) {
	m.set(func(s *State) {
		s.Trigger.Slope = hal.Falling
		if rising {
			s.Trigger.Slope = hal.Rising
		}
	})
}

// SetTrigType picks edge, pulse, slope or video; out of range is edge.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetTrigType(t int) {
	if t < 0 || t > 3 {
		t = 0
	}
	m.set(func(s *State) { s.TrigType = t })
}

// SetHoldoff stages the holdoff (0: off) and returns it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetHoldoff(sec float64) float64 {
	if !(sec > 0) || math.IsInf(sec, 0) {
		sec = 0
	}
	m.set(func(s *State) { s.Trigger.HoldoffS = sec })
	return sec
}

// SetPulseParams sets the pulse qualifier.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetPulseParams(lvlFrac, wMinNs, wMaxNs float64, cond int) {
	m.set(func(s *State) {
		s.Qual.PulseLvl, s.Qual.PulseMinNs, s.Qual.PulseMaxNs, s.Qual.PulseCond = lvlFrac, wMinNs, wMaxNs, cond
	})
}

// SetSlopeParams sets the slope qualifier.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetSlopeParams(loFrac, hiFrac, tMinNs, tMaxNs float64, cond int) {
	m.set(func(s *State) {
		q := &s.Qual
		q.SlopeLo, q.SlopeHi, q.SlopeMinNs, q.SlopeMaxNs, q.SlopeCond = loFrac, hiFrac, tMinNs, tMaxNs, cond
	})
}

// SetVideoParams sets the video trigger's standard, line and sync polarity.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetVideoParams(std, line int, neg bool) {
	m.set(func(s *State) { s.Qual.VideoStd, s.Qual.VideoLine, s.Qual.VideoNeg = std, line, neg })
}

// SetAcqMode picks the acquisition mode by its number (Controls).
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (m *Model) SetAcqMode(md int) {
	if md < 0 || md >= len(acqModes) {
		md = 0
	}
	m.set(func(s *State) { s.Mode = acqModes[md] })
}

// SetAvgCount sets the average's frame count.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (m *Model) SetAvgCount(n int) { m.set(func(s *State) { s.Averages = max(n, 1) }) }

// SetEresLen sets the high-resolution filter's length.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (m *Model) SetEresLen(l int) { m.set(func(s *State) { s.EresLen = max(l, 1) }) }

// SetETS switches equivalent-time sampling.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (m *Model) SetETS(on bool) { m.set(func(s *State) { s.ETS = on }) }
