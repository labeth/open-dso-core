// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render

import (
	"image"
	"slices"
	"strconv"
	"sync"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// keysColumnW is the softkey column's width: an open menu's choices, and
// under them the workspace rail.
const keysColumnW = 116

// TouchKey is the key a touch at (x, y) on screen m presses: F1..F5 in the
// softkey column on the right (a menu's choices or the workspace rail's),
// the slot whose row is nearest; "" elsewhere. A touch screen gives a
// panel without softkeys the same keys the display draws.
// TRLC-LINKS: REQ-DSO-212
func TouchKey(m *Metrics, x, y int) string {
	if x < m.W-keysColumnW || x >= m.W || y < 0 || y >= m.H {
		return ""
	}
	best, bd := 0, -1
	for i, ky := range m.SoftkeyY {
		d := y - ky
		if d < 0 {
			d = -d
		}
		if bd < 0 || d < bd {
			best, bd = i, d
		}
	}
	return [...]string{"f1", "f2", "f3", "f4", "f5"}[best]
}

// ---- the touch-first layout (ADR-TOUCH-FIRST-LAYOUT) ----
//
// A scope with a touch screen and no F-keys lays the same UI model out for
// a finger: a readout bar on top whose chips open their sheets (a channel's,
// the timebase's, the trigger's), the plot over the full width between
// the bars, and a touch bar at the bottom with the workspaces, the
// workspace's views as a segmented control and the actions the panel's
// keys also run. A sheet is a panel of touch-sized rows on the right, the
// scope still showing left of it; a tap there closes it. Every drawn
// target is also a TouchTarget: the hit test runs the same drawing on a
// canvas that keeps no pixels, so what a finger hits is what is drawn.

// The touch layout's geometry.
const (
	TouchTopH   = 40  // the readout bar
	TouchBarH   = 56  // the touch bar
	touchChipW  = 104 // a channel's chip
	touchWsW    = 80  // a workspace's tab
	touchActW   = 64  // an action's button
	touchSheetW = 760 // the sheet panel
	touchTabW   = 120 // its tab column
	touchHeadH  = 44  // its header (title, paging, Close)
	touchRowH   = 48  // a row: a 36 px control in it
	touchHdrH   = 24  // a section header row
	touchCtlH   = 36  // a control's height in a row
	touchFootH  = 52  // the sheet's foot: a sub-mode's choices
	touchLabelW = 190 // a row's label column
)

// TouchTarget is one place a finger can hit on the touch-first layout and
// what it is: "ws:1" a workspace, "view:2" a view, "act:run" an action,
// "key:0" a menu's choice, "open:ch1" a readout's sheet, "plot" the
// waveform, and in a sheet "tab:3", "row:4", "opt:4:1", "more:4",
// "dec:4", "inc:4", "slide:4", "val:4", "fk:4:0", "choose:2",
// "page:1", "sheet:close", "sheet:panel", "sheet:outside"; "mkey:1:2" a key
// of the match editor; "zoom:reset" the zoom's reset.
type TouchTarget struct {
	R  image.Rectangle
	ID string
}

// TouchTargets are the touch-first layout's targets on screen m under hud,
// bottom first: the last one containing a point is what a finger there
// hits (TouchAt).
// TRLC-LINKS: REQ-DSO-299
func TouchTargets(m *Metrics, hud HUD) []TouchTarget {
	out := []TouchTarget{}
	td := touchDraw{sf: nullCanvas{m: m}, out: &out}
	td.chrome(nil, hud, false)
	return out
}

// TouchAt is the target at (x, y): the topmost of ts containing it.
// TRLC-LINKS: REQ-DSO-299
func TouchAt(ts []TouchTarget, x, y int) (TouchTarget, bool) {
	p := image.Pt(x, y)
	for _, t := range slices.Backward(ts) {
		if p.In(t.R) {
			return t, true
		}
	}
	return TouchTarget{}, false
}

// TouchPlot is the band the plot takes on screen m under the touch-first
// layout: between the readout bar and the touch bar.
// TRLC-LINKS: REQ-DSO-299
func TouchPlot(m *Metrics) image.Rectangle {
	return image.Rect(0, TouchTopH, m.W, m.H-TouchBarH)
}

// TouchSheetRows is how many rows of touchRowH the sheet panel shows on
// screen m (with a foot of choices: fewer).
// TRLC-LINKS: REQ-DSO-299
func TouchSheetRows(m *Metrics, foot bool) int {
	_, y0, _, y1 := touchRowsArea(m, foot)
	return max(1, (y1-y0)/touchRowH)
}

// touchRowsArea is the sheet panel's rows area.
// TRLC-LINKS: REQ-DSO-299
func touchRowsArea(m *Metrics, foot bool) (x0, y0, x1, y1 int) {
	x0, x1 = m.W-touchSheetW+touchTabW+8, m.W-8
	y0, y1 = touchHeadH, m.H-8
	if foot {
		y1 = m.H - touchFootH - 4
	}
	return x0, y0, x1, y1
}

// nullCanvas is a canvas that keeps no pixels: the hit test's drawing.
type nullCanvas struct{ m *Metrics }

// TRLC-LINKS: REQ-DSO-299
func (n nullCanvas) Format() hal.PixelFormat { return hal.XRGB8888 }

// TRLC-LINKS: REQ-DSO-299
func (n nullCanvas) Metrics() *Metrics { return n.m }

// TRLC-LINKS: REQ-DSO-299
func (nullCanvas) SetPixel(int, int, Color) {}

// TRLC-LINKS: REQ-DSO-299
func (nullCanvas) Fill(Color) {}

// TRLC-LINKS: REQ-DSO-299
func (nullCanvas) At(int, int) Color { return Transparent }

// touchBandKey names a band's metrics: the screen's and the plane under it.
type touchBandKey struct {
	m     *Metrics
	under image.Rectangle
}

// touchBands holds the band metrics made so far (a screen has one).
var touchBands struct {
	sync.Mutex
	m map[touchBandKey]*Metrics
}

// touchBand is the metrics of the band between the bars on screen m: as
// tall as the band, the trace mapped onto the hardware plane's rows when
// one shows under the canvas (else clear of the band's edges).
// TRLC-LINKS: REQ-DSO-299
func touchBand(m *Metrics, under image.Rectangle) *Metrics {
	k := touchBandKey{m, under}
	touchBands.Lock()
	defer touchBands.Unlock()
	if b, ok := touchBands.m[k]; ok {
		return b
	}
	b := *m
	b.H = m.H - TouchTopH - TouchBarH
	b.TraceTop, b.TraceBot = 8, b.H-4
	if !under.Empty() {
		b.TraceTop, b.TraceBot = max(under.Min.Y-TouchTopH, 0), min(under.Max.Y-1-TouchTopH, b.H-1)
	}
	b.MaxPlotTop = min(m.MaxPlotTop, b.H*5/8)
	for i, y := range m.SoftkeyY {
		b.SoftkeyY[i] = y * b.H / m.H
	}
	if touchBands.m == nil {
		touchBands.m = map[touchBandKey]*Metrics{}
	}
	touchBands.m[k] = &b
	return &b
}

// bandOf is rows y0.. of canvas sf as a canvas of screen bm (sf's width,
// bm.H rows), sharing sf's pixels; an in-memory canvas keeps its fast
// paths, any other draws through an offset.
// TRLC-LINKS: REQ-DSO-299
func bandOf(sf Canvas, y0 int, bm *Metrics) Canvas {
	switch c := sf.(type) {
	case *Image565:
		if c.w == bm.W && y0+bm.H <= c.h {
			pix := c.pix[y0*c.w : (y0+bm.H)*c.w]
			return &Image565{Pix: c.Pix[2*y0*c.w : 2*(y0+bm.H)*c.w], pix: pix, m: bm, w: bm.W, h: bm.H}
		}
	case *Image32:
		if c.w == bm.W && y0+bm.H <= c.h {
			pix := c.pix[y0*c.w : (y0+bm.H)*c.w]
			b := &Image32{Pix: c.Pix[4*y0*c.w : 4*(y0+bm.H)*c.w], pix: pix, m: bm, format: c.format, opaque: c.opaque, w: bm.W, h: bm.H}
			if !c.under.Empty() {
				b.under = c.under.Sub(image.Pt(0, y0)).Intersect(image.Rect(0, 0, bm.W, bm.H))
			}
			return b
		}
	}
	return offsetCanvas{sf, y0, bm}
}

// offsetCanvas draws onto rows y0.. of another canvas.
type offsetCanvas struct {
	c  Canvas
	y0 int
	m  *Metrics
}

// TRLC-LINKS: REQ-DSO-299
func (o offsetCanvas) Format() hal.PixelFormat { return o.c.Format() }

// TRLC-LINKS: REQ-DSO-299
func (o offsetCanvas) Metrics() *Metrics { return o.m }

// TRLC-LINKS: REQ-DSO-299
func (o offsetCanvas) SetPixel(x, y int, c Color) {
	if y >= 0 && y < o.m.H {
		o.c.SetPixel(x, y+o.y0, c)
	}
}

// TRLC-LINKS: REQ-DSO-299
func (o offsetCanvas) Fill(c Color) { fillRect(o.c, 0, o.y0, o.m.W, o.m.H, c) }

// TRLC-LINKS: REQ-DSO-299
func (o offsetCanvas) At(x, y int) Color {
	if y < 0 || y >= o.m.H {
		return Transparent
	}
	return o.c.At(x, y+o.y0)
}

// renderTouch is Render under the touch-first layout: the workspace's
// picture in the band between the bars, then the bars, a selection box,
// the sheet panel and the match editor over it.
// TRLC-LINKS: REQ-DSO-299
func renderTouch(sf Canvas, f *sample.Frame, hud HUD, live bool, persist []Layer) {
	sm := metricsOf(sf)
	bm := touchBand(sm, underOf(sf))
	inner := hud
	inner.UI.Sheet, inner.UI.Keys, inner.UI.Rail, inner.UI.RailNote = nil, nil, false, nil
	inner.UI.Layout.Sheet = false
	inner.Match.Open = false
	var layers []Layer
	if len(persist) > 0 && persist[0] != nil {
		if l, ok := bandOf(persist[0], TouchTopH, bm).(Layer); ok {
			layers = append(layers, l)
		}
	}
	render(bandOf(sf, TouchTopH, bm), f, inner, live, layers...)
	td := touchDraw{sf: sf}
	td.chrome(f, hud, live)
}

// touchDraw draws the touch layout's chrome and, given out, collects its
// targets.
type touchDraw struct {
	sf  Canvas
	out *[]TouchTarget
}

// add records target r as id.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) add(r image.Rectangle, id string) {
	if t.out != nil {
		*t.out = append(*t.out, TouchTarget{R: r, ID: id})
	}
}

// chrome draws (and records) the bars, the zoom's reset, the selection box,
// the sheet panel and the match editor.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) chrome(f *sample.Frame, hud HUD, live bool) {
	sm := metricsOf(t.sf)
	ui := &hud.UI
	t.add(TouchPlot(sm), "plot")
	t.topBar(f, hud, live)
	t.bottomBar(ui.Bar, ui.WS)
	if b := ui.Bar; b != nil && b.Zoom != "" && ui.Sheet == nil {
		r := image.Rect(sm.W-178, TouchTopH+6, sm.W-8, TouchTopH+6+touchCtlH)
		fillRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colMenuBg)
		strokeRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colTrig)
		DrawText(t.sf, r.Min.X+10, r.Min.Y+14, b.Zoom, colTrig, 1)
		DrawTextRight(t.sf, r.Max.X-10, r.Min.Y+14, "Reset", colInfo, 1)
		t.add(r, "zoom:reset")
	}
	if b := ui.Box; b != nil {
		x0, x1 := min(b.X0, b.X1), max(b.X0, b.X1)
		y0, y1 := min(b.Y0, b.Y1), max(b.Y0, b.Y1)
		dashRect(t.sf, x0, y0, x1-x0+1, y1-y0+1, colTrig)
	}
	if ui.Sheet != nil {
		t.sheet(ui.Sheet)
		if hud.Notice != "" {
			drawNotice(t.sf, fullPlotOf(sm), hud.Notice)
		}
	}
	if hud.Match.Open { // the keyboard over everything, its choices in the bar
		if t.out != nil {
			*t.out = (*t.out)[:0]
		}
		drawMatchEditorIn(t.sf, hud.Match, sm.W, sm.H-TouchBarH, t.add)
		t.bottomBar(ui.Bar, ui.WS)
	}
}

// button draws one button of label b in r: filled amber when selected,
// else outlined, its label centred.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) button(r image.Rectangle, b wsview.Btn, scale int) {
	fg := colorOf(b.Col)
	if b.Sel {
		fillRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colTrig)
		fg = colBG
	} else {
		fillRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colBG)
		strokeRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colGrid)
		if b.Dim {
			fg = colDim
		}
	}
	label := cutText(b.Label, (r.Dx()-8)/(6*scale))
	DrawText(t.sf, r.Min.X+(r.Dx()-TextWidth(label, scale))/2, r.Min.Y+(r.Dy()-7*scale)/2, label, fg, scale)
}

// topBar is the readout bar: the liveness strip, a chip per channel, the
// timebase's and the trigger's chips (each opens its sheet), the status
// slot and the workspace's name.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) topBar(f *sample.Frame, hud HUD, live bool) {
	sm := metricsOf(t.sf)
	fillRect(t.sf, 0, 0, sm.W, TouchTopH, colBG)
	strip := liveStrip(f, live)
	hline(t.sf, 0, sm.W-1, 0, strip)
	hline(t.sf, 0, sm.W-1, 1, strip)
	hline(t.sf, 0, sm.W-1, TouchTopH-1, colGrid)
	var clip [MaxChans]bool
	if f != nil && f.Len() > 0 {
		clip = frameClipped(f)
	}
	chip := func(x, w int) image.Rectangle {
		r := image.Rect(x, 5, x+w, TouchTopH-4)
		strokeRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colGrid)
		return r
	}
	x := 4
	for ch := range hud.numChans() {
		c := hud.Ch[ch]
		r := chip(x, touchChipW-4)
		col := chanColor(ch)
		head := chanName(ch) + cplTag(c.Cpl)
		if c.Probe > 1 {
			head += " " + strconv.FormatFloat(c.Probe, 'g', -1, 64) + "x"
		}
		DrawText(t.sf, r.Min.X+6, r.Min.Y+4, head, col, 1)
		if clip[ch] {
			DrawTextRight(t.sf, r.Max.X-5, r.Min.Y+4, "CLIP", colTrig, 1)
		}
		if hud.shown(ch) {
			DrawText(t.sf, r.Min.X+6, r.Min.Y+14, fmtVolt(c.VdivV*max(c.Probe, 1)), col, 2)
		} else {
			DrawText(t.sf, r.Min.X+6, r.Min.Y+14, "off", colDim, 2)
		}
		t.add(image.Rect(r.Min.X, 0, r.Max.X, TouchTopH), "open:ch"+strconv.Itoa(ch+1))
		x += touchChipW
	}
	r := chip(x, 120)
	head := "TIME"
	switch hud.ViewMode {
	case 1:
		head = "TIME  X-Y"
	case 2:
		head = "TIME  FFT"
	case 3:
		head = "TIME  BODE"
	case 4:
		head = "TIME  SPGM"
	}
	DrawText(t.sf, r.Min.X+6, r.Min.Y+4, head, colDim, 1)
	if hud.Zoom > 1 {
		DrawTextRight(t.sf, r.Max.X-5, r.Min.Y+4, "x"+strconv.Itoa(hud.Zoom), colTrig, 1)
	}
	DrawText(t.sf, r.Min.X+6, r.Min.Y+14, fmtTdiv(hud.TdivS), colInfo, 2)
	t.add(image.Rect(r.Min.X, 0, r.Max.X, TouchTopH), "open:time")
	x += 124
	r = chip(x, 190)
	what, state := trigParts(hud)
	stateCol := colTrig
	if state == stateStop {
		stateCol = colStale
	}
	DrawText(t.sf, r.Min.X+6, r.Min.Y+4, "TRIG", colDim, 1)
	DrawTextRight(t.sf, r.Max.X-5, r.Min.Y+4, state, stateCol, 1)
	if TextWidth(what, 2) <= r.Dx()-12 {
		DrawText(t.sf, r.Min.X+6, r.Min.Y+14, what, colTrig, 2)
	} else {
		DrawText(t.sf, r.Min.X+6, r.Min.Y+17, cutText(what, (r.Dx()-12)/6), colTrig, 1)
	}
	t.add(image.Rect(r.Min.X, 0, r.Max.X, TouchTopH), "open:trig")
	if hud.Counter != "" {
		DrawText(t.sf, r.Max.X+8, 24, cutText(hud.Counter, max(0, (sm.W-100-r.Max.X)/6)), colInfo, 1)
	}
	DrawStatusSlot(t.sf, hud)
	DrawTextRight(t.sf, sm.W-6, 15, wsview.Names[clampI(int(hud.UI.WS), 0, wsview.Count-1)], colTrig, 2)
}

// bottomBar is the touch bar: the workspaces (or a menu's choices), the
// workspace's views as a segmented control and the actions at the right.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) bottomBar(b *wsview.TouchBar, active wsview.WS) {
	if b == nil {
		return
	}
	sm := metricsOf(t.sf)
	y0 := sm.H - TouchBarH
	fillRect(t.sf, 0, y0, sm.W, TouchBarH, colMenuBg)
	hline(t.sf, 0, sm.W-1, y0, colGrid)
	by, bh := y0+5, TouchBarH-9
	ax := sm.W - 4 - len(b.Actions)*touchActW
	for i, a := range b.Actions {
		r := image.Rect(ax+i*touchActW+2, by, ax+(i+1)*touchActW-2, by+bh)
		t.button(r, a, 1)
		t.add(r, a.ID)
	}
	if len(b.Keys) > 0 {
		w := min(160, (ax-8)/len(b.Keys))
		for i, k := range b.Keys {
			r := image.Rect(4+i*w+2, by, 4+(i+1)*w-2, by+bh)
			t.button(r, k, 1)
			t.add(r, k.ID)
		}
		return
	}
	for i := range wsview.Count {
		r := image.Rect(4+i*touchWsW+2, by, 4+(i+1)*touchWsW-2, by+bh)
		fg, fg2, tag := colInfo, colDim, colDim
		if wsview.WS(i) == active {
			fillRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colTrig)
			fg, fg2, tag = colBG, colBG, colBG
		} else {
			fillRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colBG)
			strokeRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colGrid)
		}
		drawPicto(t.sf, wsview.WS(i), r.Min.X+(r.Dx()-22)/2, by+8, fg, fg2)
		name := wsview.Tags[i]
		DrawText(t.sf, r.Min.X+(r.Dx()-TextWidth(name, 1))/2, by+30, name, tag, 1)
		t.add(r, "ws:"+strconv.Itoa(i))
	}
	vx := 4 + wsview.Count*touchWsW + 10
	n := len(b.Views)
	if n == 0 {
		return
	}
	w := min(120, (ax-10-vx)/n)
	strokeRect(t.sf, vx, by, w*n, bh, colGrid)
	for i, v := range b.Views {
		r := image.Rect(vx+i*w, by, vx+(i+1)*w, by+bh)
		fg := colorOf(v.Col)
		switch {
		case v.Sel:
			fillRect(t.sf, r.Min.X+1, r.Min.Y+1, r.Dx()-2, r.Dy()-2, colTrig)
			fg = colBG
		case v.Dim:
			fg = colDim
		}
		if i > 0 {
			vline(t.sf, r.Min.X, by, by+bh-1, colGrid)
		}
		label := cutText(v.Label, (w-8)/6)
		DrawText(t.sf, r.Min.X+(w-TextWidth(label, 1))/2, by+(bh-7)/2, label, fg, 1)
		t.add(r, v.ID)
	}
}

// sheet is the sheet panel: the tab column, the header with paging and
// Close, the rows (or the picker's bytes), a row's chooser and the
// sub-mode's choices along the foot.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) sheet(sh *wsview.Sheet) {
	sm := metricsOf(t.sf)
	px0 := sm.W - touchSheetW
	t.add(image.Rect(0, 0, sm.W, sm.H), "sheet:outside")
	fillRect(t.sf, px0, 0, touchSheetW, sm.H, colBG)
	vline(t.sf, px0, 0, sm.H-1, colTrig)
	t.add(image.Rect(px0, 0, sm.W, sm.H), "sheet:panel")
	pitch := min(44, sm.H/max(len(sh.Tabs), 1))
	active := ""
	for i, tb := range sh.Tabs {
		r := image.Rect(px0+6, i*pitch+2, px0+touchTabW-2, (i+1)*pitch-1)
		fg := colDim
		if tb.Own {
			fg = colInfo
		}
		if tb.Active {
			fillRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colTrig)
			fg, active = colBG, tb.Name
		} else {
			strokeRect(t.sf, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), colGrid)
		}
		DrawText(t.sf, r.Min.X+8, r.Min.Y+(r.Dy()-7)/2, cutText(tb.Name, (r.Dx()-12)/6), fg, 1)
		t.add(r, "tab:"+strconv.Itoa(i))
	}
	foot := len(sh.Keys) > 0
	x0, y0, x1, y1 := touchRowsArea(sm, foot)
	DrawText(t.sf, x0, 6, sh.WSName, colDim, 1)
	DrawText(t.sf, x0, 18, active, colTrig, 2)
	cl := image.Rect(sm.W-96, 4, sm.W-8, 4+touchCtlH)
	t.button(cl, wsview.Btn{Label: "Close"}, 1)
	t.add(cl, "sheet:close")
	more := false
	if sh.Sub != nil {
		DrawText(t.sf, x0, y0+8, sh.Sub.Title, colTrig, 1)
		drawTokens(t.sf, x0, y0+28, x1, sh.Sub.Toks, sh.Sub.Cur)
		for i, l := range sh.Sub.Lines {
			DrawText(t.sf, x0, y0+56+i*12, cutText(l.Text, (x1-x0)/6), colorOf(l.Col), 1)
		}
	} else {
		y := y0
		for i := max(sh.Scroll, 0); i < len(sh.Rows); i++ {
			h := touchRowH
			if sh.Rows[i].Kind == wsview.Header {
				h = touchHdrH
			}
			if y+h > y1 {
				more = true
				break
			}
			t.row(sh.Rows[i], i, x0, y, x1, h)
			y += h
		}
	}
	page := func(first, last bool) {
		up := image.Rect(cl.Min.X-128, 4, cl.Min.X-68, 4+touchCtlH)
		dn := image.Rect(cl.Min.X-64, 4, cl.Min.X-4, 4+touchCtlH)
		t.button(up, wsview.Btn{Label: "Up", Dim: first}, 1)
		t.add(up, "page:-1")
		t.button(dn, wsview.Btn{Label: "Down", Dim: last}, 1)
		t.add(dn, "page:1")
	}
	if c := sh.Choose; c != nil {
		if cm := t.chooser(c, x0, y0, x1, y1); c.First > 0 || cm {
			page(c.First == 0, !cm)
		}
	} else if sh.Scroll > 0 || more {
		page(sh.Scroll == 0, !more)
	}
	if foot {
		w := (x1 - x0) / len(sh.Keys)
		for k, b := range sh.Keys {
			r := image.Rect(x0+k*w+2, sm.H-touchFootH, x0+(k+1)*w-2, sm.H-8)
			t.button(r, b, 1)
			t.add(r, b.ID)
		}
	}
}

// row draws sheet row i (r) at y, h high, between x0 and x1: its label, and
// its controls as targets: an enum's options (or one chooser when they do
// not fit), a number's steppers, slider and value, a row's buttons.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) row(r wsview.Row, i, x0, y, x1, h int) {
	id := strconv.Itoa(i)
	if r.Kind == wsview.Header {
		DrawText(t.sf, x0+4, y+h-11, r.Header, colTrig, 1)
		hline(t.sf, x0+10+TextWidth(r.Header, 1), x1, y+h-8, colGrid)
		return
	}
	t.add(image.Rect(x0, y+2, x1, y+h-2), "row:"+id)
	if r.Focus {
		fillRect(t.sf, x0, y+6, 3, h-12, colTrig)
	}
	lc := colInfo
	if !r.Rel {
		lc = colDim
	}
	DrawText(t.sf, x0+10, y+(h-7)/2, cutText(r.Label, (touchLabelW-16)/6), lc, 1)
	cx, cy := x0+touchLabelW, y+(h-touchCtlH)/2
	ctl := func(x0, x1 int) image.Rectangle { return image.Rect(x0, cy, x1, cy+touchCtlH) }
	switch r.Kind {
	case wsview.Enum:
		t.enumRow(r, id, cx, cy, x1, ctl)
	case wsview.Number:
		dec := ctl(cx, cx+44)
		val := ctl(x1-124, x1)
		inc := ctl(val.Min.X-50, val.Min.X-6)
		t.button(dec, wsview.Btn{Label: "-", Dim: !r.Rel}, 2)
		t.button(inc, wsview.Btn{Label: "+", Dim: !r.Rel}, 2)
		t.add(dec, "dec:"+id)
		t.add(inc, "inc:"+id)
		sc, vc := colGrid, colTrig
		if r.Edit {
			sc = colTrig
		}
		if !r.Rel {
			vc = colDim
		}
		strokeRect(t.sf, val.Min.X, val.Min.Y, val.Dx(), val.Dy(), sc)
		DrawTextRight(t.sf, val.Max.X-8, cy+(touchCtlH-7)/2, cutText(r.Value, (val.Dx()-12)/6), vc, 1)
		t.add(val, "val:"+id)
		if sx0, sx1 := dec.Max.X+16, inc.Min.X-16; sx1 > sx0 {
			my := cy + touchCtlH/2
			hline(t.sf, sx0, sx1, my, colGrid)
			hline(t.sf, sx0, sx1, my+1, colGrid)
			kx := sx0 + int(min(max(r.Gauge, 0), 1)*float64(sx1-sx0)+0.5)
			kc := colInfo
			if r.Rel {
				hline(t.sf, sx0, kx, my, colTrig)
				hline(t.sf, sx0, kx, my+1, colTrig)
				kc = colTrig
			}
			fillRect(t.sf, kx-4, cy+8, 9, touchCtlH-16, kc)
			t.add(image.Rect(sx0-12, y, sx1+12, y+h), "slide:"+id)
		}
	case wsview.Action, wsview.Slot, wsview.Bytes:
		x := cx
		for k, a := range r.Acts {
			w := max(64, TextWidth(a.Text, 1)+20)
			if x+w > x1 {
				break
			}
			rr := ctl(x, x+w)
			t.button(rr, wsview.Btn{Label: a.Text, Dim: a.Dim, Sel: a.Flash}, 1)
			t.add(rr, "fk:"+id+":"+strconv.Itoa(k))
			x += w + 6
		}
		st, col := r.Status, colDim
		if r.Kind != wsview.Action {
			st = r.Value
		}
		if r.Armed {
			col = colTrig
		}
		if room := (x1 - x - 12) / 6; st != "" && room > 4 {
			DrawTextRight(t.sf, x1-6, y+(h-7)/2, cutText(st, room), col, 1)
		}
	case wsview.Header: // drawn above
	}
}

// enumRow is an enum row's options as buttons, or, when they do not fit,
// its value as one target that opens the chooser.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) enumRow(r wsview.Row, id string, cx, cy, x1 int, ctl func(x0, x1 int) image.Rectangle) {
	w := 0
	for _, o := range r.Opts {
		w += max(44, TextWidth(o.Text, 1)+18) + 6
	}
	if w <= x1-cx {
		x := cx
		for k, o := range r.Opts {
			ow := max(44, TextWidth(o.Text, 1)+18)
			rr := ctl(x, x+ow)
			if o.Sel && o.Quiet { // selected, but off or not in use: grey
				fillRect(t.sf, rr.Min.X, rr.Min.Y, rr.Dx(), rr.Dy(), colGrid)
				DrawText(t.sf, rr.Min.X+(ow-TextWidth(o.Text, 1))/2, cy+(touchCtlH-7)/2, o.Text, colInfo, 1)
			} else {
				t.button(rr, wsview.Btn{Label: o.Text, Sel: o.Sel, Dim: !r.Rel}, 1)
			}
			t.add(rr, "opt:"+id+":"+strconv.Itoa(k))
			x += ow + 6
		}
		return
	}
	cur := "-"
	for _, o := range r.Opts {
		if o.Sel {
			cur = o.Text
		}
	}
	rr := ctl(cx, x1)
	strokeRect(t.sf, rr.Min.X, rr.Min.Y, rr.Dx(), rr.Dy(), colGrid)
	vc := colTrig
	if !r.Rel {
		vc = colDim
	}
	DrawText(t.sf, rr.Min.X+10, cy+(touchCtlH-7)/2, cutText(cur, (rr.Dx()-90)/6), vc, 1)
	DrawTextRight(t.sf, rr.Max.X-10, cy+(touchCtlH-7)/2, strconv.Itoa(len(r.Opts))+" choices", colDim, 1)
	t.add(rr, "more:"+id)
}

// chooser is a row's list of choices over the rows area: a grid of large
// targets from c.First, and Cancel; it reports whether more follow.
// TRLC-LINKS: REQ-DSO-299
func (t touchDraw) chooser(c *wsview.Chooser, x0, y0, x1, y1 int) (more bool) {
	t.add(image.Rect(x0, y0, x1, y1), "choose:-1")
	fillRect(t.sf, x0, y0, x1-x0, y1-y0, colMenuBg)
	strokeRect(t.sf, x0, y0, x1-x0, y1-y0, colTrig)
	DrawText(t.sf, x0+12, y0+16, cutText(c.Title, (x1-x0-130)/6), colTrig, 1)
	cancel := image.Rect(x1-100, y0+6, x1-8, y0+6+touchCtlH)
	t.button(cancel, wsview.Btn{Label: "Cancel"}, 1)
	t.add(cancel, "choose:-1")
	per, cols := TouchChooserPage(x1-x0, y1-y0)
	w := (x1 - x0 - 16) / cols
	first := max(c.First, 0)
	for k := first; k < len(c.Opts); k++ {
		j := k - first
		if j >= per {
			return true
		}
		ry := y0 + 50 + (j/cols)*touchRowH
		r := image.Rect(x0+8+(j%cols)*w+2, ry, x0+8+(j%cols+1)*w-2, ry+touchCtlH+4)
		t.button(r, wsview.Btn{Label: c.Opts[k].Text, Sel: c.Opts[k].Sel}, 1)
		t.add(r, "choose:"+strconv.Itoa(k))
	}
	return false
}

// TouchChooserPage is how many choices a chooser w by h shows, and in how
// many columns.
// TRLC-LINKS: REQ-DSO-299
func TouchChooserPage(w, h int) (per, cols int) {
	cols = 3
	if w > 600 {
		cols = 4
	}
	return max(1, (h-56)/touchRowH) * cols, cols
}

// TouchChooserSize is the chooser's size on screen m (the rows area).
// TRLC-LINKS: REQ-DSO-299
func TouchChooserSize(m *Metrics, foot bool) (w, h int) {
	x0, y0, x1, y1 := touchRowsArea(m, foot)
	return x1 - x0, y1 - y0
}

// TouchRowsShown is how many of rows, from first, the sheet panel on screen
// m shows (with a foot of choices: fewer).
// TRLC-LINKS: REQ-DSO-299
func TouchRowsShown(m *Metrics, foot bool, rows []wsview.Row, first int) int {
	_, y0, _, y1 := touchRowsArea(m, foot)
	n := 0
	for i := max(first, 0); i < len(rows); i++ {
		h := touchRowH
		if rows[i].Kind == wsview.Header {
			h = touchHdrH
		}
		if y0+h > y1 {
			break
		}
		y0 += h
		n++
	}
	return n
}
