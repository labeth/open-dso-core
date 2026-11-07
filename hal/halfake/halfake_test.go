package halfake

import (
	"context"
	"errors"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

var _ hal.Board = (*Board)(nil)

// cycle runs one owner cycle of the hal package's threading contract and
// returns the frame it read.
func cycle(t *testing.T, b hal.Board, c0 hal.Channel, req hal.Request, win hal.Window) *sample.Frame {
	t.Helper()
	b.Platform().Service()
	if _, err := b.FrontEnd().Set(0, c0); err != nil {
		t.Fatal(err)
	}
	plan, err := b.Planner().Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Acquirer().Configure(plan); err != nil {
		t.Fatal(err)
	}
	spec := hal.TriggerSpec{Stages: []hal.TrigStage{{Node: hal.NodeEdge, Source: hal.Source{Kind: hal.Analog}, Levels: hal.Levels{Slope: hal.Rising}}}}
	if err := b.Trigger().Program(spec); err != nil {
		t.Fatal(err)
	}
	if err := b.Acquirer().Arm(); err != nil {
		t.Fatal(err)
	}
	st, err := b.Acquirer().Wait(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Done || b.Acquirer().State() != hal.Ready {
		t.Fatalf("status %+v state %v", st, b.Acquirer().State())
	}
	f := &sample.Frame{}
	yields := 0
	if err := b.Readout().Read(hal.ReadRequest{Record: st.Record, Windows: []hal.Window{win}}, []*sample.Frame{f}, func() { yields++ }); err != nil {
		t.Fatal(err)
	}
	if yields != len(f.Traces)-1 {
		t.Errorf("%d yields for %d traces", yields, len(f.Traces))
	}
	return f
}

func TestOwnerCycle8Bit(t *testing.T) {
	b := New(Config{})
	if _, err := b.Platform().BringUp(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := cycle(t, b, hal.Channel{On: true, VoltsDiv: 0.5, Probe: 1},
		hal.Request{TimePerDivS: 1e-6, Depth: 1000},
		hal.Window{Role: hal.Main, StartSample: -500, Span: 1000, Want: hal.RawFullRate})
	want := []string{"BringUp", "Service", "FrontEnd.Set", "Configure", "Program", "Arm", "Wait", "Read"}
	if got := b.Calls(); !slices.Equal(got, want) {
		t.Errorf("calls %v, want %v", got, want)
	}
	if f.Content != hal.RawFullRate || len(f.Traces) != 2 || f.TriggerAt != 500 {
		t.Fatalf("frame content %v traces %d trigger %g", f.Content, len(f.Traces), f.TriggerAt)
	}
	tr := &f.Traces[0]
	if tr.Format.Storage != sample.U8 || len(tr.Codes8) != 1000 || tr.Valid != 1000 || len(tr.Codes16) != 0 {
		t.Fatalf("trace storage %v codes8 %d codes16 %d", tr.Format.Storage, len(tr.Codes8), len(tr.Codes16))
	}
	// The square wave rises at the trigger and swings ±2 divisions.
	if v := tr.Volts(500); v < 0.99 || v > 1.01 {
		t.Errorf("volts after the trigger %g, want 1 (2 div at 0.5 V/div)", v)
	}
	if v := tr.Volts(499); v > -0.99 || v < -1.01 {
		t.Errorf("volts before the trigger %g, want -1", v)
	}
}

func TestOwnerCycle16BitPeakPairs(t *testing.T) {
	b := New(Config{Channels: 4, Format: sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000}})
	f := cycle(t, b, hal.Channel{On: true, VoltsDiv: 1},
		hal.Request{TimePerDivS: 1e-3, Depth: 10000},
		hal.Window{StartSample: -5000, Span: 10000, Points: 100, Want: hal.PeakPairs})
	if f.Content != hal.PeakPairs || len(f.Traces) != 4 {
		t.Fatalf("content %v traces %d", f.Content, len(f.Traces))
	}
	tr := f.Traces[3]
	if len(tr.Codes16) != 200 || tr.Valid != 200 || len(tr.Codes8) != 0 {
		t.Fatalf("codes16 %d valid %d", len(tr.Codes16), tr.Valid)
	}
	// A bucket of the 100-sample square wave holds both levels.
	if tr.Codes16[20] >= tr.Codes16[21] {
		t.Errorf("bucket min %#x max %#x", tr.Codes16[20], tr.Codes16[21])
	}
}

func TestRefusals(t *testing.T) {
	b := New(Config{})
	ctx := context.Background()
	refused := func(name string, err error) {
		t.Helper()
		if _, ok := hal.AsRefusal(err); !ok {
			t.Errorf("%s: err %v, want a refusal", name, err)
		}
	}
	_, err := b.FrontEnd().Set(0, hal.Channel{VoltsDiv: 0.0001})
	refused("below the ladder", err)
	_, err = b.FrontEnd().Set(5, hal.Channel{VoltsDiv: 1})
	refused("no such channel", err)
	refused("zone trigger", b.Trigger().Program(hal.TriggerSpec{Stages: []hal.TrigStage{{Node: hal.NodeZone, Source: hal.Source{Kind: hal.Analog}}}}))
	_, err = b.Planner().Plan(hal.Request{TimePerDivS: 1e-6, Segments: 4})
	refused("segments", err)
	_, err = b.Planner().Plan(hal.Request{TimePerDivS: 1e-6, Need: []hal.Feature{{Name: "spectrum", Accepts: sample.ContentSetOf(sample.Resampled)}}})
	refused("content", err)

	plan, _ := b.Planner().Plan(hal.Request{TimePerDivS: 1e-6})
	_ = b.Acquirer().Configure(plan)
	_ = b.Acquirer().Arm()
	st, _ := b.Acquirer().Wait(ctx, time.Second)
	f := &sample.Frame{}
	refused("resampled", b.Readout().Read(hal.ReadRequest{Record: st.Record, Windows: []hal.Window{{Span: 1000, Want: hal.Resampled}}}, []*sample.Frame{f}, nil))
	refused("compressed raw", b.Readout().Read(hal.ReadRequest{Windows: []hal.Window{{Span: 1000, Points: 10, Want: hal.RawFullRate}}}, []*sample.Frame{f}, nil))
	refused("lamp", b.Input().SetLamps([]string{"math"}))
	refused("budget", b.Storage().Settings().Put("settings", make([]byte, 5000)))
}

func TestArmKindsAndStop(t *testing.T) {
	b := New(Config{})
	ctx := context.Background()
	plan, _ := b.Planner().Plan(hal.Request{TimePerDivS: 1e-6})
	_ = b.Acquirer().Configure(plan)
	arm := func() {
		t.Helper()
		if err := b.Acquirer().Arm(); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Acquirer().Wait(ctx, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	arm() // a change: commit
	arm() // nothing staged: re-arm
	if _, err := b.FrontEnd().Set(1, hal.Channel{On: true, VoltsDiv: 2}); err != nil {
		t.Fatal(err)
	}
	if b.FrontEnd().Get(1).VoltsDiv != 1 {
		t.Error("a staged setting applied before Arm")
	}
	arm() // a change: commit
	if b.FrontEnd().Get(1).VoltsDiv != 2 {
		t.Error("Arm did not apply the staged setting")
	}
	_ = b.Acquirer().Stop()
	arm() // after the user's STOP: commit
	if got := strings.Join(b.ArmKinds(), ","); got != "commit,rearm,commit,commit" {
		t.Errorf("arm kinds %s", got)
	}
	b.SetWaitError(hal.ErrTimeout)
	_ = b.Acquirer().Arm()
	if _, err := b.Acquirer().Wait(ctx, time.Millisecond); !errors.Is(err, hal.ErrTimeout) {
		t.Errorf("wait err %v", err)
	}
}

func TestNotTaken(t *testing.T) {
	b := New(Config{NotTaken: true})
	if _, err := b.Platform().BringUp(context.Background()); !errors.Is(err, hal.ErrNotTaken) {
		t.Fatalf("err %v", err)
	}
}

func TestDevices(t *testing.T) {
	b := New(Config{})
	fb := b.Display().Framebuffer()
	back := fb.Back()
	back[0], back[1] = 0x00, 0xf8 // red, RGB565 little-endian
	if err := fb.Present(); err != nil {
		t.Fatal(err)
	}
	spec := b.Display().Spec()
	img := image.NewRGBA(image.Rect(0, 0, spec.W, spec.H))
	if err := b.Display().Capture(img); err != nil {
		t.Fatal(err)
	}
	if c := img.RGBAAt(0, 0); c.R != 0xf8 || c.G != 0 || c.B != 0 {
		t.Errorf("captured %v", c)
	}
	if _, ok := b.Display().Renderer(); ok {
		t.Error("the fake has no hardware renderer")
	}
	b.Inject(hal.InputEvent{Kind: hal.KeyEvent, Key: "run", Down: true})
	if ev := <-b.Input().Events(); ev.Key != "run" {
		t.Errorf("event %+v", ev)
	}
	if err := b.Input().SetLamps([]string{"run"}); err != nil || !slices.Equal(b.Lamps(), []string{"run"}) {
		t.Errorf("lamps %v %v", b.Lamps(), err)
	}
	kv := b.Storage().Settings()
	if err := kv.Put("settings", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if v, ok := kv.Get("settings"); !ok || string(v) != "{}" {
		t.Errorf("kv %q %v", v, ok)
	}
	if v, err := b.Census().Run(context.Background(), false); err != nil || len(v) == 0 {
		t.Errorf("census %v %v", v, err)
	}
	for name, ok := range map[string]bool{
		"streamer": second(b.Streamer()), "images": second(b.Images()), "stacker": second(b.Stacker()),
		"segments": second(b.Segments()), "counter": second(b.Counter()), "logic": second(b.LogicProbe()),
		"generator": second(b.Generator()),
	} {
		if ok {
			t.Errorf("%s present", name)
		}
	}
	for _, part := range hal.OptionalParts {
		if b.Profile().Absent[part] == "" {
			t.Errorf("no reason for the absent %s", part)
		}
	}
	if g := b.Trigger().LevelGrid(hal.Source{Kind: hal.Analog}); g.Step <= 0 || g.Min >= g.Max {
		t.Errorf("level grid %+v", g)
	}
	if len(b.Platform().Diag()) == 0 {
		t.Error("no diag")
	}
}

func second[T any](_ T, ok bool) bool { return ok }

func TestExport(t *testing.T) {
	b := New(Config{})
	plan, _ := b.Planner().Plan(hal.Request{TimePerDivS: 1e-6, Depth: 2000})
	_ = b.Acquirer().Configure(plan)
	_ = b.Acquirer().Arm()
	st, _ := b.Acquirer().Wait(context.Background(), time.Second)
	var tr sample.Trace
	if err := b.Readout().Export(context.Background(), st.Record, 1, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Ch != 1 || tr.Valid != plan.Depth || len(tr.Codes8) != plan.Depth {
		t.Errorf("export ch %d valid %d codes %d, depth %d", tr.Ch, tr.Valid, len(tr.Codes8), plan.Depth)
	}
}

func TestTriggerDefault(t *testing.T) {
	b := New(Config{})
	st, err := b.Trigger().Default(hal.NodeEdge, hal.Source{Kind: hal.Analog, Index: 1})
	if err != nil || st.Node != hal.NodeEdge || st.Source.Index != 1 || st.Levels.Slope != hal.Rising {
		t.Errorf("default %+v %v", st, err)
	}
	if _, err := b.Trigger().Default(hal.NodeVideo, hal.Source{Kind: hal.Analog}); err == nil {
		t.Error("a node the board lacks has a default")
	}
}

func TestSelfCal(t *testing.T) {
	b := New(Config{})
	var steps []float64
	rep, err := b.FrontEnd().SelfCal(context.Background(), func(x float64) { steps = append(steps, x) })
	if err != nil || len(rep.Failed) != 0 || !rep.Source.Valid || rep.Source.Author != "open" {
		t.Fatalf("report %+v %v", rep, err)
	}
	if len(steps) == 0 || steps[len(steps)-1] != 1 {
		t.Errorf("progress %v", steps)
	}
	if c := b.FrontEnd().Calibration(); c != rep.Source {
		t.Errorf("calibration %+v after the report's %+v", c, rep.Source)
	}
	plan, _ := b.Planner().Plan(hal.Request{TimePerDivS: 1e-6})
	_ = b.Acquirer().Configure(plan)
	_ = b.Acquirer().Arm()
	if _, err := b.FrontEnd().SelfCal(context.Background(), nil); err == nil {
		t.Error("self-calibration while armed")
	}
}

func TestBeeperAndTouch(t *testing.T) {
	b := New(Config{})
	in := b.Input()
	if err := in.SetSound(true); err != nil {
		t.Fatal(err)
	}
	if err := in.Beep(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if beeps, sound := b.Beeps(); !sound || len(beeps) != 1 {
		t.Errorf("beeps %v sound %v", beeps, sound)
	}
	if _, ok := hal.AsRefusal(in.SetTouch(false)); !ok {
		t.Error("touch switch on a board without touch is not refused")
	}
}
