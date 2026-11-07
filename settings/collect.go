// ENGMODEL-OWNER-UNIT: FU-DSO-SETTINGS

package settings

import (
	"errors"
	"math"

	"github.com/labeth/open-dso-core/dsp"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
)

// Engine is the instrument's controls the setup flows through: the same
// calls the panel, the web and SCPI make, each clamping and staging.
type Engine = instrument.Controls

// Analog is the vertical front end at the BNC. It may be nil (a scope
// without it neither collects nor restores the channels).
//
// Optional: Channels() int (the channels it has; 2 without), BWL(ch) and
// SetBWL(ch, on) (the 20 MHz limit), ChannelState(ch) (the offset,
// coupling, probe and limit under one lock), OffsetSet(ch) (an offset was
// set; without it Stats' offset words say so), ProbeOK(x) (a probe factor
// it takes; without it 1, 10 and 100).
type Analog interface {
	// Emitted reports that the front end was driven: false keeps a
	// scope's boot rule of a seeded, never written front end, so the
	// file's V/div is not restored over it.
	Emitted() bool
	VdivV(ch int) float64
	SetVdivV(ch int, v float64) error
	SetOffsetV(ch int, volts float64)
	OffsetReqV(ch int) float64
	SetCoupling(ch, mode int) error
	Coupling(ch int) int
	SetProbe(ch int, x float64)
	ProbeFactor(ch int) float64
}

// Extension is the board's part of the setup: the trigger level as the
// board keeps it (the SDS1000CML+'s comparator DAC code in level_code, a
// HAL scope's volts in level_v). It may be nil: the level is then neither
// collected nor restored.
type Extension interface {
	CollectTrigger(t *Trigger)
	ApplyTrigger(t Trigger)
}

// Panel is the UI model's slice of the setup (*panel.Controller).
// ApplySettingsView keeps the domains its tabs keep. It may be nil.
type Panel interface {
	SettingsView() ViewState
	ApplySettingsView(v ViewState)
}

// channelsOf is the channels fe has, at least 2 and at most MaxChannels.
// TRLC-LINKS: REQ-DSO-089
func channelsOf(fe Analog) int {
	if c, ok := fe.(interface{ Channels() int }); ok {
		return max(2, min(c.Channels(), MaxChannels))
	}
	return 2
}

// channel is the setup's channel ch.
// TRLC-LINKS: REQ-DSO-089
func (s *Settings) channel(ch int) *Channel {
	if ch < 2 {
		return &s.Ch[ch]
	}
	return &s.ChMore[ch-2]
}

// Collect assembles the current setup from its owners: the controls'
// snapshot, the front end and the UI model. Cheap (copies under locks, no
// hardware access): safe on the saver's goroutine.
// TRLC-LINKS: REQ-DSO-089
func Collect(eng Engine, fe Analog, pc Panel, ext Extension) Settings {
	s := Settings{Version: Version}
	var st instrument.Stats
	if eng != nil {
		st = eng.Snapshot()
		s.TdivS = st.TdivSet()
		s.Trigger = Trigger{
			Qualifiers: st.TrigQual,
			Rising:     st.TrigRising,
			Source:     st.TrigSource,
			Type:       st.TrigType,
			Norm:       st.Norm,
			HoldoffS:   st.HoldoffS,
			PosFrac:    st.TrigPosFrac,
		}
		if ext != nil {
			ext.CollectTrigger(&s.Trigger)
		}
		s.Acq = Acq{Mode: st.AcqMode, AvgCount: st.AvgCount, EresLen: st.EresLen, PrecisionRateHz: st.PrecisionRateHz}
		if md, ok := eng.(interface{ MemDepth() int }); ok {
			s.Acq.MemDepth = md.MemDepth()
		}
		s.Acq.RollStream = st.RollStream
		// OffC1/OffC2 are a board's staged offset words; 0 is the boot's
		// offset never touched, which a restore must not overwrite with an
		// explicit 0 V.
		s.Ch[0].OffsetSet = st.OffC1 != 0
		s.Ch[1].OffsetSet = st.OffC2 != 0
		if _, ok := eng.(interface{ SetBodeMode(bool, int, int) }); ok {
			s.Bode = Bode{Set: true, Ref: st.BodeRef & 1, Dut: st.BodeDut & 1}
		}
	}
	if fe != nil {
		collectChannels(&s, fe)
	}
	if pc != nil {
		v := pc.SettingsView()
		s.ViewMode = v.ViewMode
		s.Decode = v.Decode
		s.Display = v.Display
		s.UI = v.UI
		s.Serial = v.Serial
		s.Screen = v.Screen
		s.Sound = v.Sound
	}
	return s
}

// collectChannels reads every channel's vertical setup from fe.
// TRLC-LINKS: REQ-DSO-089
func collectChannels(s *Settings, fe Analog) {
	s.VertSet = fe.Emitted()
	bwl, hasBWL := fe.(interface{ BWL(ch int) bool })
	// One lock a channel where the front end reads them together.
	all, hasAll := fe.(interface {
		ChannelState(ch int) (offV float64, cpl int, probe float64, bwl bool)
	})
	set, hasSet := fe.(interface{ OffsetSet(ch int) bool })
	for ch := range channelsOf(fe) {
		c := s.channel(ch)
		c.VdivV = fe.VdivV(ch)
		if hasSet {
			c.OffsetSet = set.OffsetSet(ch)
		}
		if hasAll {
			c.OffsetV, c.Coupling, c.Probe, c.BWL = all.ChannelState(ch)
			continue
		}
		c.OffsetV = fe.OffsetReqV(ch)
		c.Coupling = fe.Coupling(ch)
		c.Probe = fe.ProbeFactor(ch)
		if hasBWL {
			c.BWL = bwl.BWL(ch)
		}
	}
}

// Apply restores a setup through the owning setters, the same paths the
// panel and the web use, so every clamp and side effect (staging, offset
// re-anchoring, the trigger's map) applies. Out-of-domain values are
// clamped by the setters or skipped with a log line; Apply never panics on
// a hostile Settings value.
// TRLC-LINKS: REQ-DSO-072
func Apply(s Settings, eng Engine, fe Analog, pc Panel, ext Extension, logf func(string, ...any)) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if eng != nil {
		applyEngine(s, eng, ext, logf)
	}
	if fe != nil {
		applyChannels(s, fe, logf)
	}
	if b, ok := eng.(interface{ SetBodeMode(bool, int, int) }); ok && s.Bode.Set {
		ref, dut := s.Bode.Ref&1, s.Bode.Dut&1
		if ref == dut {
			dut = 1 - ref
		}
		b.SetBodeMode(eng.Snapshot().BodeMode != 0, ref, dut) // the armed state stays as it is
	}
	if pc != nil {
		pc.ApplySettingsView(ViewState{
			ViewMode: s.ViewMode, Decode: s.Decode, Display: s.Display, UI: s.UI, Serial: s.Serial,
			Screen: s.Screen, Sound: s.Sound,
		})
	}
}

// applyEngine restores the acquisition and trigger settings.
// TRLC-LINKS: REQ-DSO-072
func applyEngine(s Settings, eng Engine, ext Extension, logf func(string, ...any)) {
	if finite(s.TdivS) && s.TdivS > 0 {
		if _, ok := eng.SetTdiv(s.TdivS); !ok {
			logf("settings: tdiv %g not in ladder — skipped", s.TdivS)
		}
	}
	eng.SetNorm(s.Trigger.Norm)
	eng.SetTrigSlope(s.Trigger.Rising)
	eng.SetTrigSource(s.Trigger.Source) // clamps to the scope's channels
	eng.SetTrigType(s.Trigger.Type)     // clamps to edge on out-of-range
	if ext != nil {
		ext.ApplyTrigger(s.Trigger)
	}
	if p := s.Trigger.PosFrac; finite(p) && p > 0 && p <= 1 {
		eng.SetTrigPosFrac(p)
	}
	if s.Acq.MemDepth > 0 {
		eng.SetMemDepth(s.Acq.MemDepth) // clamps to the backend's range
	}
	if finite(s.Trigger.HoldoffS) {
		eng.SetHoldoff(s.Trigger.HoldoffS) // clamps [0, 10] s
	}
	if rate, ok := eng.(interface{ SetPrecisionRate(float64) float64 }); ok && finite(s.Acq.PrecisionRateHz) && s.Acq.PrecisionRateHz > 0 {
		rate.SetPrecisionRate(s.Acq.PrecisionRateHz)
	}
	applyQualifiers(s.Trigger.Qualifiers, eng)
	if rs, ok := eng.(interface{ SetRollStream(bool) }); ok {
		rs.SetRollStream(s.Acq.RollStream)
	}
	eng.SetAcqMode(s.Acq.Mode) // clamps to normal on out-of-range
	if s.Acq.AvgCount > 0 {
		eng.SetAvgCount(s.Acq.AvgCount) // clamps [1, 256]
	}
	if s.Acq.EresLen > 0 {
		eng.SetEresLen(s.Acq.EresLen) // clamps [1, 64] odd
	}
}

// applyQualifiers restores the pulse, slope and video trigger qualifiers.
// A missing qualifier block in an older file has PulseLvl 0.
// TRLC-LINKS: REQ-DSO-072
func applyQualifiers(q instrument.TrigQual, eng Engine) {
	if q.PulseLvl <= 0 {
		return
	}
	if finite(q.PulseLvl) && finite(q.PulseMinNs) && finite(q.PulseMaxNs) && q.PulseMinNs >= 0 && q.PulseMaxNs >= 0 {
		eng.SetPulseParams(q.PulseLvl, q.PulseMinNs, q.PulseMaxNs, q.PulseCond)
	}
	if finite(q.SlopeLo) && finite(q.SlopeHi) && finite(q.SlopeMinNs) && finite(q.SlopeMaxNs) && q.SlopeMinNs >= 0 && q.SlopeMaxNs >= 0 {
		eng.SetSlopeParams(q.SlopeLo, q.SlopeHi, q.SlopeMinNs, q.SlopeMaxNs, q.SlopeCond)
	}
	eng.SetVideoParams(q.VideoStd, q.VideoLine, q.VideoNeg)
}

// probeOK reports a probe factor fe takes: 1, 10 and 100 unless it says.
// TRLC-LINKS: REQ-DSO-072
func probeOK(fe Analog, x float64) bool {
	if p, ok := fe.(interface{ ProbeOK(float64) bool }); ok {
		return finite(x) && p.ProbeOK(x)
	}
	return x == 1 || x == 10 || x == 100 // the domain the web keeps
}

// applyChannels restores each channel's vertical settings. A channel past
// C2 the file never saved (V/div 0) is left as it is.
// TRLC-LINKS: REQ-DSO-072
func applyChannels(s Settings, fe Analog, logf func(string, ...any)) {
	setBWL, hasBWL := fe.(interface{ SetBWL(ch int, on bool) error })
	for ch := range channelsOf(fe) {
		cs := *s.channel(ch)
		if ch >= 2 && cs.VdivV == 0 {
			continue
		}
		// V/div only when the saved front end had been driven: restoring
		// boot defaults onto a front end never written would break a
		// scope's seed-don't-emit start rule for no gain. Coupling and
		// probe are software and always safe.
		if s.VertSet {
			var refused *hal.Refusal
			switch err := fe.SetVdivV(ch, cs.VdivV); {
			case errors.As(err, &refused):
				logf("settings: C%d vdiv %g not in ladder — skipped", ch+1, cs.VdivV)
			case err != nil:
				logf("settings: SetVdiv C%d: %v", ch+1, err)
			}
		}
		if cs.OffsetSet && finite(cs.OffsetV) {
			fe.SetOffsetV(ch, cs.OffsetV) // the front end's offset law clamps
		}
		if cs.Coupling >= dsp.CoupleDC && cs.Coupling <= dsp.CoupleGND {
			_ = fe.SetCoupling(ch, cs.Coupling)
		}
		if probeOK(fe, cs.Probe) {
			fe.SetProbe(ch, cs.Probe)
		}
		if hasBWL && s.VertSet {
			if err := setBWL.SetBWL(ch, cs.BWL); err != nil {
				logf("settings: BWL C%d: %v", ch+1, err)
			}
		}
	}
}

// finite rejects NaN and ±Inf before a value reaches float-to-code math
// (encoding/json cannot produce them, but a collected snapshot could carry
// one, and Apply is fuzzed directly).
// TRLC-LINKS: REQ-DSO-072
func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// ModelLevel is the settings extension of a scope whose trigger level is
// volts in the instrument state: level_v, with level_set (0 V is a level).
type ModelLevel struct{ Model *instrument.Model }

var _ Extension = ModelLevel{}

// CollectTrigger writes the level's volts.
// TRLC-LINKS: REQ-DSO-089
func (x ModelLevel) CollectTrigger(t *Trigger) {
	t.LevelV, t.LevelSet = x.Model.State().Trigger.LevelV, true
}

// ApplyTrigger stages a saved level; the board snaps it to its grid when
// it commits.
// TRLC-LINKS: REQ-DSO-072
func (x ModelLevel) ApplyTrigger(t Trigger) {
	if !t.LevelSet || !finite(t.LevelV) {
		return
	}
	_ = x.Model.Change(func(st *instrument.State) error { st.Trigger.LevelV = t.LevelV; return nil })
}
