package panel

import (
	"math"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/decode/synth"
	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/settings"
	"github.com/labeth/open-dso-core/superres"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// withoutC1 is f with C1's trace dropped, as a scope with C1 switched off
// publishes it.
func withoutC1(f *sample.Frame) *sample.Frame {
	f.Traces = f.Traces[1:]
	return f
}

// A scope with C1 switched off still analyses its other channels: the
// record's length comes from the traces the frame carries.
// TRLC-LINKS: REQ-DSO-216, REQ-DSO-1003
func TestC1OffStillMeasures(t *testing.T) {
	c, m, _ := halRig(t, 4)
	f := sample.View8Frame(withoutC1(frame12(4000, 100, 3, 2, 30, 1e-6)))
	if f.Codes8Len() != 4000 || framecalc.Valid(f) != 4000 {
		t.Fatalf("record length %d, valid %d", f.Codes8Len(), framecalc.Valid(f))
	}
	c.mu.Lock()
	c.ui.ws, c.ui.measAll = wsview.Measure, true
	c.mu.Unlock()
	c.OnFrame(f)
	c.amu.Lock()
	vals, ok, ok1 := c.ana.allVals[3], c.ana.allOK[3], c.ana.allOK[0]
	c.amu.Unlock()
	if want := 4 * m.State().Channels[3].VoltsDiv; !ok || math.Abs(vals[measIndex("pkpk")]-want) > 0.02*want {
		t.Fatalf("C4 with C1 off: %v (ok %v), want Vpp %g", vals, ok, want)
	}
	if ok1 {
		t.Fatal("C1 switched off was measured")
	}
	c.captureRefFrom(f, 0)
	if r := c.RefView()[0]; r.Ch[0] != nil || len(r.Ch[3]) != RefCols {
		t.Fatalf("REF A with C1 off: C1 %d columns, C4 %d", len(r.Ch[0]), len(r.Ch[3]))
	}
}

// The bus search reads the two busiest lines: a bus on C3 and C4 beside
// idle C1 and C2; a two-channel scope always reads C1 and C2.
// TRLC-LINKS: REQ-DSO-214
func TestBusPairFollowsTheBusyLines(t *testing.T) {
	c, _, _ := halRig(t, 4)
	f := sample.View8Frame(frame12(4000, 100, 2, 2, 30, 1e-6))
	g := sample.View8Frame(frame12(4000, 60, 3, 2, 30, 1e-6))
	f.Traces[3] = g.Traces[3]
	if p := c.busPair(f); p != [2]int{2, 3} {
		t.Fatalf("pair %v, want C3 and C4", p)
	}
	c2, _, _ := halRig(t, 2)
	if p := c2.busPair(f); p != [2]int{0, 1} {
		t.Fatalf("two channels: pair %v", p)
	}
	res := framecalc.DetectPair(f, "hex", [2]int{2, 3})
	if res.Roles.ChA < 2 && res.Roles.ChB < 2 && res.OK && res.Proto != "off" {
		t.Fatalf("roles on the pair's lanes, not the scope's channels: %+v", res.Roles)
	}
}

// The stack's two lanes carry any channel: C1 and C2 as before, C1 and the
// chosen channel past C2.
// TRLC-LINKS: REQ-DSO-140
func TestSuperresLanesAnyChannel(t *testing.T) {
	for _, tc := range []struct {
		ch    int
		lanes [2]int
		align int
		other int
	}{{0, [2]int{0, 1}, 0, 1}, {1, [2]int{0, 1}, 1, 0}, {2, [2]int{0, 2}, 1, 0}, {3, [2]int{0, 3}, 1, 0}} {
		l, a := superres.Lanes(tc.ch)
		if l != tc.lanes || a != tc.align || superres.Other(tc.ch) != tc.other {
			t.Fatalf("C%d: lanes %v align %d other %d", tc.ch+1, l, a, superres.Other(tc.ch))
		}
	}
	f := sample.View8Frame(withoutC1(frame12(64, 16, 3, 2, 30, 1e-6)))
	c1, c2 := srFrameCopy(f, 64, [2]int{0, 3}, nil, nil)
	if len(c1) != 64 || &c1[0] != &c2[0] {
		t.Fatal("C1 off: the C4 lane did not stand for both")
	}
}

// References survive a restart in their own store: every channel's columns
// and what they were stored at.
// TRLC-LINKS: REQ-DSO-138
func TestReferencesRoundTripTheirStore(t *testing.T) {
	c, _, _ := halRig(t, 4)
	c.captureRefFrom(sample.View8Frame(frame12(4000, 100, 2, 2, 30, 1e-6)), 1)
	kv := &memKV{}
	if err := settings.SaveRefs(kv, c.RefsState()); err != nil {
		t.Fatal(err)
	}
	r, ok := settings.LoadRefs(kv, t.Logf)
	if !ok {
		t.Fatal("no references read back")
	}
	d, _, _ := halRig(t, 4)
	g0 := d.RefsGen()
	d.ApplyRefs(r)
	if d.RefsGen() == g0 {
		t.Fatal("restoring did not move RefsGen")
	}
	a, b := c.RefView()[1], d.RefView()[1]
	if !b.Show || len(b.Ch[2]) != RefCols || string(a.Ch[2]) != string(b.Ch[2]) || a.Vdiv != b.Vdiv {
		t.Fatalf("REF B back: %+v", b)
	}
	if d.RefsExport()[1].Setup == nil {
		t.Fatal("the stored setup did not come back")
	}
}

// The mask's tolerance and autoset's clip window are fractions of a
// division: the 8-bit converter's numbers at 25 codes, scaled at 30.
// TRLC-LINKS: REQ-DSO-139, REQ-DSO-225
func TestToleranceAndClipInDivisions(t *testing.T) {
	if w := clipWindowAt(25); w.lo != 26 || w.hi != 230 {
		t.Fatalf("clip window at 25: %+v", w)
	}
	if w := clipWindowAt(30); w.lo != 128-122 || w.hi != 128+122 {
		t.Fatalf("clip window at 30: %+v", w)
	}
	for _, tol := range maskTols {
		if got := maskTolCodes(tol[1], 25); got != tol[1] {
			t.Fatalf("tolerance %d at 25: %d", tol[1], got)
		}
	}
	if got := maskTolCodes(20, 30); got != 24 {
		t.Fatalf("tolerance 20 at 30: %d", got)
	}
}

// A MEAS row's live values in /api/sheets move SheetsSeq (at most every
// liveEvery), so a remote rereading on it sees them.
// TRLC-LINKS: REQ-DSO-217, REQ-DSO-211
func TestLiveValuesMoveSheetsSeq(t *testing.T) {
	c, _, _ := halRig(t, 4)
	now := time.Unix(1000, 0)
	c.SetClock(func() time.Time { return now })
	c.mu.Lock()
	c.ui.ws, c.ui.measAll = wsview.Measure, true
	c.mu.Unlock()
	c.OnFrame(frame12(4000, 100, 3, 2, 30, 1e-6))
	s0 := c.Sheets().SheetsSeq
	now = now.Add(time.Second)
	f := frame12(4000, 50, 3, 2, 30, 1e-6)
	f.Seq, f.ContentID = 2, 2
	c.OnFrame(f)
	c.publish()
	if s1 := c.PanelStatus().SheetsSeq; s1 == s0 {
		t.Fatalf("SheetsSeq stayed %d while C4's frequency moved", s0)
	}
	s1 := c.PanelStatus().SheetsSeq
	now = now.Add(100 * time.Millisecond)
	f = frame12(4000, 100, 3, 2, 30, 1e-6)
	f.Seq, f.ContentID = 3, 3
	c.OnFrame(f)
	c.publish()
	if s2 := c.PanelStatus().SheetsSeq; s2 != s1 {
		t.Fatalf("SheetsSeq moved within liveEvery: %d -> %d", s1, s2)
	}
}

// A bus busy a few percent of the time on C3 and C4 (a caliper's frames,
// 2 ms in 30, its data line at its other level for under 2 % of the
// samples) is the pair the search reads, not one of its lines beside an
// idle C1.
// TRLC-LINKS: REQ-DSO-214
func TestBusPairFindsASparseBus(t *testing.T) {
	c, _, _ := halRig(t, 4)
	b, err := synth.Make(synth.Spec{Proto: "caliper"}, 1e5)
	if err != nil {
		t.Fatal(err)
	}
	clk, data := b.Lines[0].Codes(), b.Lines[1].Codes()
	n := len(clk)
	f := sample.View8Frame(frame12(n, 100, 2, 2, 30, 1e-5))
	idle := make([]uint8, n)
	for i := range idle {
		idle[i] = 128
	}
	for ch, codes := range [][]uint8{idle, idle, clk, data} {
		f.Traces[ch].Codes8, f.Traces[ch].Valid = codes, n
	}
	if p := c.busPair(f); p != [2]int{2, 3} {
		t.Fatalf("pair %v, want C3 and C4", p)
	}
}
