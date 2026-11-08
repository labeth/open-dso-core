package engine

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// recordSpan is the whole stored record the test board places.
const recordSpan = 3000

// wholeBoard is the fake whose readout places its whole stored record; with
// stall, a capture waits until it is forced.
type wholeBoard struct {
	*halfake.Board
	stall  bool
	forced *atomic.Bool
	forces *atomic.Int32
}

func newWholeBoard(stall bool) wholeBoard {
	return wholeBoard{Board: halfake.New(halfake.Config{}), stall: stall, forced: new(atomic.Bool), forces: new(atomic.Int32)}
}

func (b wholeBoard) Readout() hal.Readout { return wholeReadout{b.Board.Readout()} }

func (b wholeBoard) Acquirer() hal.Acquirer {
	if !b.stall {
		return b.Board.Acquirer()
	}
	return stallAcquirer{b.Board.Acquirer(), b}
}

type wholeReadout struct{ hal.Readout }

func (wholeReadout) ScreenWindow(r hal.ScreenRequest) (hal.Window, error) {
	if r.Record {
		return hal.Window{Role: hal.Main, StartSample: -recordSpan / 2, Span: recordSpan}, nil
	}
	return hal.Window{Role: hal.Main}, nil
}

type stallAcquirer struct {
	hal.Acquirer
	b wholeBoard
}

func (a stallAcquirer) Wait(ctx context.Context, d time.Duration) (hal.Status, error) {
	if !a.b.forced.Swap(false) {
		time.Sleep(time.Millisecond)
		return hal.Status{}, hal.ErrTimeout
	}
	return a.Acquirer.Wait(ctx, d)
}

func (a stallAcquirer) Force() error {
	a.b.forced.Store(true)
	a.b.forces.Add(1)
	return a.Acquirer.Force()
}

// collector runs a HAL loop over b and keeps what it publishes.
type collector struct {
	mu     sync.Mutex
	frames []*sample.Frame
	cancel context.CancelFunc
	done   chan struct{}
}

func startLoop(b hal.Board, inst *instrument.Model) *collector {
	c := &collector{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	l, _ := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond, Publish: func(f *sample.Frame) {
		c.mu.Lock()
		c.frames = append(c.frames, f)
		c.mu.Unlock()
	}})
	go func() { l.Run(ctx); close(c.done) }()
	return c
}

func (c *collector) waitFor(t *testing.T, n int) []*sample.Frame {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := append([]*sample.Frame(nil), c.frames...)
		c.mu.Unlock()
		if len(got) >= n {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("published fewer than %d frames", n)
	return nil
}

func (c *collector) stop() []*sample.Frame {
	c.cancel()
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frames
}

// A finished SINGLE shows its live frame and then its whole stored record;
// a setting committed while stopped shows the held record again, without
// reading the board.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func TestHALSingleShowsWholeRecord(t *testing.T) {
	b := newWholeBoard(false)
	inst := instrument.New(loopState(hal.Single), nil)
	c := startLoop(b, inst)
	got := c.waitFor(t, 2)
	if got[0].Len() == recordSpan || got[1].Len() != recordSpan {
		t.Fatalf("live frame %d samples, then %d: want the screen, then the record of %d", got[0].Len(), got[1].Len(), recordSpan)
	}
	if got[1].ContentID == 0 {
		t.Fatal("the whole record has no content id")
	}
	reads := strings.Count(strings.Join(b.Calls(), " "), "Read")
	_ = inst.Change(func(s *instrument.State) error { s.Channels[0].VoltsDiv = 2; return nil })
	got = c.waitFor(t, 3)
	frames := c.stop()
	if inst.State().Running {
		t.Fatal("the single shot did not stop the run")
	}
	if len(frames) != 3 {
		t.Fatalf("published %d frames", len(frames))
	}
	again := got[2]
	if again.Seq != 3 || again.ContentID != got[1].ContentID || again.Len() != recordSpan {
		t.Fatalf("replay seq %d content %d len %d", again.Seq, again.ContentID, again.Len())
	}
	if got[1].Seq != 2 {
		t.Fatal("the replay renumbered the held frame")
	}
	if n := strings.Count(strings.Join(b.Calls(), " "), "Read"); n != reads {
		t.Fatalf("the replay read the board: %d reads, then %d", reads, n)
	}
}

// An average stays shown: a finished averaged SINGLE reads no record over it.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-012
func TestHALAverageStaysShown(t *testing.T) {
	b := newWholeBoard(false)
	s := loopState(hal.Single)
	s.Mode, s.Averages = hal.Average, 2
	inst := instrument.New(s, nil)
	c := startLoop(b, inst)
	deadline := time.Now().Add(5 * time.Second)
	for inst.State().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	for _, f := range c.stop() {
		if f.Len() == recordSpan {
			t.Fatal("the stopped record was read over the average")
		}
	}
	if inst.State().Running {
		t.Fatal("the averaged single shot did not stop")
	}
}

// A STOP that lands in a waiting capture lets it finish, forced once
// stopWait passed without a trigger, and shows its record whole.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func TestHALStopInWaitShowsWholeRecord(t *testing.T) {
	b := newWholeBoard(true)
	inst := instrument.New(loopState(hal.NormalSweep), nil)
	c := startLoop(b, inst)
	time.Sleep(30 * time.Millisecond) // armed, waiting for a trigger that never comes
	stopAt := time.Now()
	inst.SetRunning(false)
	got := c.waitFor(t, 1)
	took := time.Since(stopAt)
	time.Sleep(20 * time.Millisecond)
	frames := c.stop()
	if len(frames) != 1 || got[0].Len() != recordSpan {
		t.Fatalf("published %d frames, the first %d samples", len(frames), got[0].Len())
	}
	if took < stopWait {
		t.Fatalf("forced after %v, before the stop's wait of %v", took, stopWait)
	}
	if b.forces.Load() != 1 {
		t.Fatalf("forced %d times", b.forces.Load())
	}
	if inst.State().Running {
		t.Fatal("still running")
	}
}

// On a board that cannot place its record a STOP aborts the waiting
// capture and the last frame stays shown.
// TRLC-LINKS: REQ-DSO-008
func TestHALStopAbortsWithoutRecord(t *testing.T) {
	b := newWholeBoard(true)
	inst := instrument.New(loopState(hal.NormalSweep), nil)
	c := startLoop(plainBoard{b}, inst)
	time.Sleep(30 * time.Millisecond)
	inst.SetRunning(false)
	time.Sleep(stopWait + 50*time.Millisecond)
	if frames := c.stop(); len(frames) != 0 {
		t.Fatalf("published %d frames", len(frames))
	}
	if b.forces.Load() != 0 {
		t.Fatal("the stop forced a capture")
	}
}

// plainBoard is the stalling board without a readout that places records.
type plainBoard struct{ wholeBoard }

func (b plainBoard) Readout() hal.Readout { return b.Board.Readout() }
