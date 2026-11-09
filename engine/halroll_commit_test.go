package engine

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// scaledStream is fakeStream whose blocks state their scale (1 V a code
// step over 25 codes a division at the V/div it is told) and that can be
// made to fail one lease.
type scaledStream struct {
	fakeStream
	vdiv atomic.Uint64 // math.Float64bits
	fail atomic.Bool
}

func (s *scaledStream) Lease(ctx context.Context) (hal.StreamBlock, error) {
	if s.fail.Swap(false) {
		return hal.StreamBlock{}, errors.New("stream overrun")
	}
	b, err := s.fakeStream.Lease(ctx)
	if err != nil {
		return b, err
	}
	sc := sample.Scale{Zero: 128, PerCode: math.Float64frombits(s.vdiv.Load()) / 25}
	for range b.Formats {
		b.Scales = append(b.Scales, sc)
	}
	return b, nil
}

func (s *scaledStream) TakesCommits() bool { return true }

type scaledRollBoard struct {
	*halfake.Board
	st *scaledStream
}

func (b scaledRollBoard) Streamer() (hal.Streamer, bool) { return b.st, true }

func (b scaledRollBoard) Profile() hal.Profile {
	p := b.Board.Profile()
	p.Timebase.RollFromS = 100e-3
	return p
}

// newestRoll waits for a roll frame published after seq whose newest
// sample is covered.
func newestRoll(t *testing.T, c *HALCycle, seq uint64) *sample.Frame {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if g, _ := c.Consume(); g != nil && g.Seq > seq && g.Flags.Has(sample.Roll) && !g.Traces[0].Held[g.Len()-1] {
			return g
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no roll frame")
	return nil
}

// A V/div committed while the view rolls is committed as it comes: the
// board's stream goes on (no new start, the view kept), the samples before
// the change are drawn again at the new V/div from their volts, and the
// samples after it come at the scale the board's blocks state.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-008
func TestHALRollCommitsWhileItRolls(t *testing.T) {
	st := &scaledStream{}
	st.vdiv.Store(math.Float64bits(1))
	b := scaledRollBoard{halfake.New(halfake.Config{}), st}
	s := loopState(hal.Auto)
	s.TimeDivS = 200e-3
	inst := instrument.New(s, nil)
	l, c := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	f := newestRoll(t, c, 0)
	starts := st.starts.Load()
	if starts != 1 {
		t.Fatalf("%d stream starts", starts)
	}
	n := f.Len()
	old := f.Traces[0].Codes8[n-1]
	if err := inst.Change(func(s *instrument.State) error {
		s.Channels[0].VoltsDiv = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st.vdiv.Store(math.Float64bits(2))
	deadline := time.Now().Add(5 * time.Second)
	var g *sample.Frame
	for g == nil || g.Traces[0].Scale.PerCode != 2.0/25 {
		if time.Now().After(deadline) {
			t.Fatalf("no frame at the new scale: %+v", g.Traces[0].Scale)
		}
		g = newestRoll(t, c, f.Seq)
	}
	if got := st.starts.Load(); got != starts || st.stops.Load() != 0 {
		t.Fatalf("the commit restarted the stream: %d starts, %d stops", got, st.stops.Load())
	}
	if inst.Acquisition() != instrument.Rolling {
		t.Errorf("run state %v, want Rolling", inst.Acquisition())
	}
	// a sample captured at 1 V/div reads half as many codes from the
	// centre at 2 V/div
	want := 128 + (float64(old)-128)/2
	found := false
	for _, v := range g.Traces[0].Codes8[:g.Len()/2] {
		if math.Abs(float64(v)-want) <= 0.5 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no sample from before the change drawn at the new V/div (want code %.1f)", want)
	}
}

// A stream that ends under the view (a loss) starts again with the view
// kept: the samples it held stay, and the gap the restart lost is held at
// the last value; a peak view holds its last (min, max) pair.
// TRLC-LINKS: REQ-DSO-010
func TestHALRollKeepsItsViewAcrossARestart(t *testing.T) {
	st := &scaledStream{}
	st.vdiv.Store(math.Float64bits(1))
	b := scaledRollBoard{halfake.New(halfake.Config{}), st}
	s := loopState(hal.Auto)
	s.TimeDivS = 200e-3
	inst := instrument.New(s, nil)
	l, c := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	newestRoll(t, c, 0)
	st.fail.Store(true)
	deadline := time.Now().Add(5 * time.Second)
	for st.starts.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the stream did not start again after the loss")
		}
		time.Sleep(time.Millisecond)
	}

	fm := sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	sc := sample.Scale{Zero: 128, PerCode: 1}
	block := func(codes ...byte) hal.StreamBlock {
		return hal.StreamBlock{Payload: hal.PayloadChunks, Formats: []sample.CodeFormat{fm}, Scales: []sample.Scale{sc}, Bytes: codes}
	}
	none := func(int, sample.CodeFormat) (sample.Scale, float64) { return sample.Scale{}, 0 }
	r := halRoll{screen: 8, peak: true, plan: hal.Plan{SampleS: 0.1}}
	r.reset([]int{0}, fm)
	now := time.Now()
	r.add(block(130, 140), none, now)
	if r.volts[0][len(r.volts[0])-8] != 2 || r.held[len(r.held)-1] {
		t.Fatalf("the first samples fill the view's past: %v held %v", r.volts[0], r.held)
	}
	r.again = true // the stream started again: 0.5 s later, two new samples
	r.add(block(150, 160), none, now.Add(500*time.Millisecond))
	v, h := r.volts[0][len(r.volts[0])-8:], r.held[len(r.held)-8:]
	want := []float32{2, 2, 12, 2, 12, 2, 22, 32}
	wantHeld := []bool{true, false, false, true, true, true, false, false}
	for i := range want {
		if v[i] != want[i] || h[i] != wantHeld[i] {
			t.Fatalf("the kept view %v held %v, want %v held %v", v, h, want, wantHeld)
		}
	}
}

// A V/div committed while SOUND streams is committed as it comes: the
// board's stream goes on without a gap, and the lanes take the scale each
// block states.
// TRLC-LINKS: REQ-DSO-265, REQ-DSO-008
func TestHALSoundCommitsWithoutAGap(t *testing.T) {
	st := &scaledStream{}
	st.vdiv.Store(math.Float64bits(1))
	b := scaledRollBoard{halfake.New(halfake.Config{}), st}
	inst := instrument.New(loopState(hal.Auto), nil)
	l, c := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	c.LeaseSound(10 * time.Second)
	if c.SoundWait(5000, 5*time.Second) <= 5000 {
		t.Fatal("the ring did not fill")
	}
	if err := inst.Change(func(s *instrument.State) error {
		s.Channels[0].VoltsDiv = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st.vdiv.Store(math.Float64bits(2))
	time.Sleep(200 * time.Millisecond) // the owner commits it at its next step
	next := c.SoundInfo().Next
	if c.SoundWait(next+5000, 5*time.Second) <= next+5000 {
		t.Fatal("the ring stopped after the commit")
	}
	if n := st.starts.Load(); n != 1 || st.stops.Load() != 0 {
		t.Fatalf("the commit restarted the stream: %d starts, %d stops", n, st.stops.Load())
	}
	blk := c.SoundRead(0, 1<<20, nil, nil)
	if len(blk.Gaps) != 0 {
		t.Errorf("gaps in the ring at %v", blk.Gaps)
	}
	if n := len(blk.Scales); n == 0 || blk.Scales[n-1].VdivV[0] != 2 {
		t.Errorf("lane scales %+v: want the block's 2 V/div last", blk.Scales)
	}
}
