// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// The loop's default pacing.
const (
	defaultWaitSlice = 250 * time.Millisecond
	defaultIdleWait  = 50 * time.Millisecond
)

// LoopConfig wires a Loop.
type LoopConfig struct {
	Board      hal.Board
	Instrument *instrument.Model
	// WaitSlice is how long one Acquirer.Wait may block before the loop
	// serves its commit point again; default 250 ms.
	WaitSlice time.Duration
	// IdleWait is the pause while stopped, or after a failed arm, before
	// the loop looks for work again; default 50 ms.
	IdleWait time.Duration
	// Publish hears every published frame on the owner goroutine, after
	// the instrument and the loop's consumers can see it; nil: none.
	Publish func(*sample.Frame)
	Logf    func(format string, a ...any)
}

// Loop is the core engine's owner loop over a board (HAL v1 §2.1), the one
// goroutine that touches the hardware. Each pass serves the board's
// housekeeping, runs LONG work posted to the instrument with the
// acquisition stopped, commits a changed instrument state in the HAL's
// order (FrontEnd.Set, Planner.Plan, Acquirer.Configure, Trigger.Program),
// arms, waits, reads one window per enabled channel and publishes the
// frame. A refused commit goes back to the last state the board took and
// is reported to the instrument's listeners. A single shot stops the run
// once its frame is published.
type Loop struct {
	cfg  LoopConfig
	b    hal.Board
	inst *instrument.Model

	gen     uint64           // the generation committed last
	good    instrument.State // the state last committed without a refusal
	running bool

	wedged atomic.Bool

	mu     sync.Mutex
	latest *sample.Frame // the newest published frame
	fresh  bool          // published since the last Consume
}

// NewLoop builds a loop over cfg's board and instrument, filling in the
// defaults for the zero fields.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008
func NewLoop(cfg LoopConfig) *Loop {
	if cfg.WaitSlice <= 0 {
		cfg.WaitSlice = defaultWaitSlice
	}
	if cfg.IdleWait <= 0 {
		cfg.IdleWait = defaultIdleWait
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Loop{cfg: cfg, b: cfg.Board, inst: cfg.Instrument, good: cfg.Instrument.State()}
}

// Wedged reports that the loop panicked and parked: it serves nothing
// until its context ends, so the health it feeds stops advancing.
// TRLC-LINKS: REQ-DSO-001
func (l *Loop) Wedged() bool { return l.wedged.Load() }

// Consume hands the newest published frame to the loop's one consumer (an
// engine fan-out); fresh is false when nothing was published since the
// last call. A published frame never changes.
// TRLC-LINKS: REQ-DSO-007
func (l *Loop) Consume() (*sample.Frame, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fresh := l.fresh
	l.fresh = false
	return l.latest, fresh
}

// Run is the owner loop; it returns when ctx ends. A panic is contained:
// logged, the loop marked wedged and parked until ctx ends, so the process
// keeps its hardware handles and its supervisor sees the health stop.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008
func (l *Loop) Run(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			l.cfg.Logf("engine: PANIC (wedged, parked): %v\n%s", r, debug.Stack())
			l.wedged.Store(true)
			<-ctx.Done()
		}
	}()
	for ctx.Err() == nil {
		l.pass(ctx)
	}
}

// pass is one turn of the owner's cycle.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008
func (l *Loop) pass(ctx context.Context) {
	a := l.b.Acquirer()
	l.b.Platform().Service()
	p := l.inst.Take()
	if len(p.Work) > 0 {
		// LONG work runs with the acquisition stopped; the next arm is a
		// commit arm of the whole state.
		if a.State() == hal.Armed {
			_ = a.Stop()
		}
		for _, w := range p.Work {
			w(ctx)
		}
		l.gen = 0
	}
	s, gen := p.State, p.Gen
	if gen != l.gen {
		if a.State() == hal.Armed {
			_ = a.Stop()
		}
		l.commitState(s, gen)
	}
	if !l.running {
		if a.State() == hal.Armed {
			_ = a.Stop()
		}
		l.idle(ctx)
		return
	}
	if a.State() != hal.Armed {
		if err := a.Arm(); err != nil {
			l.cfg.Logf("arm: %v", err)
			l.idle(ctx)
			return
		}
	}
	if p.Force {
		if err := a.Force(); err != nil {
			l.cfg.Logf("force: %v", err)
		}
	}
	st, err := a.Wait(ctx, l.cfg.WaitSlice)
	switch {
	case errors.Is(err, hal.ErrTimeout):
		if s.Sweep != hal.Auto {
			l.inst.SetRun(instrument.Ready)
		}
		return
	case ctx.Err() != nil:
		return
	case err != nil:
		l.cfg.Logf("wait: %v", err)
		return
	case !st.Done:
		return
	}
	if st.Triggered && !st.Forced {
		l.inst.SetRun(instrument.Triggered)
	} else {
		l.inst.SetRun(instrument.AutoRun)
	}
	if f := l.read(s); f != nil {
		l.publish(f)
	}
	if s.Sweep == hal.Single {
		_ = a.Stop()
		s.Running = false
		l.running = false
		l.good = s
		l.inst.Applied(s, gen)
	}
}

// commitState commits s, or on a refusal the last state the board took,
// and reports what was applied to the instrument.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1001
func (l *Loop) commitState(s instrument.State, gen uint64) {
	applied, err := l.commit(s)
	if err != nil {
		// The board refused: go back to the last state it took.
		l.cfg.Logf("settings refused: %v", err)
		l.inst.Refused(err)
		if applied, err = l.commit(l.good); err != nil {
			l.cfg.Logf("settings: restoring the last state: %v", err)
		}
	}
	l.good = applied
	l.gen = gen
	l.running = applied.Running
	l.inst.Applied(applied, gen)
	l.inst.SetRun(instrument.Armed)
}

// commit stages s on the board in the order the HAL requires and returns
// what the board applied (snapped values).
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1001
func (l *Loop) commit(s instrument.State) (instrument.State, error) {
	s = s.Clone()
	for c := range s.Channels {
		got, err := l.b.FrontEnd().Set(c, s.Channels[c])
		if err != nil {
			return s, err
		}
		s.Channels[c] = got
	}
	plan, err := l.b.Planner().Plan(s.Request())
	if err != nil {
		return s, err
	}
	if err := l.b.Acquirer().Configure(plan); err != nil {
		return s, err
	}
	return s, l.b.Trigger().Program(s.TriggerSpec())
}

// read reads the main window of every enabled channel of the halted record
// into one frame, a window a channel when the board reads one channel a
// window.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-1003
func (l *Loop) read(s instrument.State) *sample.Frame {
	on := s.Request().Channels
	if len(on) == 0 {
		_ = l.b.Acquirer().Stop()
		return nil
	}
	var ws []hal.Window
	if lim := l.b.Readout().Limits(); lim.ChannelsPerWindow == 1 {
		for _, c := range on {
			ws = append(ws, hal.Window{Role: hal.Main, Channels: []int{c}})
		}
	} else {
		ws = []hal.Window{{Role: hal.Main, Channels: on}}
	}
	fs := make([]*sample.Frame, len(ws))
	for i := range fs {
		fs[i] = &sample.Frame{}
	}
	if err := l.b.Readout().Read(hal.ReadRequest{Windows: ws}, fs, l.b.Platform().Service); err != nil {
		l.cfg.Logf("readout: %v", err)
		_ = l.b.Acquirer().Stop()
		return nil
	}
	f := fs[0]
	for _, g := range fs[1:] {
		f.Traces = append(f.Traces, g.Traces...)
	}
	return f
}

// publish hands f to the instrument, the loop's consumer and the Publish
// hook.
// TRLC-LINKS: REQ-DSO-007, REQ-DSO-020
func (l *Loop) publish(f *sample.Frame) {
	l.mu.Lock()
	l.latest, l.fresh = f, true
	l.mu.Unlock()
	l.inst.Published(f)
	if l.cfg.Publish != nil {
		l.cfg.Publish(f)
	}
}

// idle waits IdleWait or until ctx ends.
// TRLC-LINKS: REQ-DSO-008
func (l *Loop) idle(ctx context.Context) {
	t := time.NewTimer(l.cfg.IdleWait)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
