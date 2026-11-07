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
		TrigRising: s.Trigger.Slope() != hal.Falling, TrigSource: s.Trigger.Source().Index,
		TrigType: int(s.Trigger.running()), TrigQual: qualOf(s), HoldoffS: s.Trigger.Active().HoldoffS,
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
	if !(sec > 0) || math.IsInf(sec, 0) {
		sec = 0
	}
	m.set(func(s *State) { s.Trigger.Running().HoldoffS = sec })
	return sec
}

// The UI model's qualifier settings give levels as fractions of the
// source's screen (vertDivs divisions, 0 its bottom) and times in
// nanoseconds, with the conditions any, < min, > max and in range; the
// HAL's qualifier compares Shorter with its upper limit and Longer with
// its lower. A condition maps onto the limit it compares: "< min" is
// Shorter than min, "> max" Longer than max, so the other limit is the
// one the panel does not show for that condition.
const vertDivs = 8

// levelOfFrac is the level at fraction f of channel ch's screen, volts at
// the tip; fracOfLevel its inverse.
// TRLC-LINKS: REQ-DSO-011
func levelOfFrac(s *State, src hal.Source, f float64) float64 {
	if src.Kind != hal.Analog || src.Index < 0 || src.Index >= len(s.Channels) {
		return 0
	}
	c := s.Channels[src.Index]
	return (f-.5)*vertDivs*c.VoltsDiv - c.OffsetV
}

// TRLC-LINKS: REQ-DSO-011
func fracOfLevel(s *State, src hal.Source, v float64) float64 {
	if src.Kind != hal.Analog || src.Index < 0 || src.Index >= len(s.Channels) || !(s.Channels[src.Index].VoltsDiv > 0) {
		return .5
	}
	c := s.Channels[src.Index]
	return .5 + (v+c.OffsetV)/(vertDivs*c.VoltsDiv)
}

// qualSet sets a stage's time condition from the panel's condition and
// limits (nanoseconds).
// TRLC-LINKS: REQ-DSO-011
func qualSet(st *hal.TrigStage, minNs, maxNs float64, cond int) {
	q := stageQual(st)
	q.MinS, q.MaxS = minNs/1e9, maxNs/1e9
	switch cond {
	case 1: // < min
		q.Op, q.MaxS = hal.Shorter, minNs/1e9
	case 2: // > max
		q.Op, q.MinS = hal.Longer, maxNs/1e9
	case 3:
		q.Op = hal.Inside
	default:
		q.Op = 0
	}
}

// qualGet is a stage's time condition as the panel's condition and limits.
// TRLC-LINKS: REQ-DSO-011
func qualGet(st hal.TrigStage) (minNs, maxNs float64, cond int) {
	q := st.Qual
	if q == nil {
		return 0, 0, 0
	}
	minNs, maxNs = q.MinS*1e9, q.MaxS*1e9
	switch q.Op {
	case hal.Shorter:
		return maxNs, maxNs, 1
	case hal.Longer:
		return minNs, minNs, 2
	case hal.Inside, hal.Outside:
		return minNs, maxNs, 3
	}
	return minNs, maxNs, 0
}

// qualOf is state s's pulse, slope and video settings as the panel reads
// them.
// TRLC-LINKS: REQ-DSO-011
func qualOf(s State) TrigQual {
	var q TrigQual
	if c := s.Trigger.Configs[TrigPulse]; len(c.Stages) > 0 {
		st := c.Stages[0]
		q.PulseLvl = fracOfLevel(&s, st.Source, st.Levels.High)
		q.PulseMinNs, q.PulseMaxNs, q.PulseCond = qualGet(st)
	}
	if c := s.Trigger.Configs[TrigSlope]; len(c.Stages) > 0 {
		st := c.Stages[0]
		q.SlopeLo, q.SlopeHi = fracOfLevel(&s, st.Source, st.Levels.Low), fracOfLevel(&s, st.Source, st.Levels.High)
		q.SlopeMinNs, q.SlopeMaxNs, q.SlopeCond = qualGet(st)
	}
	if c := s.Trigger.Configs[TrigVideo]; len(c.Stages) > 0 && c.Stages[0].Video != nil {
		v := c.Stages[0].Video
		q.VideoStd = 1 // NTSC
		if v.Standard == "pal" {
			q.VideoStd = 0
		}
		if !v.AnyLine {
			q.VideoLine = v.Line
		}
		q.VideoNeg = !v.Positive
	}
	return q
}

// SetPulseParams sets the pulse trigger's level and width condition.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetPulseParams(lvlFrac, wMinNs, wMaxNs float64, cond int) {
	m.set(func(s *State) {
		st := firstStage(&s.Trigger.Configs[TrigPulse])
		st.Levels.High = levelOfFrac(s, st.Source, lvlFrac)
		qualSet(st, wMinNs, wMaxNs, cond)
	})
}

// SetSlopeParams sets the slope trigger's levels and time condition.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetSlopeParams(loFrac, hiFrac, tMinNs, tMaxNs float64, cond int) {
	m.set(func(s *State) {
		st := firstStage(&s.Trigger.Configs[TrigSlope])
		st.Levels.Low, st.Levels.High = levelOfFrac(s, st.Source, loFrac), levelOfFrac(s, st.Source, hiFrac)
		qualSet(st, tMinNs, tMaxNs, cond)
	})
}

// SetVideoParams sets the video trigger's standard (0 PAL, 1 NTSC), line
// (0: every line) and sync polarity.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) SetVideoParams(std, line int, neg bool) {
	m.set(func(s *State) {
		st := firstStage(&s.Trigger.Configs[TrigVideo])
		if st.Video == nil {
			st.Video = &hal.Video{Field: 1}
		}
		v := st.Video
		v.Standard = "ntsc"
		if std == 0 {
			v.Standard = "pal"
		}
		v.AnyLine, v.Line, v.Positive = line <= 0, max(line, 1), !neg
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
func (m *Model) SetAvgCount(n int) { m.set(func(s *State) { s.Averages = max(n, 1) }) }

// SetEresLen sets the high-resolution filter's length.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (m *Model) SetEresLen(l int) { m.set(func(s *State) { s.EresLen = max(l, 1) }) }

// SetETS switches equivalent-time sampling.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (m *Model) SetETS(on bool) { m.set(func(s *State) { s.ETS = on }) }
