// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"fmt"
	"image"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// The touch-first layout (ADR-TOUCH-FIRST-LAYOUT, REQ-DSO-299). A scope
// with a touch screen and no F-keys gets the same workspaces, views,
// sheets and rows laid out for a finger (render's touch geometry): the
// readouts open their sheets, a touch bar switches workspaces and views
// and runs the actions, a sheet's rows carry their own controls. Gestures
// on the waveform act on the view alone and never change a setting: a
// one-finger drag draws a box that zooms the view into it, a pinch zooms
// the view about the fingers, two fingers pan a zoomed view, a double tap
// resets the view's zoom; with the cursor tab open a drag places the
// selected cursor. The zoom is the scope's own zoom window (time.zoom,
// time.zoom_pos), so the LCD, the sheets and the pages read one zoom.

// TouchFirst reports whether scope p lays its UI out for touch: a touch
// screen and none of the F-keys F1..F5 among its keys. The choice is the
// profile's facts, the same for any scope that has them.
// TRLC-LINKS: REQ-DSO-299
func TouchFirst(p hal.Profile) bool {
	if !p.Display.Touch {
		return false
	}
	for _, k := range p.Inputs.Keys {
		if n, ok := KeyName(k); ok && slices.Contains(softkeys, n) {
			return false
		}
	}
	return true
}

// SetTouchLayout lays the UI out for touch on screen m (TouchFirst): the
// view gains the touch bar and the sheet panel, and a touch is hit-tested
// against what the screen draws. The views below the bars get the band
// between them.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) SetTouchLayout(m *render.Metrics) {
	c.mu.Lock()
	c.touchM = m
	c.mu.Unlock()
	c.screenHeight.Store(int64(render.TouchPlot(m).Dy()))
}

// touchLayout is the touch-first layout's screen, nil without it.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchLayout() *render.Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.touchM
}

// The gestures a touch can be part of.
const (
	gNone    = iota
	gPending // one finger on the waveform, not yet moved: a tap or a box
	gBox     // drawing a selection box
	gTwo     // two fingers down, not yet a pinch or a pan
	gPinch   // two fingers zooming the view
	gPan     // two fingers panning the zoomed view
	gCursor  // placing a cursor (the cursor tab open)
	gRow     // a finger on a sheet row: a tap focuses it, a drag scrolls
	gScroll  // scrolling the sheet's rows
	gSlide   // dragging a number row's slider
	gDone    // the touch did its job: the rest of it is ignored
)

// touchSlop is how far a finger moves before a touch is a drag.
const touchSlop = 10

// doubleTap bounds a double tap: the second within this time and distance.
const (
	doubleTapGap  = 400 * time.Millisecond
	doubleTapNear = 40
)

// boxMinPx is the narrowest selection box that zooms (the web page's).
const boxMinPx = 12

// contact is one finger: where it went down and is, and what it hit.
type contact struct {
	x0, y0, x, y int
	id           string          // the target it went down on
	r            image.Rectangle // that target's place
	swallow      bool            // it woke the screen: the rest of it does nothing
}

// touchIn is the gestures under way (the panel goroutine's).
type touchIn struct {
	contacts map[int]*contact
	gest     int
	prim     int    // the finger the gesture is of
	pair     [2]int // the two fingers of a two-finger gesture
	outside  bool   // the pending touch began outside an open sheet
	vert     bool   // the pinch or pan is vertical
	zoom0    int    // the view's zoom and pan when the gesture began
	off0     float64
	d0       float64 // the record point under the fingers then (screen windows from the trigger)
	scroll0  int
	lastTap  time.Time
	tapAt    image.Point
	noted    bool // the gesture's notice was shown
}

// touchEvent runs one touch of the touch-first layout: a finger down, a
// move or a finger up.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchEvent(m *render.Metrics, ev hal.InputEvent) {
	t := &c.tin
	if t.contacts == nil {
		t.contacts = map[int]*contact{}
	}
	id, x, y := ev.Contact, ev.TouchX, ev.TouchY
	switch {
	case ev.Down && ev.Gesture != hal.TouchMove:
		if _, ok := t.contacts[id]; ok { // a start without its end: end it first
			c.touchEnd(id, x, y)
		}
		if c.noteInput(true) { // it woke the screen: nothing else
			t.contacts[id] = &contact{swallow: true}
			return
		}
		c.touchStart(m, id, x, y)
	case ev.Down:
		c.touchMove(m, id, x, y)
	default:
		c.touchEnd(id, x, y)
	}
}

// liveContacts are the fingers down that take part (not woken ones).
// TRLC-LINKS: REQ-DSO-299
func (t *touchIn) liveContacts() int {
	n := 0
	for _, k := range t.contacts {
		if !k.swallow {
			n++
		}
	}
	return n
}

// touchStart is a finger down: a second finger on the waveform makes a
// two-finger gesture; a first one hits what the screen shows there.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchStart(m *render.Metrics, id, x, y int) {
	t := &c.tin
	if t.liveContacts() > 0 {
		k := &contact{x0: x, y0: y, x: x, y: y}
		t.contacts[id] = k
		var first int
		for i, o := range t.contacts {
			if i != id && !o.swallow {
				first = i
			}
		}
		if t.gest == gPending || t.gest == gBox {
			c.setBox(nil)
			f := t.contacts[first]
			f.x0, f.y0 = f.x, f.y // the gesture measures from both fingers down
			t.gest, t.pair, t.noted = gTwo, [2]int{first, id}, false
			s := c.snap()
			t.zoom0, t.off0 = max(s.zoom, 1), s.zoomOff
			t.d0 = c.recordAt(s, float64(f.x+x)/2/float64(m.W))
			return
		}
		if t.gest != gDone {
			k.swallow = true // a finger beside a slider, a scroll or a pinch does nothing
			return
		}
		for _, o := range t.contacts { // a button did its job: this is a tap of its own
			o.swallow = true
		}
	}
	t.prim = id
	hud := c.HUD(c.nch)
	tg, _ := render.TouchAt(render.TouchTargets(m, hud), x, y)
	k := &contact{x0: x, y0: y, x: x, y: y, id: tg.ID, r: tg.R}
	t.contacts[id] = k
	t.outside = false
	switch {
	case tg.ID == "plot":
		t.gest = gPending
	case tg.ID == "sheet:outside" && c.cursorTabOpen():
		t.gest, t.outside = gPending, true // a drag places a cursor, a tap closes the sheet
	case strings.HasPrefix(tg.ID, "slide:"):
		t.gest = gSlide
		c.touchSlide(k)
	case strings.HasPrefix(tg.ID, "row:") || tg.ID == "sheet:panel":
		t.gest = gRow
		c.mu.Lock()
		t.scroll0 = c.ui.tScroll[c.ui.tab]
		c.mu.Unlock()
	default:
		t.gest = gDone
		c.touchRun(m, tg.ID)
	}
}

// touchMove is a finger moving: the box grows, the pinch or pan follows,
// the slider or the rows follow, a cursor is placed.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchMove(m *render.Metrics, id, x, y int) {
	t := &c.tin
	k := t.contacts[id]
	if k == nil || k.swallow {
		return
	}
	k.x, k.y = x, y
	two := t.gest == gTwo || t.gest == gPinch || t.gest == gPan
	if two && id != t.pair[0] && id != t.pair[1] || !two && id != t.prim {
		return
	}
	moved := absInt(x-k.x0)+absInt(y-k.y0) > touchSlop
	switch t.gest {
	case gPending:
		if !moved {
			return
		}
		t.gest = gBox
		if t.outside {
			t.gest = gCursor
		}
		c.touchMove(m, id, x, y)
	case gBox:
		pr := render.TouchPlot(m)
		cx := func(v, lo, hi int) int { return min(max(v, lo), hi-1) }
		c.setBox(&wsview.Box{X0: cx(k.x0, pr.Min.X, pr.Max.X), Y0: cx(k.y0, pr.Min.Y, pr.Max.Y), X1: cx(x, pr.Min.X, pr.Max.X), Y1: cx(y, pr.Min.Y, pr.Max.Y)})
	case gCursor:
		c.touchCursor(m, x, y)
	case gTwo, gPinch, gPan:
		c.twoFingers(m)
	case gSlide:
		c.touchSlide(k)
	case gRow:
		if absInt(y-k.y0) > touchSlop {
			t.gest = gScroll
			c.touchMove(m, id, x, y)
		}
	case gScroll:
		c.mu.Lock()
		c.ui.tScroll[c.ui.tab] = max(0, t.scroll0-(y-k.y0)/(render.TouchPlot(m).Dy()/10))
		c.mu.Unlock()
	}
}

// touchEnd is a finger up: a tap on the waveform (two make a double tap),
// a box zooms, a row tapped is focused.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchEnd(id, x, y int) {
	t := &c.tin
	k := t.contacts[id]
	delete(t.contacts, id)
	if k == nil || k.swallow {
		if t.liveContacts() == 0 {
			t.gest = gNone
		}
		return
	}
	m := c.touchLayout()
	two := t.gest == gTwo || t.gest == gPinch || t.gest == gPan
	if !two && id != t.prim {
		return
	}
	switch t.gest {
	case gPending:
		if t.outside {
			c.mu.Lock()
			c.closeSheetLocked()
			c.ui.tChoose = ""
			c.mu.Unlock()
			break
		}
		now := c.now()
		p := image.Pt(x, y)
		if now.Sub(t.lastTap) < doubleTapGap && absInt(p.X-t.tapAt.X)+absInt(p.Y-t.tapAt.Y) < doubleTapNear {
			t.lastTap = time.Time{}
			c.zoomReset()
			break
		}
		t.lastTap, t.tapAt = now, p
	case gBox:
		if m != nil {
			c.applyBox(m, k.x0, x)
		}
		c.setBox(nil)
	case gRow:
		if m != nil {
			c.touchRun(m, k.id)
		}
	}
	switch {
	case t.liveContacts() == 0:
		t.gest = gNone
	case t.gest == gTwo || t.gest == gPinch || t.gest == gPan:
		t.gest = gDone // the other finger lifts before anything else
	}
}

// absInt is |v|.
// TRLC-LINKS: REQ-DSO-299
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// setBox shows the selection box b (nil: none).
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) setBox(b *wsview.Box) {
	c.mu.Lock()
	c.ui.tBoxOn = b != nil
	if b != nil {
		c.ui.tBox = *b
	}
	c.mu.Unlock()
}

// winRatio is the screen's window over the record (its columns over the
// record's valid ones): what the zoom pan's record fraction is in screen
// windows.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) winRatio() float64 {
	c.amu.Lock()
	g := c.ana.g
	c.amu.Unlock()
	if g.valid > 0 && g.winCols > 0 && g.winCols <= g.valid {
		return float64(g.winCols) / float64(g.valid)
	}
	return 1
}

// recordAt is where in the record screen fraction fx is now, in screen
// windows from the trigger: zoomed, the pan's centre plus fx's distance
// from the middle over the zoom; else fx from the trigger's place.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) recordAt(s *sv, fx float64) float64 {
	if s.zoom > 1 {
		return s.zoomOff/c.winRatio() + (fx-0.5)/float64(s.zoom)
	}
	pos := s.st.TrigPosFrac
	if !(pos >= 0 && pos <= 1) {
		pos = 0.5
	}
	return fx - pos
}

// zoomTo sets the view's zoom z with record point d (screen windows from
// the trigger) at screen fraction fx: the zoom window of the scope's own
// zoom, a view of the record already acquired.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) zoomTo(z int, d, fx float64) {
	_ = c.setZoom(z) // z is one of zoomFactors
	if z > 1 {
		_ = c.setZoomOff((d - (fx-0.5)/float64(z)) * c.winRatio()) // clamped to the record
	}
}

// snapZoom is the offered zoom nearest v on a log scale; below 1, off.
// TRLC-LINKS: REQ-DSO-299
func snapZoom(v float64) int {
	if !(v > 1) {
		return 1
	}
	best, bd := 1, math.Inf(1)
	for _, f := range zoomFactors {
		if d := math.Abs(math.Log(v / float64(f))); d < bd {
			best, bd = f, d
		}
	}
	return best
}

// applyBox zooms the view into the box drawn from x0 to x1: the largest
// zoom whose window still holds the whole box, centred on it. A box under
// boxMinPx wide does nothing (the view has no vertical zoom, so its
// height does not matter).
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) applyBox(m *render.Metrics, x0, x1 int) {
	if absInt(x1-x0) < boxMinPx {
		return
	}
	s := c.snap()
	z := max(s.zoom, 1)
	w := float64(m.W)
	bw := float64(absInt(x1-x0)) / w
	nz := 1
	for _, f := range zoomFactors {
		if float64(f) <= float64(z)/bw {
			nz = f
		}
	}
	nz = max(nz, z)
	c.zoomTo(nz, c.recordAt(s, float64(x0+x1)/2/w), 0.5)
	c.toast(fmt.Sprintf("Zoom %dx on the box   double tap: zoom off", nz), 1400*time.Millisecond)
}

// zoomReset is the double tap and the zoom's Reset: the whole record again.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) zoomReset() {
	if c.snap().zoom > 1 {
		_ = c.setZoom(1) // 1x is always offered
		c.toast("Zoom off", 900*time.Millisecond)
	}
}

// twoFingers follows a two-finger gesture: once the fingers have moved it
// is a pinch (their spread changed most) or a pan (their middle moved
// most), horizontal or vertical; a horizontal pinch zooms the view about
// the fingers, a horizontal pan moves the zoomed view. The view has no
// vertical zoom or pan: those say so once.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) twoFingers(m *render.Metrics) {
	t := &c.tin
	a, b := t.contacts[t.pair[0]], t.contacts[t.pair[1]]
	if a == nil || b == nil {
		return
	}
	dx0, dx := absInt(a.x0-b.x0), absInt(a.x-b.x)
	dy0, dy := absInt(a.y0-b.y0), absInt(a.y-b.y)
	cx0, cx := (a.x0+b.x0)/2, (a.x+b.x)/2
	cy0, cy := (a.y0+b.y0)/2, (a.y+b.y)/2
	if t.gest == gTwo {
		spread := max(absInt(dx-dx0), absInt(dy-dy0))
		move := max(absInt(cx-cx0), absInt(cy-cy0))
		if spread < 2*touchSlop && move < 2*touchSlop {
			return
		}
		if spread >= move {
			t.gest, t.vert = gPinch, absInt(dy-dy0) > absInt(dx-dx0)
		} else {
			t.gest, t.vert = gPan, absInt(cy-cy0) > absInt(cx-cx0)
		}
	}
	note := func(msg string) {
		if !t.noted {
			t.noted = true
			c.toast(msg, 1600*time.Millisecond)
		}
	}
	w := float64(m.W)
	switch {
	case t.gest == gPinch && t.vert:
		note("The view has no vertical zoom: V/div sets the scale")
	case t.gest == gPinch:
		z := snapZoom(float64(t.zoom0) * float64(max(dx, 1)) / float64(max(dx0, 1)))
		if z != max(c.snap().zoom, 1) {
			c.zoomTo(z, t.d0, float64(cx)/w)
		}
	case t.vert:
		note("The view has no vertical pan: Position moves the trace")
	case t.zoom0 <= 1:
		note("Pan: zoom in first (pinch or draw a box)")
	default:
		_ = c.setZoomOff(t.off0 - float64(cx-cx0)/w/float64(t.zoom0)*c.winRatio()) // clamped to the record
	}
}

// cursorTabOpen reports whether the sheet shows the cursor tab: a drag on
// the waveform beside it places a cursor.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) cursorTabOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ui.open && sheetTabs[c.ui.tab].id == "curs"
}

// touchCursor places the cursor nearest the finger at it (the time
// cursors across, the voltage cursors down the plot) and selects it.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchCursor(m *render.Metrics, x, y int) {
	s := c.snap()
	if !s.curOn {
		c.toast("Cursors are off: switch them on in this sheet", 1200*time.Millisecond)
		return
	}
	pr := render.TouchPlot(m)
	top := pr.Min.Y + min(wsDefs[s.ui.ws].plotTop, pr.Dy()*5/8)
	f := float64(x) / float64(m.W)
	pos := s.curX
	if s.curType == 1 {
		f, pos = float64(y-top)/float64(max(pr.Max.Y-top, 1)), s.curY
	}
	f = min(max(f, 0), 1)
	k := 0
	if math.Abs(pos[1]-f) < math.Abs(pos[0]-f) {
		k = 1
	}
	c.mu.Lock()
	c.curSel = k
	c.placeCursorLocked(s, k, f)
	c.mu.Unlock()
}

// sheetRowAt is the sheet row a touch target's row number names: the
// tab's row index (the view's rows carry the section headers too); ok
// false for a header or none.
// TRLC-LINKS: REQ-DSO-299
func sheetRowAt(rows []*row, vi int) (int, bool) {
	n := 0
	for i, r := range rows {
		if r.hdr != "" {
			if n == vi {
				return 0, false
			}
			n++
		}
		if n == vi {
			return i, true
		}
		n++
	}
	return 0, false
}

// viewRowOf is the view's row number of tab row ti.
// TRLC-LINKS: REQ-DSO-299
func viewRowOf(rows []*row, ti int) int {
	n := 0
	for i, r := range rows {
		if r.hdr != "" {
			n++
		}
		if i == ti {
			return n
		}
		n++
	}
	return n
}

// touchSlide sets a number row from where the finger is on its slider: a
// ladder's step, else the value across the row's range.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchSlide(k *contact) {
	i, ok := c.touchRow(strings.TrimPrefix(k.id, "slide:"))
	if !ok {
		return
	}
	s := c.snap()
	r := tabRows(s.ui.tab, s.shape())[i]
	if r.kind != wsview.Number {
		return
	}
	in := k.r.Inset(12) // the slider's track inside its target
	f := min(max(float64(k.x-in.Min.X)/float64(max(in.Dx(), 1)), 0), 1)
	var v float64
	if r.ladder != nil {
		l := r.ladder(s)
		if len(l) == 0 {
			return
		}
		v = l[int(math.Round(f*float64(len(l)-1)))]
	} else {
		lo, hi := r.lim(s)
		v = lo + f*(hi-lo)
	}
	if math.Abs(v-r.val(s)) <= 1e-12*math.Max(1, math.Abs(v)) {
		return
	}
	if err := r.setNumber(c, s, v); err != nil {
		c.toast(err.Error(), 1500*time.Millisecond)
		return
	}
	c.setAdjTarget(r)
}

// touchRow focuses the sheet row the view's row number vi names; ok false
// for a header.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchRow(vi string) (int, bool) {
	n, err := strconv.Atoi(vi)
	if err != nil {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	i, ok := sheetRowAt(tabRows(c.ui.tab, c.shape()), n)
	if ok {
		c.ui.rowAt[c.ui.tab], c.ui.edit = i, false
	}
	return i, ok
}

// touchRun runs what a tap on target id does: the same function the key,
// the knob or the F-key it stands for runs.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchRun(m *render.Metrics, id string) {
	c.lcdActs()
	defer c.lcdActs()
	kind, arg, _ := strings.Cut(id, ":")
	n, _ := strconv.Atoi(arg)
	switch kind {
	case "ws": // the active one's tab stays (its views are on the segmented control)
		if c.snap().ui.ws != wsview.WS(n) {
			c.switchWS(wsview.WS(n))
		}
	case "view":
		c.selectView(n)
	case "act":
		if arg == "tools" {
			c.mu.Lock()
			c.ui.tTools = !c.ui.tTools
			c.mu.Unlock()
			return
		}
		c.button(arg) // auto, run, single, menu
	case "tool":
		c.toolRun(arg)
	case "key":
		c.fKey(n)
	case "pick":
		c.pickMove(n)
	case "mcur":
		c.matchMoveCursor(n)
	case "mclear":
		c.matchClear()
	case "open": // a channel's chip: its key; the timebase's and the trigger's: their menu keys
		key := arg
		switch arg {
		case "time":
			key = KeyHorizMenu
		case "trig":
			key = KeyTrigMenu
		}
		c.button(key)
	case "zoom":
		c.zoomReset()
	case "sheet":
		if arg == "close" || arg == "outside" {
			c.mu.Lock()
			c.closeSheetLocked()
			c.ui.tChoose = ""
			c.mu.Unlock()
		}
	case "tab":
		c.mu.Lock()
		if order, _ := tabOrder(c.ui.ws, c.nch); n >= 0 && n < len(order) {
			c.leaveModesLocked()
			c.ui.tab, c.ui.via, c.ui.tChoose = order[n], "", ""
		}
		c.mu.Unlock()
	case "page":
		c.touchPage(m, n)
	case "choose":
		c.touchChoose(n)
	case "mkey":
		r, col, _ := strings.Cut(arg, ":")
		ri, _ := strconv.Atoi(r)
		ci, _ := strconv.Atoi(col)
		c.mu.Lock()
		c.kb.row, c.kb.col = ri, ci
		c.mu.Unlock()
		c.matchTypeKey()
	default:
		c.touchRowControl(kind, arg)
	}
}

// touchRowControl runs a sheet row's control: "row" focuses it, "opt"
// picks an option, "more" and "val" open its chooser, "dec" and "inc" step
// it, "fk" runs its choice as its F-key would.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchRowControl(kind, arg string) {
	vi, k, _ := strings.Cut(arg, ":")
	ki, _ := strconv.Atoi(k)
	i, ok := c.touchRow(vi)
	if !ok {
		return
	}
	s := c.snap()
	r := tabRows(s.ui.tab, s.shape())[i]
	var err error
	switch kind {
	case "opt":
		if o := r.options(s); ki >= 0 && ki < len(o) {
			err = r.setEnum(c, s, o[ki].v)
		}
	case "more":
		c.openChooser(r.id, false)
	case "val":
		if r.presets == nil && r.def == nil {
			c.toast("Set it with - and +, or the slider", 1200*time.Millisecond)
			return
		}
		c.openChooser(r.id, true)
	case "dec", "inc":
		err = r.stepN(c, choose(kind == "dec", -1, 1))
		c.setAdjTarget(r)
	case "fk":
		c.sheetPressF(ki)
	}
	if err != nil {
		c.toast(err.Error(), 2000*time.Millisecond)
	}
}

// openChooser opens row id's chooser: its presets and its default, or its
// options.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) openChooser(id string, presets bool) {
	c.mu.Lock()
	c.ui.tChoose, c.ui.tChoosePre, c.ui.tChooseFirst = id, presets, 0
	c.mu.Unlock()
}

// chooserOpts are the open chooser's choices and what each runs.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) chooserOpts(s *sv) (r *row, opts []wsview.Opt, runs []func() error) {
	for _, x := range tabRows(s.ui.tab, s.shape()) {
		if x.id == s.ui.tChoose {
			r = x
		}
	}
	if r == nil {
		return nil, nil, nil
	}
	if !s.ui.tChoosePre {
		cur := r.get(s)
		for _, o := range r.options(s) {
			opts = append(opts, wsview.Opt{Text: o.t, Sel: o.v == cur})
			runs = append(runs, func() error { return r.setEnum(c, c.snap(), o.v) })
		}
		return r, opts, runs
	}
	if r.presets != nil {
		v := r.val(s)
		for _, p := range r.presets(s) {
			if p.run != nil {
				opts = append(opts, wsview.Opt{Text: p.t})
				runs = append(runs, func() error { return p.run(c) })
				continue
			}
			opts = append(opts, wsview.Opt{Text: p.t, Sel: math.Abs(v-p.v) <= 1e-12+1e-9*math.Abs(p.v)})
			runs = append(runs, func() error { return r.setNumber(c, c.snap(), p.v) })
		}
	}
	if r.def != nil {
		opts = append(opts, wsview.Opt{Text: "Default"})
		runs = append(runs, func() error { return r.def(c) })
	}
	return r, opts, runs
}

// touchChoose is a choice of the open chooser (-1: cancel): it runs and the
// chooser closes.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchChoose(k int) {
	s := c.snap()
	r, _, runs := c.chooserOpts(s)
	c.mu.Lock()
	c.ui.tChoose = ""
	c.mu.Unlock()
	if r == nil || k < 0 || k >= len(runs) {
		return
	}
	if err := runs[k](); err != nil {
		c.toast(err.Error(), 2000*time.Millisecond)
		return
	}
	c.setAdjTarget(r)
}

// touchPage pages the open chooser, or the sheet's rows, by a page.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchPage(m *render.Metrics, dir int) {
	s := c.snap()
	if s.ui.tChoose != "" {
		_, opts, _ := c.chooserOpts(s)
		per, _ := render.TouchChooserPage(render.TouchChooserSize(m, s.ui.sub == subPick))
		c.mu.Lock()
		c.ui.tChooseFirst = min(max(0, c.ui.tChooseFirst+dir*per), max(0, len(opts)-1))
		c.mu.Unlock()
		return
	}
	page := render.TouchSheetRows(m, s.ui.sub == subPick)
	n := len(c.sheetView(s).Rows)
	c.mu.Lock()
	c.ui.tScroll[c.ui.tab] = min(max(0, c.ui.tScroll[c.ui.tab]+dir*page), max(0, n-1))
	c.mu.Unlock()
}

// selectView shows the workspace's view n of its segmented control: the
// workspace key again until it is up (COMPARE's one button stacks a master
// or cancels one).
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) selectView(n int) {
	for range 4 {
		s := c.snap()
		if s.ui.ws == wsview.Compare {
			c.nextView()
			return
		}
		if wsViewIndex(s) == n {
			return
		}
		c.nextView()
	}
}

// wsViewIndex is the workspace's view now, as its segmented control counts.
// TRLC-LINKS: REQ-DSO-299
func wsViewIndex(s *sv) int {
	u := &s.ui
	switch u.ws {
	case wsview.Scope:
		return choose(s.viewMode == viewXY, 1, 0)
	case wsview.Bus:
		switch {
		case u.busEye:
			return 1
		case u.busStream:
			return 2
		case u.busVisual:
			return 3
		}
	case wsview.Spectrum:
		switch s.viewMode {
		case viewSpectrogram:
			return 1
		case viewBode:
			return 2
		case viewSound:
			return 3
		}
	case wsview.Measure:
		return choose(u.measAll, 1, 0)
	case wsview.Compare:
	}
	return 0
}

// wsViewLabels are the workspaces' views on their segmented controls.
var wsViewLabels = [wsview.Count][]string{
	{"Y-T", "X-Y"},
	{"Packets", "Eye", "Stream", "Visual"},
	{"FFT", "Spectro", "Bode", "Sound"},
	{"Stats", "All values"},
	nil,
}

// toolRun is one of the knobs' jobs offered as a button: Adjust and
// Horizontal Position turned a step either way or pushed, the S/DIV push.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) toolRun(arg string) {
	switch arg {
	case "adj-":
		c.dispatch(KnobAdjust, -1, 1)
	case "adj+":
		c.dispatch(KnobAdjust, 1, 1)
	case "adjpush":
		c.button(KeyAdjustPush)
	case "pos-":
		c.dispatch(KnobHorizPos, -1, 1)
	case "pos+":
		c.dispatch(KnobHorizPos, 1, 1)
	case "pospush":
		c.button(KeyHorizPosPush)
	case "zoom":
		c.button(KeyTdivPush)
	case "done":
		c.mu.Lock()
		c.ui.tTools = false
		c.mu.Unlock()
	}
}

// fkeyBtns are F-key choices as touch buttons "key:0".. .
// TRLC-LINKS: REQ-DSO-299
func fkeyBtns(keys []fkey) []wsview.Btn {
	out := make([]wsview.Btn, 0, len(keys))
	for i, k := range keys {
		label := k.label
		if k.value != "" && k.value != "-" && len(label)+len(k.value) < 20 {
			label += " " + k.value
		}
		out = append(out, wsview.Btn{ID: "key:" + strconv.Itoa(i), Label: label, Sel: k.sel, Dim: k.dim})
	}
	return out
}

// matchCursorBtns move the match editor's cursor and clear its pattern
// (the Position knob's and its push's jobs there).
var matchCursorBtns = []wsview.Btn{{ID: "mcur:-1", Label: "< cursor"}, {ID: "mcur:1", Label: "cursor >"}, {ID: "mclear", Label: "Clear all"}}

// pickMoveBtns move the byte picker's first byte.
var pickMoveBtns = []wsview.Btn{{ID: "pick:-1", Label: "< byte"}, {ID: "pick:1", Label: "byte >"}}

// touchBar is the touch bar of state s: the workspace's views (COMPARE's
// master), a menu's choices or the knobs' jobs when those are up, and the
// actions.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchBar(s *sv) *wsview.TouchBar {
	u := &s.ui
	b := &wsview.TouchBar{}
	run := wsview.Btn{ID: "act:run", Label: "RUN", Col: wsview.Run}
	if !s.st.Running {
		run.Label, run.Col = "STOP", wsview.Stop
	}
	b.Actions = []wsview.Btn{
		{ID: "act:auto", Label: "AUTO"},
		run,
		{ID: "act:single", Label: "SINGLE", Sel: s.st.Single},
		{ID: "act:tools", Label: "TOOLS", Sel: u.tTools, Dim: u.open || u.wpick},
		{ID: "act:menu", Label: "MENU", Sel: u.open},
	}
	if s.zoom > 1 {
		b.Zoom = fmt.Sprintf("Zoom %dx", s.zoom)
	}
	switch {
	case u.open && u.sub == subMatch: // the match editor's choices and its cursor; it closes with them
		b.Keys, b.Actions = append(fkeyBtns(c.matchKeys()), matchCursorBtns...), nil
		return b
	case u.wpick:
		b.Keys = append(fkeyBtns(c.pickKeys(s)), pickMoveBtns...)
		return b
	case u.tTools && !u.open:
		b.Keys = []wsview.Btn{
			{ID: "tool:adj-", Label: "< Adjust"},
			{ID: "tool:adj+", Label: "Adjust >"},
			{ID: "tool:adjpush", Label: "Adjust push"},
			{ID: "tool:pos-", Label: "< Position"},
			{ID: "tool:pos+", Label: "Position >"},
			{ID: "tool:pospush", Label: "Position push"},
			{ID: "tool:zoom", Label: "Zoom on/off"},
			{ID: "tool:done", Label: "Done", Sel: true},
		}
		return b
	}
	if u.ws == wsview.Compare {
		t := c.cmpAgainText(s)
		b.Views = []wsview.Btn{{ID: "view:0", Label: strings.ToUpper(t[:1]) + t[1:], Sel: s.stacking, Dim: strings.HasPrefix(t, "needs")}}
		return b
	}
	cur := wsViewIndex(s)
	for i, l := range wsViewLabels[u.ws] {
		b.Views = append(b.Views, wsview.Btn{ID: "view:" + strconv.Itoa(i), Label: l, Sel: i == cur})
	}
	return b
}

// touchView is the view of state s under the touch-first layout: the
// workspace as with no sheet open (the scope shows beside the sheet
// panel), the bars, the selection box and the sheet panel.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchView(s *sv, m *render.Metrics) wsview.View {
	u := &s.ui
	open := u.open
	u.open = false
	v := c.keysView(s)
	u.open = open
	v.Layout.Touch, v.Layout.MenuW = true, 0
	v.Rail, v.RailNote, v.Keys = false, nil, nil
	v.Bar = c.touchBar(s)
	if u.tBoxOn {
		b := u.tBox
		v.Box = &b
	}
	if open {
		v.Sheet = c.sheetView(s)
		c.touchSheet(s, m, v.Sheet)
	}
	touchWording(&v)
	return v
}

// touchWords are the F-key phrases of the views' texts and what a finger
// does instead, longest first.
var touchWords = strings.NewReplacer(
	" (F1)", "", " (F2)", "", " (F3)", "", " (F4)", "", " (F5)", "",
	"F5 again cancels", "Cancel stack cancels", "F5 again stacks it", "Stack master stacks it",
	"F5 again", "Stack master", "F5: cancel", "tap: cancel",
	"F1 again", "the view buttons", "F2 again", "the view buttons", "F3 again", "the view buttons", "F4 again", "the view buttons",
	"F1 edit, F2 from screen", "Edit or From screen",
)

// touchPickLines are the byte picker's lines for a finger.
var touchPickLines = []wsview.Span{
	{Text: "| = idle gap (a message starts).  Underlined: the bytes Trig on would add.", Col: wsview.Dim},
	{Text: "< byte and byte >: the first byte.  Trig on 1-4: trigger on 1-4 bytes from it.", Col: wsview.Text},
	{Text: "Cancel: back, the trigger as it was.", Col: wsview.Dim},
}

// touchWording rewrites view v's F-key phrases for the touch layout: the
// views' "F2 again" ends go (the segmented control shows the views), the
// notices and rows name the buttons a finger taps.
// TRLC-LINKS: REQ-DSO-299
func touchWording(v *wsview.View) {
	w := touchWords.Replace
	spans := func(ss []wsview.Span) []wsview.Span { // a copy: the spans may be shared
		out := make([]wsview.Span, len(ss))
		for i, s := range ss {
			out[i] = wsview.Span{Text: w(s.Text), Col: s.Col}
		}
		return out
	}
	pane := func(p *wsview.Pane) {
		p.Again, p.Caption, p.Right = "", w(p.Caption), w(p.Right)
	}
	v.Notice = w(v.Notice)
	if b := v.Bus; b != nil {
		pane(&b.Pane)
		b.Msg = spans(b.Msg)
		b.Hint, b.Empty = w(b.Hint), w(b.Empty)
	}
	if v.Eye != nil {
		pane(&v.Eye.Pane)
	}
	if v.Visual != nil {
		pane(&v.Visual.Pane)
	}
	if v.Spec != nil {
		pane(&v.Spec.Pane)
	}
	if v.Meas != nil {
		pane(&v.Meas.Pane)
	}
	if v.Sound != nil {
		pane(&v.Sound.Pane)
	}
	if cp := v.Cmp; cp != nil {
		pane(&cp.Pane)
		for i := range cp.Rows {
			r := &cp.Rows[i]
			r.Info, r.Extra, r.Action, r.BarNote = w(r.Info), w(r.Extra), w(r.Action), w(r.BarNote)
		}
	}
	if v.Picker != nil {
		v.Picker.Lines = touchPickLines
	}
	if sh := v.Sheet; sh != nil {
		sh.Status = spans(sh.Status)
		for i := range sh.Rows {
			sh.Rows[i].Header = w(sh.Rows[i].Header)
		}
		if sh.Sub != nil {
			sh.Sub.Lines = touchPickLines
		}
	}
}

// touchSheet makes sheet sh the touch panel's: the first row shown (it
// follows the focus when the focus moved), the open chooser, the match
// row's buttons and the byte picker's choices along the foot.
// TRLC-LINKS: REQ-DSO-299
func (c *Controller) touchSheet(s *sv, m *render.Metrics, sh *wsview.Sheet) {
	u := &s.ui
	rows := tabRows(u.tab, s.shape())
	for vi := range sh.Rows {
		if i, ok := sheetRowAt(rows, vi); ok && rows[i].kind == wsview.Bytes {
			for _, k := range c.rowKeys(s, rows[i]) {
				sh.Rows[vi].Acts = append(sh.Rows[vi].Acts, wsview.Act{Text: k.label, Dim: k.dim})
			}
		}
	}
	foot := u.sub == subPick
	if foot {
		sh.Keys = append(fkeyBtns(c.pickKeys(s)), pickMoveBtns...)
	}
	focus := clampInt(u.rowAt[u.tab], 0, len(rows)-1)
	fv := viewRowOf(rows, focus)
	scroll := min(max(u.tScroll[u.tab], 0), max(len(sh.Rows)-1, 0))
	if u.tFocus[u.tab] != focus+1 { // the focus moved (a knob, a tap): show it
		if fv < scroll {
			scroll = max(0, fv-1)
		}
		for scroll < fv && fv >= scroll+render.TouchRowsShown(m, foot, sh.Rows, scroll) {
			scroll++
		}
	}
	c.mu.Lock()
	c.ui.tScroll[u.tab], c.ui.tFocus[u.tab] = scroll, focus+1
	c.mu.Unlock()
	sh.Scroll = scroll
	if u.tChoose != "" {
		if r, opts, _ := c.chooserOpts(s); r != nil {
			sh.Choose = &wsview.Chooser{Title: r.label, Opts: opts, First: u.tChooseFirst}
		}
	}
}
