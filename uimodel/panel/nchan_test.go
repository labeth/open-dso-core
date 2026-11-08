package panel

import (
	"math"
	"slices"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// tabIDs are the sheet's tab ids /api/sheets lists.
func tabIDs(c *Controller) []string {
	var out []string
	for _, t := range c.Sheets().Tabs {
		out = append(out, t.ID)
	}
	return out
}

// A four-channel scope has a tab, rows, keys and knobs for every channel,
// and its channel choices (trigger, FFT, math, X-Y, measurements) offer all
// four; each reaches the instrument state.
// TRLC-LINKS: REQ-DSO-212, REQ-DSO-135
func TestFourChannelScopeControlsEveryChannel(t *testing.T) {
	c, m, _ := halRig(t, 4)
	if c.Channels() != 4 {
		t.Fatalf("channels %d", c.Channels())
	}
	tabs := tabIDs(c)
	for _, id := range []string{"ch1", "ch2", "ch3", "ch4"} {
		if !slices.Contains(tabs, id) {
			t.Fatalf("no %s tab: %v", id, tabs)
		}
	}
	if r := c.SheetSet("ch3.vdiv", 0.2, ""); !r.OK {
		t.Fatalf("ch3.vdiv: %+v", r)
	}
	if got := m.State().Channels[2].VoltsDiv; math.Abs(got-0.2) > 1e-9 {
		t.Fatalf("C3 V/div %g", got)
	}
	if r := c.SheetSet("ch4.coupling", "AC", ""); !r.OK {
		t.Fatalf("ch4.coupling: %+v", r)
	}
	if got := m.State().Channels[3].Coupling; got != hal.AC {
		t.Fatalf("C4 coupling %v", got)
	}
	v0 := m.State().Channels[3].VoltsDiv
	c.Input(knob(KnobCh4Vdiv, -1))
	if got := m.State().Channels[3].VoltsDiv; !(got > v0) {
		t.Fatalf("CH4 V/DIV counter-clockwise: %g from %g", got, v0)
	}
	c.Input(knob(KnobCh3Pos, 5))
	if got := m.State().Channels[2].OffsetV; !(got > 0) {
		t.Fatalf("CH3 position up: offset %g", got)
	}
	c.Input(key(KeyCh4))
	if st := c.PanelStatus(); !st.Sheet.Open || st.Sheet.Tab != "ch4" {
		t.Fatalf("the 4 key: sheet %+v", st.Sheet)
	}
	if r := c.SheetSet("ch3.on", "Off", ""); !r.OK || c.TraceOn(2) {
		t.Fatalf("ch3.on Off: %+v, shown %v", r, c.TraceOn(2))
	}
	if d, _ := c.Snapshot(); len(d.ShowCh) != 4 || d.ShowCh[2] || !d.ShowCh[3] {
		t.Fatalf("display ShowCh %v", d.ShowCh)
	}
	if r := c.SheetSet("trig.edge", "C4 Fall", ""); !r.OK {
		t.Fatalf("trig.edge: %+v", r)
	}
	if src := m.State().Trigger.Source; src.Index != 3 || m.State().Trigger.Slope != hal.Falling {
		t.Fatalf("trigger %+v", m.State().Trigger)
	}
	if r := c.SheetSet("view.fft_source", "CH3", ""); !r.OK || c.snap().fftSrc != 2 {
		t.Fatalf("view.fft_source: %+v", r)
	}
	if r := c.SheetSet("cmp.math_b", "CH4", ""); !r.OK || c.snap().mathCh != [2]int{0, 3} {
		t.Fatalf("cmp.math_b: %+v, pair %v", r, c.snap().mathCh)
	}
	if r := c.SheetSet("cmp.math_a", "CH4", ""); !r.OK || c.snap().mathCh != [2]int{3, 0} {
		t.Fatalf("cmp.math_a onto B's channel swaps: %+v, pair %v", r, c.snap().mathCh)
	}
	if r := c.SheetSet("cmp.math", "C4-C1", ""); !r.OK || c.snap().mathMode != 2 {
		t.Fatalf("cmp.math named for its pair: %+v", r)
	}
	if hud := c.HUD(4); hud.MathCh != [2]int{3, 0} || hud.MathMode != 2 || hud.Ch[3].VdivV <= 0 {
		t.Fatalf("HUD math %v %d, C4 %+v", hud.MathCh, hud.MathMode, hud.Ch[3])
	}
	if r := c.SheetSet("view.xy_y", "CH3", ""); !r.OK || c.snap().xyCh != [2]int{0, 2} {
		t.Fatalf("view.xy_y: %+v", r)
	}
	if r := c.SheetSet("meas.pkpk", "C3", ""); !r.OK {
		t.Fatalf("meas.pkpk C3: %+v", r)
	}
	if d, _ := c.Snapshot(); !slices.Contains(d.MeasItems, "3:pkpk") {
		t.Fatalf("readouts %v", d.MeasItems)
	}
}

// A two-channel scope has neither the channels past two nor the rows that
// choose between more than two: its sheets, keys and choices are the
// two-channel model's.
// TRLC-LINKS: REQ-DSO-212, REQ-DSO-135
func TestTwoChannelScopeHasNoMoreChannels(t *testing.T) {
	c, _, _ := halRig(t, 2)
	if tabs := tabIDs(c); slices.Contains(tabs, "ch3") || slices.Contains(tabs, "ch4") || len(tabs) != numTabs-2 {
		t.Fatalf("tabs %v", tabs)
	}
	for _, id := range []string{"ch3.vdiv", "cmp.math_a", "view.xy_x"} {
		if r := c.SheetSet(id, 1, ""); r.OK {
			t.Fatalf("%s set on two channels", id)
		}
	}
	if c.InjectButton(KeyCh3) || c.InjectKnob(KnobCh4Vdiv, 1, 1) {
		t.Fatal("a third channel's key or knob taken")
	}
	for _, tb := range c.Sheets().Tabs {
		for _, r := range tb.Rows {
			if r.ID == "trig.edge" && len(r.Options) != 4 {
				t.Fatalf("trig.edge options %v", r.Options)
			}
			if r.ID == "meas.pkpk" && len(r.Options) != 4 {
				t.Fatalf("meas.pkpk options %v", r.Options)
			}
		}
	}
	if d, _ := c.Snapshot(); d.ShowCh != nil {
		t.Fatalf("ShowCh on two channels: %v", d.ShowCh)
	}
}

// frame12 is a four-channel frame of 12-bit codes left-aligned in 16-bit
// words at cpd8 codes a division on the 8-bit view: a square wave of amp
// divisions either side of the centre, period samples long, on channel sq,
// the others flat at the centre.
func frame12(n, period, sq int, amp, cpd8, sampleS float64) *sample.Frame {
	fm := sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Max: 0xFFF0, Mid: 0x8000}
	f := &sample.Frame{
		Content: sample.RawFullRate, SampleS: sampleS, CaptureS: sampleS, AcqDecim: 1, Depth: n, WinPoints: n,
		TriggerAt: float64(n / 2), TrigKind: sample.HardwareNode, Seq: 1, ContentID: 1,
		Status: sample.Status{Triggered: true, Done: true, Coherent: true},
	}
	f.Snapshot.TdivS = sampleS * float64(n) / 10
	step := amp * cpd8 * fm.Unit8() // words for amp divisions
	for ch := range 4 {
		t := sample.Trace{Ch: ch, Format: fm, Codes16: make([]uint16, n), Valid: n, CodesPerDiv: cpd8 * fm.Unit8()}
		for i := range t.Codes16 {
			w := float64(fm.Mid)
			if ch == sq {
				if (i/(period/2))%2 == 0 {
					w += step
				} else {
					w -= step
				}
			}
			t.Codes16[i] = uint16(w)
		}
		f.Traces = append(f.Traces, t)
	}
	return f
}

// The panes read a 12-bit frame on the 8-bit view at its own codes a
// division: MEASURE's values and the spectrum are those of the signal put
// in, on a channel past C2.
// TRLC-LINKS: REQ-DSO-216, REQ-DSO-217, REQ-DSO-1003
func TestAnalysisReadsTwelveBitFrames(t *testing.T) {
	c, m, _ := halRig(t, 4)
	if r := c.SheetSet("ch3.vdiv", 0.5, ""); !r.OK {
		t.Fatalf("ch3.vdiv: %+v", r)
	}
	vdiv := m.State().Channels[2].VoltsDiv
	const n, period = 4000, 100
	f := frame12(n, period, 2, 2, 30, 1e-6) // ±2 div: 4 div peak to peak
	c.mu.Lock()
	c.ui.ws, c.ui.measAll = wsview.Measure, true
	c.mu.Unlock()
	c.OnFrame(f)
	c.amu.Lock()
	vals, ok := c.ana.allVals[2], c.ana.allOK[2]
	c.amu.Unlock()
	if !ok {
		t.Fatal("no C3 values from a 12-bit frame")
	}
	if got, want := vals[measIndex("pkpk")], 4*vdiv; math.Abs(got-want) > 0.02*want {
		t.Fatalf("C3 Vpp %g, want %g", got, want)
	}
	if got, want := vals[measIndex("freq")], 1/(period*1e-6); math.Abs(got-want) > 0.01*want {
		t.Fatalf("C3 frequency %g, want %g", got, want)
	}
	if v := c.voltsAt(2); math.Abs(v-vdiv/30) > 1e-12 {
		t.Fatalf("C3 volts a code on the 8-bit view %g, want %g", v, vdiv/30)
	}

	if r := c.SheetSet("view.fft_source", "CH3", ""); !r.OK {
		t.Fatalf("view.fft_source: %+v", r)
	}
	f.Seq, f.ContentID = 2, 2
	c.mu.Lock()
	c.ui.ws, c.viewMode = wsview.Spectrum, viewFFT
	c.mu.Unlock()
	c.OnFrame(f)
	c.amu.Lock()
	specOK, peaks := c.ana.specOK, append([]Peak(nil), c.ana.peaks...)
	c.amu.Unlock()
	if !specOK || len(peaks) == 0 {
		t.Fatalf("no spectrum of C3: ok %v peaks %v", specOK, peaks)
	}
	top := peaks[0]
	for _, p := range peaks {
		if p.DB > top.DB {
			top = p
		}
	}
	if want := 1 / (period * 1e-6); math.Abs(top.Freq-want) > 0.05*want {
		t.Fatalf("largest peak %g Hz, want %g", top.Freq, want)
	}
}

// voltsAt is channel ch's volts a code at the probe tip as the views take
// it now.
func (c *Controller) voltsAt(ch int) float64 { return voltsPerCode(c.snap(), ch) }

// Every row a four-channel scope has beyond a two-channel one is set through
// /api/sheets and reads back what was set.
// TRLC-LINKS: REQ-DSO-212
func TestFourChannelRowsReachableThroughSheetsAPI(t *testing.T) {
	c, _, _ := halRig(t, 4)
	want := map[string]bool{}
	for _, tb := range sheetTabs {
		for _, rw := range tb.rows {
			if !rowShown(rw.id, rowShape{nch: 2}) && rowShown(rw.id, rowShape{nch: 4}) && rw.kind != wsview.Action {
				want[rw.id] = true
			}
		}
	}
	if len(want) < 18 {
		t.Fatalf("only %d rows past two channels", len(want))
	}
	for _, tab := range c.Sheets().Tabs {
		for _, m := range tab.Rows {
			if want[m.ID] && !m.Available { // a part this scope lacks (the display invert)
				delete(want, m.ID)
			}
			if !want[m.ID] || !settable(m) {
				continue
			}
			v, ok := otherValue(m)
			if !ok {
				continue
			}
			if res := c.SheetSet(m.ID, v, ""); !res.OK {
				t.Errorf("%s: set %v: %s", m.ID, v, res.Error)
				continue
			}
			if got := rowModel(c.snap(), rowByID[m.ID]); !sameValue(got.Value, v, tolOf(m)) {
				t.Errorf("%s: set %v, reads %v", m.ID, v, got.Value)
				continue
			}
			delete(want, m.ID)
		}
	}
	for id := range want {
		t.Errorf("%s not reached", id)
	}
}

// A channel switched on or off anywhere (a sheet, a key, a web page writing
// the instrument state) is the one the UI model shows.
// TRLC-LINKS: REQ-DSO-212, REQ-DSO-135
func TestChannelShownFollowsTheInstrumentState(t *testing.T) {
	c, m, _ := halRig(t, 4)
	if r := c.SheetSet("ch3.on", "Off", ""); !r.OK || m.State().Channels[2].On {
		t.Fatalf("ch3.on Off: %+v, state on %v", r, m.State().Channels[2].On)
	}
	if err := m.Change(func(st *instrument.State) error { st.Channels[3].On = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if c.TraceOn(3) || c.snap().chDisp[3] {
		t.Fatal("C4 switched off in the state still shown")
	}
	c.Input(key(KeyCh4))
	if !m.State().Channels[3].On {
		t.Fatal("the 4 key did not switch C4 on in the state")
	}
}

// /api/sheets carries a MEAS row's live values as the LCD shows them beside
// the row, on every channel measured.
// TRLC-LINKS: REQ-DSO-217
func TestSheetsCarryLiveMeasurements(t *testing.T) {
	c, _, _ := halRig(t, 4)
	live := func() []string {
		for _, tb := range c.Sheets().Tabs {
			for _, r := range tb.Rows {
				if r.ID == "meas.freq" {
					return r.Live
				}
			}
		}
		return nil
	}
	if l := live(); l != nil {
		t.Fatalf("live values before any frame: %v", l)
	}
	c.mu.Lock()
	c.ui.ws, c.ui.measAll = wsview.Measure, true
	c.mu.Unlock()
	c.OnFrame(frame12(4000, 100, 3, 2, 30, 1e-6))
	if l := live(); len(l) != 4 || l[3] != "10kHz" {
		t.Fatalf("live frequencies %q", l)
	}
}

// A frame of the channels switched on (C2 off: traces C1, C3, C4) is read
// by channel: C3's values are C3's signal.
// TRLC-LINKS: REQ-DSO-216, REQ-DSO-1003
func TestAnalysisReadsChannelsByNumber(t *testing.T) {
	c, m, _ := halRig(t, 4)
	f := frame12(4000, 100, 2, 2, 30, 1e-6)
	f.Traces = append(f.Traces[:1], f.Traces[2:]...)
	c.mu.Lock()
	c.ui.ws, c.ui.measAll = wsview.Measure, true
	c.mu.Unlock()
	c.OnFrame(f)
	c.amu.Lock()
	vals, ok := c.ana.allVals[2], c.ana.allOK[2]
	c.amu.Unlock()
	if want := 4 * m.State().Channels[2].VoltsDiv; !ok || math.Abs(vals[measIndex("pkpk")]-want) > 0.02*want {
		t.Fatalf("C3 read as %v (ok %v), want Vpp %g", vals, ok, want)
	}
}
