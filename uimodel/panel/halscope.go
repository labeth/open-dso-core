// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"math"
	"time"

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
	bode    *engine.Bode // the frequency response the owner loop measures (engine.HALConfig.Bode)
	sound   SoundSource  // the owner loop's audio stream; nil: the board streams none
	cycle   CycleSource  // the owner loop's cycle; nil: none attached
}

// SoundSource is the owner loop's audio stream over the board's stream
// (engine.HALCycle on a board with a hal.Streamer of codes).
type SoundSource interface {
	LeaseSound(time.Duration)
	SoundInfo() engine.SoundInfo
	SoundLatest(n int, q1, q2 []uint16) engine.SoundBlock
	SoundRead(from uint64, maxN int, q1, q2 []uint16) engine.SoundBlock
	SetSoundMarks(engine.SoundMarkConfig)
	SoundMarksSince(from uint64) []engine.SoundMark
}

// NewHALScope is the UI model's view of board b through instrument state
// m; product is the firmware line the SYS tab names.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-008
func NewHALScope(m *instrument.Model, b hal.Board, product string) *HALScope {
	return &HALScope{m: m, b: b, product: product, bode: engine.NewBode()}
}

// WithBode gives the UI model the frequency-response sweep the owner
// measures on its captures (engine.HALConfig.Bode).
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-066
func (s *HALScope) WithBode(b *engine.Bode) *HALScope {
	s.bode = b
	return s
}

// WithSound gives the UI model the owner loop's audio stream: SOUND runs
// over the board's stream.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-265
func (s *HALScope) WithSound(x SoundSource) *HALScope {
	s.sound = x
	return s
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
func (s *HALScope) Engine() Engine {
	e := halEngine{Model: s.m, mask: s.mask, bode: s.bode, b: s.b, cycle: s.cycle}
	if s.sound != nil {
		if t, ok := s.sound.(soundTaker); ok {
			return halTakeEngine{halSoundEngine{e, s.sound}, t}
		}
		return halSoundEngine{e, s.sound}
	}
	return e
}

// halSoundEngine is the HAL engine with the owner loop's audio stream.
type halSoundEngine struct {
	halEngine
	SoundSource
}

// halTakeEngine is the HAL engine with the audio stream's takes
// (engine.HALCycle: STOP's take, NORMAL and SINGLE on the board's marks).
type halTakeEngine struct {
	halSoundEngine
	soundTaker
}

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
	mask  *engine.FrameMask
	bode  *engine.Bode
	b     hal.Board
	cycle CycleSource
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

// Snapshot is the Model's with the mask test's counts, the frequency
// response's state, the board's roll-stream preference and what the
// board's converter checks found.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-014, REQ-DSO-066, REQ-DSO-231
func (e halEngine) Snapshot() instrument.Stats {
	st := e.Model.Snapshot()
	if rs, ok := e.b.(RollStreamer); ok {
		st.RollStream = rs.RollStream()
	}
	fillConverterHealth(e.b, &st)
	fillSessionStatus(e.session(), &st)
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

// The records the HAL scope's leases ask for, in samples across the screen:
// as the scope that sets the product's live readout reads them (a bus
// search's screen of SearchSamples, an evenly sampled view of
// sampleViewSamples, a spectrum's of spectrumViewSamples), within the
// board's record.
const (
	sampleViewSamples   = 8192
	spectrumViewSamples = 16384
	searchViewSamples   = 32768
)

// LeaseSampleView asks for evenly sampled records for d (autoset, a bus
// detection's decimated look): a screen of sampleViewSamples.
// TRLC-LINKS: REQ-DSO-214, REQ-DSO-010
func (e halEngine) LeaseSampleView(d time.Duration) {
	e.Lease("sample", instrument.RecordNeed{Samples: sampleViewSamples}, d)
}

// LeaseSearchView asks for the records a bus search reads whole frames of
// slow buses in, for d.
// TRLC-LINKS: REQ-DSO-214, REQ-DSO-010
func (e halEngine) LeaseSearchView(d time.Duration) {
	e.Lease("search", instrument.RecordNeed{Samples: searchViewSamples}, d)
}

// LeaseSpectrumView asks for records a spectrum is taken of, for d.
// TRLC-LINKS: REQ-DSO-070, REQ-DSO-010
func (e halEngine) LeaseSpectrumView(d time.Duration) {
	e.Lease("spectrum", instrument.RecordNeed{Samples: spectrumViewSamples}, d)
}

// LeaseBusSamples asks for records sampled every sampleS at the coarsest
// for a bus decode, for d; sampleS <= 0 ends it.
// TRLC-LINKS: REQ-DSO-214, REQ-DSO-010
func (e halEngine) LeaseBusSamples(sampleS float64, d time.Duration) {
	if !(sampleS > 0) {
		d = 0
	}
	e.Lease("bus", instrument.RecordNeed{SampleS: sampleS}, d)
}

// LeaseRawFor asks for full-rate records for d as holder (the eye, a bus
// search's full-rate look, super-resolution); d <= 0 ends holder's lease.
// TRLC-LINKS: REQ-DSO-215, REQ-DSO-010
func (e halEngine) LeaseRawFor(holder string, d time.Duration) {
	e.Lease("raw "+holder, instrument.RecordNeed{FullRate: true}, d)
}

// LeaseViewWindow asks, for surface sf, for the stretch a..b (screen
// widths from the trigger) of the live frames at full detail, drawn over
// cols columns, for d; d <= 0 ends it.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-021
func (e halEngine) LeaseViewWindow(sf engine.ViewSurface, a, b float64, cols int, d time.Duration) {
	e.LeaseView(int(sf), instrument.ViewWindow{A: a, B: b, Cols: cols}, d)
}

// LeaseDecodeView is the shared full-rate lease.
// TRLC-LINKS: REQ-DSO-215, REQ-DSO-010
func (e halEngine) LeaseDecodeView(d time.Duration) { e.LeaseRawFor("", d) }

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

// Record is the planner's deepest record at tdivS for every channel the
// Model has on (what a full-rate lease reads); the decimation counts the
// full rate's samples a point stands for.
// TRLC-LINKS: REQ-DSO-214
func (h halHardware) Record(tdivS float64) (float64, int) {
	st := withTdiv(h.s.m.State(), tdivS)
	if d := deepestRecord(h.s.b.Profile()); d > st.Depth {
		st.Depth = d
	}
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

// deepestRecord is the deepest record a readout hands the CPU: the
// profile's record depth, within its raw access (0: unknown).
// TRLC-LINKS: REQ-DSO-214
func deepestRecord(p hal.Profile) int {
	d := p.MaxDepth
	if r := p.RawAccess.MaxDepth; r > 0 && (d <= 0 || r < d) {
		d = r
	}
	return d
}

// withTdiv is s at timebase t, the trigger at the same place across the
// screen (a delay of a slower timebase's screen lies past a fast record).
// TRLC-LINKS: REQ-DSO-214
func withTdiv(s instrument.State, t float64) instrument.State {
	if s.TimeDivS > 0 {
		s.DelayS *= t / s.TimeDivS
	}
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

// StreamAbsent is why the board has no gap-free stream ("" when it has
// one): SOUND's audio and BUS's Stream view need it.
// TRLC-LINKS: REQ-DSO-273
func (h halHardware) StreamAbsent() string {
	if _, ok := h.s.b.Streamer(); ok {
		return ""
	}
	return h.s.b.Profile().AbsentReason("streamer")
}

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
