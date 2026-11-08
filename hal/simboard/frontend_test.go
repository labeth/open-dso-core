package simboard

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/siggen"
)

// A second-order front end rings on a fast edge by the overshoot it is
// given, settles to the level, and rises in about the time its bandwidth
// allows; the same signal gives the same samples every time.
// TRLC-LINKS: REQ-DSO-1001
func TestRingingEdge(t *testing.T) {
	sig, err := siggen.Parse("square,10e6,3.3,1.65")
	if err != nil {
		t.Fatal(err)
	}
	v := view{sig: sig, scale: 1}
	const dt = 2e-9
	run := func() []float64 {
		vs := make([]float64, 500)
		for i := range vs {
			vs[i] = v.volts(float64(i) * dt)
		}
		ringLimit(vs, v, 0, dt, 100e6, 0.11)
		return vs
	}
	vs := run()
	hi, lo := math.Inf(-1), math.Inf(1)
	for _, x := range vs {
		hi, lo = max(hi, x), min(lo, x)
	}
	if over := (hi - 3.3) / 3.3; math.Abs(over-0.11) > 0.03 {
		t.Errorf("overshoot %.3f, want about 0.11 (max %.3f V)", over, hi)
	}
	if under := -lo / 3.3; math.Abs(under-0.11) > 0.03 {
		t.Errorf("undershoot %.3f, want about 0.11 (min %.3f V)", under, lo)
	}
	// The top settles before the next edge (each half period is 25 samples).
	if top := vs[24]; math.Abs(top-3.3) > 0.05 {
		t.Errorf("settled top %.3f V, want 3.3 V", top)
	}
	if again := run(); !equalFloats(vs, again) {
		t.Error("the same signal rang differently twice")
	}
}

// equalFloats reports whether a and b hold the same values.
// TRLC-LINKS: REQ-DSO-1001
func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A level that is a DAC's code steps that code's volts at the BNC, at the
// probe tip, on a grid through 0 V: 1 V at 10x is the code nearest 1 V.
// TRLC-LINKS: REQ-DSO-1001
func TestLevelDACGrid(t *testing.T) {
	b, err := New(Config{
		Profile:    hal.Profile{Channels: 1, Format: format8, Display: hal.DisplaySpec{DivsV: 8}},
		LevelStepV: 1.0 / 911,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.FrontEnd().Set(0, hal.Channel{On: true, VoltsDiv: 1, Probe: 10}); err != nil {
		t.Fatal(err)
	}
	g := b.Trigger().LevelGrid(hal.Source{Kind: hal.Analog})
	if math.Abs(g.Step-10.0/911) > 1e-12 {
		t.Fatalf("step %g, want %g", g.Step, 10.0/911)
	}
	if got, want := g.Snap(1), 91*10.0/911; math.Abs(got-want) > 1e-9 {
		t.Errorf("1 V snaps to %g, want %g (code 91)", got, want)
	}
	if r := math.Mod(math.Abs(g.Min)/g.Step, 1); r > 1e-6 && r < 1-1e-6 {
		t.Errorf("grid %+v does not pass through 0 V", g)
	}
}
