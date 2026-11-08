// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"fmt"
	"image"
	"slices"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// The layout is the profile's facts: a touch screen and no F-keys.
// TRLC-LINKS: REQ-DSO-299
func TestTouchFirstIsTheProfilesChoice(t *testing.T) {
	p := hal.Profile{Display: hal.DisplaySpec{Touch: true}, Inputs: hal.InputSpec{Keys: []string{"run", "menu", "ch1"}}}
	if !TouchFirst(p) {
		t.Error("a touch screen without F-keys is not laid out for touch")
	}
	p.Inputs.Keys = append(p.Inputs.Keys, "F3")
	if TouchFirst(p) {
		t.Error("a panel with F-keys is laid out for touch")
	}
	p.Inputs.Keys, p.Display.Touch = []string{"run"}, false
	if TouchFirst(p) {
		t.Error("a scope without a touch screen is laid out for touch")
	}
}

// touchRig is a controller laid out for touch on the 1024×600 screen.
type touchRig struct {
	t   *testing.T
	c   *Controller
	eng *fakeEng
	fe  *fakeFE
	m   *render.Metrics
}

// newTouch is a touch rig.
func newTouch(t *testing.T) *touchRig {
	t.Helper()
	c, eng, fe := newC(t)
	m := &render.Metrics1024x600
	c.SetTouchLayout(m)
	return &touchRig{t: t, c: c, eng: eng, fe: fe, m: m}
}

// ev is one touch report.
func (r *touchRig) ev(id, x, y int, down bool, g string) {
	r.c.Input(hal.InputEvent{Source: hal.FromTouch, Kind: hal.TouchEvent, Contact: id, TouchX: x, TouchY: y, Down: down, Gesture: g})
}

// target is the target whose id starts with prefix, as the screen draws it.
func (r *touchRig) target(prefix string) (render.TouchTarget, bool) {
	ts := render.TouchTargets(r.m, r.c.HUD(r.c.nch))
	for _, tg := range slices.Backward(ts) {
		if strings.HasPrefix(tg.ID, prefix) {
			return tg, true
		}
	}
	return render.TouchTarget{}, false
}

// tap taps the middle of the target whose id starts with prefix.
func (r *touchRig) tap(prefix string) {
	r.t.Helper()
	tg, ok := r.target(prefix)
	if !ok {
		r.t.Fatalf("no target %q on the screen", prefix)
	}
	p := tg.R.Min.Add(tg.R.Max).Div(2)
	r.ev(0, p.X, p.Y, true, "")
	r.ev(0, p.X, p.Y, false, "")
}

// drag moves contact id from a to b in steps.
func (r *touchRig) drag(id int, a, b image.Point, steps int, down, up bool) {
	if down {
		r.ev(id, a.X, a.Y, true, "")
	}
	for i := 1; i <= steps; i++ {
		p := a.Add(b.Sub(a).Mul(i).Div(steps))
		r.ev(id, p.X, p.Y, true, hal.TouchMove)
	}
	if up {
		r.ev(id, b.X, b.Y, false, "")
	}
}

// settings is what the gestures must not change: every engine and front-end
// call, the timebase and the channels' scales.
func (r *touchRig) settings() string {
	r.fe.mu.Lock()
	defer r.fe.mu.Unlock()
	var b strings.Builder
	for _, k := range r.eng.calls {
		b.WriteString(k.what + ";")
	}
	for _, k := range r.fe.calls {
		b.WriteString(k.what + ";")
	}
	st := r.eng.Snapshot()
	fmt.Fprint(&b, st.TdivS, st.TrigPosFrac, st.TrigSource, st.Norm, st.Running)
	return b.String()
}

// Every workspace, view and sheet is reached by touch, and a sheet row's
// controls set it.
// TRLC-LINKS: REQ-DSO-299
func TestTouchReachesWorkspacesViewsAndSheets(t *testing.T) {
	r := newTouch(t)
	v := r.c.view(r.c.snap())
	if !v.Layout.Touch || v.Rail || v.Bar == nil || v.Layout.MenuW != 0 {
		t.Fatalf("the touch view: touch %v rail %v bar %v menuW %d", v.Layout.Touch, v.Rail, v.Bar != nil, v.Layout.MenuW)
	}
	for i, name := range wsview.Names {
		r.tap("ws:" + string(rune('0'+i)))
		if s := r.c.snap(); s.ui.ws != wsview.WS(i) {
			t.Fatalf("the %s tab: workspace %d", name, s.ui.ws)
		}
		for k := range wsViewLabels[i] {
			r.tap("view:" + string(rune('0'+k)))
			if got := wsViewIndex(r.c.snap()); got != k {
				t.Fatalf("%s view %d (%s): shows %d", name, k, wsViewLabels[i][k], got)
			}
		}
	}
	r.tap("ws:0")
	for _, chip := range []struct{ id, tab string }{{"open:trig", "trig"}, {"open:time", "time"}, {"open:ch2", "ch2"}} {
		r.tap(chip.id)
		s := r.c.snap()
		if !s.ui.open || sheetTabs[s.ui.tab].id != chip.tab {
			t.Fatalf("%s: sheet open %v on %s", chip.id, s.ui.open, sheetTabs[s.ui.tab].id)
		}
		r.tap("sheet:close")
		if r.c.snap().ui.open {
			t.Fatalf("Close left the %s sheet open", chip.tab)
		}
	}
	// a tap beside the panel closes it
	r.tap("open:trig")
	r.ev(0, 100, 300, true, "")
	r.ev(0, 100, 300, false, "")
	if r.c.snap().ui.open {
		t.Fatal("a tap beside the sheet left it open")
	}
	// an option of a row sets it; a stepper steps a number row
	r.tap("open:trig")
	before := r.settings()
	r.tap("opt:2:1") // Mode: Normal
	if !r.eng.Snapshot().Norm {
		t.Fatalf("the trigger mode's Normal button: calls %v", r.eng.calls)
	}
	r.tap("inc:")
	if r.settings() == before {
		t.Fatal("a number row's + changed nothing")
	}
	// a long option list opens the chooser; a choice sets it and closes it
	r.tap("tab:")
	for i := range 20 {
		if tg, ok := r.target("more:"); ok {
			p := tg.R.Min.Add(tg.R.Max).Div(2)
			r.ev(0, p.X, p.Y, true, "")
			r.ev(0, p.X, p.Y, false, "")
			break
		}
		r.c.sheetTurnTab(1)
		if i == 19 {
			t.Fatal("no row offers a chooser")
		}
	}
	if r.c.snap().ui.tChoose == "" {
		t.Fatal("the chooser did not open")
	}
	r.tap("choose:0")
	if r.c.snap().ui.tChoose != "" {
		t.Fatal("a choice left the chooser open")
	}
}

// The gestures on the waveform zoom and pan the view of the record and
// change no setting: a box zooms into it, a pinch zooms, two fingers pan,
// a double tap resets.
// TRLC-LINKS: REQ-DSO-299
func TestTouchGesturesChangeOnlyTheView(t *testing.T) {
	r := newTouch(t)
	plot := render.TouchPlot(r.m)
	mid := plot.Min.Add(plot.Max).Div(2)
	before := r.settings()

	r.drag(0, image.Pt(400, mid.Y-50), image.Pt(500, mid.Y+50), 5, true, false)
	if v := r.c.view(r.c.snap()); v.Box == nil {
		t.Fatal("no selection box while dragging")
	}
	r.ev(0, 500, mid.Y+50, false, "")
	s := r.c.snap()
	if s.zoom < 5 || r.c.view(s).Box != nil {
		t.Fatalf("a box a tenth of the screen wide: zoom %d", s.zoom)
	}
	if off := s.zoomOff; off >= 0 { // the box is left of the trigger
		t.Fatalf("the zoom is not on the box: pan %v", off)
	}

	r.ev(0, 0, 0, false, "")
	r.ev(0, 100, mid.Y, true, "") // a double tap: the whole record
	r.ev(0, 100, mid.Y, false, "")
	r.ev(0, 100, mid.Y, true, "")
	r.ev(0, 100, mid.Y, false, "")
	if z := r.c.snap().zoom; z > 1 {
		t.Fatalf("a double tap left zoom %d", z)
	}

	// a pinch out: two fingers spreading apart across the screen
	r.ev(0, mid.X-40, mid.Y, true, "")
	r.ev(1, mid.X+40, mid.Y, true, "")
	for i := 1; i <= 8; i++ {
		r.ev(0, mid.X-40-i*40, mid.Y, true, hal.TouchMove)
		r.ev(1, mid.X+40+i*40, mid.Y, true, hal.TouchMove)
	}
	z := r.c.snap().zoom
	if z < 5 {
		t.Fatalf("a pinch out by 9x: zoom %d", z)
	}
	r.ev(0, 0, 0, false, "")
	r.ev(1, 0, 0, false, "")

	// two fingers moving together pan the zoomed view
	off0 := r.c.snap().zoomOff
	r.ev(0, mid.X-20, mid.Y, true, "")
	r.ev(1, mid.X+20, mid.Y, true, "")
	for i := 1; i <= 5; i++ {
		r.ev(0, mid.X-20+i*30, mid.Y, true, hal.TouchMove)
		r.ev(1, mid.X+20+i*30, mid.Y, true, hal.TouchMove)
	}
	r.ev(0, 0, 0, false, "")
	r.ev(1, 0, 0, false, "")
	if s := r.c.snap(); s.zoom != z || !(s.zoomOff < off0) {
		t.Fatalf("a pan right: zoom %d (was %d), pan %v (was %v)", s.zoom, z, s.zoomOff, off0)
	}

	// a pinch in past 1x ends the zoom
	r.ev(0, mid.X-300, mid.Y, true, "")
	r.ev(1, mid.X+300, mid.Y, true, "")
	for i := 1; i <= 9; i++ {
		r.ev(0, mid.X-300+i*30, mid.Y, true, hal.TouchMove)
		r.ev(1, mid.X+300-i*30, mid.Y, true, hal.TouchMove)
	}
	r.ev(0, 0, 0, false, "")
	r.ev(1, 0, 0, false, "")
	if z := r.c.snap().zoom; z != 1 {
		t.Fatalf("a pinch in to a tenth: zoom %d", z)
	}

	// a vertical pinch and pan say the view has no vertical zoom
	r.ev(0, mid.X, mid.Y-30, true, "")
	r.ev(1, mid.X, mid.Y+30, true, "")
	for i := 1; i <= 5; i++ {
		r.ev(0, mid.X, mid.Y-30-i*20, true, hal.TouchMove)
		r.ev(1, mid.X, mid.Y+30+i*20, true, hal.TouchMove)
	}
	r.ev(0, 0, 0, false, "")
	r.ev(1, 0, 0, false, "")

	if after := r.settings(); after != before {
		t.Fatalf("the gestures changed settings:\n%s\n%s", before, after)
	}
}

// A one-finger drag on the waveform beside the cursor tab places the
// nearer cursor; a tap there closes the sheet.
// TRLC-LINKS: REQ-DSO-299
func TestTouchPlacesCursors(t *testing.T) {
	r := newTouch(t)
	r.c.mu.Lock()
	r.c.curOn, r.c.curType = true, 0
	r.c.openSheetLocked(tabIndex("curs"), "")
	r.c.mu.Unlock()
	y := render.TouchPlot(r.m).Min.Y + 200
	r.drag(0, image.Pt(40, y), image.Pt(120, y), 4, true, true)
	s := r.c.snap()
	if !s.ui.open {
		t.Fatal("placing a cursor closed the sheet")
	}
	if got := s.curX[s.curSel]; got < 0.1 || got > 0.13 {
		t.Fatalf("cursor %d at %v, the finger at %v", s.curSel, got, 120.0/1024)
	}
	r.ev(0, 100, y, true, "")
	r.ev(0, 100, y, false, "")
	if r.c.snap().ui.open {
		t.Fatal("a tap beside the cursor tab left it open")
	}
}

// Every choice an F-key made is a target a finger can tap: the byte
// picker's, the match editor's and the knobs' jobs.
// TRLC-LINKS: REQ-DSO-299
func TestTouchKeysAreButtons(t *testing.T) {
	r := newTouch(t)
	r.tap("act:tools")
	ids := func() []string {
		var out []string
		for _, tg := range render.TouchTargets(r.m, r.c.HUD(r.c.nch)) {
			out = append(out, tg.ID)
		}
		return out
	}
	for _, id := range []string{"tool:adj-", "tool:adj+", "tool:adjpush", "tool:pos-", "tool:pos+", "tool:pospush", "tool:zoom", "tool:done"} {
		if !slices.Contains(ids(), id) {
			t.Fatalf("TOOLS: no %s in %v", id, ids())
		}
	}
	r.tap("tool:zoom")
	if r.c.snap().zoom <= 1 {
		t.Fatal("TOOLS Zoom did not zoom")
	}
	r.tap("tool:done")
	if r.c.snap().ui.tTools {
		t.Fatal("Done left the tools up")
	}
	r.c.mu.Lock()
	r.c.openSheetLocked(tabIndex("bus"), "")
	r.c.mu.Unlock()
	r.c.openMatchSub()
	for _, id := range []string{"key:0", "key:2", "key:3", "key:4", "mkey:0:0", "mcur:-1", "mcur:1", "mclear"} {
		if !slices.Contains(ids(), id) {
			t.Fatalf("the match editor: no %s in %v", id, ids())
		}
	}
	r.tap("mkey:0:1")
	if toks := r.c.MatchEditor().Toks; len(toks) != 1 {
		t.Fatalf("a key of the match editor typed %v", toks)
	}
	r.tap("key:4") // Cancel
	if r.c.MatchEditor().Open {
		t.Fatal("Cancel left the match editor open")
	}
}

// Taps that overlap (the next finger down before the last one lifted) each
// press what they hit.
// TRLC-LINKS: REQ-DSO-299
func TestTouchOverlappingTaps(t *testing.T) {
	r := newTouch(t)
	at := func(prefix string) image.Point {
		tg, ok := r.target(prefix)
		if !ok {
			t.Fatalf("no %s", prefix)
		}
		return tg.R.Min.Add(tg.R.Max).Div(2)
	}
	a, b := at("ws:1"), at("ws:2")
	r.ev(0, a.X, a.Y, true, "")
	r.ev(1, b.X, b.Y, true, "")
	r.ev(0, a.X, a.Y, false, "")
	r.ev(1, b.X, b.Y, false, "")
	if ws := r.c.snap().ui.ws; ws != wsview.Spectrum {
		t.Fatalf("BUS then SPECTRUM tapped overlapping: workspace %d", ws)
	}
}
