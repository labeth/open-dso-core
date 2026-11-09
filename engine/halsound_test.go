package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// streamBoard is the fake with a stream of 8-bit codes: blocks of 1000
// samples a channel, the codes counting up.
type streamBoard struct {
	*halfake.Board
	st *fakeStream
}

func (b streamBoard) Streamer() (hal.Streamer, bool) { return b.st, true }

type fakeStream struct {
	mu      sync.Mutex
	spec    hal.StreamSpec
	running bool
	first   uint64
	starts  atomic.Int32
	stops   atomic.Int32
}

func (s *fakeStream) Kinds() []hal.Payload { return []hal.Payload{hal.PayloadCodes} }

func (s *fakeStream) Start(spec hal.StreamSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spec, s.running = spec, true
	s.starts.Add(1)
	return nil
}

func (s *fakeStream) Lease(ctx context.Context) (hal.StreamBlock, error) {
	select {
	case <-ctx.Done():
		return hal.StreamBlock{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 1000
	fm := sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	b := hal.StreamBlock{Payload: hal.PayloadCodes, First: s.first, Bytes: make([]byte, n*len(s.spec.Channels))}
	for range s.spec.Channels {
		b.Formats = append(b.Formats, fm)
	}
	for i := range b.Bytes {
		b.Bytes[i] = byte(s.first + uint64(i%n))
	}
	s.first += uint64(n)
	return b, nil
}

func (s *fakeStream) Release(hal.StreamBlock) {}

func (s *fakeStream) Stop() error {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	s.stops.Add(1)
	return nil
}

func (s *fakeStream) Faults() hal.StreamFaults { return hal.StreamFaults{} }

// A SOUND lease takes the acquisition for the board's stream of codes: the
// ring fills with both lanes as Q8.8 at the stream's rate; the lease's end
// stops the stream and the captures run again.
// TRLC-LINKS: REQ-DSO-265
func TestHALSoundOverTheStream(t *testing.T) {
	st := &fakeStream{}
	b := streamBoard{halfake.New(halfake.Config{}), st}
	inst := instrument.New(loopState(hal.Auto), nil)
	l, c := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	if !c.Streams() {
		t.Fatal("a board with a stream of codes does not stream SOUND")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	c.LeaseSound(time.Second)
	if c.SoundWait(5000, 5*time.Second) <= 5000 {
		t.Fatal("the ring did not fill")
	}
	in := c.SoundInfo()
	if !in.Active || in.State != "streaming" || !(in.RateHz > 0) {
		t.Fatalf("sound info %+v", in)
	}
	blk := c.SoundLatest(10, nil, nil)
	if blk.N != 10 || blk.Q1[1]-blk.Q1[0] != 256 || blk.Q2[0] != blk.Q1[0] {
		t.Fatalf("latest %d samples %v / %v, want the ramp in Q8.8 on both lanes", blk.N, blk.Q1, blk.Q2)
	}
	if len(st.spec.Channels) != 2 {
		t.Fatalf("streamed %v, want the two channels on", st.spec.Channels)
	}
	c.LeaseSound(0)
	deadline := time.Now().Add(5 * time.Second)
	for st.stops.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if st.stops.Load() == 0 || c.SoundInfo().Active {
		t.Fatal("the stream did not stop at the lease's end")
	}
}

// markingStream is fakeStream with the hardware's level-crossing marks: a
// rise mark every fifth block, on its 500th sample, on the marked channel;
// its blocks say 10 kSa/s.
type markingStream struct {
	fakeStream
	want   atomic.Pointer[hal.MarkSpec]
	blocks int
}

func (s *markingStream) SetMarks(m hal.MarkSpec) error {
	if m.FromTrigger {
		m = hal.MarkSpec{Channel: 0, Level: 128, Rise: true}
	}
	s.want.Store(&m)
	return nil
}

func (s *markingStream) Marking() (hal.MarkSpec, bool) {
	if m := s.want.Load(); m != nil {
		return *m, true
	}
	return hal.MarkSpec{}, true
}

func (s *markingStream) Lease(ctx context.Context) (hal.StreamBlock, error) {
	b, err := s.fakeStream.Lease(ctx)
	if err != nil {
		return b, err
	}
	s.blocks++
	b.SampleS = 1e-4 // 10 kSa/s: a take's post-trigger time is a block or so
	if m := s.want.Load(); m != nil && m.Rise && s.blocks%5 == 0 {
		b.Marks = []hal.Mark{{At: b.First + 500, Kind: hal.MarkRise, Channel: m.Channel, Count: 1}}
	}
	return b, nil
}

type markBoard struct {
	*halfake.Board
	st *markingStream
}

func (b markBoard) Streamer() (hal.Streamer, bool) { return b.st, true }

// On a board that marks level crossings, the marks go into the ring at the
// ring's ordinals; AUTO streams live and a STOP makes the newest seconds the
// take; NORMAL runs armed on the trigger's marks and makes the stretch
// around the one that fires the take, its trigger at the mark; SINGLE then
// stops the run.
// TRLC-LINKS: REQ-DSO-272, REQ-DSO-275
func TestHALSoundMarksAndTakes(t *testing.T) {
	st := &markingStream{}
	b := markBoard{halfake.New(halfake.Config{}), st}
	inst := instrument.New(loopState(hal.Auto), nil)
	l, c := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	c.SetSoundTakeTimes(0.0001, 0.0001) // bounded to the shortest take
	c.SetSoundMarks(SoundMarkConfig{Ch: 1, Level: 90, Rising: true})
	c.LeaseSound(10 * time.Second)
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("%s: sound info %+v", what, c.SoundInfo())
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitFor("the asked marks", func() bool { return len(c.SoundMarksSince(0)) > 0 })
	in := c.SoundInfo()
	if !in.Marks || in.MarkCfg.Ch != 1 || in.MarkCfg.Level != 90 || in.Mode != "live" {
		t.Fatalf("live marks: %+v", in)
	}
	m := c.SoundMarksSince(0)[0]
	if m.Ch != 1 || !m.Rising || m.N != 1 || (m.At-500)%1000 != 0 {
		t.Fatalf("mark %+v", m)
	}
	inst.SetRunning(false)
	waitFor("the stop take", func() bool { tk := c.SoundTake(); return tk != nil && tk.Why == "stop" })
	inst.SetRunning(true)
	inst.SetNorm(true)
	waitFor("a normal take", func() bool { tk := c.SoundTake(); return tk != nil && tk.Why == "normal" })
	tk := c.SoundTake()
	if !tk.HasTrig || tk.TrigCh != 0 || !tk.TrigRising || tk.TrigLevel != 128 || (tk.Trig-500)%1000 != 0 || tk.Trig < tk.First || tk.Trig >= tk.First+uint64(tk.N) {
		t.Fatalf("normal take %+v", tk)
	}
	inst.SetSingle()
	waitFor("a single take", func() bool { tk := c.SoundTake(); return tk != nil && tk.Why == "single" })
	waitFor("the single's stop", func() bool { s, _ := inst.Wanted(); return !s.Running })
}
