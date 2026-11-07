package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// stubAcq is an acquirer whose record is ready after a number of polls,
// triggered or not, on a fake clock.
type stubAcq struct {
	log      *[]string
	readyAt  int // polls until the record is ready; 0: never
	trigAt   int // polls until the hardware triggers; 0: never
	polls    int
	forced   bool
	waitErr  error
	armErr   error
	state    hal.AcqState
	forceErr error
}

func (a *stubAcq) Configure(hal.Plan) error { return nil }

func (a *stubAcq) Arm() error {
	*a.log = append(*a.log, "Arm")
	a.polls, a.forced = 0, false
	if a.armErr != nil {
		return a.armErr
	}
	a.state = hal.Armed
	return nil
}

func (a *stubAcq) Wait(context.Context, time.Duration) (hal.Status, error) {
	a.polls++
	trig := a.trigAt > 0 && a.polls >= a.trigAt
	if a.waitErr != nil {
		*a.log = append(*a.log, "Wait fault")
		return hal.Status{}, a.waitErr
	}
	if a.forced || a.readyAt > 0 && a.polls >= a.readyAt {
		*a.log = append(*a.log, "Wait ready")
		a.state = hal.Ready
		return hal.Status{Triggered: trig, Forced: a.forced, Done: true}, nil
	}
	return hal.Status{Triggered: trig}, hal.ErrTimeout
}

func (a *stubAcq) Force() error {
	*a.log = append(*a.log, "Force")
	a.forced = true
	return a.forceErr
}

func (a *stubAcq) Stop() error {
	*a.log = append(*a.log, "Stop")
	a.state = hal.Idle
	return nil
}

func (a *stubAcq) State() hal.AcqState { return a.state }

// scriptCycle is a cycle whose answers a test sets.
type scriptCycle struct {
	log      *[]string
	capture  Capture
	verdict  Verdict
	watch    func(settings bool) Interrupt
	liveness bool
	holdoff  time.Duration
	display  time.Duration
	running  bool
	single   bool
	gen      uint64
	force    bool
	stop     func()
}

func (c *scriptCycle) Open()                      {}
func (c *scriptCycle) Close()                     {}
func (c *scriptCycle) Serve()                     {}
func (c *scriptCycle) Claim(context.Context) bool { return false }
func (c *scriptCycle) Plan(context.Context) (Capture, bool) {
	return c.capture, true
}

func (c *scriptCycle) Watch(settings bool) Interrupt {
	if c.watch != nil {
		return c.watch(settings)
	}
	return Continue
}
func (c *scriptCycle) ArmFailed(err error) { *c.log = append(*c.log, "arm failed: "+err.Error()) }
func (c *scriptCycle) Fault(err, haltErr error) {
	*c.log = append(*c.log, fmt.Sprintf("fault %v %v", err, haltErr))
}

func (c *scriptCycle) Frame(_ context.Context, _ Capture, a Armed) (*sample.Frame, Verdict, bool) {
	*c.log = append(*c.log, fmt.Sprintf("frame forced %v user %v", a.Forced, a.UserForced))
	v := c.verdict
	v.UserForced = v.UserForced || a.UserForced
	return &sample.Frame{Status: a.Status}, v, true
}
func (c *scriptCycle) Liveness() bool { *c.log = append(*c.log, "liveness"); return c.liveness }
func (c *scriptCycle) Publish(_ *sample.Frame, _ Verdict, publish bool) {
	*c.log = append(*c.log, fmt.Sprintf("publish %v", publish))
}

func (c *scriptCycle) Done(time.Time, time.Duration) {
	*c.log = append(*c.log, "done")
	if c.stop != nil {
		c.stop()
	}
}
func (c *scriptCycle) Pacing() (time.Duration, time.Duration) { return c.holdoff, c.display }
func (c *scriptCycle) Beat()                                  {}
func (c *scriptCycle) Running() bool                          { return c.running }
func (c *scriptCycle) SingleArmed() bool                      { return c.single }
func (c *scriptCycle) SingleGen() uint64                      { return c.gen }
func (c *scriptCycle) TakeForce() bool                        { f := c.force; c.force = false; return f }
func (c *scriptCycle) ForcePending() bool                     { return c.force }
func (c *scriptCycle) StopOnFail()                            { *c.log = append(*c.log, "stop on fail"); c.running = false }

func (c *scriptCycle) SingleDone(Verdict) {
	*c.log = append(*c.log, "single done")
	c.single, c.running = false, false
}

// fakeClock advances only when slept.
func fakeClock() (Clock, *time.Time) {
	now := time.Unix(1000, 0)
	return Clock{Now: func() time.Time { return now }, Sleep: func(d time.Duration) { now = now.Add(d) }}, &now
}

func onePass(t *testing.T, acq *stubAcq, c *scriptCycle) (string, time.Duration) {
	t.Helper()
	clk, now := fakeClock()
	start := *now
	l := NewLoop(LoopConfig{Acquirer: acq, Cycle: c, Clock: clk})
	stopAfter := time.AfterFunc(5*time.Second, l.Stop)
	defer stopAfter.Stop()
	l.pass(context.Background())
	return strings.Join(*c.log, "; "), now.Sub(start)
}

// AUTO forces after its wait unless the hardware triggered; the FORCE key
// forces once in NORMAL; a free-running capture is never forced.
// TRLC-LINKS: REQ-DSO-011
func TestArmAndWaitForces(t *testing.T) {
	for _, tc := range []struct {
		name    string
		acq     stubAcq
		cap     Capture
		force   bool
		running bool
		want    string
	}{
		{
			"auto forced",
			stubAcq{},
			Capture{Normal: true, AutoWait: 10 * time.Millisecond},
			false, true,
			"Arm; Force; Wait ready; frame forced true user false; liveness; publish true; done",
		},
		{
			"auto triggered late",
			stubAcq{trigAt: 2, readyAt: 40},
			Capture{Normal: true, AutoWait: 10 * time.Millisecond},
			false, true,
			"Arm; Wait ready; frame forced false user false; liveness; publish true; done",
		},
		{
			"norm force key",
			stubAcq{},
			Capture{Normal: true, Norm: true, AutoWait: 10 * time.Millisecond},
			true, true,
			"Arm; Force; Wait ready; frame forced true user true; publish true; done",
		},
		{
			"free run",
			stubAcq{readyAt: 3},
			Capture{AutoWait: 10 * time.Millisecond},
			true, true,
			"Arm; Wait ready; frame forced false user false; liveness; publish true; done",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			acq := tc.acq
			acq.log = &log
			c := &scriptCycle{log: &log, capture: tc.cap, force: tc.force, running: tc.running, liveness: true}
			if got, _ := onePass(t, &acq, c); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// A changed setting or other work halts the capture; another owner of the
// acquisition leaves it; a failed wait is reported after the halt.
// TRLC-LINKS: REQ-DSO-008
func TestArmAndWaitInterrupts(t *testing.T) {
	fault := errors.New("fifo overflow")
	for _, tc := range []struct {
		name  string
		watch func(bool) Interrupt
		acq   stubAcq
		want  string
	}{
		{"settings changed", func(s bool) Interrupt {
			if s {
				return Abort
			}
			return Continue
		}, stubAcq{}, "Arm; Stop"},
		{"work first", func(bool) Interrupt { return Abort }, stubAcq{}, "Arm; Stop"},
		{"other owner", func(bool) Interrupt { return Leave }, stubAcq{}, "Arm"},
		{"wait fault", nil, stubAcq{waitErr: fault}, "Arm; Wait fault; Stop; fault fifo overflow <nil>"},
		{"arm fault", nil, stubAcq{armErr: fault}, "Arm; arm failed: fifo overflow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			acq := tc.acq
			acq.log = &log
			c := &scriptCycle{log: &log, capture: Capture{Normal: true, Norm: true}, watch: tc.watch, running: true}
			if got, _ := onePass(t, &acq, c); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// The gate: AUTO's liveness rule for unqualified frames, NORMAL's hold, a
// mask stop, and a single shot completed only by the press its capture
// served.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-014
func TestGate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		v        Verdict
		norm     bool
		liveness bool
		single   bool
		gen      uint64
		want     string
	}{
		{"qualified", Verdict{Qualified: true, Complete: true}, true, false, false, 0, "publish true"},
		{"norm holds", Verdict{}, true, true, false, 0, "publish false"},
		{"auto liveness", Verdict{}, false, false, false, 0, "liveness; publish false"},
		{"mask stop", Verdict{MaskStop: true}, true, false, false, 0, "stop on fail; publish true"},
		{"single done", Verdict{Qualified: true, Complete: true}, true, false, true, 0, "publish true; single done"},
		{"single block incomplete", Verdict{Qualified: true}, true, false, true, 0, "publish true"},
		{"single pressed later", Verdict{Qualified: true, Complete: true}, true, false, true, 1, "publish true"},
		{"single full recall", Verdict{Qualified: true, Complete: true, FullRecall: true}, true, false, true, 0, "publish true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			c := &scriptCycle{log: &log, liveness: tc.liveness, single: tc.single, gen: tc.gen, running: true}
			l := NewLoop(LoopConfig{Cycle: c})
			l.gate(&sample.Frame{}, tc.v, tc.norm, 0)
			if got := strings.Join(log, "; "); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// A triggered frame holds the next arm off by the holdoff; the display's
// share holds from the arm.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func TestPacing(t *testing.T) {
	var log []string
	acq := &stubAcq{log: &log, readyAt: 1, trigAt: 1}
	c := &scriptCycle{log: &log, capture: Capture{Normal: true, Norm: true}, verdict: Verdict{Qualified: true, Complete: true}, running: true, holdoff: 120 * time.Millisecond}
	if _, took := onePass(t, acq, c); took != 120*time.Millisecond {
		t.Fatalf("holdoff took %v", took)
	}
	log = nil
	c = &scriptCycle{log: &log, capture: Capture{Normal: true}, verdict: Verdict{}, running: true, liveness: true, display: 30 * time.Millisecond}
	if _, took := onePass(t, &stubAcq{log: &log, readyAt: 1}, c); took != 30*time.Millisecond {
		t.Fatalf("display pace took %v", took)
	}
}
