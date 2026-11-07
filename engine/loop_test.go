package engine

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

func loopState(sweep hal.Sweep) instrument.State {
	ch := hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6}
	return instrument.State{
		Channels: []hal.Channel{ch, ch},
		TimeDivS: 1e-6,
		Trigger:  instrument.Edge{Source: hal.Source{Kind: hal.Analog}, Slope: hal.Rising},
		Sweep:    sweep,
		Running:  true,
	}
}

// runLoop runs a loop over b until it has published n frames or the
// timeout passes, and returns the frames.
func runLoop(t *testing.T, b *halfake.Board, inst *instrument.Model, n int) []*sample.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	var got []*sample.Frame
	l, _ := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond, Publish: func(f *sample.Frame) {
		mu.Lock()
		got = append(got, f)
		if len(got) >= n {
			cancel()
		}
		mu.Unlock()
	}})
	l.Run(ctx)
	mu.Lock()
	defer mu.Unlock()
	return got
}

// The loop commits in the HAL's order, arms, waits, reads and publishes;
// re-arms without committing again while nothing changes; the published
// frames are numbered from 1.
// TRLC-LINKS: REQ-DSO-001, REQ-DSO-008, REQ-DSO-020
func TestLoopCycle(t *testing.T) {
	b := halfake.New(halfake.Config{})
	inst := instrument.New(loopState(hal.Auto), nil)
	frames := runLoop(t, b, inst, 3)
	if len(frames) < 3 {
		t.Fatalf("published %d frames", len(frames))
	}
	calls := strings.Join(b.Calls(), " ")
	want := strings.Join([]string{"Service", "FrontEnd.Set", "FrontEnd.Set", "Configure", "Program", "Arm", "Service", "Wait", "Read", "Service"}, " ")
	if !strings.HasPrefix(calls, want) {
		t.Fatalf("calls %q\nwant prefix %q", calls, want)
	}
	if n := strings.Count(calls, "Program"); n != 1 {
		t.Fatalf("committed %d times: %s", n, calls)
	}
	if kinds := b.ArmKinds(); kinds[0] != "commit" || slices.Contains(kinds[1:], "commit") {
		t.Fatalf("arm kinds %v", kinds)
	}
	for i, f := range frames {
		if f.Seq != uint64(i+1) {
			t.Fatalf("frame %d published as seq %d: a consumer waiting for the next frame never wakes", i, f.Seq)
		}
	}
	f := frames[0]
	if len(f.Traces) != 2 || f.Len() == 0 {
		t.Fatalf("frame traces %d len %d", len(f.Traces), f.Len())
	}
	if inst.Acquisition() != instrument.Triggered {
		t.Fatalf("run state %v", inst.Acquisition())
	}
	inst.WithFrame(func(g *sample.Frame) {
		if g == nil {
			t.Fatal("the instrument has no frame")
		}
	})
}

// A single shot publishes one frame and stops the run in the instrument.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func TestLoopSingle(t *testing.T) {
	b := halfake.New(halfake.Config{})
	inst := instrument.New(loopState(hal.Single), nil)
	ctx, cancel := context.WithCancel(context.Background())
	l, c := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for inst.State().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done
	if inst.State().Running {
		t.Fatal("the single shot did not stop the run")
	}
	if n := strings.Count(strings.Join(b.Calls(), " "), "Read"); n != 1 {
		t.Fatalf("read %d records", n)
	}
	if _, fresh := c.Consume(); !fresh {
		t.Fatal("no frame for the consumer")
	}
	if inst.Acquisition() != instrument.Stopped {
		t.Fatalf("run state %v", inst.Acquisition())
	}
}

// A state the board refuses at the commit goes back to the last state it
// took and reaches the instrument's listeners.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1001
func TestLoopRefusedCommit(t *testing.T) {
	b := halfake.New(halfake.Config{})
	inst := instrument.New(loopState(hal.Auto), nil)
	var heard []error
	var mu sync.Mutex
	inst.OnRefused(func(err error) { mu.Lock(); heard = append(heard, err); mu.Unlock() })
	ctx, cancel := context.WithCancel(context.Background())
	l, _ := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	_ = inst.Change(func(s *instrument.State) error {
		s.Trigger.Source = hal.Source{Kind: hal.Digital}
		return nil
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(heard)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(5 * time.Millisecond)
	cancel()
	<-done
	if len(heard) != 1 {
		t.Fatalf("refusals heard: %v", heard)
	}
	if _, ok := hal.AsRefusal(heard[0]); !ok {
		t.Fatalf("not a refusal: %v", heard[0])
	}
	if got := inst.State().Trigger.Source.Kind; got != hal.Analog {
		t.Fatalf("the refused source stayed: %v", got)
	}
}

// LONG work runs on the owner with the acquisition stopped, and the next
// arm commits the whole state again; a forced trigger reaches the board.
// TRLC-LINKS: REQ-DSO-008
func TestLoopPostedWorkAndForce(t *testing.T) {
	b := halfake.New(halfake.Config{})
	inst := instrument.New(loopState(hal.NormalSweep), nil)
	ran := make(chan hal.AcqState, 1)
	inst.Post(func(context.Context) { ran <- b.Acquirer().State() })
	_ = inst.Force()
	frames := runLoop(t, b, inst, 2)
	if len(frames) < 2 {
		t.Fatalf("published %d frames", len(frames))
	}
	if st := <-ran; st == hal.Armed {
		t.Fatal("LONG work ran while armed")
	}
	if inst.ForcePending() {
		t.Fatal("the force was not taken")
	}
}

type panicBoard struct{ *halfake.Board }

type panicPlatform struct{ hal.Platform }

func (panicPlatform) Service() { panic("service fault") }

func (b panicBoard) Platform() hal.Platform { return panicPlatform{b.Board.Platform()} }

// A panic parks the loop wedged until its context ends.
// TRLC-LINKS: REQ-DSO-001
func TestLoopPanicParks(t *testing.T) {
	inst := instrument.New(loopState(hal.Auto), nil)
	l, _ := NewHALLoop(HALConfig{Board: panicBoard{halfake.New(halfake.Config{})}, Instrument: inst})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !l.Wedged() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !l.Wedged() {
		t.Fatal("not wedged")
	}
	select {
	case <-done:
		t.Fatal("returned before its context ended")
	default:
	}
	cancel()
	<-done
}
