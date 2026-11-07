// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// TRLC-LINKS: REQ-DSO-011
func TestQualifyPulseFlatReject(t *testing.T) {
	flat := make([]uint8, 500)
	for i := range flat {
		flat[i] = 128 + uint8(i%3) // 2-code noise
	}
	if xc := QualifyPulse(flat, 100, DefaultTrigParams(), true); xc != -1 {
		t.Fatalf("flat rail produced pulse event %v", xc)
	}
}

// TRLC-LINKS: REQ-DSO-011
func TestQualifySlope(t *testing.T) {
	// A slow ramp (100 samples lo→hi) and a fast step, both rising.
	sig := make([]uint8, 2000)
	for i := range sig {
		sig[i] = 50
	}
	for i := range 100 { // slow ramp at 500..599: 50 → 200
		sig[500+i] = uint8(50 + 150*i/100)
	}
	for i := 600; i < 1400; i++ {
		sig[i] = 200
	}
	sig[1400] = 50 // fall back
	for i := 1401; i < 2000; i++ {
		sig[i] = 50
	}

	p := DefaultTrigParams()
	// lo=0.2, hi=0.8 of span 150: lo≈80, hi≈170. The ramp crosses lo at
	// ~i=520 and hi at ~i=580 → traversal ≈60 samples ≈ 6 µs at 100 ns.
	p.SlopeTMinNs, p.SlopeTMaxNs, p.SlopeCond = 4000, 8000, CondInside
	xc := QualifySlope(sig, 100, p, true)
	if xc < 570 || xc > 590 {
		t.Fatalf("slope anchor %v, want ≈580 (second-threshold crossing)", xc)
	}

	// A window that excludes the 6 µs traversal → hold.
	p.SlopeTMinNs, p.SlopeTMaxNs, p.SlopeCond = 0, 1000, CondInside
	if xc := QualifySlope(sig, 100, p, true); xc != -1 {
		t.Fatalf("slope traversal should not qualify: %v", xc)
	}
}

// TRLC-LINKS: REQ-DSO-011
func TestQualifySlopeSingleStepEdge(t *testing.T) {
	// A hard square edge that spans lo→hi in ONE sample step (the decimated
	// cal-square case): the traversal exists at index c with time 0 and must
	// be found, not missed.
	sig := make([]uint8, 2048)
	for i := range sig {
		if (i/256)%2 == 0 {
			sig[i] = 40
		} else {
			sig[i] = 210
		}
	}
	p := DefaultTrigParams() // loFrac 0.2, hiFrac 0.8, cond any
	xc := QualifySlope(sig, 400, p, true)
	if xc < 0 {
		t.Fatal("single-step rising edge not detected (regression: k must start at c)")
	}
	// Rising crossings (40→210) are at 256, 768, 1280, 1792; the nearest to
	// centre 1024 is 768 (tie with 1280, first wins). The sub-sample frac is
	// (lvl−40)/(210−40) with lvl≈170.
	if xc < 767 || xc > 769 {
		t.Fatalf("single-step slope anchor %v, want ≈768 (nearest rising crossing)", xc)
	}
}

// TRLC-LINKS: REQ-DSO-011
func TestQualifyVideo(t *testing.T) {
	// Composite-ish: negative sync pulses every 200 samples dipping to 20
	// from a 150 rail; "video" content rides above.
	sig := make([]uint8, 2000)
	for i := range sig {
		sig[i] = 150
	}
	for line := range 10 {
		base := line * 200
		for i := range 15 {
			if base+i < len(sig) {
				sig[base+i] = 20
			}
		}
	}
	p := DefaultTrigParams() // PAL, line 0 (any), negative sync

	// Any line: the sync edge nearest the centre (sample 1000 → line 5 at 1000).
	xc := QualifyVideo(sig, 320, p)
	if xc < 995 || xc > 1005 {
		t.Fatalf("video any-line anchor %v, want ≈1000", xc)
	}

	// Line 3: the 3rd sync EDGE in the record. The record starts inside the
	// first sync pulse (no crossing), so detected edges are at ≈200, 400,
	// 600 — the 3rd is ≈600.
	p.VideoLine = 3
	xc = QualifyVideo(sig, 320, p)
	if xc < 595 || xc > 605 {
		t.Fatalf("video line-3 anchor %v, want ≈600", xc)
	}

	// A plain square (100 us levels at 320 ns samples) has no line syncs.
	sq := make([]uint8, 2000)
	for i := range sq {
		sq[i] = 20
		if (i/312)%2 == 0 {
			sq[i] = 150
		}
	}
	p.VideoLine = 0
	if xc := QualifyVideo(sq, 320, p); xc != -1 {
		t.Fatalf("a square wave triggered VIDEO at %v", xc)
	}
	p.VideoLine = 3

	// Line 100: fewer than 100 sync edges → hold.
	p.VideoLine = 100
	if xc := QualifyVideo(sig, 320, p); xc != -1 {
		t.Fatalf("line-100 with 10 lines: %v, want -1", xc)
	}
}

// TRLC-LINKS: REQ-DSO-012
func TestEresBoxcar(t *testing.T) {
	// A boxcar of 15 shrinks σ ≈ √15; check it smooths an alternating signal
	// to near its mean and preserves the ends without wrap.
	sig := make([]uint8, 200)
	for i := range sig {
		if i%2 == 0 {
			sig[i] = 100
		} else {
			sig[i] = 140
		}
	}
	scratch := make([]uint16, 200)
	EresBoxcar(sig, 15, scratch)
	for i := 20; i < 180; i++ {
		if sig[i] < 115 || sig[i] > 125 {
			t.Fatalf("boxcar[%d] = %d, want ≈120", i, sig[i])
		}
	}
	// Ends: kernel shrinks (no wrap, no fabricated tail) — still near mean.
	if sig[0] < 110 || sig[0] > 130 {
		t.Fatalf("boxcar[0] = %d (shrunk kernel)", sig[0])
	}
}

// TRLC-LINKS: REQ-DSO-012
func TestEresLenForBits(t *testing.T) {
	cases := map[float64]int{0.5: 1, 1.0: 3, 1.5: 7, 2.0: 15, 2.5: 31, 3.0: 63}
	for b, want := range cases {
		if got := EresLenForBits(b); got != want {
			t.Errorf("EresLenForBits(%v) = %d, want %d (even → odd-down)", b, got, want)
		}
	}
}

// TRLC-LINKS: REQ-DSO-012
func TestAverageRing(t *testing.T) {
	r := &AvgRing{}
	r.Reset(4, 100)
	f := &sample.Frame{Traces: []sample.Trace{{Codes8: make([]uint8, 1000), Valid: 1000}, {Ch: 1, Codes8: make([]uint8, 1000), Valid: 1000}}}
	// Frames with an edge at varying positions; alignment centres them all.
	for k := range 4 {
		edge := 480 + k*10
		for i := range f.Codes8(0) {
			if i < edge {
				f.Codes8(0)[i] = 60
			} else {
				f.Codes8(0)[i] = 200
			}
			f.Codes8(1)[i] = f.Codes8(0)[i]
		}
		r.Push(f, float64(edge))
	}
	r.MeanInto(f)
	if f.Len() != 100 || f.TriggerAt != 50 {
		t.Fatalf("mean frame: valid=%d edge=%v, want 100/50", f.Len(), f.TriggerAt)
	}
	// After alignment every ring entry has its edge at column 50: the mean
	// must be a sharp edge (low before, high after), not a smear.
	if f.Codes8(0)[40] != 60 || f.Codes8(0)[60] != 200 {
		t.Fatalf("aligned mean: c1[40]=%d c1[60]=%d, want 60/200", f.Codes8(0)[40], f.Codes8(0)[60])
	}
}

// TRLC-LINKS: REQ-DSO-012
func TestAverageRingNoOffRecordBias(t *testing.T) {
	// Frames whose edge is far off centre contribute off-record columns at
	// one window end; those must NOT be averaged as a fabricated 128 —
	// otherwise the window edge is pulled toward mid-scale.
	r := &AvgRing{}
	r.Reset(4, 200)
	f := &sample.Frame{Traces: []sample.Trace{{Codes8: make([]uint8, 400), Valid: 400}, {Ch: 1, Codes8: make([]uint8, 400), Valid: 400}}}
	for range 4 {
		for i := range f.Codes8(0) {
			f.Codes8(0)[i], f.Codes8(1)[i] = 40, 40 // a flat low rail, no 128 anywhere
		}
		r.Push(f, 350) // edge at 350 → shift 250; left columns fall off-record
	}
	r.MeanInto(f)
	// Every contributing column is 40; no column should read the 128 fill.
	for i := range 200 {
		if f.Codes8(0)[i] != 40 && f.Codes8(0)[i] != 128 {
			t.Fatalf("col %d = %d, expected 40 (real) or 128 (no-contribution)", i, f.Codes8(0)[i])
		}
		// Columns that DID have contributions must be the real 40, not a
		// 40/128 blend (e.g. 84).
		if f.Codes8(0)[i] > 40 && f.Codes8(0)[i] < 128 {
			t.Fatalf("col %d = %d is a fabricated-fill blend", i, f.Codes8(0)[i])
		}
	}
}

// A stopped record re-expressed at a new V/div and offset reads the same
// volts there; codes beyond the screen clip.
// TRLC-LINKS: REQ-DSO-015, REQ-DSO-040
func TestRescaleCodesKeepsVolts(t *testing.T) {
	volts := func(code float64, s ChScale) float64 { return (code-128)*s.Vdiv/25 - s.Off }
	from, to := ChScale{Vdiv: 2, Off: 0}, ChScale{Vdiv: 1, Off: -1}
	c := []uint8{128, 169, 87, 250}
	q := []uint16{128 << 8, 169 << 8, 87 << 8, 250 << 8}
	want := make([]float64, len(c))
	for i, v := range c {
		want[i] = volts(float64(v), from)
	}
	RescaleCodes(c, q, from, to)
	for i := range 3 {
		if got := volts(float64(c[i]), to); math.Abs(got-want[i]) > to.Vdiv/25 {
			t.Fatalf("code %d reads %.3f V, captured %.3f V", i, got, want[i])
		}
		if got := volts(float64(q[i])/256, to); math.Abs(got-want[i]) > 1e-3 {
			t.Fatalf("Q8 %d reads %.4f V, captured %.4f V", i, got, want[i])
		}
	}
	if c[3] != 255 || q[3] != 65535 {
		t.Fatalf("off-screen code %d / %d, want clipped", c[3], q[3])
	}
}

// A hardware trigger stands only where the calibrated record crosses the
// level in the trigger's direction; converter noise on a flat baseline does
// not.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-016
func TestConfirmTriggerEdge(t *testing.T) {
	edge := make([]uint8, 200)
	flat := make([]uint8, 200)
	for i := range edge {
		edge[i], flat[i] = 128, uint8(128+i%2)
		if i >= 100 {
			edge[i] = 144
		}
	}
	if !ConfirmTriggerEdge(edge, nil, 200, 98, 136, true, false) {
		t.Fatal("real rising edge refused")
	}
	if ConfirmTriggerEdge(edge, nil, 200, 98, 136, false, false) {
		t.Fatal("rising edge accepted as falling")
	}
	if ConfirmTriggerEdge(flat, nil, 200, 98, 136, true, false) {
		t.Fatal("flat baseline accepted")
	}
	if ConfirmTriggerEdge(edge, nil, 200, 160, 136, true, false) {
		t.Fatal("edge 60 samples away accepted")
	}
	env := []uint8{128, 129, 128, 129, 128, 144, 143, 144, 143, 144}
	if !ConfirmTriggerEdge(env, nil, len(env), 4, 136, true, true) || ConfirmTriggerEdge(env[:4], nil, 4, 2, 136, true, true) {
		t.Fatal("envelope confirmation")
	}
}

// A step in averaged samples reads as a one-interval ramp: SLOPE > min must
// not fire on it, while a genuinely slow ramp still qualifies.
// TRLC-LINKS: REQ-DSO-011
func TestSlopeStepIsNotSlow(t *testing.T) {
	p := TrigParams{Typ: TrigSlope, SlopeLoFrac: 0.2, SlopeHiFrac: 0.8, SlopeTMaxNs: 1000, SlopeCond: CondGreater} // > max
	step := make([]uint8, 200)
	for i := range step {
		step[i] = 50
		if i > 100 {
			step[i] = 200
		}
	}
	step[100] = 140 // the sample whose interval holds the edge
	if x := QualifySlope(step, 4864, p, true); x >= 0 {
		t.Fatalf("a step qualified as a slope > 1 us at %v", x)
	}
	ramp := make([]uint8, 200)
	for i := range ramp {
		v := 50 + (i-90)*15
		ramp[i] = uint8(min(200, max(50, v)))
	}
	if x := QualifySlope(ramp, 4864, p, true); x < 0 {
		t.Fatal("a 10-sample ramp (~49 us) should qualify as a slope > 1 us")
	}
	p.SlopeCond, p.SlopeTMinNs, p.SlopeTMaxNs = CondLess, 1000, 0 // < min
	if x := QualifySlope(step, 4864, p, true); x < 0 {
		t.Fatal("a step should qualify as a slope < 1 us")
	}
}
