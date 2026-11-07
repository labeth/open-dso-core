// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// The HAL cycle's default pacing.
const (
	defaultHALWaitSlice = 50 * time.Millisecond
	defaultIdleWait     = 50 * time.Millisecond
)

// HALConfig wires a loop that drives a board through the HAL alone.
type HALConfig struct {
	Board      hal.Board
	Instrument *instrument.Model
	// WaitSlice is how long one wait blocks before the loop serves its
	// commit point again (a FORCE press waits at most this long); default
	// 50 ms.
	WaitSlice time.Duration
	// IdleWait is the pause while stopped, or after a failed arm, before
	// the loop looks for work again; default 50 ms.
	IdleWait time.Duration
	// Publish hears every published frame on the owner goroutine, after
	// the instrument and the loop's consumer can see it; nil: none.
	Publish func(*sample.Frame)
	Logf    func(format string, a ...any)
}

// HALCycle is the owner's cycle of a board whose settings are all HAL
// terms, driven by an instrument model: it runs posted LONG work with the
// acquisition stopped, commits a changed state in the HAL's order
// (FrontEnd.Set, Planner.Plan, Acquirer.Configure, Trigger.Program), reads
// one window per enabled channel (a window a channel on a board that reads
// one channel a window) and publishes every frame read. A refused commit
// goes back to the last state the board took and is reported to the
// instrument's listeners; a single shot stops the run once its frame is
// published. The board sweeps AUTO and holds off itself.
type HALCycle struct {
	cfg  HALConfig
	b    hal.Board
	inst *instrument.Model

	seq     uint64           // the last published frame's sequence number
	gen     uint64           // the generation committed last
	cur     instrument.State // the state committed last
	good    instrument.State // the state last committed without a refusal
	running bool

	mu     sync.Mutex
	latest *sample.Frame // the newest published frame
	fresh  bool          // published since the last Consume
}

var _ Cycle = (*HALCycle)(nil)

// NewHALLoop builds the owner loop of cfg's board over its HAL cycle.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008
func NewHALLoop(cfg HALConfig) (*Loop, *HALCycle) {
	if cfg.WaitSlice <= 0 {
		cfg.WaitSlice = defaultHALWaitSlice
	}
	if cfg.IdleWait <= 0 {
		cfg.IdleWait = defaultIdleWait
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	c := &HALCycle{cfg: cfg, b: cfg.Board, inst: cfg.Instrument, good: cfg.Instrument.State()}
	return NewLoop(LoopConfig{
		Acquirer:  cfg.Board.Acquirer(),
		Cycle:     c,
		WaitSlice: cfg.WaitSlice,
		OnPanic: func(r any, stack []byte) {
			cfg.Logf("engine: PANIC (wedged, parked): %v\n%s", r, stack)
		},
	}), c
}

// Consume hands the newest published frame to the cycle's one consumer
// (an engine fan-out); fresh is false when nothing was published since the
// last call. A published frame never changes.
// TRLC-LINKS: REQ-DSO-007
func (c *HALCycle) Consume() (*sample.Frame, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fresh := c.fresh
	c.fresh = false
	return c.latest, fresh
}

// Open has nothing to start.
// TRLC-LINKS: REQ-DSO-001
func (c *HALCycle) Open() {}

// Close has nothing to end.
// TRLC-LINKS: REQ-DSO-001
func (c *HALCycle) Close() {}

// Serve is the board's housekeeping.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008
func (c *HALCycle) Serve() { c.b.Platform().Service() }

// Claim runs posted LONG work with the acquisition stopped; the pass goes
// on to commit the whole state again.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) Claim(ctx context.Context) bool {
	work := c.inst.TakeWork()
	if len(work) == 0 {
		return false
	}
	c.stopArmed()
	for _, w := range work {
		w(ctx)
	}
	c.gen = 0
	return false
}

// Plan commits a changed state and plans a capture while running.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1001
func (c *HALCycle) Plan(ctx context.Context) (Capture, bool) {
	s, gen := c.inst.Wanted()
	if gen != c.gen {
		c.stopArmed()
		c.commitState(s, gen)
	}
	if !c.running {
		c.stopArmed()
		c.idle(ctx)
		return Capture{}, false
	}
	return Capture{Normal: true, Norm: c.cur.Sweep != hal.Auto}, true
}

// Watch aborts a waiting capture when the state changed or LONG work was
// posted.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) Watch(bool) Interrupt {
	if c.inst.Generation() != c.gen || c.inst.HasWork() {
		return Abort
	}
	return Continue
}

// ArmFailed logs the failure and pauses before the next pass.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) ArmFailed(err error) {
	c.cfg.Logf("arm: %v", err)
	time.Sleep(c.cfg.IdleWait)
}

// Fault logs a failed wait or halt; the loop's end is no fault.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) Fault(err, haltErr error) {
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		c.cfg.Logf("wait: %v", err)
	}
	if haltErr != nil {
		c.cfg.Logf("stop: %v", haltErr)
	}
}

// Frame reads the main window of every enabled channel of the ready
// record into one frame and reports the run state it was captured in.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-1003
func (c *HALCycle) Frame(ctx context.Context, _ Capture, a Armed) (*sample.Frame, Verdict, bool) {
	st := a.Status
	if st.Triggered && !st.Forced {
		c.inst.SetRun(instrument.Triggered)
	} else {
		c.inst.SetRun(instrument.AutoRun)
	}
	on := c.cur.Request().Channels
	if len(on) == 0 {
		_ = c.b.Acquirer().Stop()
		return nil, Verdict{}, false
	}
	var ws []hal.Window
	if lim := c.b.Readout().Limits(); lim.ChannelsPerWindow == 1 {
		for _, ch := range on {
			ws = append(ws, hal.Window{Role: hal.Main, Channels: []int{ch}})
		}
	} else {
		ws = []hal.Window{{Role: hal.Main, Channels: on}}
	}
	fs := make([]*sample.Frame, len(ws))
	for i := range fs {
		fs[i] = &sample.Frame{}
	}
	if err := c.b.Readout().Read(ctx, hal.ReadRequest{Windows: ws}, fs, c.b.Platform().Service); err != nil {
		c.cfg.Logf("readout: %v", err)
		_ = c.b.Acquirer().Stop()
		return nil, Verdict{}, false
	}
	f := fs[0]
	for _, g := range fs[1:] {
		f.Traces = append(f.Traces, g.Traces...)
	}
	// A FORCE press the wait had no time to take is this frame's: shown
	// even in NORMAL.
	forced := st.Forced || a.UserForced || c.inst.TakeForce()
	return f, Verdict{Qualified: st.Triggered && !forced, Complete: true, UserForced: forced}, true
}

// Liveness shows every frame: the board's AUTO has no gate of its own.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) Liveness() bool { return true }

// Publish numbers a shown frame (Frame.Seq, from 1: a consumer waits for
// the frame after the one it holds) and hands it to the instrument, the
// cycle's consumer and the Publish hook.
// TRLC-LINKS: REQ-DSO-007, REQ-DSO-020
func (c *HALCycle) Publish(f *sample.Frame, _ Verdict, publish bool) {
	if !publish {
		return
	}
	c.seq++
	f.Seq = c.seq
	c.mu.Lock()
	c.latest, c.fresh = f, true
	c.mu.Unlock()
	c.inst.Published(f)
	if c.cfg.Publish != nil {
		c.cfg.Publish(f)
	}
}

// Done has nothing to note.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) Done(time.Time, time.Duration) {}

// Pacing is none: the board holds off itself and draws its own display.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) Pacing() (holdoff, display time.Duration) { return 0, 0 }

// Beat has no heartbeat to advance.
// TRLC-LINKS: REQ-DSO-001
func (c *HALCycle) Beat() {}

// Running reports the committed run.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) Running() bool { return c.running }

// SingleArmed reports a single shot running.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) SingleArmed() bool { return c.running && c.cur.Sweep == hal.Single }

// SingleGen is the committed generation: a new SINGLE is a new state.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) SingleGen() uint64 { return c.gen }

// TakeForce takes the instrument's pending forced trigger.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) TakeForce() bool { return c.inst.TakeForce() }

// ForcePending looks at the instrument's pending forced trigger.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) ForcePending() bool { return c.inst.ForcePending() }

// StopOnFail stops the run (the cycle runs no mask test).
// TRLC-LINKS: REQ-DSO-014
func (c *HALCycle) StopOnFail() { c.stopRun() }

// SingleDone stops the run once the single shot's frame is published.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) SingleDone(Verdict) { c.stopRun() }

// stopRun stops the acquisition and the run, and gives the instrument the
// stopped state unless a controller changed it meanwhile.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (c *HALCycle) stopRun() {
	_ = c.b.Acquirer().Stop()
	s := c.cur.Clone()
	s.Running = false
	c.cur, c.good, c.running = s, s, false
	c.inst.Applied(s, c.gen)
}

// commitState commits s, or on a refusal the last state the board took,
// and reports what was applied to the instrument.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1001
func (c *HALCycle) commitState(s instrument.State, gen uint64) {
	applied, err := c.commit(s)
	if err != nil {
		// The board refused: go back to the last state it took.
		c.cfg.Logf("settings refused: %v", err)
		c.inst.Refused(err)
		if applied, err = c.commit(c.good); err != nil {
			c.cfg.Logf("settings: restoring the last state: %v", err)
		}
	}
	c.cur, c.good, c.gen = applied, applied, gen
	c.running = applied.Running
	c.inst.Applied(applied, gen)
	c.inst.SetRun(instrument.Armed)
}

// commit stages s on the board in the order the HAL requires and returns
// what the board applied (snapped values).
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1001
func (c *HALCycle) commit(s instrument.State) (instrument.State, error) {
	s = s.Clone()
	for ch := range s.Channels {
		got, err := c.b.FrontEnd().Set(ch, s.Channels[ch])
		if err != nil {
			return s, err
		}
		s.Channels[ch] = got
	}
	plan, err := c.b.Planner().Plan(s.Request())
	if err != nil {
		return s, err
	}
	if err := c.b.Acquirer().Configure(plan); err != nil {
		return s, err
	}
	return s, c.b.Trigger().Program(s.TriggerSpec())
}

// stopArmed stops an armed acquisition.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) stopArmed() {
	if c.b.Acquirer().State() == hal.Armed {
		_ = c.b.Acquirer().Stop()
	}
}

// idle waits IdleWait or until ctx ends.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) idle(ctx context.Context) {
	t := time.NewTimer(c.cfg.IdleWait)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
