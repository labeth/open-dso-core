// Package conformance is the HAL contract as a test suite: any board runs it
// from its own tests with Run, against the real board, its fakes or a
// simulator, and gets one subtest per clause of the contract (package hal's
// documentation and HAL-V1). A clause a board's harness cannot exercise is
// skipped with the reason its Options give, never passed silently.
//
//	func TestHALConformance(t *testing.T) {
//		conformance.Run(t, newBoard(t), conformance.Options{BringUp: true})
//	}
package conformance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"slices"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// Options say what the board's harness can do. The zero value runs every
// clause on a board that is not brought up yet.
type Options struct {
	// BringUp runs Platform.BringUp first; leave it false when the caller
	// brought the board up.
	BringUp bool
	// Request is the acquisition the clauses plan (zero: every channel at
	// the timebase step nearest 10 µs/div, automatic depth).
	Request hal.Request
	// WaitTimeout bounds one record (default 5 s).
	WaitTimeout time.Duration
	// NoHoldOff is why the harness cannot keep a trigger from firing (a
	// fake whose records always trigger); the clauses that need a trigger
	// that never fires are then skipped with it.
	NoHoldOff string
	// CommitArms counts the board's commit arms so far, where the board can
	// tell; nil skips the clauses that need it.
	CommitArms func() int
	// Inject feeds one event into the board's input queue the way its
	// panel would; nil skips the input round trip.
	Inject func(hal.InputEvent) bool
	// Skip names clauses to skip, each with its reason.
	Skip map[string]string
}

// Clauses are the subtests Run runs, in order.
var Clauses = []string{
	"profile", "devices", "planner", "need-refusal", "stage-commit", "unchanged-noop",
	"record", "wait-poll", "wait-timeout", "wait-cancel", "force", "freerun",
	"readout", "readout-windows", "readout-refusal", "stopped-record",
	"trigger-refusal", "trigger-default", "frontend-snap", "input", "storage", "optional-parts",
}

// suite is one run of the clauses over a board.
type suite struct {
	b    hal.Board
	o    Options
	p    hal.Profile
	id   hal.Identity
	req  hal.Request
	init []hal.Channel // each channel's setting the run starts from
}

// Run runs every clause of the HAL contract against b, one subtest each.
// TRLC-LINKS: REQ-DSO-1001
func Run(t *testing.T, b hal.Board, o Options) {
	t.Helper()
	if o.WaitTimeout <= 0 {
		o.WaitTimeout = 5 * time.Second
	}
	s := &suite{b: b, o: o}
	if o.BringUp {
		id, err := b.Platform().BringUp(t.Context())
		if err != nil {
			t.Fatalf("BringUp: %v", err)
		}
		s.id = id
	}
	s.p = b.Profile()
	if s.p.Channels <= 0 {
		t.Fatalf("the profile has %d channels: nothing to run", s.p.Channels)
	}
	s.req = s.request()
	s.init = s.setChannels(t)
	checks := map[string]func(*testing.T){
		"profile": s.profile, "devices": s.devices, "planner": s.planner, "need-refusal": s.needRefusal,
		"stage-commit": s.stageCommit, "unchanged-noop": s.unchangedNoop, "record": s.record,
		"wait-poll": s.waitPoll, "wait-timeout": s.waitTimeout, "wait-cancel": s.waitCancel,
		"force": s.force, "freerun": s.freeRun, "readout": s.readout, "readout-windows": s.readoutWindows,
		"readout-refusal": s.readoutRefusal, "stopped-record": s.stoppedRecord,
		"trigger-refusal": s.triggerRefusal, "trigger-default": s.triggerDefault,
		"frontend-snap": s.frontEndSnap, "input": s.input, "storage": s.storage, "optional-parts": s.optionalParts,
	}
	for _, name := range Clauses {
		fn := checks[name]
		t.Run(name, func(t *testing.T) {
			if why, ok := o.Skip[name]; ok {
				t.Skip(why)
			}
			guard(t, fn)
		})
	}
	_ = b.Acquirer().Stop()
}

// guard runs fn and reports a panic as the clause's failure, so one broken
// facet does not end the run of the others.
// TRLC-LINKS: REQ-DSO-1001
func guard(t *testing.T, fn func(*testing.T)) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	fn(t)
}

// request is the run's request: Options.Request, or every channel at the
// timebase step nearest 10 µs/div.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) request() hal.Request {
	r := s.o.Request
	if len(r.Channels) == 0 {
		for ch := range s.p.Channels {
			r.Channels = append(r.Channels, ch)
		}
	}
	if r.TimePerDivS <= 0 {
		r.TimePerDivS = nearest(s.p.Timebase.TdivS, 10e-6)
	}
	if r.PretrigFrac == 0 {
		r.PretrigFrac = 0.5
	}
	return r
}

// nearest is the value of ladder nearest v on a log scale; v when the
// ladder is empty.
// TRLC-LINKS: REQ-DSO-1001
func nearest(ladder []float64, v float64) float64 {
	best, d := v, math.Inf(1)
	for _, x := range ladder {
		if x > 0 {
			if e := math.Abs(math.Log(x / v)); e < d {
				best, d = x, e
			}
		}
	}
	return best
}

// setChannels stages every channel of the request on, at the ladder step
// nearest 1 V/div, and returns what the board made of it.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) setChannels(t *testing.T) []hal.Channel {
	t.Helper()
	fe := s.b.FrontEnd()
	out := make([]hal.Channel, s.p.Channels)
	if fe == nil {
		return out
	}
	for ch := range s.p.Channels {
		c := fe.Get(ch)
		c.On = slices.Contains(s.req.Channels, ch)
		if c.Probe <= 0 {
			c.Probe = 1
		}
		if c.Impedance == 0 {
			c.Impedance = 1e6
		}
		var vd []float64
		for _, st := range fe.Ladder(ch) {
			vd = append(vd, st.VoltsDiv)
		}
		c.VoltsDiv = nearest(vd, 1)
		got, err := fe.Set(ch, c)
		if err != nil {
			t.Logf("FrontEnd.Set(%d, %+v): %v", ch, c, err)
			got = c
		}
		out[ch] = got
	}
	return out
}

// edge is the board's default edge stage on the first requested channel,
// with sweep sw.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) edge(sw hal.Sweep) (hal.TriggerSpec, error) {
	src := hal.Source{Kind: hal.Analog, Index: s.req.Channels[0]}
	st, err := s.b.Trigger().Default(hal.NodeEdge, src)
	if err != nil {
		return hal.TriggerSpec{}, fmt.Errorf("Default(edge, %+v): %w", src, err)
	}
	return hal.TriggerSpec{Stages: []hal.TrigStage{st}, Sweep: sw}, nil
}

// unreachable is a Normal edge trigger at the top of its level grid, which
// the run's signals stay below.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) unreachable() (hal.TriggerSpec, error) {
	spec, err := s.edge(hal.NormalSweep)
	if err != nil {
		return spec, err
	}
	st := &spec.Stages[0]
	g := s.b.Trigger().LevelGrid(st.Source)
	st.Levels.High, st.Levels.Slope = g.Max, hal.Rising
	if st.Levels.Low > st.Levels.High {
		st.Levels.Low = st.Levels.High
	}
	return spec, nil
}

// commit plans the run's request, configures it and programs spec.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) commit(spec hal.TriggerSpec) (hal.Plan, error) {
	plan, err := s.b.Planner().Plan(s.req)
	if err != nil {
		return plan, fmt.Errorf("plan: %w", err)
	}
	if err := s.b.Acquirer().Configure(plan); err != nil {
		return plan, fmt.Errorf("configure: %w", err)
	}
	if err := s.b.Trigger().Program(spec); err != nil {
		return plan, fmt.Errorf("program: %w", err)
	}
	return plan, nil
}

// autoAfter is how long the run's AUTO waits for a trigger before it
// forces one, as the engine's AUTO does.
const autoAfter = 300 * time.Millisecond

// waitSlice is one Wait of the run's owner loop.
const waitSlice = 50 * time.Millisecond

// await waits for a record the way the engine's owner loop does: in short
// Waits, forcing a trigger in AUTO once nothing triggered for a while.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) await(ctx context.Context, sw hal.Sweep) (hal.Status, error) {
	start := time.Now()
	forced := false
	for {
		st, err := s.b.Acquirer().Wait(ctx, waitSlice)
		if !errors.Is(err, hal.ErrTimeout) {
			return st, err
		}
		el := time.Since(start)
		if el > s.o.WaitTimeout {
			return st, fmt.Errorf("no record in %v: %w", s.o.WaitTimeout, err)
		}
		if sw == hal.Auto && !forced && !st.Triggered && el > autoAfter {
			if err := s.b.Acquirer().Force(); err != nil {
				return st, fmt.Errorf("force: %w", err)
			}
			forced = true
		}
	}
}

// acquire commits spec, arms and waits for a record.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) acquire(ctx context.Context, spec hal.TriggerSpec) (hal.Plan, hal.Status, error) {
	plan, err := s.commit(spec)
	if err != nil {
		return plan, hal.Status{}, err
	}
	if err := s.b.Acquirer().Arm(); err != nil {
		return plan, hal.Status{}, fmt.Errorf("arm: %w", err)
	}
	st, err := s.await(ctx, spec.Sweep)
	return plan, st, err
}

// mainRead reads the main window of every requested channel as the engine
// does: one window, or one per channel when the board reads one at a time.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) mainRead(ctx context.Context, rec hal.RecordID) ([]*sample.Frame, error) {
	var ws []hal.Window
	if s.b.Readout().Limits().ChannelsPerWindow == 1 {
		for _, ch := range s.req.Channels {
			ws = append(ws, hal.Window{Role: hal.Main, Channels: []int{ch}})
		}
	} else {
		ws = []hal.Window{{Role: hal.Main, Channels: s.req.Channels}}
	}
	fs := make([]*sample.Frame, len(ws))
	for i := range fs {
		fs[i] = &sample.Frame{}
	}
	err := s.b.Readout().Read(ctx, hal.ReadRequest{Record: rec, Segments: [2]int{0, 1}, Windows: ws}, fs, s.b.Platform().Service)
	return fs, err
}

// isRefusal fails t unless err is a *Refusal with a feature and a reason.
// TRLC-LINKS: REQ-DSO-1001
func isRefusal(t *testing.T, err error, what string) {
	t.Helper()
	r, ok := hal.AsRefusal(err)
	switch {
	case err == nil:
		t.Errorf("%s: accepted; want a *hal.Refusal", what)
	case !ok:
		t.Errorf("%s: %v; want a *hal.Refusal", what, err)
	case r.Reason == "" || r.Feature == "":
		t.Errorf("%s: refusal %+v without its feature or reason", what, *r)
	}
}
