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
	shots uint64 // counts the shots armed: every SINGLE press is one
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
	s, run, last, be := m.want, m.run, m.last, m.backends
	m.mu.Unlock()
	st := Stats{
		Running: s.Running, Norm: s.Sweep == hal.NormalSweep || s.Sweep == hal.Single, Single: s.Single,
		TdivS: s.TimeDivS, DisplayedS: s.TimeDivS, TrigPosFrac: posFrac(s),
		TrigRising: s.Trigger.Slope() != hal.Falling, TrigSource: s.Trigger.Source().Index,
		TrigType: int(s.Trigger.running()), TrigQual: s.Qual, HoldoffS: s.Trigger.Active().HoldoffS,
		AvgCount: s.Averages, EresLen: s.EresLen, ETS: s.ETS, MemDepth: s.Depth,
		Roll:       run == Rolling,
		SerialMode: s.SerialMode, SerialSet: len(s.Serial.Bytes) > 0 || len(s.Serial.Pattern) > 0,
		TriggerBackend: be.Trigger, SerialBackend: be.Serial,
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

// posFrac is the trigger's place across the screen in state s: the screen
// centre DelayS after the trigger puts the trigger left of the centre.
// TRLC-LINKS: REQ-DSO-008
func posFrac(s State) float64 {
	if s.TimeDivS <= 0 {
		return .5
	}
	return .5 - s.DelayS/(10*s.TimeDivS)
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
		s.DelayS = (.5 - f) * 10 * tdivS
	})
	return Timebase{TdivS: tdivS}, true
}

// SetTrigPosFrac places the trigger across the screen.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) SetTrigPosFrac(frac float64) {
	frac = math.Min(1, math.Max(0, frac))
	m.set(func(s *State) { s.DelayS = (.5 - frac) * 10 * s.TimeDivS })
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
// Every press is a fresh shot (Shots): a capture whose record was ready
// before it does not complete it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetSingle() {
	m.runChange(func(s *State, r *runMemory) { arm(s, s.Sweep, r) })
}

// arm arms a single shot in s, a fresh one even when one is armed;
// replaced is the trigger mode it replaces.
// TRLC-LINKS: REQ-DSO-011
func arm(s *State, replaced hal.Sweep, r *runMemory) {
	r.shots++
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

// SetTrigSource takes the running type's source among the analog
// channels; a type without a single source (a pattern) keeps its inputs.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetTrigSource(ch int) {
	m.set(func(s *State) {
		if ch >= 0 && ch < len(s.Channels) {
			_ = s.Trigger.SetSource(hal.Source{Kind: hal.Analog, Index: ch}, len(s.Channels))
		}
	})
}

// SetTrigSlope picks the rising or the falling edge of the running type.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetTrigSlope(rising bool) {
	m.set(func(s *State) {
		sl := hal.Falling
		if rising {
			sl = hal.Rising
		}
		_ = s.Trigger.SetSlope(sl, len(s.Channels))
	})
}

// SetTrigType picks the trigger type by its number (TrigType); out of range
// is edge. A type the trigger's capabilities do not serve is refused and
// the type stays (SetTrigCaps); SetTrigTypeOf says why.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetTrigType(t int) {
	if t < 0 || t >= int(NumTrigTypes) {
		t = 0
	}
	_ = m.SetTrigTypeOf(TrigType(t))
}

// SetTrigTypeOf picks trigger type t, or returns why it cannot run.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetTrigTypeOf(t TrigType) error {
	if t < 0 || t >= NumTrigTypes {
		return hal.Refuse("trigger type", "not a trigger type")
	}
	return m.Change(func(s *State) error { s.Trigger.Type = t; return nil })
}

// TrigTypes is every trigger type with whether the trigger's capabilities
// serve it and why not; without capabilities every type is offered.
// TRLC-LINKS: REQ-DSO-011
func (m *Model) TrigTypes() []TypeAvail {
	m.mu.Lock()
	caps := m.caps
	m.mu.Unlock()
	out := make([]TypeAvail, NumTrigTypes)
	for t := range NumTrigTypes {
		out[t] = TypeAvail{Type: t, OK: true}
		if caps != nil {
			out[t].OK, out[t].Why = t.Avail(caps())
		}
	}
	return out
}

// TypeAvail is a trigger type with whether it can run and why not.
type TypeAvail struct {
	Type TrigType
	OK   bool
	Why  string
}

// SetHoldoff stages the running type's holdoff (0: off) and returns it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetHoldoff(sec float64) float64 {
	sec = ClampHoldoff(sec)
	m.set(func(s *State) { s.Trigger.Running().HoldoffS = sec })
	return sec
}

// SetPulseParams sets the pulse trigger as the UI model gives it: the
// level a fraction of the source's swing, widths in nanoseconds; the owner
// places them on the swing it learns (TrigConfig.Placed).
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetPulseParams(lvlFrac, wMinNs, wMaxNs float64, cond int) {
	m.set(func(s *State) {
		wMinNs, wMaxNs = OrderPair(wMinNs, wMaxNs)
		s.Qual.PulseLvl, s.Qual.PulseMinNs, s.Qual.PulseMaxNs, s.Qual.PulseCond = ClampFrac(lvlFrac), wMinNs, wMaxNs, ClampCond(cond)
		s.Trigger.Configs[TrigPulse].Placed = true
	})
}

// SetSlopeParams sets the slope trigger as the UI model gives it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetSlopeParams(loFrac, hiFrac, tMinNs, tMaxNs float64, cond int) {
	m.set(func(s *State) {
		q := &s.Qual
		loFrac, hiFrac = OrderPair(ClampFrac(loFrac), ClampFrac(hiFrac))
		tMinNs, tMaxNs = OrderPair(tMinNs, tMaxNs)
		q.SlopeLo, q.SlopeHi, q.SlopeMinNs, q.SlopeMaxNs, q.SlopeCond = loFrac, hiFrac, tMinNs, tMaxNs, ClampCond(cond)
		s.Trigger.Configs[TrigSlope].Placed = true
	})
}

// SetVideoParams sets the video trigger's standard (0 PAL, 1 NTSC), line
// (0: every line) and sync polarity as the UI model gives them.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetVideoParams(std, line int, neg bool) {
	if std != 1 {
		std = 0
	}
	line = max(line, 0)
	m.set(func(s *State) {
		s.Qual.VideoStd, s.Qual.VideoLine, s.Qual.VideoNeg = std, line, neg
		s.Trigger.Configs[TrigVideo].Placed = true
	})
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
func (m *Model) SetAvgCount(n int) { m.set(func(s *State) { s.Averages = ClampAverages(n) }) }

// SetEresLen sets the high-resolution filter's length.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (m *Model) SetEresLen(l int) { m.set(func(s *State) { s.EresLen = ClampEresLen(l) }) }

// SetETS switches equivalent-time sampling.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (m *Model) SetETS(on bool) { m.set(func(s *State) { s.ETS = on }) }
