package simboard

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// The acquisition's pacing.
const (
	// scanEvals bounds the generator evaluations one Wait poll spends
	// looking for the trigger, so a fast signal on a slow poll cannot hold
	// the owner.
	scanEvals = 200_000
	// pollEvery is how often a waiting acquisition looks for its trigger.
	pollEvery = 5 * time.Millisecond
	// autoFloor is the least time AUTO waits for a trigger before it shows
	// an untriggered record; the record's own span is added.
	autoFloor = 100 * time.Millisecond
)

// acquisition is an armed acquisition while it waits.
type acquisition struct {
	set      setup
	armedAt  float64 // signal time of the arm
	scanFrom float64 // the trigger scan's next instant
	edge     edgeState
	found    bool
	forced   bool    // completed without its trigger condition
	freeRun  bool    // the trigger is not armed at all
	trigT    float64 // signal time of the trigger
	doneAt   float64 // signal time the record is complete
	force    bool    // a forced trigger was asked for
}

// record is the stored record: the instant it was taken at and the setup
// it was taken with.
type record struct {
	valid  bool
	set    setup
	trigT  float64
	status hal.Status
	free   bool // untriggered (a free-running trigger)
}

// geometry is where a plan's screen lies in its record.
type geometry struct {
	sampleS float64 // between record samples
	win     int     // record samples across the screen
	k0      int     // the trigger's sample within the screen
	first   int64   // the record's first sample, from the trigger
	n       int     // the record's samples
	pre     float64 // seconds the record holds before the trigger
	post    float64 // seconds after it
}

// geometryOf is p's screen in its record: the screen centre DelayS after
// the trigger, the trigger on a sample, the record's samples beyond the
// screen split evenly before and after it, its pretrigger room before.
// TRLC-LINKS: REQ-DSO-1001
func geometryOf(p hal.Plan) geometry {
	g := geometry{sampleS: p.SampleS, win: max(p.WinSamples, 1)}
	half := float64(g.win) * g.sampleS / 2
	g.k0 = int(math.Round((half - p.Request.DelayS) / g.sampleS))
	room := max(p.Request.PretrigRoom, 0)
	g.n = max(p.Depth, g.win+room)
	extra := g.n - g.win - room
	before := int64(g.k0 + extra/2 + room)
	g.first = -before
	g.pre = max(float64(before), 0) * g.sampleS
	g.post = max(float64(int64(g.n)-before), 0) * g.sampleS
	return g
}

type planner struct{ b *Board }

// Plan picks the fastest rate the channels allow and a record that spans
// the screen: every converter sample while they fit the depth, decimated
// beyond that to a record of RecordScreens screens. Averaging and ERES are
// math on frames, not records, so they are refused like the stream modes the
// board does not have.
// TRLC-LINKS: REQ-DSO-1001
func (p planner) Plan(r hal.Request) (hal.Plan, error) {
	prof := p.b.profile
	switch {
	case r.Segments > 1:
		return hal.Plan{}, hal.Refuse("segments", prof.AbsentReason("segments"))
	case r.Mode == hal.Stream:
		return hal.Plan{}, hal.Refuse("stream", prof.AbsentReason("streamer"))
	case r.Mode == hal.RollMode:
		return hal.Plan{}, hal.Refuse("roll", "the simulated board has no roll stream: slow timebases take the whole record")
	case r.Mode == hal.Average || r.Mode == hal.HighRes:
		return hal.Plan{}, hal.Refuse("acquisition mode", "the simulated board acquires normal and peak-detect records; averaging and ERES are math on frames")
	case r.TimePerDivS <= 0 || math.IsNaN(r.TimePerDivS) || math.IsInf(r.TimePerDivS, 0):
		return hal.Plan{}, hal.Refuse("timebase", "time/div must be positive")
	}
	for _, c := range r.Channels {
		if c < 0 || c >= prof.Channels {
			return hal.Plan{}, hal.Refuse(fmt.Sprintf("channel %d", c+1), "this scope has no such channel")
		}
	}
	rate := 0.0
	for _, rt := range prof.Rates {
		if rt.MaxChannels >= len(r.Channels) && rt.SamplesPerS > rate {
			rate = rt.SamplesPerS
		}
	}
	if rate == 0 {
		return hal.Plan{}, hal.Refuse("channels", fmt.Sprintf("no sample rate holds %d channels", len(r.Channels)))
	}
	span := r.TimePerDivS * float64(prof.Display.DivsH)
	depth := p.b.cfg.Depth
	screens := max(p.b.cfg.RecordScreens, 1)
	if r.Depth > 0 {
		depth, screens = min(r.Depth, prof.MaxDepth), 1
	}
	plan := hal.Plan{Request: r, Mode: r.Mode, ETSFactor: 1, Segments: 1, AcqDecim: 1, SampleS: 1 / rate}
	full := span * rate
	if full <= float64(depth) {
		plan.WinSamples = max(int(math.Round(full)), 2)
		plan.Content = sample.ContentSetOf(sample.RawFullRate, sample.RawDecimated)
	} else {
		plan.AcqDecim = int(math.Ceil(full * screens / float64(depth)))
		plan.SampleS = float64(plan.AcqDecim) / rate
		plan.WinSamples = max(int(math.Round(span/plan.SampleS)), 2)
		plan.Pick = r.Mode != hal.PeakDetect
		plan.Content = sample.ContentSetOf(sample.RawDecimated)
		if r.Mode == hal.PeakDetect {
			plan.Content = sample.ContentSetOf(sample.PeakPairs)
		}
	}
	// A full-rate record holds the depth, beyond the screen when the screen
	// takes fewer samples (a scope's record is no shorter at a fast
	// timebase); a decimated one spans the screen.
	plan.Depth = max(plan.WinSamples, min(int(float64(plan.WinSamples)*screens), depth)) + max(r.PretrigRoom, 0)
	if plan.AcqDecim == 1 {
		plan.Depth = max(plan.Depth, depth)
	}
	if plan.Depth > prof.MaxDepth {
		return hal.Plan{}, hal.Refuse("record", fmt.Sprintf("%d samples do not fit the %d-sample record", plan.Depth, prof.MaxDepth))
	}
	// The samples before the trigger are the record's: a screen further
	// before the trigger than the deepest record reaches is refused, not
	// waited for.
	if g := geometryOf(plan); -g.first > int64(prof.MaxDepth) {
		return hal.Plan{}, hal.Refuse("horizontal position", fmt.Sprintf(
			"the screen lies %.3g s before the trigger; at this time/div the record holds %.3g s before it",
			g.pre, float64(prof.MaxDepth)*plan.SampleS))
	}
	for _, f := range r.Need {
		if f.Accepts&plan.Content == 0 {
			return hal.Plan{}, hal.Refuse(f.Name, fmt.Sprintf("this timebase gives %v records, which it does not take", plan.Content))
		}
		if f.MinDepth > plan.Depth {
			return hal.Plan{}, hal.Refuse(f.Name, fmt.Sprintf("needs %d samples, the record holds %d", f.MinDepth, plan.Depth))
		}
	}
	return plan, nil
}

type acquirer struct{ b *Board }

// Configure stages p.
// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Configure(p hal.Plan) error {
	if p.WinSamples < 1 || p.SampleS <= 0 {
		return hal.Refuse("plan", "a plan needs a sample interval and a screen of samples")
	}
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	a.b.staged.plan = p
	return nil
}

// Arm takes the staged setup and starts looking for its trigger once the
// record's pretrigger part has filled.
// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Arm() error {
	b := a.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.staged.plan.WinSamples == 0 {
		return errors.New("simboard: arm before a plan was configured")
	}
	b.applied = b.staged.clone()
	b.stopped = false
	now := b.now()
	g := geometryOf(b.applied.plan)
	from := now + g.pre
	if h := b.applied.trigger.HoldoffS; h > 0 {
		from = max(from, b.lastTrig+h)
	}
	b.acq = acquisition{set: b.applied, armedAt: now, scanFrom: from, freeRun: b.applied.trigger.FreeRun || len(b.applied.trigger.Stages) == 0}
	b.state = hal.Armed
	return nil
}

// Wait serves the armed acquisition until its record is complete or
// timeout passes: it scans the source for the trigger up to the board's
// clock, forces AUTO's record once its wait is over, and completes a
// record only when its post-trigger part has passed and the frame interval
// since the last one is over.
// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Wait(ctx context.Context, timeout time.Duration) (hal.Status, error) {
	b := a.b
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return hal.Status{}, err
		}
		st, ready, next, err := b.poll()
		if err != nil || ready {
			return st, err
		}
		left := time.Until(deadline)
		if left <= 0 {
			return st, hal.ErrTimeout
		}
		t := time.NewTimer(min(next, left))
		select {
		case <-ctx.Done():
			t.Stop()
			return hal.Status{}, ctx.Err()
		case <-t.C:
		}
	}
}

// poll advances the armed acquisition to the board's clock; ready says its
// record is stored, next how long to sleep before the next look.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) poll() (st hal.Status, ready bool, next time.Duration, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != hal.Armed {
		return hal.Status{}, false, 0, errors.New("simboard: wait while not armed")
	}
	q := &b.acq
	now := b.now()
	g := geometryOf(q.set.plan)
	if !q.found {
		switch {
		case q.force || q.freeRun:
			q.found, q.forced, q.trigT = true, true, max(now, q.scanFrom)
		default:
			b.scan(q, now)
		}
		if !q.found && q.set.trigger.Sweep == hal.Auto {
			wait := autoFloor.Seconds() + g.pre + g.post
			if now-q.armedAt >= wait {
				q.found, q.forced, q.trigT = true, true, max(now, q.scanFrom)
			}
		}
		if q.found {
			q.doneAt = q.trigT + g.post
		}
	}
	st = hal.Status{Triggered: q.found && !q.forced}
	if !q.found {
		return st, false, pollEvery, nil
	}
	wall := time.Now()
	if gap := b.frameInterval() - wall.Sub(b.lastDone); gap > 0 {
		return st, false, gap, nil
	}
	if now < q.doneAt {
		return st, false, time.Duration((q.doneAt - now) * float64(time.Second)), nil
	}
	b.recSeq++
	b.lastDone = wall
	if !q.forced {
		b.lastTrig = q.trigT
	} else {
		b.forced++
	}
	b.waits++
	st = hal.Status{
		Triggered: !q.forced, Forced: q.forced && !q.freeRun, Done: true, Coherent: true,
		Record: hal.RecordID{Epoch: 1, Seq: b.recSeq},
	}
	b.rec = record{valid: true, set: q.set, trigT: q.trigT, status: st, free: q.freeRun}
	b.state = hal.Ready
	return st, true, 0, nil
}

// Force completes the armed acquisition without its trigger condition.
// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Force() error {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	a.b.acq.force = true
	return nil
}

// Stop ends the acquisition; the stored record stays readable.
// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Stop() error {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	a.b.stopped = true
	if a.b.state == hal.Armed || a.b.state == hal.Ready {
		a.b.state = hal.Idle
	}
	return nil
}

// State implements hal.Acquirer.
// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) State() hal.AcqState {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	return a.b.state
}
