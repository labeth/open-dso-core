// ENGMODEL-OWNER-UNIT: FU-DSO-STREAMVIEW

package streamview

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/decode/synth"
	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// midiStream streams a MIDI bus on its first channel (C2 a flat line) as
// 8-bit codes at 1 MSa/s, 1000 samples a block; drop makes the block after
// it go missing (a sequence jump).
type midiStream struct {
	bits    []byte
	mu      sync.Mutex
	spec    hal.StreamSpec
	first   uint64
	drop    atomic.Int32
	starts  atomic.Int32
	stops   atomic.Int32
	running bool
}

func (s *midiStream) Kinds() []hal.Payload { return []hal.Payload{hal.PayloadCodes} }

func (s *midiStream) Start(spec hal.StreamSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spec, s.running = spec, true
	s.starts.Add(1)
	return nil
}

func (s *midiStream) Lease(ctx context.Context) (hal.StreamBlock, error) {
	select {
	case <-ctx.Done():
		return hal.StreamBlock{}, ctx.Err()
	case <-time.After(200 * time.Microsecond):
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	const n = 1000
	if s.drop.Load() > 0 && s.first >= 5000 {
		s.drop.Add(-1)
		s.first += n
	}
	fm := sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	b := hal.StreamBlock{Payload: hal.PayloadCodes, First: s.first, Bytes: make([]byte, n*len(s.spec.Channels)), SampleS: 1e-6}
	for range s.spec.Channels {
		b.Formats = append(b.Formats, fm)
	}
	for i := range n {
		v := byte(60)
		if s.bits[int(s.first+uint64(i))%len(s.bits)] != 0 {
			v = 200
		}
		b.Bytes[i] = v // C1
		if len(s.spec.Channels) > 1 {
			b.Bytes[n+i] = 128 // C2
		}
	}
	s.first += n
	return b, nil
}

func (s *midiStream) Release(hal.StreamBlock) {}

func (s *midiStream) Stop() error {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	s.stops.Add(1)
	return nil
}

func (s *midiStream) Faults() hal.StreamFaults { return hal.StreamFaults{} }

// midiBoard is the fake with a stream of codes whose fastest stream plan
// samples every microsecond.
type midiBoard struct {
	*halfake.Board
	st *midiStream
}

func (b midiBoard) Streamer() (hal.Streamer, bool) { return b.st, true }

func (b midiBoard) Profile() hal.Profile {
	p := b.Board.Profile()
	p.Timebase.RollFromS = 100e-6
	return p
}

// midiRig runs a HAL loop over a MIDI stream: a note on (0x90 0x3C 0x64),
// a note off and a program change, repeating.
func midiRig(t *testing.T) (*engine.HALCycle, *midiStream, *instrument.Model) {
	t.Helper()
	bus, err := synth.Make(synth.Spec{Proto: "midi", Payload: []int{0x90, 0x3c, 0x64, 0x80, 0x3c, 0x00, 0xc0, 0x05}}, 1e6)
	if err != nil {
		t.Fatal(err)
	}
	st := &midiStream{bits: bus.Lines[0].Bits}
	ch := hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6}
	inst := instrument.New(instrument.State{Channels: []hal.Channel{ch, ch}, TimeDivS: 1e-3, Sweep: hal.Auto, Running: true}, nil)
	l, c := engine.NewHALLoop(engine.HALConfig{Board: midiBoard{halfake.New(halfake.Config{}), st}, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return c, st, inst
}

// midiParams is MIDI on C1 sliced at mid-scale.
func midiParams() engine.SerialParams {
	return engine.SerialParams{Proto: engine.SerMIDI, ChA: 0, ChB: 1, Baud: 31250, Threshold: 128, HaveThr: true, Addr: -1, RW: 2}
}

// A board's stream of codes is decoded in software as it arrives: the
// transcript holds the bus's bytes and marks a lost block, the run stops
// while it streams, and a manual stop ends the board's stream.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func TestHALDecodedStreamManual(t *testing.T) {
	c, st, inst := midiRig(t)
	st.drop.Store(1)
	if !c.DecodesStreams() {
		t.Fatal("a board with a stream of codes decodes no stream")
	}
	ctl := New(HALSource{C: c}, t.Logf)
	if err := ctl.Start(midiParams(), engine.StreamManual, 0, "MIDI C1"); err != nil {
		t.Fatal(err)
	}
	if inst.State().Running {
		t.Fatal("the run goes on while the stream runs")
	}
	deadline := time.Now().Add(5 * time.Second)
	for ctl.Snapshot().Units < 64 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	v := ctl.Snapshot()
	if v.Units < 64 || v.Errors > 1 || // the frame the lost block cut
		!strings.Contains(v.Mode, "gap-free, software decode of the 1 us stream") {
		t.Fatalf("view %+v", v)
	}
	p := ctl.Lines(0, 1000)
	var sb strings.Builder
	lost := false
	for _, l := range p.Lines {
		lost = lost || strings.HasPrefix(l.Note, "LOST")
		for _, u := range l.Units {
			fmt.Fprintf(&sb, "%02X ", u)
		}
	}
	text := sb.String()
	if !strings.Contains(text, "90 3C 64 80 3C 00 C0 05") {
		t.Fatalf("transcript units %q, want the bus's bytes", text)
	}
	if !lost {
		t.Fatal("the lost block is not marked")
	}
	ctl.Stop()
	if got := ctl.Snapshot(); got.State != Stopped || got.RecordAvailable {
		t.Fatalf("after the stop: %+v", got)
	}
	if st.stops.Load() == 0 {
		t.Fatal("the board's stream did not stop")
	}
}

// A stream stopped on a value ends its transcript at the frame that holds
// it, without a record; a lost block is marked in the transcript.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func TestHALDecodedStreamOnValue(t *testing.T) {
	c, st, _ := midiRig(t)
	st.drop.Store(1)
	ctl := New(HALSource{C: c}, t.Logf)
	if err := ctl.Start(midiParams(), engine.StreamOnValue, 0xC0, "MIDI C1"); err != nil {
		t.Fatal(err)
	}
	v := wait(t, ctl, Triggered)
	if v.RecordAvailable || v.TriggerLine >= 0 && v.Lines == 0 {
		t.Fatalf("view %+v", v)
	}
	p := ctl.Lines(0, 1000)
	last := p.Lines[len(p.Lines)-1]
	if len(last.Units) == 0 || last.Units[0] != 0xC0 {
		t.Fatalf("the transcript ends on %+v, want the program change", last)
	}
}

// A bus whose shortest feature the board's stream cannot resolve is
// refused with the rates.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func TestHALDecodedStreamTooSlow(t *testing.T) {
	c, _, _ := midiRig(t)
	p := midiParams()
	p.Proto, p.Baud = engine.SerWS281x, 0
	_, err := c.StartDecoded(context.Background(), p, engine.StreamManual, 0)
	if err == nil || !strings.Contains(err.Error(), "needs a sample every") {
		t.Fatalf("WS281x on a 1 MSa/s stream: %v", err)
	}
	if err := New(HALSource{C: c}, t.Logf).Check(p); err == nil || !strings.Contains(err.Error(), "needs a sample every") {
		t.Fatalf("the check ahead: %v", err)
	}
	if err := New(HALSource{C: c}, t.Logf).Check(midiParams()); err != nil {
		t.Fatalf("the check ahead refuses MIDI on a 1 MSa/s stream: %v", err)
	}
}
