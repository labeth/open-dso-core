// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"fmt"
	"math"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/prototrig"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// The board decides every trigger (ADR-SERIAL-HARDWARE-TRIGGER): a pulse,
// slope or video trigger is the board's qualifier and a serial trigger its
// protocol trigger, never software deciding on the records. Their levels
// are placed on each channel's extent, learned from the records read (the
// qualifier's level is a fraction of the signal's swing, a bus is sliced
// at its middle unless the request names a threshold). A trigger the board
// cannot serve yet (the levels still being learned) or at all (refused,
// with the board's reason) runs free: captures complete untriggered, and
// the instrument's backends say why.

// minSwingDivs is the least swing, in divisions, a learned extent places
// levels on: under it a channel is a rail (engine.FlatRejectSpan's 40 codes
// at 25 codes a division).
const minSwingDivs = 1.6

// extentMove is how far, in divisions, a learned extent may move before a
// trigger placed on it is placed again.
const extentMove = 0.25

// extent is a channel's learned signal extent, volts at the tip, and its
// 8-bit view (the decoders' codes, which a serial request's thresholds are
// in): volts a view code step and the view's code for 0 V.
type extent struct {
	lo, hi     float64
	have       bool
	voltsCode8 float64 // volts per 8-bit view code at the V/div read
	zero8      float64 // the view code of 0 V at the offset read
	vdiv       float64
}

// learn takes the extents of the channels of frame f.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013
func (c *HALCycle) learn(f *sample.Frame) {
	for i := range f.Traces {
		t := &f.Traces[i]
		if t.Ch < 0 || t.Ch >= len(c.ext) || t.Valid < 2 || !(t.CodesPerDiv > 0) || !(t.Scale.PerCode > 0) {
			continue
		}
		if words := len(t.Codes8) + len(t.Codes16); words < t.Valid {
			continue // an envelope's pairs: the next record's samples tell
		}
		lo, hi := math.Inf(1), math.Inf(-1)
		for k := range t.Valid {
			v := t.Volts(k)
			lo, hi = min(lo, v), max(hi, v)
		}
		u := t.Format.Unit8()
		e := extent{lo: lo, hi: hi, have: true, vdiv: t.Scale.PerCode * t.CodesPerDiv}
		e.voltsCode8 = u * t.Scale.PerCode
		e.zero8 = 128 + (t.Scale.Word(0)-float64(t.Format.Mid))/u
		was := c.ext[t.Ch]
		// A record of the line at rest inside the swing learned before (a
		// sparse bus's idle between its frames: a caliper's 30 ms, an IR
		// remote's 40) says nothing new of its levels: the extent stays, or
		// a trigger placed on it would be refused for no swing and run free,
		// and its records stay the idle ones.
		if was.have && was.vdiv == e.vdiv && was.hi-was.lo >= minSwingDivs*was.vdiv && e.hi-e.lo < minSwingDivs*e.vdiv &&
			e.lo >= was.lo-extentMove*was.vdiv && e.hi <= was.hi+extentMove*was.vdiv {
			continue
		}
		c.ext[t.Ch] = e
		if !c.placed[t.Ch] {
			continue
		}
		div := max(e.vdiv, 1e-12)
		if !was.have || math.Abs(e.lo-was.lo) > extentMove*div || math.Abs(e.hi-was.hi) > extentMove*div {
			c.replace = true
		}
	}
}

// swing is channel ch's learned extent when it holds a signal, why it does
// not otherwise.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) swing(ch int) (extent, string) {
	if ch < 0 || ch >= len(c.ext) || !c.ext[ch].have {
		return extent{}, fmt.Sprintf("learning the signal levels on C%d", ch+1)
	}
	e := c.ext[ch]
	if e.hi-e.lo < minSwingDivs*e.vdiv {
		return e, fmt.Sprintf("no signal swing on C%d", ch+1)
	}
	return e, ""
}

// qualifierOf is a qualifier condition (Cond*) on limits minNs and maxNs in
// HAL terms: less than the minimum is Shorter than it, more than the
// maximum Longer than it, the other limit kept for the counter's range.
// TRLC-LINKS: REQ-DSO-011
func qualifierOf(cond int, minNs, maxNs float64) hal.Qualifier {
	lo, hi := minNs/1e9, maxNs/1e9
	switch cond {
	case CondLess:
		return hal.Qualifier{Op: hal.Shorter, MinS: hi, MaxS: lo}
	case CondGreater:
		return hal.Qualifier{Op: hal.Longer, MinS: hi, MaxS: lo}
	case CondInside:
		return hal.Qualifier{Op: hal.Inside, MinS: lo, MaxS: hi}
	}
	return hal.Qualifier{MinS: lo, MaxS: hi}
}

// qualStage is state s's pulse, slope or video trigger on the edge's
// source, its levels on the learned extent; why says why there is none.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) qualStage(s instrument.State, base hal.TrigStage) (hal.TrigStage, string) {
	ch := base.Source.Index
	e, why := c.swing(ch)
	if why != "" {
		return base, why
	}
	q := s.Qual
	at := func(frac float64) float64 { return e.lo + min(max(frac, 0), 1)*(e.hi-e.lo) }
	st := base
	switch TrigType(s.Trigger.Type) {
	case TrigPulse:
		st.Node = hal.NodePulse
		st.Levels.High = at(q.PulseLvl)
		qual := qualifierOf(q.PulseCond, q.PulseMinNs, q.PulseMaxNs)
		st.Qual = &qual
	case TrigSlope:
		st.Node = hal.NodeSlope
		st.Levels.Low, st.Levels.High = at(q.SlopeLo), at(q.SlopeHi)
		qual := qualifierOf(q.SlopeCond, q.SlopeMinNs, q.SlopeMaxNs)
		st.Qual = &qual
	case TrigVideo:
		st.Node = hal.NodeVideo
		// the sync slice, 30 % of the swing from the sync tip
		st.Levels.High = at(0.3)
		if !q.VideoNeg {
			st.Levels.High = at(0.7)
		}
		v := hal.Video{Standard: "pal", Line: q.VideoLine, AnyLine: q.VideoLine == 0, Positive: !q.VideoNeg}
		if q.VideoStd == 1 {
			v.Standard = "ntsc"
		}
		st.Video = &v
	case TrigEdge:
		return base, "an edge trigger has no qualifier"
	default:
		return base, "the trigger type has no qualifier"
	}
	return st, ""
}

// busStage is state s's serial trigger as the board's protocol trigger on
// the bus's first line: the request whole (prototrig.Bus), its thresholds
// in volts, each line sliced at the middle of its learned extent unless
// the request names a threshold.
// TRLC-LINKS: REQ-DSO-013
func (c *HALCycle) busStage(s instrument.State, base hal.TrigStage) (hal.TrigStage, string) {
	p := s.Serial
	if p.Proto <= 0 {
		return base, "no bus to trigger on: the decoder is off"
	}
	level := func(ch int, have bool, code float64) (float64, string) {
		e, why := c.swing(ch)
		if why != "" {
			return 0, why
		}
		if have {
			return (code - e.zero8) * e.voltsCode8, ""
		}
		return (e.lo + e.hi) / 2, ""
	}
	a, why := level(p.ChA, p.HaveThr, p.Threshold)
	if why != "" {
		return base, why
	}
	b := a
	if p.ChB != p.ChA && p.ChB >= 0 && p.ChB < len(c.ext) {
		// a second line silent (a UART's unused role) keeps the first's level
		if lb, whyB := level(p.ChB, p.HaveThrB, p.ThresholdB); whyB == "" {
			b = lb
		}
	}
	st := base
	st.Node = hal.NodeProtocol
	st.Source = hal.Source{Kind: hal.Analog, Index: p.ChA}
	st.Levels.High = a
	st.Proto = &prototrig.Bus{Request: p, Levels: [2]float64{a, b}}
	return st, ""
}

// programTrigger programs state s's trigger: the serial trigger, else the
// running type. A pulse, slope or video type the UI model set (its
// configuration Placed) is placed on the learned swing; one the board
// cannot serve yet runs free, and the backends say why; a board that runs
// nothing free is left on the edge trigger, with the reason said. A type
// set in its own levels and limits (a host interface's) is programmed as
// it is: the board takes it or refuses it with the reason, which the
// commit reports.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013
func (c *HALCycle) programTrigger(s instrument.State) error {
	spec := s.TriggerSpec()
	edge := s.Trigger.Configs[instrument.TrigEdge]
	var fallback hal.TrigStage
	if len(edge.Stages) > 0 {
		fallback = edge.Stages[0]
	}
	var be instrument.Backends
	clear(c.placed)
	var want hal.TrigStage
	what, why := "", ""
	switch t := s.Trigger.Type; {
	case s.SerialMode != 0:
		what = "serial"
		want, why = c.busStage(s, fallback)
		c.placeOn(s.Serial.ChA, s.Serial.ChB)
	case (t == instrument.TrigPulse || t == instrument.TrigSlope || t == instrument.TrigVideo) &&
		s.Trigger.Active().Placed && len(spec.Stages) > 0:
		what = "trigger"
		want, why = c.qualStage(s, spec.Stages[0])
		c.placeOn(spec.Stages[0].Source.Index)
	}
	tr := c.b.Trigger()
	if what == "" {
		c.inst.SetBackends(be)
		return tr.Program(spec)
	}
	edgeSpec := s.Trigger.Spec(spec.Sweep)
	edgeSpec.Coupling, edgeSpec.NoiseReject, edgeSpec.HoldoffS = edge.Coupling, edge.NoiseReject, edge.HoldoffS
	if why == "" {
		ws := spec
		ws.Stages = []hal.TrigStage{want}
		err := tr.Program(ws)
		if err == nil {
			setBackend(&be, what, "hardware")
			c.inst.SetBackends(be)
			return nil
		}
		why = err.Error()
		if r, ok := hal.AsRefusal(err); ok {
			why = r.Reason
		}
	}
	setBackend(&be, what, "none: "+why)
	c.inst.SetBackends(be)
	edgeSpec.Stages = []hal.TrigStage{fallback}
	edgeSpec.FreeRun = true
	if err := tr.Program(edgeSpec); err != nil {
		edgeSpec.FreeRun = false
		return tr.Program(edgeSpec)
	}
	return nil
}

// placeOn marks the channels whose learned extent places the trigger: a
// move of one places it again.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) placeOn(chs ...int) {
	for _, ch := range chs {
		if ch >= 0 && ch < len(c.placed) {
			c.placed[ch] = true
		}
	}
}

// setBackend records backend v for what ("trigger" or "serial") in b.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013
func setBackend(b *instrument.Backends, what, v string) {
	if what == "serial" {
		b.Serial = v
	} else {
		b.Trigger = v
	}
}
