// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// The audio stream on a board with a gap-free stream of sample codes
// (hal.Streamer, PayloadCodes): while a SOUND consumer holds the lease (the
// scope's SOUND view, a page listening), the owner takes the acquisition
// for the board's stream at its fastest stream rate and keeps the newest
// 2^SoundRingLog2 samples of two lanes (the first two channels on) in a
// ring the consumers copy from, as the SDS1000CML+ does from its stream
// image. It is gap-free: a block the board lost, or a restart (a setting
// committed, a STOP and RUN), is a gap the ring records, so playback and
// analysis start afresh there. Captures stop while it runs: the stream owns
// the board's transfer path (ADR-HAL-STREAM-GOROUTINE). The board marks no
// level crossings: SOUND's marks and takes are the stream's own.

// soundBlockWait bounds one pass's wait for the stream's next block: the
// owner's commit point comes round at least this often.
const soundBlockWait = 50 * time.Millisecond

// halSound is the cycle's audio stream.
type halSound struct {
	lease atomic.Int64 // unix nanoseconds the lease runs to
	ring  *SoundRing

	mu   sync.Mutex
	info SoundInfo

	// owner only
	st      hal.Streamer
	running bool
	gap     bool          // the next block follows a gap
	chans   []int         // the scope channels the lanes carry
	scale   SoundScale    // the lanes' scale at the stream's start
	q       [2][]uint16   // a block's lanes in Q8.8
	last    *sample.Frame // the newest frame read, whose traces give the lanes' scale
	retryAt time.Time
	rate    float64 // the stream's samples a second
}

// soundStreamer is the board's stream of sample codes, false when it has
// none.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) soundStreamer() (hal.Streamer, bool) {
	st, ok := c.b.Streamer()
	if !ok || !slices.Contains(st.Kinds(), hal.PayloadCodes) {
		return nil, false
	}
	return st, true
}

// Streams reports a board whose stream carries SOUND.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) Streams() bool {
	_, ok := c.soundStreamer()
	return ok
}

// LeaseSound keeps the audio stream running for d from now (0 releases).
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) LeaseSound(d time.Duration) {
	if d <= 0 {
		c.snd.lease.Store(0)
		return
	}
	c.snd.lease.Store(time.Now().Add(min(d, SoundLeaseMax)).UnixNano())
}

// soundWanted reports a current lease.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) soundWanted() bool { return time.Now().UnixNano() < c.snd.lease.Load() }

// SoundInfo reports the audio stream's state.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) SoundInfo() SoundInfo {
	s := &c.snd
	s.mu.Lock()
	in := s.info
	s.mu.Unlock()
	if in.State == "" {
		in.State = "off"
	}
	if r := s.ring; r != nil {
		r.Mu.Lock()
		in.Next, in.Oldest, in.LastData = r.Next, r.OldestLocked(), r.Last
		r.Mu.Unlock()
	}
	return in
}

// SoundRead copies up to limit samples of each lane from ordinal from (or
// the oldest held) into q1, q2.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) SoundRead(from uint64, limit int, q1, q2 []uint16) SoundBlock {
	return c.soundRing().Read(from, limit, q1, q2, c.SoundInfo().RateHz)
}

// SoundLatest copies the newest n samples of each lane.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) SoundLatest(n int, q1, q2 []uint16) SoundBlock {
	return c.soundRing().Latest(n, q1, q2, c.SoundInfo().RateHz)
}

// SoundWait parks until the ring holds samples past after (or timeout) and
// returns the next ordinal.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) SoundWait(after uint64, timeout time.Duration) uint64 {
	return c.soundRing().Wait(after, timeout)
}

// SetSoundMarks is refused in silence: the board's stream marks no level
// crossings (SoundInfo.Marks stays false).
// TRLC-LINKS: REQ-DSO-272
func (c *HALCycle) SetSoundMarks(SoundMarkConfig) {}

// SoundMarksSince is none: the board's stream marks no level crossings.
// TRLC-LINKS: REQ-DSO-272
func (c *HALCycle) SoundMarksSince(uint64) []SoundMark { return nil }

// soundRing is the ring, made on first use.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) soundRing() *SoundRing {
	s := &c.snd
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ring == nil {
		s.ring = NewSoundRing(SoundRingLog2)
	}
	return s.ring
}

// setSound publishes the stream's state.
// TRLC-LINKS: REQ-DSO-265
func (s *halSound) set(fn func(in *SoundInfo)) {
	s.mu.Lock()
	fn(&s.info)
	s.mu.Unlock()
}

// soundStep runs the audio stream one step on the owner; false when no
// lease holds (the stream stopped) and the pass goes on with captures.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) soundStep(ctx context.Context) bool {
	s := &c.snd
	if !c.soundWanted() {
		if s.running {
			c.soundStop()
		}
		if s.info.State != "" && s.info.State != "off" {
			s.set(func(in *SoundInfo) { in.State, in.Active, in.Mode = "off", false, "" })
			c.soundRing().Release()
		}
		return false
	}
	st, ok := c.soundStreamer()
	if !ok {
		s.set(func(in *SoundInfo) { in.State, in.Active = c.b.Profile().AbsentReason("streamer"), false })
		return false
	}
	s.st = st
	want, gen := c.inst.Wanted()
	if gen != c.gen || c.inst.HasWork() || (s.running && !want.Running) {
		if s.running {
			c.soundStop() // a commit, posted work or a STOP: the stream starts again after it
		}
		return false // the pass commits the state first
	}
	if !want.Running {
		s.set(func(in *SoundInfo) { in.Mode = "stopped" })
		c.idle(ctx)
		return true
	}
	if !s.running {
		if time.Now().Before(s.retryAt) {
			c.idle(ctx)
			return true
		}
		if err := c.soundStart(); err != nil {
			c.cfg.Logf("sound: %v", err)
			s.set(func(in *SoundInfo) { in.State, in.Active, in.Err = "error", false, err.Error() })
			s.retryAt = time.Now().Add(time.Second)
			c.gen = 0 // the state is committed again for the captures
			return true
		}
	}
	c.soundDrain(ctx)
	return true
}

// soundStart stages the fastest stream plan of the committed state, starts
// the board's stream and opens the ring at a gap.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) soundStart() error {
	s := &c.snd
	s.set(func(in *SoundInfo) { in.State, in.Err = "starting", "" })
	c.stopArmed()
	r, plan, err := c.streamPlan()
	if err != nil {
		return err
	}
	if err := c.b.Acquirer().Configure(plan); err != nil {
		return err
	}
	s.chans = s.chans[:0]
	for _, ch := range r.Channels {
		if len(s.chans) < 2 {
			s.chans = append(s.chans, ch)
		}
	}
	if len(s.chans) == 0 {
		return hal.Refuse("SOUND", "no channel is on")
	}
	s.scale = c.soundScale(s.chans)
	if err := s.st.Start(hal.StreamSpec{Payload: hal.PayloadCodes, Channels: s.chans, SampleS: plan.SampleS}); err != nil {
		return err
	}
	s.running, s.gap = true, true
	s.rate = 0
	if plan.SampleS > 0 {
		s.rate = 1 / plan.SampleS // until a block says what the board delivers
	}
	s.set(func(in *SoundInfo) {
		in.State, in.Active, in.RateHz, in.Started, in.Mode = "streaming", true, s.rate, time.Now(), "live"
	})
	return nil
}

// streamPlan is the committed state's request in the board's stream mode
// at the fastest timebase the board streams at (the profile's roll
// timebase, else the first of its ladder the planner takes), and its plan.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) streamPlan() (hal.Request, hal.Plan, error) {
	return c.streamPlanOf(c.cur.Request())
}

// streamPlanOf is streamPlan of request r. ANY: the planner is pure.
// TRLC-LINKS: REQ-DSO-265, REQ-DSO-018
func (c *HALCycle) streamPlanOf(r hal.Request) (hal.Request, hal.Plan, error) {
	r.Mode = hal.RollMode
	tb := c.b.Profile().Timebase
	tdivs := []float64{tb.RollFromS}
	if tb.RollFromS <= 0 {
		tdivs = slices.Sorted(slices.Values(tb.TdivS))
	}
	var err error
	for _, t := range tdivs {
		r.TimePerDivS = t
		var plan hal.Plan
		if plan, err = c.b.Planner().Plan(r); err == nil {
			return r, plan, nil
		}
	}
	if err == nil {
		err = hal.Refuse("SOUND", "the board names no timebase it streams at")
	}
	return r, hal.Plan{}, err
}

// soundDrain moves the stream's blocks into the ring for up to
// soundBlockWait.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) soundDrain(ctx context.Context) {
	s := &c.snd
	ring := c.soundRing()
	end := time.Now().Add(soundBlockWait)
	for time.Now().Before(end) {
		lctx, cancel := context.WithDeadline(ctx, end)
		b, err := s.st.Lease(lctx)
		cancel()
		if err != nil {
			if ctx.Err() == nil && lctx.Err() == nil {
				c.soundStop() // the stream ended under us: started again, after a gap
				s.set(func(in *SoundInfo) { in.Losses++; in.Err = err.Error() })
			}
			return
		}
		if b.SampleS > 0 && s.rate != 1/b.SampleS {
			s.rate = 1 / b.SampleS // the rate the board delivers at
			s.set(func(in *SoundInfo) { in.RateHz = s.rate })
		}
		s.push(ring, b)
		s.st.Release(b)
	}
	if f := s.st.Faults(); f.LostBlocks+f.SeqJumps > 0 {
		s.set(func(in *SoundInfo) { in.Losses = int(f.LostBlocks + f.SeqJumps) }) //nolint:gosec // G115: counted losses
	}
}

// push puts block b's lanes into the ring as Q8.8 words.
// TRLC-LINKS: REQ-DSO-265
func (s *halSound) push(ring *SoundRing, b hal.StreamBlock) {
	for lane := range 2 {
		s.q[lane] = s.q[lane][:0]
	}
	for i := range min(len(b.Formats), 2) {
		bytes, words := b.Run(i)
		fm := b.Formats[i]
		q := s.q[i]
		for _, v := range bytes {
			q = append(q, fm.Q88(uint32(v)))
		}
		for _, v := range words {
			q = append(q, fm.Q88(uint32(v)))
		}
		s.q[i] = q
	}
	if len(b.Formats) == 1 { // one lane: the second repeats it
		s.q[1] = append(s.q[1], s.q[0]...)
	}
	ring.PushQ(s.q[0], s.q[1], s.scale, s.gap, time.Now())
	s.gap = false
}

// soundStop stops the board's stream; the next start opens at a gap and
// the state is committed again for the captures.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) soundStop() {
	s := &c.snd
	if s.st != nil {
		if err := s.st.Stop(); err != nil {
			c.cfg.Logf("sound: stop: %v", err)
		}
	}
	s.running = false
	s.set(func(in *SoundInfo) { in.Active = false })
	c.gen = 0
}

// soundScale is the lanes' scale for channels chans: from the newest frame
// read when it holds them at the committed V/div, else from the committed
// channel settings over the profile's code format.
// TRLC-LINKS: REQ-DSO-265
func (c *HALCycle) soundScale(chans []int) SoundScale {
	sc := SoundScale{}
	if len(chans) != 2 || chans[0] != 0 || chans[1] != 1 {
		sc.Chans = slices.Clone(chans)
	}
	for lane, ch := range chans {
		if ch < 0 || ch >= len(c.cur.Channels) {
			continue
		}
		hc := c.cur.Channels[ch]
		fm := c.b.Profile().Format
		scale := sample.Scale{Zero: float64(fm.Mid), PerCode: 0, OffsetV: hc.OffsetV}
		if f := c.snd.last; f != nil {
			for i := range f.Traces {
				if t := &f.Traces[i]; t.Ch == ch && t.CodesPerDiv > 0 {
					fm = t.Format
					scale = t.Scale
					if p := hc.VoltsDiv / t.CodesPerDiv; scale.PerCode != p {
						scale = sample.Scale{Zero: float64(fm.Mid), PerCode: p, OffsetV: hc.OffsetV}
					}
				}
			}
		}
		if !(scale.PerCode > 0) {
			continue
		}
		// a Q8.8 code q of the lane reads (q/256-128)·VdivV/25 - OffV volts
		// at the analog input: the probe's factor out of the tip's scale
		probe := hc.Probe
		if !(probe > 0) {
			probe = 1
		}
		sc.VdivV[lane] = 25 * scale.PerCode * fm.Unit8() / probe
		sc.OffV[lane] = (scale.OffsetV - (float64(fm.Mid)-scale.Zero)*scale.PerCode) / probe
	}
	return sc
}
