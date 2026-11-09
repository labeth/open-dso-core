// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"math"
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

// halRoll is the cycle's roll view. The view keeps its samples in volts:
// a setting committed while it rolls (a V/div, an offset, the trigger) is
// committed as it comes, the stream goes on, and the view is drawn again
// at the new scale (the SDS1000CML+'s roll keeps its view the same way). A
// stream started again under the same roll plan (a loss, posted work)
// keeps the view, placing its first samples by the host clock after the
// newest ones.
type halRoll struct {
	st      hal.Streamer
	running bool
	retryAt time.Time
	plan    hal.Plan
	payload hal.Payload
	chans   []int // the channels streamed (the board's plan's)
	screen  int
	peak    bool                // the view's samples are (min, max) pairs
	formats []sample.CodeFormat // each streamed channel's codes, as the newest block gave them
	scales  []sample.Scale      // each streamed channel's volts of its codes, as the newest block gave them; zero PerCode: the committed state's
	volts   [][]float32         // each streamed channel's newest samples in volts, oldest first (at most 2·screen)
	held    []bool              // the samples no capture covered
	filled  int                 // samples received since the view started
	lastAt  time.Time           // when the newest samples came
	again   bool                // the stream started again under a kept view: its first block is placed by the host clock
	pubAt   time.Time
	fresh   bool   // samples since the last frame
	shown   bool   // the newest frame published is the view's (a STOP keeps the image it ran on)
	refused uint64 // the generation whose roll the board refused: it captures
}

// rollPayload is the board's roll stream payload: captured chunks, else a
// gap-free stream of codes; false when it has neither.
// TRLC-LINKS: REQ-DSO-010
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
	case c.need != (instrument.RecordNeed{}) || c.soundWanted() || c.dec.active || c.sessionActive():
		return false
	case c.roll.refused != 0 && c.roll.refused == c.gen:
		return false
	}
	_, _, ok := c.rollPayload()
	return ok
}

// rollStep runs the roll view one step on the owner; false when the state
// does not roll (the stream stopped) and the pass goes on with captures.
// A setting committed while the view rolls under the same roll plan, on a
// board whose stream takes commits, is committed in the step, the stream
// going on.
// TRLC-LINKS: REQ-DSO-010
func (c *HALCycle) rollStep(ctx context.Context) bool {
	r := &c.roll
	s, gen := c.inst.Wanted()
	need := c.inst.RecordNeed()
	if r.running && (gen != c.gen || need != c.need) && (!takesCommits(r.st) || !c.rollCommit(s, gen, need)) {
		c.rollStop() // the pass commits first; a roll it still wants starts again
		return false
	}
	if gen != c.gen || need != c.need {
		if r.running {
			c.rollStop()
		}
		return false // the pass commits first
	}
	if !c.rollWanted() {
		if r.running {
			c.rollStop()
		}
		if c.running {
			r.volts = nil // leaving roll: the next roll starts a fresh view
		}
		return false
	}
	if r.running {
		if plan, err := c.rollPlan(); err != nil || !sameRoll(plan, r.plan) {
			c.rollStop() // the board rolls this state another way now (its roll stream)
			return false
		}
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

// rollCommit commits state s while the view rolls: the board takes it
// while it streams (its front end written between its blocks). false when
// s does not roll under the running roll plan: the stream is started again
// (or the captures run).
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-008
func (c *HALCycle) rollCommit(s instrument.State, gen uint64, need instrument.RecordNeed) bool {
	r := &c.roll
	c.need = need
	c.commitState(s, gen)
	if !c.rollWanted() {
		return false
	}
	plan, err := c.rollPlan()
	if err != nil || !sameRoll(plan, r.plan) {
		return false
	}
	if err := c.b.Acquirer().Configure(plan); err != nil {
		return false
	}
	r.plan, r.fresh = plan, true // drawn again at the new scale
	c.inst.SetRun(instrument.Rolling)
	return true
}

// takesCommits reports a stream that takes the settings committed while it
// runs (hal.CommitStreamer); any other is started again behind a commit.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-265
func takesCommits(st hal.Streamer) bool {
	cs, ok := st.(hal.CommitStreamer)
	return ok && cs.TakesCommits()
}

// rollPlan is the committed state planned in the board's roll mode at its
// own timebase.
// TRLC-LINKS: REQ-DSO-010
func (c *HALCycle) rollPlan() (hal.Plan, error) {
	req := c.request(c.cur)
	req.Mode = hal.RollMode
	return c.b.Planner().Plan(req)
}

// sameRoll reports roll plans a and b roll the same view: the timebase,
// the samples' spacing and kind, the screen and the channels streamed.
// TRLC-LINKS: REQ-DSO-010
func sameRoll(a, b hal.Plan) bool {
	return a.Request.TimePerDivS == b.Request.TimePerDivS && a.SampleS == b.SampleS && a.WinSamples == b.WinSamples &&
		a.AcqDecim == b.AcqDecim && a.Content == b.Content && slices.Equal(a.Request.Channels, b.Request.Channels)
}

// rollStart plans the committed state in the board's roll mode at its own
// timebase and starts the board's stream of the channels its plan streams,
// the view empty (every sample held), or kept when it rolled under the same
// plan.
// TRLC-LINKS: REQ-DSO-010
func (c *HALCycle) rollStart() error {
	r := &c.roll
	st, payload, _ := c.rollPayload()
	c.stopArmed()
	plan, err := c.rollPlan()
	if err != nil {
		return err
	}
	if err := c.b.Acquirer().Configure(plan); err != nil {
		return err
	}
	chans := plan.Request.Channels
	if len(c.cur.Request().Channels) == 0 || len(chans) == 0 {
		return hal.Refuse("roll", "no channel is on")
	}
	if err := st.Start(hal.StreamSpec{Payload: payload, Channels: chans, SampleS: plan.SampleS}); err != nil {
		return err
	}
	if im, ok := c.b.Images(); ok {
		c.inst.SetImage(im.Current().Name) // the board loads the image its stream runs on
	}
	peak := plan.Content.Has(sample.PeakPairs) && plan.AcqDecim > 1
	screen := max(plan.WinSamples, 2)
	keep := r.volts != nil && r.payload == payload && r.screen == screen && r.peak == peak &&
		r.plan.SampleS == plan.SampleS && r.plan.Request.TimePerDivS == plan.Request.TimePerDivS
	r.st, r.payload, r.running, r.peak, r.screen = st, payload, true, peak, screen
	r.plan = plan
	if keep {
		r.keepChannels(chans)
		r.again = true
	} else {
		r.reset(chans, c.b.Profile().Format)
	}
	r.fresh, r.pubAt = true, time.Time{}
	c.inst.SetRun(instrument.Rolling)
	return nil
}

// reset empties the view for channels chans: every sample held.
// TRLC-LINKS: REQ-DSO-010
func (r *halRoll) reset(chans []int, fm sample.CodeFormat) {
	r.chans = slices.Clone(chans)
	r.formats, r.scales, r.volts = r.formats[:0], r.scales[:0], r.volts[:0]
	for range r.chans {
		r.formats = append(r.formats, fm)
		r.scales = append(r.scales, sample.Scale{})
		r.volts = append(r.volts, make([]float32, r.screen))
	}
	r.held = slices.Repeat([]bool{true}, r.screen)
	r.filled, r.lastAt, r.again = 0, time.Time{}, false
}

// keepChannels keeps the view of each channel streamed again; a channel
// new to the stream starts at 0 V.
// TRLC-LINKS: REQ-DSO-010
func (r *halRoll) keepChannels(chans []int) {
	if slices.Equal(chans, r.chans) {
		return
	}
	n := len(r.held)
	formats, scales, volts := make([]sample.CodeFormat, len(chans)), make([]sample.Scale, len(chans)), make([][]float32, len(chans))
	for i, ch := range chans {
		if k := slices.Index(r.chans, ch); k >= 0 {
			formats[i], scales[i], volts[i] = r.formats[k], r.scales[k], r.volts[k]
			continue
		}
		volts[i] = make([]float32, n)
		if len(r.formats) > 0 {
			formats[i] = r.formats[0]
		}
	}
	r.chans, r.formats, r.scales, r.volts = slices.Clone(chans), formats, scales, volts
}

// rollDrain moves the stream's blocks into the view for up to
// rollPublishEvery.
// TRLC-LINKS: REQ-DSO-010
func (c *HALCycle) rollDrain(ctx context.Context) {
	r := &c.roll
	end := time.Now().Add(rollPublishEvery)
	for time.Now().Before(end) {
		lctx, cancel := context.WithDeadline(ctx, end)
		b, err := r.st.Lease(lctx)
		ended := lctx.Err() // before cancel: the lease's own deadline, or the loop's end
		cancel()
		if err != nil {
			if ctx.Err() == nil && ended == nil {
				c.rollStop() // the stream ended under us: started again, the view kept
				r.retryAt = time.Now().Add(100 * time.Millisecond)
			}
			return
		}
		r.add(b, c.rollScale, time.Now())
		r.st.Release(b)
		if r.fresh && time.Since(r.pubAt) >= rollPublishEvery {
			return
		}
	}
}

// add appends a block's samples to the view in volts, keeping the newest
// screen: each channel's codes at the scale the block states (else the
// committed state's, scaleOf). The first block after the stream started
// again under a kept view follows the samples the restart lost, held. A
// block of no samples still brings its scale: the view is drawn at it.
// TRLC-LINKS: REQ-DSO-010
func (r *halRoll) add(b hal.StreamBlock, scaleOf func(ch int, fm sample.CodeFormat) (sample.Scale, float64), now time.Time) {
	nch := min(len(b.Formats), len(r.chans))
	for i := range nch {
		r.formats[i] = b.Formats[i]
		if i < len(b.Scales) && b.Scales[i].PerCode > 0 {
			if b.Scales[i] != r.scales[i] {
				r.fresh = true
			}
			r.scales[i] = b.Scales[i]
		}
	}
	n := 0
	if nch > 0 {
		bytes, words := b.Run(0)
		n = len(bytes) + len(words)
	}
	if n == 0 {
		return
	}
	gap := 0
	if r.again {
		if !r.lastAt.IsZero() && r.plan.SampleS > 0 {
			missing := now.Sub(r.lastAt).Seconds() - float64(n)*r.plan.SampleS
			gap = min(max(0, int(math.Round(missing/r.plan.SampleS))), r.screen+1)
		}
		r.again = false
	}
	for i := range nch {
		sc := r.scales[i]
		if !(sc.PerCode > 0) {
			sc, _ = scaleOf(r.chans[i], r.formats[i])
		}
		v := r.volts[i]
		if gap > 0 {
			// hold the last value; a peak view holds its last (min, max)
			// pair, so the envelope runs on instead of a line through it
			k := len(v)
			h := [2]float32{v[k-1], v[k-1]}
			if r.peak && k >= 2 {
				h = [2]float32{v[k-2], v[k-1]}
			}
			for j := range gap {
				v = append(v, h[j%2])
			}
		}
		at := len(v)
		bytes, words := b.Run(i)
		for _, x := range bytes {
			v = append(v, float32(sc.Volts(float64(x))))
		}
		for _, x := range words {
			v = append(v, float32(sc.Volts(float64(x))))
		}
		if r.filled == 0 { // the first samples also fill the view's past: no capture
			for k := range at {
				v[k] = v[at]
			}
		}
		r.volts[i] = trimTo(v, r.screen)
	}
	r.held = append(r.held, slices.Repeat([]bool{true}, gap)...)
	if len(b.Held) == n {
		r.held = append(r.held, b.Held...)
	} else {
		r.held = append(r.held, make([]bool, n)...)
	}
	r.held = trimTo(r.held, r.screen)
	r.filled += gap + n
	r.lastAt = now
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

// rollPublish publishes the view's newest screen as a roll frame of the
// channels on, its volts as codes at each channel's newest scale; volts
// off the codes' range clip, as on the screen, but stay in the view.
// TRLC-LINKS: REQ-DSO-010
func (c *HALCycle) rollPublish() {
	r := &c.roll
	f := &sample.Frame{
		Content: sample.RawDecimated, Flags: sample.Roll, SampleS: r.plan.SampleS, CaptureS: r.plan.SampleS,
		AcqDecim: max(r.plan.AcqDecim, 1), Depth: r.screen, WinPoints: r.screen, TriggerAt: -1,
		TrigKind: sample.Untriggered, Status: sample.Status{Coherent: true, Done: true},
		Snapshot: sample.Settings{TdivS: c.cur.TimeDivS, DisplayedS: c.cur.TimeDivS},
	}
	if r.peak {
		f.Content = sample.PeakPairs
	}
	held := slices.Clone(r.held[len(r.held)-r.screen:])
	on := c.cur.Request().Channels
	for i, ch := range r.chans {
		if !slices.Contains(on, ch) {
			continue
		}
		fm := r.formats[i]
		sc, cpd := r.scales[i], 0.0
		if sc.PerCode > 0 {
			if ch >= 0 && ch < len(c.cur.Channels) && c.cur.Channels[ch].VoltsDiv > 0 {
				cpd = c.cur.Channels[ch].VoltsDiv / sc.PerCode // drawn at the committed V/div
			} else {
				_, cpd = c.rollScale(ch, fm)
			}
		} else {
			sc, cpd = c.rollScale(ch, fm)
		}
		volts := r.volts[i][len(r.volts[i])-r.screen:]
		t := sample.Trace{Ch: ch, Format: fm, Valid: r.screen, Held: held, Scale: sc, CodesPerDiv: cpd}
		lo, hi := float64(fm.Min), float64(fm.Max)
		word := func(v float32) float64 {
			w := (float64(v)+sc.OffsetV)/sc.PerCode + sc.Zero
			return min(max(math.Round(w), lo), hi) // builtin min/max: NaN-free, inline
		}
		if fm.Storage == sample.U16 {
			t.Codes16 = make([]uint16, r.screen)
			for k, v := range volts {
				t.Codes16[k] = uint16(word(v))
			}
		} else {
			t.Codes8 = make([]uint8, r.screen)
			for k, v := range volts {
				t.Codes8[k] = uint8(word(v))
			}
		}
		f.Traces = append(f.Traces, t)
	}
	r.fresh, r.pubAt, r.shown = false, time.Now(), true
	c.Publish(f, Verdict{}, true)
}

// rollScale is channel ch's scale in the committed state: the newest frame
// read's codes per division for the channel (in format fm's words), else
// the graticule's.
// TRLC-LINKS: REQ-DSO-010
func (c *HALCycle) rollScale(ch int, fm sample.CodeFormat) (sample.Scale, float64) {
	cpd := 0.0
	if f := c.snd.last; f != nil {
		for i := range f.Traces {
			if t := &f.Traces[i]; t.Ch == ch && t.CodesPerDiv > 0 {
				cpd = t.CodesPerDiv8() * fm.Unit8()
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

// rollHoldsImage reports the image the roll view ran on stays: the
// committed state rolls (the board's stream loads the image it runs on),
// or a STOP keeps the view shown (RUN rolls again without a load).
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-011
func (c *HALCycle) rollHoldsImage() bool {
	if !c.running {
		return c.roll.shown
	}
	return c.rollWanted()
}

// rollStop stops the board's stream; the state is committed again for the
// captures.
// TRLC-LINKS: REQ-DSO-010
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
