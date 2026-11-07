package simboard

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

var _ hal.Board = (*Board)(nil)

// format8 and format12 are an 8-bit scope's codes and 12-bit codes
// left-aligned in 16-bit words.
var (
	format8  = sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	format12 = sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000}
)

// newBoard is a brought-up board of n channels in format f, at a fast
// frame interval.
func newBoard(t *testing.T, n int, f sample.CodeFormat, cpd float64) *Board {
	t.Helper()
	b, err := New(Config{
		Profile: hal.Profile{
			Channels: n, Format: f, Rates: []hal.Rate{{SamplesPerS: 1e9, MaxChannels: n}}, MaxDepth: 100000, Bandwidth: 0,
			Display: hal.DisplaySpec{W: 800, H: 480, Format: hal.RGB565, DivsH: 10, DivsV: 8},
		},
		CodesPerDiv:   cpd,
		FrameInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Platform().BringUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	return b
}

// edge is a rising edge trigger on channel ch at level.
func edge(ch int, level float64, sweep hal.Sweep) hal.TriggerSpec {
	return hal.TriggerSpec{Sweep: sweep, Stages: []hal.TrigStage{{
		Node: hal.NodeEdge, Source: hal.Source{Kind: hal.Analog, Index: ch}, Levels: hal.Levels{High: level, Slope: hal.Rising},
	}}}
}

// acquire commits c on every channel, plans req, programs spec, arms and
// waits up to wait for a record; it returns the main window's frame.
func acquire(t *testing.T, b *Board, c hal.Channel, req hal.Request, spec hal.TriggerSpec, wait time.Duration) (*sample.Frame, hal.Status, error) {
	t.Helper()
	for ch := range b.Profile().Channels {
		if _, err := b.FrontEnd().Set(ch, c); err != nil {
			t.Fatal(err)
		}
		req.Channels = append(req.Channels, ch)
	}
	plan, err := b.Planner().Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Acquirer().Configure(plan); err != nil {
		t.Fatal(err)
	}
	if err := b.Trigger().Program(spec); err != nil {
		t.Fatal(err)
	}
	if err := b.Acquirer().Arm(); err != nil {
		t.Fatal(err)
	}
	st, err := b.Acquirer().Wait(t.Context(), wait)
	if err != nil {
		return nil, st, err
	}
	f := &sample.Frame{}
	if err := b.Readout().Read(t.Context(), hal.ReadRequest{Windows: []hal.Window{{Role: hal.Main}}}, []*sample.Frame{f}, nil); err != nil {
		t.Fatal(err)
	}
	return f, st, nil
}

// A 1 kHz square triggered on its rising edge at 0 V stands on the trigger
// point in every record: low before it, high after, on the 8-bit grid of
// 25 codes a division and on the 12-bit one.
// TRLC-LINKS: REQ-DSO-1001
func TestTriggeredSquareStandsStill(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		f    sample.CodeFormat
		cpd  float64
	}{{"2x8", 2, format8, 25}, {"4x12", 4, format12, 7680}} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBoard(t, tc.n, tc.f, tc.cpd)
			if err := b.Signals().Set(0, "square,1e3,2,0"); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				f, st, err := acquire(t, b, hal.Channel{On: true, VoltsDiv: 0.5, Probe: 1}, hal.Request{TimePerDivS: 100e-6},
					edge(0, 0, hal.NormalSweep), 2*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				if !st.Triggered || st.Forced || f.TrigKind != sample.HardwareNode {
					t.Fatalf("status %+v kind %v", st, f.TrigKind)
				}
				k := int(f.TriggerAt)
				if f.WinPoints != f.Len() || math.Abs(f.TriggerAt-float64(f.WinPoints)/2) > 1 {
					t.Fatalf("trigger at %g of %d points (%d delivered)", f.TriggerAt, f.WinPoints, f.Len())
				}
				tr := &f.Traces[0]
				if lo, hi := tr.Volts(k-3), tr.Volts(k+3); math.Abs(lo+1) > 0.05 || math.Abs(hi-1) > 0.05 {
					t.Fatalf("around the trigger %g V, %g V; want -1, 1", lo, hi)
				}
				if tr.CodesPerDiv != tc.cpd || tr.Format != tc.f || len(f.Traces) != tc.n {
					t.Fatalf("trace %+v", tr.Format)
				}
			}
		})
	}
}

// NORMAL waits for a trigger a flat input never gives; AUTO shows an
// untriggered record after its wait; FORCE completes the waiting one.
// TRLC-LINKS: REQ-DSO-1001
func TestSweeps(t *testing.T) {
	b := newBoard(t, 2, format8, 25)
	c := hal.Channel{On: true, VoltsDiv: 1}
	if _, st, err := acquire(t, b, c, hal.Request{TimePerDivS: 1e-6}, edge(0, 0.5, hal.NormalSweep), 150*time.Millisecond); !errors.Is(err, hal.ErrTimeout) || st.Triggered {
		t.Fatalf("NORMAL on a flat input: %v %+v", err, st)
	}
	if err := b.Acquirer().Force(); err != nil {
		t.Fatal(err)
	}
	st, err := b.Acquirer().Wait(t.Context(), time.Second)
	if err != nil || !st.Forced || st.Triggered {
		t.Fatalf("FORCE: %v %+v", err, st)
	}
	f, st, err := acquire(t, b, c, hal.Request{TimePerDivS: 1e-6}, edge(0, 0.5, hal.Auto), time.Second)
	if err != nil || !st.Forced || f.TrigKind != sample.Forced {
		t.Fatalf("AUTO: %v %+v", err, st)
	}
	spec := edge(0, 0.5, hal.Auto)
	spec.FreeRun = true
	if f, _, err = acquire(t, b, c, hal.Request{TimePerDivS: 1e-6}, spec, time.Second); err != nil || f.TrigKind != sample.Untriggered {
		t.Fatalf("free run: %v %v", err, f.TrigKind)
	}
	if err := b.Acquirer().Stop(); err != nil || b.Acquirer().State() != hal.Idle {
		t.Fatal("stop")
	}
	if _, err := b.Acquirer().Wait(t.Context(), 0); err == nil {
		t.Fatal("a wait while stopped")
	}
}

// The front end: offset, invert, ground and AC coupling move the trace as
// a scope's do; a V/div off the ladder is refused, one between steps snaps.
// TRLC-LINKS: REQ-DSO-1001
func TestFrontEnd(t *testing.T) {
	b := newBoard(t, 2, format8, 25)
	if err := b.Signals().Set(0, "dc,0,0,0.5"); err != nil {
		t.Fatal(err)
	}
	mean := func(c hal.Channel) float64 {
		f, _, err := acquire(t, b, c, hal.Request{TimePerDivS: 1e-6}, hal.TriggerSpec{FreeRun: true}, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return f.Traces[0].Volts(10)
	}
	for _, tc := range []struct {
		name string
		c    hal.Channel
		want float64
	}{
		{"dc", hal.Channel{On: true, VoltsDiv: 0.2}, 0.5},
		{"offset", hal.Channel{On: true, VoltsDiv: 0.2, OffsetV: -0.4}, 0.5},
		{"invert", hal.Channel{On: true, VoltsDiv: 0.2, Invert: true}, -0.5},
		{"probe 10x", hal.Channel{On: true, VoltsDiv: 2, Probe: 10}, 5},
		{"ground", hal.Channel{On: true, VoltsDiv: 0.2, Coupling: hal.GND}, 0},
		{"ac", hal.Channel{On: true, VoltsDiv: 0.2, Coupling: hal.AC}, 0},
	} {
		if got := mean(tc.c); math.Abs(got-tc.want) > tc.c.VoltsDiv/25 {
			t.Errorf("%s: %g V, want %g", tc.name, got, tc.want)
		}
	}
	got, err := b.FrontEnd().Set(0, hal.Channel{VoltsDiv: 0.3})
	if err != nil || got.VoltsDiv != 0.2 || got.Impedance != 1e6 || got.Probe != 1 {
		t.Fatalf("snap: %+v %v", got, err)
	}
	var r *hal.Refusal
	if _, err := b.FrontEnd().Set(0, hal.Channel{VoltsDiv: 100}); !errors.As(err, &r) {
		t.Fatalf("100 V/div: %v", err)
	}
	if g := b.FrontEnd().OffsetGrid(0); math.Abs(g.Step-0.2/25) > 1e-12 || g.Max != 2 {
		t.Fatalf("offset grid %+v", g)
	}
	if g := b.Trigger().LevelGrid(hal.Source{Kind: hal.Analog}); math.Abs(g.Step-0.2/25) > 1e-12 {
		t.Fatalf("level grid %+v", g)
	}
}

// What the board does not have is refused with its reason, never
// substituted.
// TRLC-LINKS: REQ-DSO-1001
func TestRefusals(t *testing.T) {
	b := newBoard(t, 2, format8, 25)
	var r *hal.Refusal
	for name, req := range map[string]hal.Request{
		"average":  {TimePerDivS: 1e-3, Mode: hal.Average},
		"stream":   {TimePerDivS: 1e-3, Mode: hal.Stream},
		"roll":     {TimePerDivS: 1, Mode: hal.RollMode},
		"segments": {TimePerDivS: 1e-3, Segments: 10},
		"channel":  {TimePerDivS: 1e-3, Channels: []int{2}},
	} {
		if _, err := b.Planner().Plan(req); !errors.As(err, &r) || r.Reason == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	src := hal.Source{Kind: hal.Analog}
	for name, spec := range map[string]hal.TriggerSpec{
		"video":    {Stages: []hal.TrigStage{{Node: hal.NodeVideo, Source: src}}},
		"protocol": {Stages: []hal.TrigStage{{Node: hal.NodeProtocol, Source: src}}},
		"zone":     {Stages: []hal.TrigStage{{Node: hal.NodeEdge, Source: src, Zones: []hal.Zone{{}}}}},
		"stages":   {Stages: []hal.TrigStage{{Node: hal.NodeEdge, Source: src}, {Node: hal.NodeEdge, Source: src}}},
		"digital":  {Stages: []hal.TrigStage{{Node: hal.NodeEdge, Source: hal.Source{Kind: hal.Digital}}}},
		"hf":       {Coupling: hal.TrigHFReject, Stages: []hal.TrigStage{{Node: hal.NodeEdge, Source: src}}},
	} {
		if err := b.Trigger().Program(spec); !errors.As(err, &r) || r.Reason == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, part := range hal.OptionalParts {
		if part == "counter" {
			continue
		}
		if b.Profile().AbsentReason(part) == "this scope has no "+part {
			t.Errorf("%s: no reason of the board's", part)
		}
	}
	if _, ok := b.Streamer(); ok {
		t.Error("a streamer")
	}
	if c, ok := b.Counter(); !ok {
		t.Error("no counter")
	} else if _, err := c.Read(); err != nil {
		t.Error(err)
	}
}

// A pulse-width trigger fires on the end of a pulse its qualifier takes,
// so a pulse train with a wider pulse every fourth period triggers on that
// one alone.
// TRLC-LINKS: REQ-DSO-1001
func TestPulseTrigger(t *testing.T) {
	b := newBoard(t, 2, format8, 25)
	// 10 µs pulses of 1 µs; every fourth period carries a 2 µs glitch in
	// its low part, a second, wider pulse.
	if err := b.Signals().Set(0, "pulse,1e5,2,0,width=1e-6,glitch=4:2e-6:2"); err != nil {
		t.Fatal(err)
	}
	spec := hal.TriggerSpec{Sweep: hal.NormalSweep, Stages: []hal.TrigStage{{
		Node: hal.NodePulse, Source: hal.Source{Kind: hal.Analog}, Levels: hal.Levels{Slope: hal.Rising},
		Qual: &hal.Qualifier{Op: hal.Longer, MinS: 1.5e-6, MaxS: 1e-3},
	}}}
	for range 3 {
		f, st, err := acquire(t, b, hal.Channel{On: true, VoltsDiv: 0.5}, hal.Request{TimePerDivS: 2e-6}, spec, 2*time.Second)
		if err != nil || !st.Triggered {
			t.Fatalf("%v %+v", err, st)
		}
		// The trigger is the wide pulse's falling edge: high 1.5 µs before it.
		k := int(f.TriggerAt)
		per := 1e-6 / f.SampleS
		if v := f.Traces[0].Volts(k - int(1.5*per)); v < 0.9 {
			t.Fatalf("1.5 µs before the trigger %g V: not inside the wide pulse", v)
		}
	}
}

// Peak detect keeps a narrow glitch a decimated record steps over.
// TRLC-LINKS: REQ-DSO-1001
func TestPeakDetect(t *testing.T) {
	b := newBoard(t, 2, format12, 7680)
	if err := b.Signals().Set(0, "pulse,1e3,2,0,width=2e-6"); err != nil {
		t.Fatal(err)
	}
	f, _, err := acquire(t, b, hal.Channel{On: true, VoltsDiv: 1}, hal.Request{TimePerDivS: 10e-3, Mode: hal.PeakDetect, Depth: 1000},
		edge(0, 0, hal.NormalSweep), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if f.Content != sample.PeakPairs || !f.Flags.Has(sample.Envelope) || f.Traces[0].Valid != 2*len(f.Traces[0].Max16) {
		t.Fatalf("content %v flags %v", f.Content, f.Flags)
	}
	highs := 0
	for _, w := range f.Traces[0].Max16 {
		if f.Traces[0].Scale.Volts(float64(w)) > 0.9 {
			highs++
		}
	}
	if highs < 5 {
		t.Fatalf("%d buckets hold a pulse, want one a millisecond", highs)
	}
}

// A record takes its signal time: at 20 ms/div the post-trigger half of
// the screen passes before the record is ready.
// TRLC-LINKS: REQ-DSO-1001
func TestSlowTimebaseTakesItsTime(t *testing.T) {
	b := newBoard(t, 2, format8, 25)
	if err := b.Signals().Set(0, "sine,50,2,0"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, _, err := acquire(t, b, hal.Channel{On: true, VoltsDiv: 0.5}, hal.Request{TimePerDivS: 20e-3}, edge(0, 0, hal.NormalSweep), 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Fatalf("a 200 ms record in %v", d)
	}
}

// The screen presents RGBA to the host and to Screen, black while dark;
// the stores tell the host every change.
// TRLC-LINKS: REQ-DSO-1001
func TestScreenAndStores(t *testing.T) {
	var presented int
	stored := map[string]string{}
	b, err := New(Config{
		Profile:   hal.Profile{Channels: 1, Format: format8, Display: hal.DisplaySpec{W: 4, H: 2, Format: hal.RGB565}},
		Stored:    map[string]map[string][]byte{"settings": {"a": []byte("1")}},
		Persist:   func(store, key string, val []byte) { stored[store+"/"+key] = string(val) },
		OnPresent: func(rgba []byte, w, h int) { presented += len(rgba) / (w * h) },
	})
	if err != nil {
		t.Fatal(err)
	}
	fb := b.Display().Framebuffer()
	back := fb.Back()
	back[0], back[1] = 0x00, 0xf8 // pure red
	if err := fb.Present(); err != nil {
		t.Fatal(err)
	}
	px, w, h, seq := b.Screen(0, 0)
	if presented != 4 || w != 4 || h != 2 || seq != 1 || px[0] != 255 || px[1] != 0 || px[3] != 255 {
		t.Fatalf("picture %v (%dx%d #%d), presented %d", px[:4], w, h, seq, presented)
	}
	if err := b.Display().Backlight(0); err != nil {
		t.Fatal(err)
	}
	if px, _, _, _ = b.Screen(0, 0); px[0] != 0 {
		t.Fatal("a dark screen shows a picture")
	}
	kv := b.Storage().Settings()
	if v, ok := kv.Get("a"); !ok || string(v) != "1" {
		t.Fatal("the stored settings")
	}
	if err := kv.Put("b", []byte("2")); err != nil || stored["settings/b"] != "2" {
		t.Fatalf("persist %v %v", err, stored)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := b.Platform().BringUp(ctx); err != nil {
		t.Fatal(err)
	}
}
