// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"errors"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// LoopConfig wires a Loop.
type LoopConfig struct {
	Acquirer hal.Acquirer
	Cycle    Cycle
	Clock    Clock // zero: the real clock
	// WaitSlice is how long one Acquirer.Wait may block before the loop
	// serves its commit point again. Zero polls: the loop sleeps
	// StatusPollWait between the polls itself.
	WaitSlice time.Duration
	// Stopping, when set, also ends the loop (a scope's own stop flag).
	Stopping func() bool
	// OnPanic hears a panic the loop contained, before it parks.
	OnPanic func(r any, stack []byte)
}

// Loop is the core engine's owner loop (HAL v1 §2.1): the one goroutine
// that touches the hardware. Each pass serves the commit point, lets other
// work claim the acquisition, has the cycle plan and commit a capture, arms
// it and waits for it while serving the commit points (a changed setting
// starts a fresh capture; AUTO and the FORCE key force a trigger), has the
// cycle read and judge the record, and then decides the publish, completes
// a single shot and paces the next pass. What the scope decides is its
// Cycle; HALCycle is the cycle of a board whose settings are all HAL terms.
type Loop struct {
	cfg  LoopConfig
	c    Cycle
	a    hal.Acquirer
	stop atomic.Bool
	park atomic.Bool

	lastPace time.Duration // the previous pass's pacing hold
}

// NewLoop builds a loop over cfg's acquirer and cycle.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008
func NewLoop(cfg LoopConfig) *Loop {
	if cfg.Clock.Now == nil {
		cfg.Clock = RealClock()
	}
	return &Loop{cfg: cfg, c: cfg.Cycle, a: cfg.Acquirer}
}

// Stop asks the loop to end at its next commit point; Run returns then.
// TRLC-LINKS: REQ-DSO-008
func (l *Loop) Stop() { l.stop.Store(true) }

// Stopping reports that the loop was asked to end.
// TRLC-LINKS: REQ-DSO-008
func (l *Loop) Stopping() bool {
	return l.stop.Load() || l.cfg.Stopping != nil && l.cfg.Stopping()
}

// Wedged reports that the loop panicked and parked: it serves nothing
// until it is stopped, so the health it feeds stops advancing.
// TRLC-LINKS: REQ-DSO-001
func (l *Loop) Wedged() bool { return l.park.Load() }

// Run is the owner loop; it returns once stopped. ctx ends it too and
// cancels a wait in flight. A panic is contained: the cycle closes, the
// loop parks until stopped (the process keeps its hardware handles, and a
// supervisor sees the health stop advancing instead of a crash loop).
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008
func (l *Loop) Run(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			if l.cfg.OnPanic != nil {
				l.cfg.OnPanic(r, debug.Stack())
			}
			l.park.Store(true)
			for !l.Stopping() && ctx.Err() == nil {
				l.cfg.Clock.Sleep(100 * time.Millisecond)
			}
		}
	}()
	l.c.Open()
	defer l.c.Close()
	for !l.Stopping() && ctx.Err() == nil {
		l.pass(ctx)
	}
}

// Capture is the capture a pass makes, as the cycle planned it.
type Capture struct {
	// Replay reads the record the board holds again (a stopped record
	// shown at a new setting): nothing is armed.
	Replay bool
	// Normal: the hardware trigger is armed, so a capture may be forced.
	// A free-running capture completes on its own.
	Normal bool
	// Norm: the sweep waits for a trigger (NORMAL or SINGLE).
	Norm bool
	// AutoWait is how long AUTO waits for a trigger before the loop
	// forces one; 0 when the board sweeps AUTO itself.
	AutoWait time.Duration
}

// Armed is how a capture was armed and served.
type Armed struct {
	ArmAt      time.Time // when the pass planned it
	ArmedAt    time.Time // when Arm returned
	Status     hal.Status
	Forced     bool   // the loop forced it (AUTO or the FORCE key)
	UserForced bool   // by the FORCE key
	SingleGen  uint64 // the SINGLE press the capture served
}

// Verdict is the cycle's judgement of a read frame.
type Verdict struct {
	Qualified  bool // its trigger stands: a hardware trigger every check kept
	Complete   bool // a block mode's block is complete (an average of N)
	FullRecall bool // a stopped record read whole: always shown
	UserForced bool // forced by the FORCE key: shown even in NORMAL
	MaskStop   bool // a mask test failed with stop-on-fail: shown, and the run stops
}

// Interrupt is what the cycle answers while a capture waits.
type Interrupt int

// The interrupts.
const (
	Continue Interrupt = iota // keep waiting
	Abort                     // halt the capture: the pass starts over
	Leave                     // another owner of the acquisition took over: leave the capture as it is
)

// Cycle is the scope's part of the owner's pass. The loop calls it on the
// owner goroutine only. It shrinks as the scopes' settings become HAL terms:
// what a board does through the HAL alone is HALCycle.
type Cycle interface {
	// Open runs before the first pass and Close after the last, also
	// when a panic ends the loop.
	Open()
	Close()
	// Serve runs the commit point's housekeeping (the board's Service and
	// the staged work the scope serves there), at the top of every pass
	// and between the polls of a wait.
	Serve()
	// Claim lets other work take this pass (a stream, an image load,
	// LONG work); true: it took the pass.
	Claim(ctx context.Context) bool
	// Plan decides and stages this pass's capture; false: nothing to
	// capture now (stopped and nothing to show again).
	Plan(ctx context.Context) (Capture, bool)
	// Watch is asked at every poll of a wait; settings says the settings
	// are due a look (every SettingsPollEvery): a change of them aborts
	// the capture for a fresh one.
	Watch(settings bool) Interrupt
	// ArmFailed reports a failed arm; Fault a wait that ended in a fault
	// (err) or a halt that failed (haltErr), after the capture was halted.
	ArmFailed(err error)
	Fault(err, haltErr error)
	// Frame reads and judges the record of capture c; false: nothing to
	// show from this pass.
	Frame(ctx context.Context, c Capture, a Armed) (*sample.Frame, Verdict, bool)
	// Liveness decides whether AUTO shows a frame its gate (a protocol
	// or zone qualifier) did not qualify.
	Liveness() bool
	// Publish publishes f when publish is set, else counts it held.
	Publish(f *sample.Frame, v Verdict, publish bool)
	// Done ends the pass at doneAt; prevPace is the previous pass's
	// pacing hold.
	Done(doneAt time.Time, prevPace time.Duration)
	// Pacing is the trigger holdoff and the least time a live capture
	// takes from its arm (the display's share of a single core), read
	// live while pacing.
	Pacing() (holdoff, display time.Duration)
	// Beat advances the owner's liveness heartbeat.
	Beat()
	RunControl
}

// RunControl is the run state the owner obeys.
type RunControl interface {
	Running() bool
	SingleArmed() bool
	// SingleGen moves with every SINGLE press: a capture armed before a
	// press does not serve it.
	SingleGen() uint64
	// TakeForce takes a pending FORCE press; ForcePending looks at it.
	TakeForce() bool
	ForcePending() bool
	// StopOnFail stops the run on a failed mask test.
	StopOnFail()
	// SingleDone completes the single shot whose frame v was published.
	SingleDone(v Verdict)
}

// pass is one turn of the owner's cycle.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008, REQ-DSO-011
func (l *Loop) pass(ctx context.Context) {
	c := l.c
	c.Serve()
	if c.Claim(ctx) {
		return
	}
	cp, ok := c.Plan(ctx)
	if !ok {
		return
	}
	a := Armed{ArmAt: l.cfg.Clock.Now(), ArmedAt: l.cfg.Clock.Now()}
	if !cp.Replay {
		if a, ok = l.armAndWait(ctx, cp, a.ArmAt); !ok {
			return
		}
	}
	f, v, ok := c.Frame(ctx, cp, a)
	if !ok {
		return
	}
	l.gate(f, v, cp.Norm, a.SingleGen)
	doneAt := l.cfg.Clock.Now()
	c.Done(doneAt, l.lastPace)
	l.paceHold(doneAt, v.Qualified && f.Status.Triggered, 0)
	if _, display := c.Pacing(); display > 0 {
		l.paceHold(a.ArmAt, false, display)
	}
	l.lastPace = l.cfg.Clock.Now().Sub(doneAt)
}

// armAndWait arms the staged capture and serves it until its record is
// ready: the commit points between polls, a settings change (a fresh
// capture with them), AUTO forced after its wait, FORCE once. ok is false
// when the pass must start over: the arm or the wait failed (reported),
// the settings changed, or other work came first.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-009, REQ-DSO-011
func (l *Loop) armAndWait(ctx context.Context, cp Capture, armAt time.Time) (a Armed, ok bool) {
	c, clk := l.c, l.cfg.Clock
	a.ArmAt = armAt
	err := l.a.Arm()
	a.ArmedAt = clk.Now()
	if err != nil {
		c.ArmFailed(err)
		return a, false
	}
	// A SINGLE pressed while this capture still waits for its event is
	// served by it; once its record is ready, a later press needs a fresh
	// capture.
	a.SingleGen = c.SingleGen()
	aborted := false
	var settingsAt time.Time
	for !l.Stopping() {
		c.Serve()
		it := c.Watch(false)
		if it == Leave {
			return a, false
		}
		c.Beat()
		if it == Abort {
			aborted = true
			break
		}
		if now := clk.Now(); now.Sub(settingsAt) >= SettingsPollEvery {
			settingsAt = now
			if c.Watch(true) != Continue {
				aborted = true
				break
			}
		}
		gen := c.SingleGen() // before the poll: a press seen here precedes the ready record
		slice := l.cfg.WaitSlice
		if cp.AutoWait > 0 && cp.Normal && !cp.Norm && !a.Forced && slice > 0 {
			// the slice ends when AUTO's wait does, so its forced trigger is not late
			slice = max(0, min(slice, cp.AutoWait-clk.Now().Sub(armAt)))
		}
		st, werr := l.a.Wait(ctx, slice)
		if werr == nil {
			a.Status = st
			break
		}
		if !errors.Is(werr, hal.ErrTimeout) {
			err = werr
			break
		}
		a.SingleGen = gen
		// AUTO forces after its wait, unless the hardware triggered: the
		// record then only fills its post-trigger part; FORCE forces once
		// now, and that frame is shown even in NORMAL.
		autoDue := cp.AutoWait > 0 && AutoForceDue(cp.Norm, c.Running(), st.Triggered, clk.Now().Sub(armAt), cp.AutoWait)
		if cp.Normal && !a.Forced && !autoDue && c.TakeForce() {
			a.UserForced, autoDue = true, true
		}
		if cp.Normal && !a.Forced && autoDue {
			err = l.a.Force()
			a.Forced = true
			if err != nil {
				break
			}
		}
		if l.cfg.WaitSlice <= 0 {
			clk.Sleep(StatusPollWait(clk.Now().Sub(a.ArmedAt)))
		}
	}
	if aborted || l.Stopping() || err != nil {
		haltErr := l.a.Stop()
		if err != nil || haltErr != nil {
			c.Fault(err, haltErr)
		}
		return a, false
	}
	return a, true
}

// gate decides whether frame f is shown and completes a single shot:
// a qualified trigger, AUTO's free run, a stopped record read whole, the
// FORCE key and a failed mask test with stop-on-fail are shown; AUTO holds
// the captures a protocol or zone gate rejected unless its liveness rule
// shows one.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013, REQ-DSO-014
func (l *Loop) gate(f *sample.Frame, v Verdict, norm bool, singleGen uint64) {
	c := l.c
	publish := v.Qualified || !norm || v.FullRecall || v.UserForced || v.MaskStop
	if publish && !v.Qualified && !v.FullRecall && !v.UserForced && !v.MaskStop {
		publish = c.Liveness()
	}
	if v.MaskStop {
		c.StopOnFail()
	}
	c.Publish(f, v, publish)
	if publish && (v.Qualified && v.Complete || v.UserForced) && c.SingleArmed() && !v.FullRecall && singleGen == c.SingleGen() {
		c.SingleDone(v)
	}
}

// paceHold waits until base after start (the holdoff after a triggered
// frame, when longer), rechecking the live holdoff: a changed setting must
// not strand STOP or FORCE behind an obsolete long hold.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (l *Loop) paceHold(start time.Time, triggered bool, base time.Duration) {
	c, clk := l.c, l.cfg.Clock
	for !l.Stopping() && c.Running() && !c.ForcePending() {
		floor := base
		if triggered {
			if h, _ := c.Pacing(); h > floor {
				floor = h
			}
		}
		d := floor - clk.Now().Sub(start)
		if d <= 0 {
			return
		}
		clk.Sleep(min(d, 50*time.Millisecond))
		c.Beat()
	}
}
