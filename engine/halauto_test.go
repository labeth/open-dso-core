package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/prototrig"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// autoBoard is the stalling fake whose acquisition does not sweep AUTO
// itself (hal.AutoWaiter).
type autoBoard struct{ wholeBoard }

func (b autoBoard) Acquirer() hal.Acquirer { return autoAcquirer{b.wholeBoard.Acquirer(), b.forced} }

// autoAcquirer waits its whole timeout while nothing forced the capture.
type autoAcquirer struct {
	hal.Acquirer
	forced *atomic.Bool
}

func (autoAcquirer) AutoWait() time.Duration { return 5 * time.Millisecond }

func (a autoAcquirer) Wait(ctx context.Context, d time.Duration) (hal.Status, error) {
	if err := ctx.Err(); err != nil {
		return hal.Status{}, err
	}
	if !a.forced.Load() {
		time.Sleep(d)
	}
	return a.Acquirer.Wait(ctx, d)
}

// On a board that does not sweep AUTO itself, AUTO forces a capture that
// waited the board's AutoWait without a trigger and shows it; NORMAL waits
// on. A board that sweeps AUTO itself is never forced by the loop.
// TRLC-LINKS: REQ-DSO-011
func TestHALAutoForcesOnABoardThatWaits(t *testing.T) {
	b := autoBoard{newWholeBoard(true)}
	inst := instrument.New(loopState(hal.Auto), nil)
	c := startLoop(b, inst)
	got := c.waitFor(t, 3)
	c.stop()
	if b.forces.Load() < 3 || !got[0].Status.Forced {
		t.Fatalf("AUTO on an untriggered board: %d forces, first frame %+v", b.forces.Load(), got[0].Status)
	}

	// the wait's slice ends with AUTO's wait: forced on time, not a slice late
	b = autoBoard{newWholeBoard(true)}
	inst = instrument.New(loopState(hal.Auto), nil)
	l, _ := NewHALLoop(HALConfig{Board: b, Instrument: inst, WaitSlice: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	l.Run(ctx)
	cancel()
	if n := b.forces.Load(); n < 10 {
		t.Fatalf("AUTO waiting 5 ms in 50 ms slices forced %d times in 200 ms", n)
	}

	b = autoBoard{newWholeBoard(true)}
	inst = instrument.New(loopState(hal.NormalSweep), nil)
	c = startLoop(b, inst)
	time.Sleep(50 * time.Millisecond)
	if frames := c.stop(); len(frames) != 0 || b.forces.Load() != 0 {
		t.Fatalf("NORMAL forced %d times, published %d", b.forces.Load(), len(frames))
	}

	w := newWholeBoard(false)
	inst = instrument.New(loopState(hal.Auto), nil)
	c = startLoop(w, inst)
	c.waitFor(t, 3)
	c.stop()
	if strings.Contains(strings.Join(w.Calls(), " "), "Force") {
		t.Fatal("the loop forced a board that sweeps AUTO itself")
	}
}

// The status counts every frame read (frames, coherent), the held ones and
// the published rate, and states the last pass's stage times, as the
// health token and the status read them.
// TRLC-LINKS: REQ-DSO-127, REQ-DSO-294
func TestHALCountsFrames(t *testing.T) {
	w := newWholeBoard(false)
	inst := instrument.New(loopState(hal.Auto), nil)
	c := startLoop(w, inst)
	got := c.waitFor(t, 5)
	time.Sleep(20 * time.Millisecond)
	c.stop()
	st := inst.Snapshot()
	if st.Frames < uint64(len(got)) || st.Coherent != st.Frames || st.Published < uint64(len(got)) || st.Held != st.Frames-st.Published {
		t.Fatalf("frames %d coherent %d published %d held %d after %d frames", st.Frames, st.Coherent, st.Published, st.Held, len(got))
	}
	if !strings.HasPrefix(st.FrameStages, "arm ") || !strings.HasSuffix(st.FrameStages, "(ms)") {
		t.Fatalf("frame stages %q", st.FrameStages)
	}
	if st.SymbolMatches != -1 {
		t.Fatalf("symbol matches %d without a serial trigger", st.SymbolMatches)
	}
}

// The record's share before the trigger is the screen's (the board places
// its record by it), and a serial trigger asks room for its match before
// the trigger: the FPGA's matcher fires at the match's end.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-014
func TestHALRequestPretrigger(t *testing.T) {
	w := newWholeBoard(false)
	s := loopState(hal.Auto)
	s.DelayS = .4 * 10 * s.TimeDivS // the trigger a tenth across the screen
	_, c := NewHALLoop(HALConfig{Board: w, Instrument: instrument.New(s, nil)})
	if f := c.request(s).PretrigFrac; f < .0999 || f > .1001 {
		t.Fatalf("pretrigger share %v, want the screen's 0.1", f)
	}
	plan := hal.Plan{SampleS: 1e-6, WinSamples: 1000}
	if r := serialRoom(s, plan); r != 0 {
		t.Fatalf("room %d without a serial trigger", r)
	}
	s.SerialMode, s.Serial = SerialTrigger, SerialParams{Proto: SerUART, Baud: 31250, Bits: 8, Bytes: []int{0x90}}
	plan.Depth = 100000
	unit := SerialUnitSamples(s.Serial, plan.SampleS, 0)
	if r := serialRoom(s, plan); unit <= 0 || r != unit {
		t.Fatalf("a UART byte's room in the record %d, unit %d", r, unit)
	}
	if r := serialWindowRoom(s, plan); r != min(2000, 3*unit) {
		t.Fatalf("a UART byte's room in the window %d, unit %d", r, unit)
	}
}

// endBoard is a fake whose protocol receivers fire at a match's end.
type endBoard struct{ wholeBoard }

func (b endBoard) Trigger() hal.Trigger { return endTrigger{b.wholeBoard.Trigger()} }

type endTrigger struct{ hal.Trigger }

func (endTrigger) FiresAtMatchEnd() bool { return true }

// uartFrame is a frame of 8N1 bytes on C1 at sampleS, idle high, a byte
// every gap samples from first, and where each byte starts and ends.
func uartFrame(bytes []int, bitS, sampleS float64, first, gap, n int) (*sample.Frame, [][2]int) {
	f := &sample.Frame{SampleS: sampleS, Traces: []sample.Trace{{Ch: 0, Codes8: make([]uint8, n), Valid: n, Format: sample.Format8, Scale: sample.Scale{Zero: 128, PerCode: .04}, CodesPerDiv: 25}}}
	f.SetLen(n)
	c := f.Traces[0].Codes8
	for i := range c {
		c[i] = 200
	}
	bit := int(bitS / sampleS)
	var at [][2]int
	for k, v := range bytes {
		s := first + k*gap
		bits := []int{0}
		for i := range 8 {
			bits = append(bits, v>>i&1)
		}
		bits = append(bits, 1)
		for i, b := range bits {
			for j := range bit {
				if b == 0 {
					c[s+i*bit+j] = 60
				}
			}
		}
		at = append(at, [2]int{s, s + 9*bit})
	}
	return f, at
}

// A board whose receivers fire at the match's end has the trigger placed
// at the start of the match that ends there (decoded from the record before
// it); a board that fires elsewhere keeps its point.
// TRLC-LINKS: REQ-DSO-013
func TestHALPlacesTheMatchStart(t *testing.T) {
	for _, atEnd := range []bool{true, false} {
		var b hal.Board = newWholeBoard(false)
		if atEnd {
			b = endBoard{newWholeBoard(false)}
		}
		s := loopState(hal.Auto)
		s.SerialMode, s.Serial = SerialTrigger, SerialParams{Proto: SerUART, Baud: 31250, Bits: 8, Parity: ParityNone, Bytes: []int{0x90}, Addr: -1, RW: 2}
		_, c := NewHALLoop(HALConfig{Board: b, Instrument: instrument.New(s, nil)})
		c.cur, c.gate = s, &c.gates[0]
		f, at := uartFrame([]int{0x41, 0x90, 0x3c}, 32e-6, 1e-6, 100, 400, 1500)
		f.TriggerAt = float64(at[1][1])
		c.placeMatchStart(f)
		want := float64(at[1][1])
		if atEnd {
			want = float64(at[1][0])
		}
		if d := f.TriggerAt - want; d < -2 || d > 2 {
			t.Fatalf("fires at the end %v: the trigger at %v, want the 0x90's %v", atEnd, f.TriggerAt, want)
		}
	}
}

// A bus trigger's levels are its thresholds (prototrig.Bus): a threshold
// the request leaves to the decoder is the learned swing's middle, and the
// request says it is set, so a board slices at the levels.
// TRLC-LINKS: REQ-DSO-013
func TestHALBusLevelsAreSet(t *testing.T) {
	s := loopState(hal.Auto)
	s.SerialMode, s.Serial = SerialTrigger, SerialParams{Proto: SerUART, Baud: 31250, Bits: 8, Bytes: []int{0x90}, Addr: -1, RW: 2}
	_, c := NewHALLoop(HALConfig{Board: newWholeBoard(false), Instrument: instrument.New(s, nil)})
	c.ext[0] = extent{lo: 0, hi: 3.3, have: true, vdiv: 1, voltsCode8: .04, zero8: 128}
	st, why := c.busStage(s, hal.TrigStage{})
	bus, ok := st.Proto.(*prototrig.Bus)
	if why != "" || !ok || !bus.Request.HaveThr || !bus.Request.HaveThrB || bus.Levels[0] != 1.65 {
		t.Fatalf("why %q, bus %+v", why, st.Proto)
	}
}
