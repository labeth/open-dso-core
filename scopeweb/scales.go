// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// scales is how the v1 wire reads the codes: the scope's own, or the
// shared one over the frames and the instrument state.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func (s *Scope) scales() Scales {
	if s.cfg.Scales != nil {
		return s.cfg.Scales
	}
	return stateScales{s}
}

// state is the instrument state; the zero state without an instrument.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) state() instrument.State {
	if s.cfg.Instrument == nil {
		return instrument.State{}
	}
	return s.cfg.Instrument.State()
}

// stateScales are the scales of the newest frame's traces, with the
// instrument state's couplings, probes, inversion and trigger position.
type stateScales struct{ s *Scope }

// Scales are each channel's volts a code and offset on the 8-bit view, as
// the newest frame's traces carry them (1 V/div at 25 codes before the
// first frame).
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func (v stateScales) Scales() (off, vpc [2]float64) {
	vpc = [2]float64{1.0 / 25, 1.0 / 25}
	v.s.cfg.Frames.WithFrame(func(f *sample.Frame) {
		if f == nil {
			return
		}
		for ch := range min(2, len(f.Traces)) {
			if t := &f.Traces[ch]; t.Scale.PerCode != 0 {
				vpc[ch], off[ch] = view8Scale(t)
			}
		}
	})
	return off, vpc
}

// view8Scale is a trace's volts a step of the 8-bit view and its offset:
// volts = (code8 − 128)·vpc − off.
// TRLC-LINKS: REQ-DSO-163
func view8Scale(t *sample.Trace) (vpc, off float64) {
	vpc = t.Scale.PerCode * t.Format.Unit8()
	off = (t.Scale.Zero-float64(t.Format.Mid))*t.Scale.PerCode + t.Scale.OffsetV
	return vpc, off
}

// Coupling is a software coupling of channel ch; a coupling the front end
// makes with a relay needs no display transform.
// TRLC-LINKS: REQ-DSO-163
func (v stateScales) Coupling(ch int) int {
	st := v.s.state()
	if ch >= len(st.Channels) || v.s.cfg.Board == nil {
		return 0
	}
	c := st.Channels[ch].Coupling
	for _, o := range v.s.cfg.Board.FrontEnd().Couplings(ch) {
		if o.Coupling == c && o.Software {
			return int(c)
		}
	}
	return 0
}

// Probe is channel ch's probe factor (1 when not set).
// TRLC-LINKS: REQ-DSO-163
func (v stateScales) Probe(ch int) float64 {
	st := v.s.state()
	if ch < len(st.Channels) && st.Channels[ch].Probe > 0 {
		return st.Channels[ch].Probe
	}
	return 1
}

// Inverted is the first two channels' inversion.
// TRLC-LINKS: REQ-DSO-163
func (v stateScales) Inverted() (inv [2]bool) {
	st := v.s.state()
	for ch := range min(2, len(st.Channels)) {
		inv[ch] = st.Channels[ch].Invert
	}
	return inv
}

// Position is the trigger's place on the screen from the delay, whether
// the scope runs and whether its sweep is a single shot.
// TRLC-LINKS: REQ-DSO-163
func (v stateScales) Position() (float64, bool, bool) {
	st := v.s.state()
	return posFrac(st, v.s.divsH()), st.Running, st.Running && st.Single
}

// divsH is the graticule's divisions across (10 by default).
// TRLC-LINKS: REQ-DSO-163
func (s *Scope) divsH() float64 {
	if d := s.profile().Display.DivsH; d > 0 {
		return float64(d)
	}
	return 10
}

// posFrac is the trigger's place on the screen (0 left, 1 right) for a
// state's delay: the screen centre sits DelayS after the trigger.
// TRLC-LINKS: REQ-DSO-163
func posFrac(st instrument.State, divs float64) float64 {
	if !(st.TimeDivS > 0) {
		return 0.5
	}
	return 0.5 - st.DelayS/(divs*st.TimeDivS)
}
