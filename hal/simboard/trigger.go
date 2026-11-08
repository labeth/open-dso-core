package simboard

import (
	"fmt"
	"math"
	"slices"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/siggen"
)

// The software trigger's hysteresis: a level must be left by this much of a
// division before it can fire again (more with noise reject).
const (
	hysteresisDiv  = 0.04
	noiseRejectDiv = 0.3
)

// view is one channel's signal as the screen shows it: the generator's
// volts through the coupling and the invert, at the probe tip. The
// generator's volts are the probe tip's (a bench source on the probe), and
// a scope shows tip volts whatever the probe factor, so the factor scales
// the V/div and never the signal.
type view struct {
	rep    *replay // a replayed record's channel in place of the generator
	n      int     // the channel
	sig    siggen.Signal
	ch     hal.Channel
	dc     float64 // the mean AC coupling removes
	scale  float64 // -1 when inverted, else 1
	ground bool
}

// viewOf is channel ch's signal under setting c.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) viewOf(ch int, c hal.Channel) view {
	v := view{sig: b.sig.Signal(ch), ch: c, scale: 1, ground: c.Coupling == hal.GND, n: ch}
	if c.Invert {
		v.scale = -v.scale
	}
	if c.Coupling == hal.AC {
		v.dc = meanOf(v.sig)
	}
	return v
}

// volts is the channel's tip voltage at t as the screen shows it.
// TRLC-LINKS: REQ-DSO-1001
func (v view) volts(t float64) float64 {
	if v.ground {
		return 0
	}
	if x, ok := v.rep.volts(v.n, t); ok {
		return x - v.dc // captured at the tip: the probe is already in it
	}
	return (v.sig.V(t) - v.dc) * v.scale
}

// meanOf is s's mean over one repetition period (its offset when it has
// none).
// TRLC-LINKS: REQ-DSO-1001
func meanOf(s siggen.Signal) float64 {
	if s.Freq <= 0 || s.Kind == "dc" || s.Kind == "noise" {
		return s.Offset
	}
	const n = 1024
	T := 1 / s.Freq
	sum := 0.0
	for i := range n {
		sum += s.V((float64(i) + 0.5) * T / n)
	}
	return sum / n
}

// scanStep is how finely the trigger scans s: a fraction of its shortest
// feature, never coarser than the record's samples.
// TRLC-LINKS: REQ-DSO-1001
func scanStep(s siggen.Signal, sampleS float64) float64 {
	step := sampleS
	switch {
	case s.BitRate > 0:
		step = min(step, 1/s.BitRate/8)
	case s.Kind == "dc" || s.Kind == "noise" || s.Freq <= 0:
	default:
		T := 1 / s.Freq
		step = min(step, T/64)
		d := s.Duty
		if s.Kind == "pulse" && s.Width > 0 {
			d = s.Width * s.Freq
		}
		if d > 0 && d < 1 {
			step = min(step, d*T/4, (1-d)*T/4)
		}
		if s.GlitchEvery > 0 && s.GlitchW > 0 {
			step = min(step, s.GlitchW/2)
		}
	}
	return max(step, 1e-12)
}

// edgeState is the trigger's comparator: which side of the level it was
// last seen on beyond the hysteresis, and when a pulse began.
type edgeState struct {
	init       bool
	low, high  bool    // armed for a rising, a falling crossing
	pulseStart float64 // NaN: no pulse begun
}

// scan looks for the armed acquisition's trigger from its scan point up to
// now, at most scanEvals evaluations; a scan that falls further behind
// than that skips ahead, as a scope misses what passes while it is busy.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) scan(q *acquisition, now float64) {
	if now <= q.scanFrom {
		return
	}
	st := q.set.trigger.Stages[0]
	if bus, ok := busOf(st.Proto); ok && st.Node == hal.NodeProtocol {
		b.scanBus(q, bus, now)
		return
	}
	ch := st.Source.Index
	v := b.viewOf(ch, q.set.channels[ch])
	v.rep = q.replay
	if q.set.trigger.Coupling == hal.TrigAC && !v.ground {
		v.dc = meanOf(v.sig)
	}
	step := scanStep(v.sig, q.set.plan.SampleS)
	if now-q.scanFrom > step*scanEvals {
		q.scanFrom = now - step*scanEvals
		q.edge = edgeState{}
	}
	level := st.Levels.High
	h := max(st.Levels.Hysteresis, hysteresisDiv*v.ch.VoltsDiv)
	if q.set.trigger.NoiseReject {
		h = max(h, noiseRejectDiv*v.ch.VoltsDiv)
	}
	e := &q.edge
	t := q.scanFrom
	prev := v.volts(t)
	if !e.init {
		e.init, e.pulseStart = true, math.NaN()
		e.low, e.high = prev < level-h, prev > level+h
	}
	for t < now {
		tn := t + step
		x := v.volts(tn)
		var at float64
		fired := false
		if e.low && x >= level { // a rising crossing
			at = refine(v, t, tn, level)
			e.low = false
			fired = b.fires(st, hal.Rising, at, e)
		}
		if !fired && e.high && x <= level { // a falling crossing
			at = refine(v, t, tn, level)
			e.high = false
			fired = b.fires(st, hal.Falling, at, e)
		}
		if x < level-h {
			e.low = true
		}
		if x > level+h {
			e.high = true
		}
		t = tn
		if fired {
			q.found, q.trigT = true, at
			break
		}
	}
	q.scanFrom = t
}

// fires reports whether a crossing of slope sl at t fires stage st: an
// edge of its slope, or the end of a pulse whose width its qualifier
// takes. e keeps where a pulse began.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) fires(st hal.TrigStage, sl hal.Slope, t float64, e *edgeState) bool {
	want := st.Levels.Slope
	if want == 0 {
		want = hal.Rising
	}
	if st.Node != hal.NodePulse {
		return want == sl || want == hal.Either
	}
	// A positive pulse (Rising) begins rising and ends falling.
	begin := want
	if begin == sl {
		e.pulseStart = t
		return false
	}
	if math.IsNaN(e.pulseStart) {
		return false
	}
	w := t - e.pulseStart
	e.pulseStart = math.NaN()
	q := st.Qual
	if q == nil || q.Op == 0 {
		return true
	}
	switch q.Op {
	case hal.Shorter:
		return w < q.MaxS
	case hal.Longer:
		return w > q.MinS
	case hal.Inside:
		return w > q.MinS && w < q.MaxS
	case hal.Outside:
		return w < q.MinS || w > q.MaxS
	}
	return false
}

// refine is where v crosses level between t0 and t1, by bisection.
// TRLC-LINKS: REQ-DSO-1001
func refine(v view, t0, t1, level float64) float64 {
	below := v.volts(t0) < level
	for range 40 {
		m := (t0 + t1) / 2
		if m <= t0 || m >= t1 {
			break
		}
		if (v.volts(m) < level) == below {
			t0 = m
		} else {
			t1 = m
		}
	}
	return t1
}

type trigger struct{ b *Board }

// Program stages s: an edge or a pulse width on one analog channel, which
// the software trigger runs; every other node is refused.
// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Program(s hal.TriggerSpec) error {
	caps := t.b.profile.Triggers
	if len(s.Stages) > caps.MaxStages {
		return hal.Refuse("trigger sequence", "the simulated board's software trigger has one stage")
	}
	if s.Reset != nil {
		return hal.Refuse("trigger reset", "the simulated board's software trigger has no reset stage")
	}
	if s.Coupling == hal.TrigHFReject || s.Coupling == hal.TrigLFReject {
		return hal.Refuse("trigger coupling", "the simulated board's software trigger has DC and AC coupling only")
	}
	for _, st := range s.Stages {
		if !caps.Has(st.Node) {
			return hal.Refuse("trigger "+st.Node.String(), "the simulated board triggers on edges, pulse widths and decoded buses only")
		}
		if st.Source.Kind != hal.Analog || st.Source.Index < 0 || st.Source.Index >= t.b.profile.Channels {
			return hal.Refuse("trigger source", "the simulated board triggers on its analog channels only")
		}
		if len(st.Zones) != 0 {
			return hal.Refuse("zone trigger", "the simulated board has no zone qualifier")
		}
		if st.Node == hal.NodeProtocol {
			if err := t.b.busStage(st); err != nil {
				return err
			}
			continue
		}
		if nc := caps.Nodes[st.Node]; st.Levels.Slope != 0 && !slices.Contains(nc.Slopes, st.Levels.Slope) {
			return hal.Refuse("trigger slope", fmt.Sprintf("the %s trigger takes no %v slope", st.Node, st.Levels.Slope))
		}
		if st.Gate != nil || st.Count > 1 {
			return hal.Refuse("trigger qualifier", "the simulated board's edge trigger has no gate or count")
		}
	}
	t.b.mu.Lock()
	defer t.b.mu.Unlock()
	t.b.staged.trigger = s
	return nil
}

// Caps implements hal.Trigger.
// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Caps() hal.TrigCaps { return t.b.profile.Triggers }

// LevelGrid is one level step at the source's staged V/div, over the
// screen's height about its centre. A board whose level is a DAC's code
// (Config.LevelStepV) steps that code's volts at the BNC, at the probe
// tip, on a grid through 0 V.
// TRLC-LINKS: REQ-DSO-1001
func (t trigger) LevelGrid(src hal.Source) hal.Grid {
	b := t.b
	if src.Kind != hal.Analog || src.Index < 0 || src.Index >= b.profile.Channels {
		return hal.Grid{}
	}
	b.mu.Lock()
	c := b.staged.channels[src.Index]
	b.mu.Unlock()
	half := float64(b.profile.Display.DivsV) / 2 * c.VoltsDiv
	lo, hi := -half-c.OffsetV, half-c.OffsetV
	if s := b.cfg.LevelStepV; s > 0 {
		step := s * max(c.Probe, 1)
		return hal.Grid{Step: step, Min: math.Ceil(lo/step-1e-9) * step, Max: math.Floor(hi/step+1e-9) * step}
	}
	return hal.Grid{Step: c.VoltsDiv / b.levelSteps(), Min: lo, Max: hi}
}

// levelSteps is how many level steps a division has.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) levelSteps() float64 {
	if s := b.cfg.LevelStepsPerDiv; s > 0 {
		return s
	}
	return b.codeSteps()
}

// codeSteps is how many converter codes a division has.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) codeSteps() float64 {
	return b.cpd / float64(uint32(1)<<b.profile.Format.Shift)
}

// Default is a rising edge or a positive pulse at 0 V on an analog channel.
// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Default(node hal.TrigNode, src hal.Source) (hal.TrigStage, error) {
	if !t.b.profile.Triggers.Has(node) {
		return hal.TrigStage{}, hal.Refuse("trigger "+node.String(), "the simulated board triggers in software on edges and pulse widths only")
	}
	if src.Kind != hal.Analog || src.Index < 0 || src.Index >= t.b.profile.Channels {
		return hal.TrigStage{}, hal.Refuse("trigger source", "the simulated board triggers on its analog channels only")
	}
	st := hal.TrigStage{Node: node, Source: src, Levels: hal.Levels{Slope: hal.Rising}}
	if node == hal.NodePulse {
		st.Qual = &hal.Qualifier{Op: hal.Longer, MinS: 1e-6, MaxS: 1e-3}
	}
	return st, nil
}

// Events implements hal.Trigger: the software trigger has no event queue.
// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Events([]hal.Event) (int, error) {
	return 0, hal.Refuse("trigger events", "the simulated board decodes no events in its trigger")
}

// Matches implements hal.Trigger: no protocol trigger.
// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Matches() (hal.MatchStats, bool) { return hal.MatchStats{}, false }
