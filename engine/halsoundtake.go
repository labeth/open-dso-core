// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/labeth/open-dso-core/hal"
)

// SOUND's marks and takes on the HAL cycle, as the SDS1000CML+ makes them
// from its stream image (ADR-SOUND). A board whose stream marks level
// crossings in hardware (hal.StreamMarker) flags them in its blocks; they
// go into the ring beside the samples, numbered by the ring's ordinal. RUN
// in the AUTO trigger mode is the live stream. STOP freezes it: the newest
// seconds become a take, the stream stops and its ring goes. In the NORMAL
// mode, and on SINGLE, the stream runs armed on the board's marks of the
// trigger (its source, level and slope, or SOUND's gate): the mark that
// fires is the trigger, and once the post-trigger time has streamed the
// stretch around it becomes the take, the trigger inside it at the
// pre-trigger time. SINGLE then stops; NORMAL arms again after the take
// and refreshes it on each trigger. The board decides every trigger: a mark
// that stands for several crossings since the last block (the hardware
// keeps the newest) is placed on the first crossing of the same level after
// the arm, inside the stretch the board says holds them. A board that marks
// nothing streams live, and its triggered modes say so.

// halTake is the takes' state: atomics for the controllers, the rest the
// owner's.
type halTake struct {
	markCfg SoundMarkConfig // what SetSoundMarks asked (s.mu)

	take     atomic.Pointer[SoundTake]
	preBits  atomic.Uint64 // float64 bits; 0 with postBits 0: the defaults
	postBits atomic.Uint64
	gateCfg  atomic.Pointer[SoundMarkConfig]

	marker   hal.StreamMarker // the board's marks, nil: it marks none
	marksOn  bool             // the running stream marks crossings
	applied  SoundMarkConfig  // the marks it makes now, by lane
	cfgFrom  uint64           // marks from this ordinal on are of applied
	base     uint64           // the ring ordinal of the stream's item 0
	takeSeq  uint64
	frozen   bool // STOPped: the take made, the stream ended
	trigMode bool // NORMAL or SINGLE: the stream runs armed
	armed    bool
	pending  bool   // triggered, the post-trigger time streaming
	armFrom  uint64 // marks from here count
	armAt    uint64 // the first sample a trigger may fall on
	nextArm  uint64 // NORMAL: the next arm after the take
	trigAt   uint64
	rising   bool
	marks    []SoundMark // a block's marks, reused
}

// SetSoundMarks asks the board to mark crossings of a level on a lane
// while no trigger mode runs (the cycle trace, a gate-armed envelope).
// TRLC-LINKS: REQ-DSO-272
func (c *HALCycle) SetSoundMarks(m SoundMarkConfig) {
	m.Ch &= 1
	c.snd.set(func(*SoundInfo) { c.snd.tk.markCfg = m })
}

// SoundMarksSince is the ring's marks from ordinal from on.
// TRLC-LINKS: REQ-DSO-272
func (c *HALCycle) SoundMarksSince(from uint64) []SoundMark {
	r := c.soundRing()
	r.Mu.Lock()
	defer r.Mu.Unlock()
	var out []SoundMark
	for _, m := range r.Marks {
		if m.At >= from {
			out = append(out, m)
		}
	}
	return out
}

// SetSoundTakeTimes sets the pre- and post-trigger seconds of a triggered
// take (bounded).
// TRLC-LINKS: REQ-DSO-275
func (c *HALCycle) SetSoundTakeTimes(pre, post float64) {
	pre, post = SoundTakeTimes(pre, post)
	c.snd.tk.preBits.Store(math.Float64bits(pre))
	c.snd.tk.postBits.Store(math.Float64bits(post))
}

// SetSoundTrigGate makes a take's trigger SOUND's gate (cfg: its lane,
// level and directions) in place of the trigger's settings; on false the
// trigger's again.
// TRLC-LINKS: REQ-DSO-275
func (c *HALCycle) SetSoundTrigGate(cfg SoundMarkConfig, on bool) {
	if !on {
		c.snd.tk.gateCfg.Store(nil)
		return
	}
	cfg.Ch &= 1
	if !cfg.Rising && !cfg.Falling {
		cfg.Rising = true
	}
	c.snd.tk.gateCfg.Store(&cfg)
}

// SoundTake is the take shown (nil: none, the stream is live). It is never
// changed once published: a new take replaces it.
// TRLC-LINKS: REQ-DSO-275
func (c *HALCycle) SoundTake() *SoundTake { return c.snd.tk.take.Load() }

// takeTimes is the pre- and post-trigger seconds.
// TRLC-LINKS: REQ-DSO-275
func (t *halTake) takeTimes() (pre, post float64) {
	pre, post = math.Float64frombits(t.preBits.Load()), math.Float64frombits(t.postBits.Load())
	if pre == 0 && post == 0 {
		return SoundTakeTimes(1, 4)
	}
	return pre, post
}

// soundMarkWant is the marks the stream should make now: the trigger's
// while a triggered mode runs (SOUND's gate, else the edge trigger's as its
// comparator sees it), else what SetSoundMarks asked.
// TRLC-LINKS: REQ-DSO-272, REQ-DSO-275
func (c *HALCycle) soundMarkWant() hal.MarkSpec {
	s := &c.snd
	t := &s.tk
	if t.trigMode {
		if g := t.gateCfg.Load(); g != nil {
			return s.markSpec(*g)
		}
		return hal.MarkSpec{FromTrigger: true}
	}
	s.mu.Lock()
	m := t.markCfg
	s.mu.Unlock()
	return s.markSpec(m)
}

// markSpec is lane mark m as the board's spec on the lane's channel.
// TRLC-LINKS: REQ-DSO-272
func (s *halSound) markSpec(m SoundMarkConfig) hal.MarkSpec {
	ch := m.Ch
	if m.Ch >= 0 && m.Ch < len(s.chans) {
		ch = s.chans[m.Ch]
	}
	return hal.MarkSpec{Channel: ch, Level: uint32(m.Level), Hyst: uint32(m.Hyst), Rise: m.Rising, Fall: m.Falling}
}

// laneMarks is the board's marking as SOUND's lane marks.
// TRLC-LINKS: REQ-DSO-272
func (s *halSound) laneMarks(m hal.MarkSpec) SoundMarkConfig {
	lane := 0
	for i, ch := range s.chans {
		if ch == m.Channel {
			lane = i
		}
	}
	return SoundMarkConfig{Ch: lane, Level: uint8(min(m.Level, 255)), Hyst: uint8(min(m.Hyst, 255)), Rising: m.Rise, Falling: m.Fall}
}

// soundMarksAsk hands the marks wanted to the board.
// TRLC-LINKS: REQ-DSO-272
func (c *HALCycle) soundMarksAsk() {
	if mk := c.snd.tk.marker; mk != nil {
		if err := mk.SetMarks(c.soundMarkWant()); err != nil {
			c.cfg.Logf("sound: marks: %v", err)
		}
	}
}

// soundMarksTake follows the board's marking (a change: marks of the
// configuration before do not count from here) and puts block b's marks
// into the ring. ring.Next is still the block's first ordinal.
// TRLC-LINKS: REQ-DSO-272
func (c *HALCycle) soundMarksTake(ring *SoundRing, b hal.StreamBlock) {
	s := &c.snd
	t := &s.tk
	if t.marker == nil {
		return
	}
	m, on := t.marker.Marking()
	t.marksOn = on
	if lm := s.laneMarks(m); on && lm != t.applied {
		t.applied = lm
		ring.Mu.Lock()
		t.cfgFrom = ring.Next
		ring.Mu.Unlock()
		s.set(func(in *SoundInfo) { in.MarkCfg = lm })
	}
	if len(b.Marks) == 0 {
		return
	}
	t.marks = t.marks[:0]
	for _, mk := range b.Marks {
		if mk.Kind != hal.MarkRise && mk.Kind != hal.MarkFall {
			continue
		}
		n := max(mk.Count, 1)
		t.marks = append(t.marks, SoundMark{At: t.base + mk.At, Ch: s.laneMarks(hal.MarkSpec{Channel: mk.Channel}).Ch, Rising: mk.Kind == hal.MarkRise, N: n})
	}
	ring.AddMarks(t.marks)
}

// soundFreeze is a step while STOPped: on the step that stops, the take (a
// triggered take kept, else the newest seconds), then the stream ends and
// its ring goes; after, nothing.
// TRLC-LINKS: REQ-DSO-275
func (c *HALCycle) soundFreeze() {
	s := &c.snd
	t := &s.tk
	if t.frozen {
		return
	}
	t.frozen = true
	if tk := t.take.Load(); tk == nil || !t.trigMode {
		ring := c.soundRing()
		c.soundPublish(ring.Latest(int(SoundStopTakeS*s.rate), nil, nil, s.rate), "stop", nil, 0)
	}
	t.armed, t.pending = false, false
	if s.running {
		c.soundStop()
	}
	c.soundRing().Release()
	s.set(func(in *SoundInfo) { in.State, in.Active = "stopped", false })
}

// soundPublish makes block b the take: why, and the trigger at trig when
// cfg is not nil.
// TRLC-LINKS: REQ-DSO-275
func (c *HALCycle) soundPublish(b SoundBlock, why string, cfg *SoundMarkConfig, trig uint64) {
	if b.N == 0 {
		return
	}
	t := &c.snd.tk
	t.takeSeq++
	tk := &SoundTake{SoundBlock: b, ID: t.takeSeq, Why: why, At: time.Now()}
	if cfg != nil {
		tk.HasTrig, tk.Trig, tk.TrigCh, tk.TrigRising, tk.TrigLevel = true, trig, cfg.Ch, cfg.Rising, cfg.Level
	}
	t.take.Store(tk)
	c.cfg.Logf("engine: SOUND take %d (%s, %.1f s)", tk.ID, why, float64(b.N)/b.RateHz)
}

// soundTrigStep runs the trigger one step after a drain: arm (once the
// pre-trigger time has streamed), take the board's mark that fires, and
// once the post-trigger time has streamed publish the take; SINGLE then
// stops, NORMAL arms again after it.
// TRLC-LINKS: REQ-DSO-275
func (c *HALCycle) soundTrigStep(single bool) {
	s := &c.snd
	t := &s.tk
	if !t.marksOn {
		s.set(func(in *SoundInfo) { in.Mode = "no edge marks on this board's stream" })
		return
	}
	ring := c.soundRing()
	rate := s.rate
	pre, post := t.takeTimes()
	preN, postN := uint64(pre*rate), uint64(math.Ceil(post*rate))
	cfg := t.applied
	ring.Mu.Lock()
	next := ring.Next
	lastGap := uint64(0)
	if k := len(ring.Gaps); k > 0 {
		lastGap = ring.Gaps[k-1]
	}
	var marks []SoundMark
	if t.armed {
		for _, m := range ring.Marks {
			if m.At >= t.armFrom {
				marks = append(marks, m)
			}
		}
	}
	ring.Mu.Unlock()
	if !t.armed && !t.pending {
		// Arm: marks of this configuration only, after the pre-trigger time
		// has streamed (from the last gap on, the stretch is one signal).
		t.armed = true
		t.armFrom = max(next, t.cfgFrom, t.nextArm)
		t.armAt = max(t.armFrom, lastGap+preN)
	}
	if t.armed {
		t.armAt = max(t.armAt, lastGap+preN) // a gap since: the pre-trigger time again
		if t.cfgFrom > t.armFrom {
			// the level, slope or source changed since the arm: only its marks count
			t.armFrom, t.armAt = t.cfgFrom, max(t.armAt, t.cfgFrom)
		}
		at, rising, found, wait := c.soundTriggerMark(marks, cfg, next)
		switch {
		case wait:
			return // its samples are not here yet
		case !found:
			s.set(func(in *SoundInfo) { in.Mode = "armed" })
			return
		}
		t.armed, t.pending, t.trigAt, t.rising = false, true, at, rising
	}
	if !t.pending {
		return
	}
	s.set(func(in *SoundInfo) { in.Mode = "triggered" })
	if next < t.trigAt+postN {
		return
	}
	t.pending = false
	from := t.trigAt - min(preN, t.trigAt)
	b := ring.Read(from, int(t.trigAt+postN-from), nil, nil, rate) //nolint:gosec // G115: a take of a few seconds of samples
	if len(b.Gaps) > 0 || b.First != from {
		t.nextArm = next // a gap in the stretch: arm again
		return
	}
	why := "normal"
	if single {
		why = "single"
	}
	tc := cfg
	tc.Rising, tc.Falling = t.rising, !t.rising
	c.soundPublish(b, why, &tc, t.trigAt)
	t.nextArm = t.trigAt + postN
	if single {
		c.inst.OwnerStop(true)
		c.want.ok = false
	}
}

// soundTriggerMark finds the first of marks that fires the armed trigger:
// on cfg's lane and slope, at or after the arm. wait reports one whose
// crossing must be found on samples not here yet (before next).
// TRLC-LINKS: REQ-DSO-275
func (c *HALCycle) soundTriggerMark(marks []SoundMark, cfg SoundMarkConfig, next uint64) (at uint64, rising, found, wait bool) {
	t := &c.snd.tk
	prevAt, havePrev := uint64(0), false
	eitherSlope := cfg.Rising && cfg.Falling
	for _, m := range marks {
		if m.Ch != cfg.Ch || m.Rising != cfg.Rising && !eitherSlope {
			continue
		}
		if m.At < t.armAt {
			prevAt, havePrev = m.At, true
			continue
		}
		if m.N <= 1 {
			return m.At, m.Rising, true, false
		}
		// Several crossings since the last read: the board kept the newest;
		// the first after the arm (and after the mark before) is on the
		// samples between.
		lo := t.armAt
		if havePrev && prevAt+1 > lo {
			lo = prevAt + 1
		}
		if m.At >= next {
			return 0, false, false, true
		}
		return c.soundFirstCrossing(lo, m.At, cfg, m.Rising), m.Rising, true, false
	}
	return 0, false, false, false
}

// soundFirstCrossing is the first sample in [lo, hi] at which lane cfg.Ch
// crosses the board's level the way its comparator does (from at or below
// the level less the hysteresis, to at or above it; mirrored falling); hi
// (the board's mark) when the samples show none.
// TRLC-LINKS: REQ-DSO-275
func (c *HALCycle) soundFirstCrossing(lo, hi uint64, cfg SoundMarkConfig, rising bool) uint64 {
	rate := c.snd.rate
	hyst := int(cfg.Hyst)
	if hyst == 0 {
		hyst = 4 // the SDS1000CML+ fabric's default
	}
	lead := uint64(rate / 20) // the state before lo
	from := lo - min(lead, lo)
	b := c.soundRing().Read(from, int(hi-from+1), nil, nil, rate) //nolint:gosec // G115: a take of a few seconds of samples
	q := b.Q1
	if cfg.Ch == 1 {
		q = b.Q2
	}
	if lo >= b.First {
		if i, ok := FirstCrossingFrom(q, int(cfg.Level), hyst, rising, int(lo-b.First)); ok { //nolint:gosec // G115: lo lies within the block read
			return b.First + uint64(i) //nolint:gosec // G115: i is an index of the block
		}
	}
	return hi
}
