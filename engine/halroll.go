// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"slices"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// The roll view on a board with a stream (hal.Streamer: captured chunks
// with held gaps, or a gap-free stream of codes), as the SDS1000CML+ rolls
// (ADR-SRAM-ROLL): at a timebase from the profile's roll_from_s a triggered
// frame waits out its whole screen, so AUTO scrolls a continuous view
// instead. The owner plans the committed state in the board's roll mode at
// its own timebase, starts the board's stream of the channels on and keeps
// the newest screen of samples, publishing a frame of it (newest sample
// last, untriggered, its uncovered samples held) about every
// rollPublishEvery. Roll needs a plain edge trigger in AUTO with nothing
// that wants triggered records: NORMAL, a single shot, averaging, a
// protocol or zone trigger, a mask test, a Bode sweep, a leased record
// need, SOUND or a decoded stream each keep the captures.

// rollPublishEvery paces the roll view's frames: a stream's blocks can
// come faster than the displays draw.
const rollPublishEvery = 40 * time.Millisecond

// halRoll is the cycle's roll view.
type halRoll struct {
	st      hal.Streamer
	running bool
	retryAt time.Time
	plan    hal.Plan
	payload hal.Payload
	chans   []int
	screen  int
	formats []sample.CodeFormat
	codes   [][]uint16 // each channel's newest samples, oldest first (at most 2·screen)
	held    []bool     // the samples no capture covered, as codes
	pubAt   time.Time
	fresh   bool   // samples since the last frame
	refused uint64 // the generation whose roll the board refused: it captures
}

// rollPayload is the board's roll stream payload: captured chunks, else a
// gap-free stream of codes; false when it has neither.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-035
func (c *HALCycle) rollPayload() (hal.Streamer, hal.Payload, bool) {
	st, ok := c.b.Streamer()
	if !ok {
		return nil, 0, false
	}
	k := st.Kinds()
	switch {
	case slices.Contains(k, hal.PayloadChunks):
		return st, hal.PayloadChunks, true
	case slices.Contains(k, hal.PayloadCodes):
		return st, hal.PayloadCodes, true
	}
	return nil, 0, false
}

// rollWanted reports a committed state the cycle rolls: running AUTO at a
// timebase from the profile's roll_from_s on a plain edge trigger, with
// nothing that needs triggered records.
// TRLC-LINKS: REQ-DSO-010
func (c *HALCycle) rollWanted() bool {
	from := c.b.Profile().Timebase.RollFromS
	s := &c.cur
	switch {
	case !(from > 0) || !c.running || s.Single || s.Sweep != hal.Auto || s.TimeDivS < from*0.999:
		return false
	case s.Mode == hal.Average || s.Trigger.Type != instrument.TrigEdge || s.SerialMode != 0 || c.gate != nil:
		return false
	case c.cfg.Mask != nil && c.cfg.Mask.MaskMode() != MaskOff:
		return false
	case c.cfg.Bode != nil && c.cfg.Bode.Armed():
		return false
	case c.need != (instrument.RecordNeed{}) || c.soundWanted() || c.dec.active:
		return false
	case c.roll.refused != 0 && c.roll.refused == c.gen:
		return false
	}
	_, _, ok := c.rollPayload()
	return ok
}

// rollStep runs the roll view one step on the owner; false when the state
// does not roll (the stream stopped) and the pass goes on with captures.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-035
func (c *HALCycle) rollStep(ctx context.Context) bool {
	r := &c.roll
	if _, gen := c.inst.Wanted(); gen != c.gen || c.inst.RecordNeed() != c.need || !c.rollWanted() {
		if r.running {
			c.rollStop() // a commit: the pass commits first; a roll it still wants starts again
		}
		return false
	}
	if !r.running {
		if time.Now().Before(r.retryAt) {
			c.idle(ctx)
			return true
		}
		if err := c.rollStart(); err != nil {
			// the board does not roll this state: it captures until the next commit
			c.cfg.Logf("roll: %v", err)
			r.refused = c.gen
			if err := c.b.Acquirer().Configure(c.plan); err != nil {
				c.cfg.Logf("roll: the capture plan again: %v", err)
			}
			return false
		}
	}
	c.rollDrain(ctx)
	if r.fresh && time.Since(r.pubAt) >= rollPublishEvery {
		c.rollPublish()
	}
	return true
}

// rollStart plans the committed state in the board's roll mode at its own
// timebase and starts the board's stream of the channels on, the view
// empty (every sample held).
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-035
func (c *HALCycle) rollStart() error {
	r := &c.roll
	st, payload, _ := c.rollPayload()
	c.stopArmed()
	req := c.request(c.cur)
	req.Mode = hal.RollMode
	plan, err := c.b.Planner().Plan(req)
	if err != nil {
		return err
	}
	if err := c.b.Acquirer().Configure(plan); err != nil {
		return err
	}
	if len(req.Channels) == 0 {
		return hal.Refuse("roll", "no channel is on")
	}
	if err := st.Start(hal.StreamSpec{Payload: payload, Channels: req.Channels, SampleS: plan.SampleS}); err != nil {
		return err
	}
	r.st, r.payload, r.plan, r.running = st, payload, plan, true
	r.chans = slices.Clone(req.Channels)
	r.screen = max(plan.WinSamples, 2)
	r.formats = r.formats[:0]
	r.codes = r.codes[:0]
	mid := c.b.Profile().Format
	for range r.chans {
		r.formats = append(r.formats, mid)
		r.codes = append(r.codes, slices.Repeat([]uint16{uint16(mid.Mid)}, r.screen)) //nolint:gosec // G115: a code format's mid fits its words
	}
	r.held = slices.Repeat([]bool{true}, r.screen)
	r.fresh, r.pubAt = true, time.Time{}
	c.inst.SetRun(instrument.Rolling)
	return nil
}

// rollDrain moves the stream's blocks into the view for up to
// rollPublishEvery.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-035
func (c *HALCycle) rollDrain(ctx context.Context) {
	r := &c.roll
	end := time.Now().Add(rollPublishEvery)
	for time.Now().Before(end) {
		lctx, cancel := context.WithDeadline(ctx, end)
		b, err := r.st.Lease(lctx)
		cancel()
		if err != nil {
			if ctx.Err() == nil && lctx.Err() == nil {
				c.rollStop() // the stream ended under us: started again
				r.retryAt = time.Now().Add(100 * time.Millisecond)
			}
			return
		}
		r.add(b)
		r.st.Release(b)
		if r.fresh && time.Since(r.pubAt) >= rollPublishEvery {
			return
		}
	}
}

// add appends a block's samples to the view, keeping the newest screen.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-035
func (r *halRoll) add(b hal.StreamBlock) {
	n := 0
	for i := range min(len(b.Formats), len(r.codes)) {
		r.formats[i] = b.Formats[i]
		bytes, words := b.Run(i)
		q := r.codes[i]
		for _, v := range bytes {
			q = append(q, uint16(v))
		}
		q = append(q, words...)
		n = len(bytes) + len(words)
		r.codes[i] = trimTo(q, r.screen)
	}
	if n == 0 {
		return
	}
	h := r.held
	if len(b.Held) == n {
		h = append(h, b.Held...)
	} else {
		h = append(h, make([]bool, n)...)
	}
	r.held = trimTo(h, r.screen)
	r.fresh = true
}

// trimTo keeps s's newest n entries, moving them to the front once s holds
// twice as many (the view's appends cost no copy most of the time).
// TRLC-LINKS: REQ-DSO-010
func trimTo[T any](s []T, n int) []T {
	if len(s) <= 2*n {
		return s
	}
	return s[:copy(s, s[len(s)-n:])]
}

// rollPublish publishes the view's newest screen as a roll frame.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-035
func (c *HALCycle) rollPublish() {
	r := &c.roll
	f := &sample.Frame{
		Content: sample.RawDecimated, Flags: sample.Roll, SampleS: r.plan.SampleS, CaptureS: r.plan.SampleS,
		AcqDecim: max(r.plan.AcqDecim, 1), Depth: r.screen, WinPoints: r.screen, TriggerAt: -1,
		TrigKind: sample.Untriggered, Status: sample.Status{Coherent: true, Done: true},
		Snapshot: sample.Settings{TdivS: c.cur.TimeDivS, DisplayedS: c.cur.TimeDivS},
	}
	if r.plan.Content.Has(sample.PeakPairs) && r.plan.AcqDecim > 1 {
		f.Content = sample.PeakPairs
	}
	held := slices.Clone(r.held[len(r.held)-r.screen:])
	for i, ch := range r.chans {
		fm := r.formats[i]
		codes := r.codes[i][len(r.codes[i])-r.screen:]
		t := sample.Trace{Ch: ch, Format: fm, Valid: r.screen, Held: held}
		if fm.Storage == sample.U16 {
			t.Codes16 = slices.Clone(codes)
		} else {
			t.Codes8 = make([]uint8, r.screen)
			for k, v := range codes {
				t.Codes8[k] = uint8(v) //nolint:gosec // G115: 8-bit codes
			}
		}
		t.Scale, t.CodesPerDiv = c.rollScale(ch, fm)
		f.Traces = append(f.Traces, t)
	}
	r.fresh, r.pubAt = false, time.Now()
	c.Publish(f, Verdict{}, true)
}

// rollScale is channel ch's scale in the committed state: the newest frame
// read's codes per division for the channel, else the graticule's.
// TRLC-LINKS: REQ-DSO-010
func (c *HALCycle) rollScale(ch int, fm sample.CodeFormat) (sample.Scale, float64) {
	cpd := 0.0
	if f := c.snd.last; f != nil {
		for i := range f.Traces {
			if t := &f.Traces[i]; t.Ch == ch && t.Format == fm {
				cpd = t.CodesPerDiv
			}
		}
	}
	if !(cpd > 0) {
		divs := c.b.Profile().Display.DivsV
		if divs <= 0 {
			divs = 8
		}
		cpd = float64(fm.Max) / float64(divs)
	}
	sc := sample.Scale{Zero: float64(fm.Mid)}
	if ch >= 0 && ch < len(c.cur.Channels) {
		hc := c.cur.Channels[ch]
		sc.PerCode, sc.OffsetV = hc.VoltsDiv/cpd, hc.OffsetV
	}
	return sc, cpd
}

// rollStop stops the board's stream; the state is committed again for the
// captures.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-035
func (c *HALCycle) rollStop() {
	r := &c.roll
	if r.st != nil {
		if err := r.st.Stop(); err != nil {
			c.cfg.Logf("roll: stop: %v", err)
		}
	}
	r.running = false
	c.gen = 0
}
