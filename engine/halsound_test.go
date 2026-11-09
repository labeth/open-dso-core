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
