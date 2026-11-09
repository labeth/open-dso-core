package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// profile: the profile is consistent in itself and with the identity
// bring-up read.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) profile(t *testing.T) {
	t.Helper()
	p := s.p
	if err := p.Format.Validate(); err != nil {
		t.Errorf("format: %v", err)
	}
	if len(p.Rates) == 0 {
		t.Error("no sample rates")
	}
	for _, r := range p.Rates {
		if r.SamplesPerS <= 0 || r.MaxChannels < 1 || r.MaxChannels > p.Channels {
			t.Errorf("rate %+v: want a positive rate for 1 … %d channels", r, p.Channels)
		}
	}
	if p.MaxDepth <= 0 {
		t.Errorf("max depth %d", p.MaxDepth)
	}
	if p.Content == 0 {
		t.Error("no content kinds")
	}
	d := p.Display
	if d.W <= 0 || d.H <= 0 || d.DivsH <= 0 || d.DivsV <= 0 {
		t.Errorf("display %dx%d with %dx%d divisions", d.W, d.H, d.DivsH, d.DivsV)
	}
	if d.Format < hal.RGB565 || d.Format > hal.ARGB8888 {
		t.Errorf("display pixel format %d", d.Format)
	}
	if a := d.Area; d.HWRenderer && (a.W <= 0 || a.H <= 0 || a.X < 0 || a.Y < 0 || a.X+a.W > d.W || a.Y+a.H > d.H) {
		t.Errorf("hardware renderer area %+v outside the %dx%d screen", a, d.W, d.H)
	}
	td := p.Timebase.TdivS
	if len(td) == 0 {
		t.Error("no time/div ladder")
	}
	for i, v := range td {
		if v <= 0 || (i > 0 && v <= td[i-1]) {
			t.Errorf("time/div ladder not positive and ascending at %d: %v", i, td)
			break
		}
	}
	if f := p.Timebase.FullRateTdivS; f != 0 && !onLadder(td, f) {
		t.Errorf("full-rate time/div %g is not on the ladder", f)
	}
	if !p.Triggers.Has(hal.NodeEdge) {
		t.Error("the profile's triggers lack the edge node")
	}
	seen := map[string]bool{}
	for _, k := range p.Inputs.Keys {
		if k == "" || seen[k] {
			t.Errorf("key %q empty or listed twice", k)
		}
		seen[k] = true
	}
	if s.o.BringUp && (p.Identity.Model != s.id.Model || p.Identity.Vendor != s.id.Vendor) {
		t.Errorf("profile identity %+v, bring-up read %+v", p.Identity, s.id)
	}
}

// onLadder reports whether v is a step of ladder, to a part in a million.
// TRLC-LINKS: REQ-DSO-1001
func onLadder(ladder []float64, v float64) bool {
	for _, x := range ladder {
		if math.Abs(x-v) <= 1e-6*math.Abs(v) {
			return true
		}
	}
	return false
}

// devices: every required facet is there and agrees with the profile.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) devices(t *testing.T) {
	t.Helper()
	b, p := s.b, s.p
	for name, nilFacet := range map[string]bool{
		"Platform": b.Platform() == nil, "Planner": b.Planner() == nil, "Acquirer": b.Acquirer() == nil,
		"Readout": b.Readout() == nil, "Trigger": b.Trigger() == nil, "FrontEnd": b.FrontEnd() == nil,
		"Display": b.Display() == nil, "Input": b.Input() == nil, "Storage": b.Storage() == nil,
	} {
		if nilFacet {
			t.Errorf("%s() is nil", name)
		}
	}
	if d := b.Display(); d != nil {
		s.display(t, d)
	}
	if in := b.Input(); in != nil {
		if !sameSet(in.Keys(), p.Inputs.Keys) {
			t.Errorf("Input.Keys %v, profile %v", in.Keys(), p.Inputs.Keys)
		}
		if !sameSet(in.Knobs(), p.Inputs.Knobs) {
			t.Errorf("Input.Knobs %v, profile %v", in.Knobs(), p.Inputs.Knobs)
		}
		if in.Events() == nil {
			t.Error("Input.Events is nil")
		}
	}
	if fe := b.FrontEnd(); fe != nil {
		s.frontEnd(t, fe)
	}
	for n := range b.Trigger().Caps().Nodes {
		if !p.Triggers.Has(n) {
			t.Errorf("Trigger.Caps has node %d the profile lacks", n)
		}
	}
	if lim := b.Readout().Limits(); lim.MaxWindows < 1 || lim.MaxPoints <= 0 || lim.ChannelsPerWindow < 0 {
		t.Errorf("Readout.Limits %+v", lim)
	}
	if d := b.Platform().Diag(); len(d) > 0 && !json.Valid(d) {
		t.Errorf("Platform.Diag is not JSON: %q", d)
	}
}

// display checks the display against the profile.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) display(t *testing.T, d hal.Display) {
	t.Helper()
	p := s.p
	if !reflect.DeepEqual(d.Spec(), p.Display) {
		t.Errorf("Display.Spec %+v, profile %+v", d.Spec(), p.Display)
	}
	if fb := d.Framebuffer(); fb != nil {
		bpp := 4
		if p.Display.Format == hal.RGB565 {
			bpp = 2
		}
		if fb.Stride() < p.Display.W*bpp || len(fb.Back()) < fb.Stride()*p.Display.H {
			t.Errorf("framebuffer stride %d, %d bytes: too small for %dx%d at %d bytes a pixel",
				fb.Stride(), len(fb.Back()), p.Display.W, p.Display.H, bpp)
		}
	}
	if _, ok := d.Renderer(); ok != p.Display.HWRenderer {
		t.Errorf("Display.Renderer present %v, profile hw_renderer %v", ok, p.Display.HWRenderer)
	}
}

// frontEnd checks each channel's ladders and grids.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) frontEnd(t *testing.T, fe hal.FrontEnd) {
	t.Helper()
	p, b := s.p, s.b
	for ch := range p.Channels {
		lad := fe.Ladder(ch)
		if len(lad) == 0 {
			t.Errorf("C%d: empty V/div ladder", ch+1)
		}
		for i, st := range lad {
			if st.VoltsDiv <= 0 || (i > 0 && st.VoltsDiv <= lad[i-1].VoltsDiv) {
				t.Errorf("C%d: V/div ladder not positive and ascending: %v", ch+1, lad)
				break
			}
		}
		if !slices.ContainsFunc(fe.Couplings(ch), func(c hal.CouplingOption) bool { return c.Coupling == hal.DC }) {
			t.Errorf("C%d: couplings %v lack DC", ch+1, fe.Couplings(ch))
		}
		if g := fe.OffsetGrid(ch); g.Step < 0 || g.Min > g.Max {
			t.Errorf("C%d: offset grid %+v", ch+1, g)
		}
		if g := b.Trigger().LevelGrid(hal.Source{Kind: hal.Analog, Index: ch}); g.Step < 0 || g.Min >= g.Max {
			t.Errorf("C%d: trigger level grid %+v", ch+1, g)
		}
	}
	if fe.Calibration().Source == "" {
		t.Error("FrontEnd.Calibration names no source")
	}
}

// sameSet reports whether a and b hold the same strings.
// TRLC-LINKS: REQ-DSO-1001
func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

// planner: a plan is pure, repeatable and inside the profile.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) planner(t *testing.T) {
	t.Helper()
	before := s.b.Acquirer().State()
	p1, err := s.b.Planner().Plan(s.req)
	if err != nil {
		t.Fatalf("Plan(%+v): %v", s.req, err)
	}
	p2, err := s.b.Planner().Plan(s.req)
	if err != nil || !reflect.DeepEqual(p1, p2) {
		t.Errorf("the same request planned twice differs: %+v / %+v (%v)", p1, p2, err)
	}
	if !slices.Equal(p1.Request.Channels, s.req.Channels) || p1.Request.TimePerDivS != s.req.TimePerDivS {
		t.Errorf("plan's request %+v, asked %+v", p1.Request, s.req)
	}
	if p1.Depth <= 0 || p1.Depth > s.p.MaxDepth {
		t.Errorf("depth %d outside 1 … %d", p1.Depth, s.p.MaxDepth)
	}
	if p1.SampleS <= 0 || p1.WinSamples <= 0 || p1.AcqDecim < 1 {
		t.Errorf("sample interval %g, window %d samples, decimation %d", p1.SampleS, p1.WinSamples, p1.AcqDecim)
	}
	if p1.Content == 0 || p1.Content&^s.p.Content != 0 {
		t.Errorf("plan content %v outside the profile's %v", p1.Content, s.p.Content)
	}
	if st := s.b.Acquirer().State(); st != before {
		t.Errorf("planning moved the acquisition from %d to %d", before, st)
	}
}

// needRefusal: a feature the plan cannot serve is a *Refusal.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) needRefusal(t *testing.T) {
	t.Helper()
	plan, err := s.b.Planner().Plan(s.req)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var other sample.ContentSet
	for c := sample.RawFullRate; c <= sample.DisplayDomain; c++ {
		if !plan.Content.Has(c) {
			other |= sample.ContentSetOf(c)
		}
	}
	if other != 0 {
		r := s.req
		r.Need = []hal.Feature{{Name: "conformance-content", Accepts: other}}
		_, err := s.b.Planner().Plan(r)
		isRefusal(t, err, fmt.Sprintf("a feature accepting only %v", other))
	}
	r := s.req
	r.Need = []hal.Feature{{Name: "conformance-depth", Accepts: plan.Content, MinDepth: 2*s.p.MaxDepth + 1}}
	_, err = s.b.Planner().Plan(r)
	isRefusal(t, err, "a feature needing more than the deepest record")
}

// stageCommit: a front-end setting is staged by Set and applied by Arm;
// Get reads what is applied.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) stageCommit(t *testing.T) {
	t.Helper()
	fe := s.b.FrontEnd()
	ch := s.req.Channels[0]
	applied := fe.Get(ch)
	var to float64
	for _, st := range fe.Ladder(ch) {
		if st.VoltsDiv != applied.VoltsDiv && (to == 0 || math.Abs(math.Log(st.VoltsDiv/applied.VoltsDiv)) < math.Abs(math.Log(to/applied.VoltsDiv))) {
			to = st.VoltsDiv
		}
	}
	if to == 0 {
		t.Skip("the ladder has one step")
	}
	c := applied
	c.VoltsDiv = to
	staged, err := fe.Set(ch, c)
	if err != nil {
		t.Fatalf("Set(%d, %g V/div): %v", ch, to, err)
	}
	defer s.restore(t, ch)
	if g := fe.Get(ch); g.VoltsDiv != applied.VoltsDiv {
		t.Errorf("Get after Set and before Arm reads %g V/div; want the applied %g (Set stages, Arm commits)", g.VoltsDiv, applied.VoltsDiv)
	}
	spec, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commit(spec); err != nil {
		t.Fatal(err)
	}
	if st := s.b.Acquirer().State(); st == hal.Armed {
		t.Error("Configure and Program armed the acquisition; only Arm may")
	}
	if err := s.b.Acquirer().Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if g := fe.Get(ch); g.VoltsDiv != staged.VoltsDiv {
		t.Errorf("Get after Arm reads %g V/div; want the staged %g", g.VoltsDiv, staged.VoltsDiv)
	}
	if _, err := s.await(t.Context(), hal.Auto); err != nil {
		t.Errorf("no record after the commit arm: %v", err)
	}
}

// stageIdle: a front-end setting staged while a capture is armed waits; at
// a commit point with nothing armed the board applies it.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) stageIdle(t *testing.T) {
	t.Helper()
	fe := s.b.FrontEnd()
	ch := s.req.Channels[0]
	applied := fe.Get(ch)
	var to float64
	for _, st := range fe.Ladder(ch) {
		if st.VoltsDiv != applied.VoltsDiv && (to == 0 || math.Abs(math.Log(st.VoltsDiv/applied.VoltsDiv)) < math.Abs(math.Log(to/applied.VoltsDiv))) {
			to = st.VoltsDiv
		}
	}
	if to == 0 {
		t.Skip("the ladder has one step")
	}
	spec, err := s.unreachable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commit(spec); err != nil {
		t.Fatal(err)
	}
	if err := s.b.Acquirer().Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	defer s.restore(t, ch)
	c := applied
	c.VoltsDiv = to
	staged, err := fe.Set(ch, c)
	if err != nil {
		t.Fatalf("Set(%d, %g V/div): %v", ch, to, err)
	}
	if s.b.Acquirer().State() == hal.Armed {
		s.b.Platform().Service()
		if g := fe.Get(ch); g.VoltsDiv != applied.VoltsDiv {
			t.Errorf("a commit point under an armed capture applied %g V/div; want it staged until nothing is armed", g.VoltsDiv)
		}
	}
	if err := s.b.Acquirer().Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	s.b.Platform().Service()
	if g := fe.Get(ch); g.VoltsDiv != staged.VoltsDiv {
		t.Errorf("Get after a commit point with nothing armed reads %g V/div; want the staged %g", g.VoltsDiv, staged.VoltsDiv)
	}
}

// restore stages channel ch's starting setting again and commits it with
// a record.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) restore(t *testing.T, ch int) {
	t.Helper()
	if _, err := s.b.FrontEnd().Set(ch, s.init[ch]); err != nil {
		t.Errorf("restoring C%d: %v", ch+1, err)
		return
	}
	spec, err := s.edge(hal.Auto)
	if err == nil {
		_, _, err = s.acquire(t.Context(), spec)
	}
	if err != nil {
		t.Errorf("restoring C%d: %v", ch+1, err)
	}
}

// unchangedNoop: configuring the staged plan and programming the staged
// spec again changes nothing, so the next Arm is a plain re-arm; a change
// makes it a commit arm.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) unchangedNoop(t *testing.T) {
	t.Helper()
	if s.o.CommitArms == nil {
		t.Skip("the harness cannot count the board's commit arms (Options.CommitArms)")
	}
	spec, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.acquire(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	n0 := s.o.CommitArms()
	for i := range 2 {
		if _, _, err := s.acquire(t.Context(), spec); err != nil {
			t.Fatal(err)
		}
		if n := s.o.CommitArms(); n != n0 {
			t.Errorf("pass %d: the unchanged plan and spec made %d commit arms", i+1, n-n0)
			n0 = n
		}
	}
	ch := s.req.Channels[0]
	if _, err := s.b.FrontEnd().Set(ch, s.b.FrontEnd().Get(ch)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.acquire(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	if n := s.o.CommitArms(); n != n0 {
		t.Errorf("the front end set to what it holds made %d commit arms", n-n0)
		n0 = n
	}
	changed := spec
	changed.Stages = slices.Clone(spec.Stages)
	changed.Stages[0].Levels.Slope = hal.Falling
	if changed.Stages[0].Levels.Slope == spec.Stages[0].Levels.Slope {
		changed.Stages[0].Levels.Slope = hal.Rising
	}
	if _, _, err := s.acquire(t.Context(), changed); err != nil {
		t.Fatal(err)
	}
	if n := s.o.CommitArms(); n != n0+1 {
		t.Errorf("a changed trigger made %d commit arms; want 1", n-n0)
	}
}

// record: an edge trigger in AUTO gives a ready record.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) record(t *testing.T) {
	t.Helper()
	spec, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	_, st, err := s.acquire(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if a := s.b.Acquirer().State(); a != hal.Ready {
		t.Errorf("state %d after a ready Wait; want Ready (%d)", a, hal.Ready)
	}
	if st.Forced && st.Triggered {
		t.Logf("status %+v: forced and triggered", st)
	}
}

// waitPoll: a zero timeout polls once and returns at once.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) waitPoll(t *testing.T) {
	t.Helper()
	spec, err := s.unreachable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commit(spec); err != nil {
		t.Fatal(err)
	}
	if err := s.b.Acquirer().Arm(); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	st, err := s.b.Acquirer().Wait(t.Context(), 0)
	if el := time.Since(t0); el > 250*time.Millisecond {
		t.Errorf("Wait with a zero timeout took %v", el)
	}
	switch {
	case s.o.NoHoldOff != "" && (err == nil || errors.Is(err, hal.ErrTimeout)):
	case !errors.Is(err, hal.ErrTimeout):
		t.Errorf("Wait(0) on a trigger that cannot fire: %v; want hal.ErrTimeout", err)
	case st.Triggered:
		t.Errorf("Wait(0) on a trigger that cannot fire reports triggered: %+v", st)
	}
	_ = s.b.Acquirer().Stop()
}

// waitTimeout: Wait returns ErrTimeout at its timeout, not a fault.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) waitTimeout(t *testing.T) {
	t.Helper()
	if s.o.NoHoldOff != "" {
		t.Skip(s.o.NoHoldOff)
	}
	spec, err := s.unreachable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commit(spec); err != nil {
		t.Fatal(err)
	}
	if err := s.b.Acquirer().Arm(); err != nil {
		t.Fatal(err)
	}
	const d = 200 * time.Millisecond
	t0 := time.Now()
	_, err = s.b.Acquirer().Wait(t.Context(), d)
	el := time.Since(t0)
	if !errors.Is(err, hal.ErrTimeout) {
		t.Errorf("Wait(%v): %v; want hal.ErrTimeout", d, err)
	}
	if el < d/2 || el > d+time.Second {
		t.Errorf("Wait(%v) returned after %v", d, el)
	}
	_ = s.b.Acquirer().Stop()
}

// waitCancel: a Wait ends promptly when its context does, with the
// context's error.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) waitCancel(t *testing.T) {
	t.Helper()
	if s.o.NoHoldOff != "" {
		t.Skip(s.o.NoHoldOff)
	}
	spec, err := s.unreachable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commit(spec); err != nil {
		t.Fatal(err)
	}
	if err := s.b.Acquirer().Arm(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	_, err = s.b.Acquirer().Wait(ctx, 10*time.Second)
	if el := time.Since(t0); el > time.Second {
		t.Errorf("Wait went on %v after its context ended", el-100*time.Millisecond)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Wait after its context ended: %v; want the context's error", err)
	}
	_ = s.b.Acquirer().Stop()
}

// force: a forced trigger completes a record a Normal trigger would not.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) force(t *testing.T) {
	t.Helper()
	spec, err := s.unreachable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commit(spec); err != nil {
		t.Fatal(err)
	}
	if err := s.b.Acquirer().Arm(); err != nil {
		t.Fatal(err)
	}
	if err := s.b.Acquirer().Force(); err != nil {
		t.Fatalf("Force: %v", err)
	}
	st, err := s.await(t.Context(), hal.NormalSweep)
	if err != nil {
		t.Fatalf("no record after Force: %v", err)
	}
	if s.o.NoHoldOff == "" && (!st.Forced || st.Triggered) {
		t.Errorf("status %+v after a forced record of a trigger that cannot fire; want forced, not triggered", st)
	}
}

// freeRun: a FreeRun spec completes every acquisition without a trigger.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) freeRun(t *testing.T) {
	t.Helper()
	spec, err := s.unreachable()
	if err != nil {
		t.Fatal(err)
	}
	spec.FreeRun = true
	_, st, err := s.acquire(t.Context(), spec)
	if err != nil {
		t.Fatalf("FreeRun gave no record: %v", err)
	}
	if s.o.NoHoldOff != "" {
		return
	}
	if st.Triggered {
		t.Errorf("FreeRun record reports triggered: %+v", st)
	}
	fs, err := s.mainRead(t.Context(), st.Record)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if k := fs[0].TrigKind; k == sample.HardwareNode || k == sample.SoftwareRefined {
		t.Errorf("FreeRun frame's trigger kind %d: a trigger fired", k)
	}
}

// readout: the main window delivers the plan's content in the profile's
// code format, with a scale snapshotted into each trace.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) readout(t *testing.T) {
	t.Helper()
	spec, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	plan, st, err := s.acquire(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := s.mainRead(t.Context(), st.Record)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var chans []int
	for _, f := range fs {
		s.checkFrame(t, f, plan)
		for i := range f.Traces {
			chans = append(chans, f.Traces[i].Ch)
		}
	}
	slices.Sort(chans)
	if !slices.Equal(chans, s.req.Channels) {
		t.Errorf("traces of channels %v; asked %v", chans, s.req.Channels)
	}
}

// checkFrame checks one frame of plan against the content and format
// rules.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) checkFrame(t *testing.T, f *sample.Frame, plan hal.Plan) {
	t.Helper()
	n := f.Len()
	if n <= 0 {
		t.Fatalf("an empty main frame")
	}
	if !plan.Content.Has(f.Content) {
		t.Errorf("frame content %v outside the plan's %v", f.Content, plan.Content)
	}
	if f.SampleS <= 0 {
		t.Errorf("frame sample interval %g", f.SampleS)
	}
	if f.TriggerAt < -1 || f.TriggerAt > float64(n) {
		t.Logf("trigger at %g of %d samples (outside the window)", f.TriggerAt, n)
	}
	raw := f.Content == sample.RawFullRate || f.Content == sample.RawDecimated
	for i := range f.Traces {
		tr := &f.Traces[i]
		if err := tr.Format.Validate(); err != nil {
			t.Errorf("C%d: %v", tr.Ch+1, err)
			continue
		}
		if raw && !reflect.DeepEqual(tr.Format, s.p.Format) {
			t.Errorf("C%d: %v frame in %+v; the profile's code format is %+v", tr.Ch+1, f.Content, tr.Format, s.p.Format)
		}
		words := len(tr.Codes8)
		if tr.Format.Storage == sample.U16 {
			words = len(tr.Codes16)
		}
		want := n
		if f.Content == sample.PeakPairs {
			want = 2 * n
		}
		if words < n || (f.Content == sample.PeakPairs && words < want) {
			t.Errorf("C%d: %d words in its %s storage for %d samples", tr.Ch+1, words, storageName(tr.Format.Storage), n)
			continue
		}
		if raw {
			for k := range n {
				if w := tr.Word(k); w < tr.Format.Min || w > tr.Format.Max {
					t.Errorf("C%d: word %d at %d outside %d … %d", tr.Ch+1, w, k, tr.Format.Min, tr.Format.Max)
					break
				}
			}
		}
		if tr.Scale.PerCode == 0 || tr.CodesPerDiv <= 0 {
			t.Errorf("C%d: scale %+v, %g codes a division", tr.Ch+1, tr.Scale, tr.CodesPerDiv)
		}
	}
}

// storageName names a word type for a message.
// TRLC-LINKS: REQ-DSO-1001
func storageName(k sample.StorageKind) string {
	b, err := k.MarshalText()
	if err != nil {
		return strconv.Itoa(int(k))
	}
	return string(b)
}

// readoutWindows: a main window of a given span fills its frame; a zoom
// window beside it delivers at most its points.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) readoutWindows(t *testing.T) {
	t.Helper()
	lim := s.b.Readout().Limits()
	spec, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	plan, st, err := s.acquire(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	ch := []int{s.req.Channels[0]}
	pts := min(100, lim.MaxPoints, plan.WinSamples)
	main := &sample.Frame{}
	req := hal.ReadRequest{Record: st.Record, Segments: [2]int{0, 1}, Windows: []hal.Window{{
		Role: hal.Main, Channels: ch, StartSample: -int64(pts) / 2, Span: int64(pts), Points: pts,
	}}}
	if err := s.b.Readout().Read(t.Context(), req, []*sample.Frame{main}, nil); err != nil {
		t.Fatalf("Read of a %d-sample main window: %v", pts, err)
	}
	if main.Len() != pts {
		t.Errorf("a %d-sample main window delivered %d", pts, main.Len())
	}
	if lim.MaxWindows < 2 {
		return
	}
	zoom := &sample.Frame{}
	req.Windows = append(req.Windows, hal.Window{
		Role: hal.Zoom, Channels: ch, StartSample: -int64(pts) / 4, Span: int64(pts) / 2, Points: pts / 2,
	})
	main = &sample.Frame{}
	if err := s.b.Readout().Read(t.Context(), req, []*sample.Frame{main, zoom}, nil); err != nil {
		t.Fatalf("Read of a main and a zoom window: %v", err)
	}
	if main.Len() != pts || zoom.Len() <= 0 || zoom.Len() > pts/2 {
		t.Errorf("main %d and zoom %d samples; asked %d and %d", main.Len(), zoom.Len(), pts, pts/2)
	}
}

// readoutRefusal: a window whose content the board cannot deliver is a
// *Refusal, never another kind.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) readoutRefusal(t *testing.T) {
	t.Helper()
	spec, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	plan, st, err := s.acquire(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	want := sample.Content(0)
	for c := sample.DisplayDomain; c >= sample.RawFullRate; c-- {
		if !plan.Content.Has(c) {
			want = c
			break
		}
	}
	if want == 0 {
		t.Skip("the plan delivers every content kind")
	}
	f := &sample.Frame{}
	err = s.b.Readout().Read(t.Context(), hal.ReadRequest{Record: st.Record, Segments: [2]int{0, 1}, Windows: []hal.Window{
		{Role: hal.Main, Channels: s.req.Channels[:1], Want: want},
	}}, []*sample.Frame{f}, nil)
	isRefusal(t, err, fmt.Sprintf("a %v window of a plan with %v", want, plan.Content))
	if err == nil && f.Content != want {
		t.Errorf("asked %v, delivered %v", want, f.Content)
	}
}

// stoppedRecord: Stop leaves the stored record readable, the same record.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) stoppedRecord(t *testing.T) {
	t.Helper()
	spec, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	_, st, err := s.acquire(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.mainRead(t.Context(), st.Record)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	n0 := 0
	if s.o.CommitArms != nil {
		n0 = s.o.CommitArms()
	}
	if err := s.b.Acquirer().Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := s.b.Acquirer().State(); got != hal.Idle && got != hal.Ready {
		t.Errorf("state %d after Stop; want Idle (%d) or Ready (%d): nothing armed, the record readable", got, hal.Idle, hal.Ready)
	}
	b, err := s.mainRead(t.Context(), st.Record)
	if err != nil {
		t.Fatalf("Read of the stored record after Stop: %v", err)
	}
	if len(a) != len(b) || !sameWords(a[0], b[0]) {
		t.Error("the stored record read after Stop differs from the one read before it")
	}
	if s.o.CommitArms == nil {
		return
	}
	if err := s.b.Acquirer().Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if n := s.o.CommitArms(); n != n0+1 {
		t.Errorf("the first Arm after Stop made %d commit arms; want 1", n-n0)
	}
	if _, err := s.await(t.Context(), hal.Auto); err != nil {
		t.Errorf("after Stop: %v", err)
	}
}

// screenRecord: on a readout that places its screen window
// (hal.ScreenReader), a Record request is the whole stored record, raw:
// readable once the acquisition stopped (Idle with a stored record), never
// a peak envelope or a display envelope whatever else the request asks,
// at least as long as the live screen window, and the same record each
// time it is read.
// TRLC-LINKS: REQ-DSO-1001, REQ-DSO-1003
func (s *suite) screenRecord(t *testing.T) {
	t.Helper()
	sr, ok := s.b.Readout().(hal.ScreenReader)
	if !ok {
		t.Skip("the readout places no screen window: the engine reads its main window")
	}
	spec, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	_, st, err := s.acquire(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	live, err := sr.ScreenWindow(hal.ScreenRequest{PosFrac: .5})
	if err != nil {
		t.Fatalf("ScreenWindow of the live screen: %v", err)
	}
	screen, err := s.windowRead(t.Context(), st.Record, live)
	if err != nil {
		t.Fatalf("Read of the live screen window: %v", err)
	}
	if err := s.b.Acquirer().Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	read := func() *sample.Frame {
		w, err := sr.ScreenWindow(hal.ScreenRequest{PosFrac: .2, PreRoom: 100, Envelope: true, Record: true})
		if err != nil {
			t.Fatalf("ScreenWindow of the stopped record: %v", err)
		}
		fs, err := s.windowRead(t.Context(), st.Record, w)
		if err != nil {
			t.Fatalf("Read of the stopped record's window: %v", err)
		}
		return fs[0]
	}
	a, b := read(), read()
	if a.Content == sample.PeakPairs || a.Flags.Has(sample.Envelope) {
		t.Errorf("the whole record came as %v (flags %b): a Record window is raw", a.Content, a.Flags)
	}
	if a.Len() < screen[0].Len() {
		t.Errorf("the whole record holds %d samples, the live screen window %d", a.Len(), screen[0].Len())
	}
	if !sameWords(a, b) {
		t.Error("the stopped record read twice differs")
	}
}

// windowRead reads window w of every requested channel as the engine does:
// one window, or one a channel when the board reads one at a time.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) windowRead(ctx context.Context, rec hal.RecordID, w hal.Window) ([]*sample.Frame, error) {
	var ws []hal.Window
	if s.b.Readout().Limits().ChannelsPerWindow == 1 {
		for _, ch := range s.req.Channels {
			x := w
			x.Channels = []int{ch}
			ws = append(ws, x)
		}
	} else {
		w.Channels = s.req.Channels
		ws = []hal.Window{w}
	}
	fs := make([]*sample.Frame, len(ws))
	for i := range fs {
		fs[i] = &sample.Frame{}
	}
	err := s.b.Readout().Read(ctx, hal.ReadRequest{Record: rec, Segments: [2]int{0, 1}, Windows: ws}, fs, s.b.Platform().Service)
	return fs, err
}

// sameWords reports whether a and b hold the same samples.
// TRLC-LINKS: REQ-DSO-1001
func sameWords(a, b *sample.Frame) bool {
	if a.Len() != b.Len() || len(a.Traces) != len(b.Traces) {
		return false
	}
	for i := range a.Traces {
		for k := range a.Len() {
			if a.Traces[i].Word(k) != b.Traces[i].Word(k) {
				return false
			}
		}
	}
	return true
}

// triggerRefusal: a node the image lacks, zones without zone hardware and
// a source the scope does not have are each a *Refusal, and a refused
// spec leaves the staged one in place.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) triggerRefusal(t *testing.T) {
	t.Helper()
	good, err := s.edge(hal.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commit(good); err != nil {
		t.Fatal(err)
	}
	caps := s.b.Trigger().Caps()
	src := good.Stages[0].Source
	for n := hal.NodeEdge; n <= hal.NodeProtocol; n++ {
		if caps.Has(n) {
			continue
		}
		st := good.Stages[0]
		st.Node = n
		isRefusal(t, s.b.Trigger().Program(hal.TriggerSpec{Stages: []hal.TrigStage{st}, Sweep: hal.Auto}), fmt.Sprintf("node %d the image lacks", n))
		break
	}
	if !caps.Has(hal.NodeZone) {
		st := good.Stages[0]
		st.Zones = []hal.Zone{{Source: src, FromS: -1e-6, ToS: 1e-6, Low: -0.1, High: 0.1}}
		isRefusal(t, s.b.Trigger().Program(hal.TriggerSpec{Stages: []hal.TrigStage{st}, Sweep: hal.Auto}), "a zone-qualified edge without zone hardware")
	}
	st := good.Stages[0]
	st.Source = hal.Source{Kind: hal.Analog, Index: s.p.Channels}
	isRefusal(t, s.b.Trigger().Program(hal.TriggerSpec{Stages: []hal.TrigStage{st}, Sweep: hal.Auto}), fmt.Sprintf("an edge on C%d of a %d-channel scope", s.p.Channels+1, s.p.Channels))
	if err := s.b.Acquirer().Arm(); err != nil {
		t.Fatalf("Arm after the refusals: %v", err)
	}
	if _, err := s.await(t.Context(), hal.Auto); err != nil {
		t.Errorf("the spec staged before the refusals gives no record: %v", err)
	}
}

// triggerDefault: Default gives each node's starting stage on the asked
// source, or a *Refusal.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) triggerDefault(t *testing.T) {
	t.Helper()
	tr := s.b.Trigger()
	for ch := range s.p.Channels {
		src := hal.Source{Kind: hal.Analog, Index: ch}
		st, err := tr.Default(hal.NodeEdge, src)
		if err != nil {
			t.Errorf("Default(edge, C%d): %v", ch+1, err)
			continue
		}
		g := tr.LevelGrid(src)
		if st.Node != hal.NodeEdge || st.Source != src || st.Levels.Slope == 0 || st.Levels.High < g.Min || st.Levels.High > g.Max {
			t.Errorf("Default(edge, C%d) = %+v; level grid %+v", ch+1, st, g)
		}
	}
	src := hal.Source{Kind: hal.Analog, Index: s.req.Channels[0]}
	nodes := slices.Sorted(maps.Keys(tr.Caps().Nodes))
	for _, n := range nodes {
		st, err := tr.Default(n, src)
		if err == nil && st.Node != n {
			t.Errorf("Default(%d) gave node %d", n, st.Node)
		}
		if _, refused := hal.AsRefusal(err); err != nil && !refused {
			t.Errorf("Default(%d): %v; want a stage or a *hal.Refusal", n, err)
		}
	}
	_, err := tr.Default(hal.NodeEdge, hal.Source{Kind: hal.Analog, Index: s.p.Channels})
	isRefusal(t, err, fmt.Sprintf("Default(edge) on C%d of a %d-channel scope", s.p.Channels+1, s.p.Channels))
}

// frontEndSnap: a V/div is staged inside the ladder's range (snapped to
// the hardware's grid, which may be finer than the ladder) or refused.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) frontEndSnap(t *testing.T) {
	t.Helper()
	fe := s.b.FrontEnd()
	ch := s.req.Channels[0]
	lad := fe.Ladder(ch)
	if len(lad) == 0 {
		t.Fatal("no ladder")
	}
	var steps []float64
	for _, st := range lad {
		steps = append(steps, st.VoltsDiv)
	}
	defer s.restore(t, ch)
	for _, v := range []float64{steps[len(steps)/2] * 1.07, steps[0] / 3} {
		c := fe.Get(ch)
		c.VoltsDiv = v
		got, err := fe.Set(ch, c)
		if _, refused := hal.AsRefusal(err); refused {
			continue
		}
		if err != nil {
			t.Errorf("Set(C%d, %g V/div): %v; want it snapped or a *hal.Refusal", ch+1, v, err)
			continue
		}
		if got.VoltsDiv < steps[0]*(1-1e-9) || got.VoltsDiv > steps[len(steps)-1]*(1+1e-9) {
			t.Errorf("Set(C%d, %g V/div) staged %g V/div, outside the ladder %g … %g", ch+1, v, got.VoltsDiv, steps[0], steps[len(steps)-1])
		}
	}
}

// input: the panel's keys and knobs are the profile's, an event comes out
// in core names, and a beeper or a touch screen the scope lacks is
// refused.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) input(t *testing.T) {
	t.Helper()
	in := s.b.Input()
	p := s.p
	if !p.Inputs.Beeper {
		isRefusal(t, in.SetSound(true), "SetSound without a beeper")
		isRefusal(t, in.Beep(time.Millisecond), "Beep without a beeper")
	} else {
		if err := in.SetSound(false); err != nil {
			t.Errorf("SetSound: %v", err)
		}
	}
	if !p.Display.Touch && !p.Inputs.Touch {
		isRefusal(t, in.SetTouch(true), "SetTouch without a touch screen")
	} else if err := in.SetTouch(true); err != nil {
		t.Errorf("SetTouch(true): %v", err)
	}
	if s.o.Inject == nil {
		t.Log("no event round trip: the harness cannot press a key (Options.Inject)")
		return
	}
	drain(in.Events())
	var want []hal.InputEvent
	if len(p.Inputs.Keys) > 0 {
		want = append(want, hal.InputEvent{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: p.Inputs.Keys[0], Down: true})
	}
	if len(p.Inputs.Knobs) > 0 {
		want = append(want, hal.InputEvent{Source: hal.FromPanel, Kind: hal.KnobEvent, Knob: p.Inputs.Knobs[0], Steps: -2})
	}
	for _, ev := range want {
		if !s.o.Inject(ev) {
			t.Errorf("Inject(%+v) refused", ev)
			continue
		}
		select {
		case got := <-in.Events():
			if got != ev {
				t.Errorf("event %+v came out as %+v", ev, got)
			}
		case <-time.After(time.Second):
			t.Errorf("event %+v did not come out of Events", ev)
		}
	}
}

// drain empties an event channel without blocking.
// TRLC-LINKS: REQ-DSO-1001
func drain(ch <-chan hal.InputEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// storage: a value put is the value got, and a value past the budget is a
// *Refusal.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) storage(t *testing.T) {
	t.Helper()
	st := s.b.Storage()
	check := func(name, key string, kv hal.KV) {
		if kv == nil {
			t.Errorf("%s store is nil", name)
			return
		}
		old, had := kv.Get(key)
		val := []byte(`{"conformance":true}`)
		if err := kv.Put(key, val); err != nil {
			t.Errorf("%s.Put(%q): %v", name, key, err)
			return
		}
		if got, ok := kv.Get(key); !ok || string(got) != string(val) {
			t.Errorf("%s.Get(%q) = %q, %v; put %q", name, key, got, ok, val)
		}
		if had {
			if err := kv.Put(key, old); err != nil {
				t.Errorf("%s: putting %q back: %v", name, key, err)
			}
		}
		if b := kv.Budget(); b > 0 {
			isRefusal(t, kv.Put(key, make([]byte, b+1)), fmt.Sprintf("%s.Put past its %d-byte budget", name, b))
		} else if b < 0 {
			t.Errorf("%s budget %d", name, b)
		}
	}
	check("Settings", "hal-conformance", st.Settings())
	if kv, ok := st.Setups(); ok {
		check("Setups", "1", kv) // setups are numbered slots
	}
	if _, err := st.Captures(); err != nil {
		t.Errorf("Captures: %v", err)
	}
}

// optionalParts: each optional part is there, or absent with the
// profile's reason, and the profile's options say the same.
// TRLC-LINKS: REQ-DSO-1001
func (s *suite) optionalParts(t *testing.T) {
	t.Helper()
	b := s.b
	type part struct {
		name    string
		present bool
		opt     hal.OptSet
	}
	present := func(_ any, ok bool) bool { return ok }
	parts := []part{
		{"streamer", present(b.Streamer()), 0},
		{"images", present(b.Images()), 0},
		{"stacker", present(b.Stacker()), hal.OptStacker},
		{"segments", present(b.Segments()), hal.OptSegments},
		{"counter", present(b.Counter()), hal.OptCounter},
		{"logic-probe", present(b.LogicProbe()), hal.OptLogicProbe},
		{"generator", present(b.Generator()), hal.OptAWG},
	}
	for _, pt := range parts {
		reason, has := s.p.Absent[pt.name]
		switch {
		case !pt.present && (!has || reason == ""):
			t.Errorf("%s is absent and the profile gives no reason for it", pt.name)
		case pt.present && has:
			t.Errorf("%s is present and the profile calls it absent: %q", pt.name, reason)
		}
		if pt.opt != 0 && s.p.Options.Has(pt.opt) != pt.present {
			t.Errorf("%s present %v, profile options say %v", pt.name, pt.present, s.p.Options.Has(pt.opt))
		}
	}
	for _, e := range b.Extensions() {
		if e.Feature == "" {
			t.Errorf("an extension without a feature name: %+v", e)
		}
		for _, r := range e.Routes {
			if r.Pattern == "" || r.Handler == nil {
				t.Errorf("extension %s: route %q without a pattern or handler", e.Feature, r.Pattern)
			}
		}
	}
}
