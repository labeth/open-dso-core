// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"math"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/settings"
)

// HALScope is the UI model's engine, front end, levels and hardware for a
// scope whose instrument state is the core's Model over a HAL board: every
// setting the panel makes is a change of the Model, which the owner loop
// commits through the board. Volts at the BNC are the tip's over the
// probe, as the UI model's front end speaks them.
type HALScope struct {
	m       *instrument.Model
	b       hal.Board
	product string
	mask    *engine.FrameMask
	bode    *engine.Bode // the frequency response, measured beside the owner loop (Watch)
}

// NewHALScope is the UI model's view of board b through instrument state
// m; product is the firmware line the SYS tab names.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-008
func NewHALScope(m *instrument.Model, b hal.Board, product string) *HALScope {
	return &HALScope{m: m, b: b, product: product, bode: engine.NewBode()}
}

// WithMask gives the UI model the mask test the owner runs on the frames
// (engine.HALConfig.Mask); without one the mask staging does nothing.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-014
func (s *HALScope) WithMask(x *engine.FrameMask) *HALScope {
	s.mask = x
	return s
}

// Engine is the Model's controls with the owner's mask test.
// TRLC-LINKS: REQ-DSO-135
func (s *HALScope) Engine() Engine { return halEngine{s.m, s.mask, s.bode} }

// Analog is the front end through the Model.
// TRLC-LINKS: REQ-DSO-135
func (s *HALScope) Analog() Analog { return halAnalog{s} }

// Levels are the trigger level and offsets through the Model.
// TRLC-LINKS: REQ-DSO-135
func (s *HALScope) Levels() Levels { return halLevels{s} }

// Hardware is the board's planner and profile.
// TRLC-LINKS: REQ-DSO-135
func (s *HALScope) Hardware() Hardware { return halHardware{s} }

// Settings is the front end as the setup file collects and restores it:
// every channel of the Model, its offset always set, any probe factor the
// Model's check takes.
// TRLC-LINKS: REQ-DSO-089
func (s *HALScope) Settings() settings.Analog { return halAnalog{s} }

// Tdivs is the profile's timebase ladder.
// TRLC-LINKS: REQ-DSO-135
func (s *HALScope) Tdivs() []float64 { return s.b.Profile().Timebase.TdivS }

// halEngine is the Model with the owner's mask test (none: the mask
// staging does nothing) and the frequency response measured beside it.
type halEngine struct {
	*instrument.Model
	mask *engine.FrameMask
	bode *engine.Bode
}

// TRLC-LINKS: REQ-DSO-014
func (e halEngine) SetMask(m *engine.Mask) {
	if e.mask != nil {
		e.mask.SetMask(m)
	}
}

// TRLC-LINKS: REQ-DSO-014
func (e halEngine) SetMaskMode(m int) {
	if e.mask != nil {
		e.mask.SetMaskMode(m)
	}
}

// TRLC-LINKS: REQ-DSO-014
func (e halEngine) ClearMaskFails() {
	if e.mask != nil {
		e.mask.ClearMaskFails()
	}
}

// Snapshot is the Model's with the mask test's counts and the frequency
// response's state.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-014, REQ-DSO-066
func (e halEngine) Snapshot() instrument.Stats {
	st := e.Model.Snapshot()
	if e.bode != nil {
		e.bode.FillStats(&st)
	}
	if e.mask != nil {
		k := e.mask.Counts()
		st.MaskMode, st.MaskPass, st.MaskFail, st.MaskSkip = k.Mode, int64(k.Pass), int64(k.Fail), int64(k.Skip) //nolint:gosec // G115: frame counts
		st.MaskRing, st.MaskStopped, st.MaskSet, st.MaskGen = k.Ring, k.Stopped, k.Set, k.Gen
	}
	return st
}

// TrigTypes are the trigger types with whether the board runs them.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-011
func (e halEngine) TrigTypes() []instrument.TypeAvail { return e.Model.TrigTypes() }

// channel is channel ch of the wanted state, ok false past the scope's.
// TRLC-LINKS: REQ-DSO-135
func (s *HALScope) channel(ch int) (hal.Channel, bool) {
	st := s.m.State()
	if ch < 0 || ch >= len(st.Channels) {
		return hal.Channel{}, false
	}
	return st.Channels[ch], true
}

// setChannel stages an edit of channel ch.
// TRLC-LINKS: REQ-DSO-135
func (s *HALScope) setChannel(ch int, fn func(*hal.Channel)) error {
	return s.m.Change(func(st *instrument.State) error {
		if ch >= 0 && ch < len(st.Channels) {
			fn(&st.Channels[ch])
		}
		return nil
	})
}

// probeOf is a channel's probe factor (at least 1).
// TRLC-LINKS: REQ-DSO-135
func probeOf(c hal.Channel) float64 { return max(c.Probe, 1) }

// halAnalog is the front end at the BNC.
type halAnalog struct{ s *HALScope }

// Channels is the scope's channel count: the instrument state's.
// TRLC-LINKS: REQ-DSO-135
func (a halAnalog) Channels() int { return len(a.s.m.State().Channels) }

// ChannelOn reports whether channel ch is on in the instrument state.
// TRLC-LINKS: REQ-DSO-135
func (a halAnalog) ChannelOn(ch int) bool {
	c, ok := a.s.channel(ch)
	return ok && c.On
}

// SetChannelOn switches channel ch on or off in the instrument state.
// TRLC-LINKS: REQ-DSO-135
func (a halAnalog) SetChannelOn(ch int, on bool) {
	_ = a.s.setChannel(ch, func(c *hal.Channel) { c.On = on })
}

// Ladder is channel ch's ladder at the probe tip for the probe the Model
// holds (the board's ladder is for the probe it has applied).
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) Ladder(ch int) []hal.Step {
	c, ok := a.s.channel(ch)
	if !ok {
		return nil
	}
	fe := a.s.b.FrontEnd()
	k := probeOf(c) / probeOf(fe.Get(ch))
	steps := fe.Ladder(ch)
	out := make([]hal.Step, len(steps))
	for i, st := range steps {
		out[i] = hal.Step{VoltsDiv: st.VoltsDiv * k, Zoom: max(st.Zoom, 1)}
	}
	return out
}

// step is the ladder's step nearest v volts a division at the tip.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) step(ch int, tip float64) (hal.Step, bool) {
	var best hal.Step
	found := false
	for _, st := range a.Ladder(ch) {
		if !found || math.Abs(math.Log(st.VoltsDiv/tip)) < math.Abs(math.Log(best.VoltsDiv/tip)) {
			best, found = st, true
		}
	}
	return best, found
}

// SetVdivV puts channel ch on the ladder's step at v volts a division at
// the BNC.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) SetVdivV(ch int, v float64) error {
	c, ok := a.s.channel(ch)
	if !ok || !(v > 0) {
		return hal.Refuse("V/div", "no such channel or step")
	}
	st, ok := a.step(ch, v*probeOf(c))
	if !ok {
		return hal.Refuse("V/div", "the front end has no ladder")
	}
	return a.s.setChannel(ch, func(c *hal.Channel) { c.VoltsDiv = st.VoltsDiv })
}

// VdivV is channel ch's V/div at the BNC.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) VdivV(ch int) float64 {
	c, ok := a.s.channel(ch)
	if !ok || c.VoltsDiv <= 0 {
		return 1
	}
	return c.VoltsDiv / probeOf(c)
}

// RangeV is the analog range's V/div at the BNC: a zoomed step's coarser
// range.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) RangeV(ch int) float64 {
	c, _ := a.s.channel(ch)
	if st, ok := a.step(ch, c.VoltsDiv); ok {
		return a.VdivV(ch) * st.Zoom
	}
	return a.VdivV(ch)
}

// OffsetReqV is channel ch's offset at the BNC.
// TRLC-LINKS: REQ-DSO-095
func (a halAnalog) OffsetReqV(ch int) float64 {
	c, _ := a.s.channel(ch)
	return c.OffsetV / probeOf(c)
}

// OffsetRangeV is the offset's reach at the BNC.
// TRLC-LINKS: REQ-DSO-095
func (a halAnalog) OffsetRangeV(ch int) float64 {
	c, ok := a.s.channel(ch)
	if !ok {
		return 0
	}
	g := a.s.b.FrontEnd().OffsetGrid(ch)
	return math.Max(math.Abs(g.Min), math.Abs(g.Max)) / probeOf(c)
}

// SetCoupling sets channel ch's coupling (dsp's numbers are the HAL's).
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) SetCoupling(ch, mode int) error {
	return a.s.setChannel(ch, func(c *hal.Channel) { c.Coupling = hal.Coupling(mode) })
}

// Coupling is channel ch's coupling.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) Coupling(ch int) int {
	c, _ := a.s.channel(ch)
	return int(c.Coupling)
}

// SetProbe sets channel ch's probe factor; the V/div and offset at the BNC
// stay.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) SetProbe(ch int, x float64) {
	x = max(x, 1)
	_ = a.s.setChannel(ch, func(c *hal.Channel) {
		k := x / probeOf(*c)
		c.Probe, c.VoltsDiv, c.OffsetV = x, c.VoltsDiv*k, c.OffsetV*k
	})
}

// ProbeFactor is channel ch's probe factor.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) ProbeFactor(ch int) float64 {
	c, _ := a.s.channel(ch)
	return probeOf(c)
}

// SetBWL switches channel ch's 20 MHz bandwidth limit.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) SetBWL(ch int, on bool) error {
	return a.s.setChannel(ch, func(c *hal.Channel) {
		c.BWLimit = 0
		if on {
			c.BWLimit = 20e6
		}
	})
}

// BWL reports channel ch's bandwidth limit.
// TRLC-LINKS: REQ-DSO-015
func (a halAnalog) BWL(ch int) bool {
	c, _ := a.s.channel(ch)
	return c.BWLimit > 0
}

// Emitted is true: the Model's front end is the state the board commits,
// never a seed kept unwritten.
// TRLC-LINKS: REQ-DSO-089
func (halAnalog) Emitted() bool { return true }

// OffsetSet is true: the Model always holds an offset.
// TRLC-LINKS: REQ-DSO-089
func (halAnalog) OffsetSet(int) bool { return true }

// SetOffsetV stages channel ch's offset at v volts at the BNC.
// TRLC-LINKS: REQ-DSO-095
func (a halAnalog) SetOffsetV(ch int, v float64) { halLevels(a).SetOffset(ch, v) }

// ProbeOK takes any factor of at least 1: the Model's check refuses what
// the board does not offer.
// TRLC-LINKS: REQ-DSO-089
func (halAnalog) ProbeOK(x float64) bool { return x >= 1 }

// halLevels are the trigger level (at the tip) and the offsets (at the
// BNC) in the Model.
type halLevels struct{ s *HALScope }

// TRLC-LINKS: REQ-DSO-098
func (l halLevels) grid(src int) hal.Grid {
	return l.s.b.Trigger().LevelGrid(hal.Source{Kind: hal.Analog, Index: src})
}

// SetTrigLevel puts the level at v volts at the tip, on the board's grid.
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) SetTrigLevel(v float64, src int) float64 {
	v = l.grid(src).Snap(v)
	_ = l.s.m.Change(func(st *instrument.State) error { return st.Trigger.SetLevel(v, len(st.Channels)) })
	return v
}

// TrigLevelOrIn is the Model's level: it always holds one.
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) TrigLevelOrIn(instrument.Stats, float64, int) float64 {
	return l.s.m.State().Trigger.LevelV()
}

// StepTrigLevel moves the level n grid steps (positive raises it).
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) StepTrigLevel(n, src int) float64 {
	g := l.grid(src)
	return l.SetTrigLevel(l.s.m.State().Trigger.LevelV()+float64(n)*g.Step, src)
}

// TrigLevelGrid is the board's level grid at the tip.
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) TrigLevelGrid(src int) hal.Grid { return l.grid(src) }

// TrigStepsV is n grid steps of C1's level at the BNC.
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) TrigStepsV(n int) float64 {
	c, _ := l.s.channel(0)
	return float64(n) * l.grid(0).Step / probeOf(c)
}

// SaveTrigLevel keeps the level's volts.
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) SaveTrigLevel() instrument.TrigLevelSaved {
	return instrument.TrigLevelSaved{Word: math.Float64bits(l.s.m.State().Trigger.LevelV()) + 1}
}

// SaveTrigLevelIn is SaveTrigLevel: the Model is the state.
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) SaveTrigLevelIn(instrument.Stats) instrument.TrigLevelSaved {
	return l.SaveTrigLevel()
}

// RestoreTrigLevel puts a kept level back.
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) RestoreTrigLevel(s instrument.TrigLevelSaved) {
	if s.Set() {
		v := math.Float64frombits(s.Word - 1)
		_ = l.s.m.Change(func(st *instrument.State) error { return st.Trigger.SetLevel(v, len(st.Channels)) })
	}
}

// ResetTrigLevel is the default setup's level, 0 V.
// TRLC-LINKS: REQ-DSO-098
func (l halLevels) ResetTrigLevel() {
	_ = l.s.m.Change(func(st *instrument.State) error { return st.Trigger.SetLevel(0, len(st.Channels)) })
}

// LevelWordV is 0: a HAL scope's setups keep the level in volts.
// TRLC-LINKS: REQ-DSO-098
func (halLevels) LevelWordV(int) float64 { return 0 }

// SetOffset stages channel ch's offset at v volts at the BNC.
// TRLC-LINKS: REQ-DSO-095
func (l halLevels) SetOffset(ch int, v float64) float64 {
	_ = l.s.setChannel(ch, func(c *hal.Channel) { c.OffsetV = v * probeOf(*c) })
	return v
}

// OffsetIn is channel ch's offset at the BNC.
// TRLC-LINKS: REQ-DSO-095
func (l halLevels) OffsetIn(ch int, _ instrument.Stats) float64 {
	return halAnalog(l).OffsetReqV(ch)
}

// OffsetStagedIn is OffsetIn: what is staged is the Model.
// TRLC-LINKS: REQ-DSO-095
func (l halLevels) OffsetStagedIn(ch int, st instrument.Stats) float64 { return l.OffsetIn(ch, st) }

// halHardware is the board's planner and profile.
type halHardware struct{ s *HALScope }

// Record is the planner's record at tdivS for every channel the Model has
// on; the decimation counts the full rate's samples a point stands for.
// TRLC-LINKS: REQ-DSO-214
func (h halHardware) Record(tdivS float64) (float64, int) {
	st := h.s.m.State()
	st.TimeDivS = tdivS
	p, err := h.s.b.Planner().Plan(st.Request())
	if err != nil || !(p.SampleS > 0) {
		return 0, 0
	}
	full := p.SampleS
	if fast, err := h.s.b.Planner().Plan(withTdiv(st, 1e-9).Request()); err == nil && fast.SampleS > 0 {
		full = fast.SampleS
	}
	return p.SampleS, max(0, int(math.Round(math.Log2(p.SampleS/full))))
}

// withTdiv is s at timebase t.
// TRLC-LINKS: REQ-DSO-214
func withTdiv(s instrument.State, t float64) instrument.State {
	s.TimeDivS = t
	return s
}

// RollMinTdivS is the profile's roll timebase.
// TRLC-LINKS: REQ-DSO-214
func (h halHardware) RollMinTdivS() float64 {
	if r := h.s.b.Profile().Timebase.RollFromS; r > 0 {
		return r
	}
	return math.Inf(1)
}

// SearchSamples is the screen's points a bus search reads.
// TRLC-LINKS: REQ-DSO-214
func (halHardware) SearchSamples() int { return 32768 }

// EnvelopePoints is a live envelope's points.
// TRLC-LINKS: REQ-DSO-214
func (halHardware) EnvelopePoints() int { return 2048 }

// StreamMode is "": a HAL scope streams no decoded bus yet.
// TRLC-LINKS: REQ-DSO-018
func (halHardware) StreamMode(int, int, bool) string { return "" }

// Product is the firmware line.
// TRLC-LINKS: REQ-DSO-212
func (h halHardware) Product() string { return h.s.product }

// TrigTypes are the trigger types the instrument state holds, with whether
// the board runs them: the TRIG tab's Type row lists those that run.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-011
func (h halHardware) TrigTypes() []instrument.TypeAvail { return h.s.m.TrigTypes() }
