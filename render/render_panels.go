// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/dsp"
	"github.com/labeth/open-dso-core/measure"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// measFor throttles the full-record auto-measurement to readout rate. The HUD
// bottom line and the MEASURE panel both recomputed measure.Compute (a full
// min/max/sum/sum²/histogram pass over 20480 samples, per channel) at EVERY LCD
// render (~8 Hz) — 11% of the core on the device profile, for a text readout
// that only needs to refresh a few times a second. Within one frame (same Seq +
// params) both surfaces share the identical Result, keeping HUD↔panel agreement
// exact; across frames the previous Result is reused until measRefresh elapses.
// The scale/coupling params key the cache so a knob turn recomputes immediately.
// Seq==0 (hand-built test frames) bypasses the cache entirely. The mutex covers
// the /api/screen.png render running on the HTTP goroutine.
const measRefresh = 250 * time.Millisecond

var (
	measMu    sync.Mutex
	measSlots [MaxChans]struct {
		seq, content uint64
		at           time.Time
		vpc, off, ss float64
		probe        float64
		cpl          int
		m            *measure.Result
	}
)

// TRLC-LINKS: REQ-DSO-021
func measFor(ch int, sig []uint8, q []uint16, guard int, seq, content uint64, vpc, off, ss, probe float64, cpl int) *measure.Result {
	if seq == 0 || ch < 0 || ch >= MaxChans {
		return measure.ComputeAcquisition(sig, q, vpc, off, ss, cpl, guard)
	}
	measMu.Lock()
	defer measMu.Unlock()
	c := &measSlots[ch]
	// The same record re-shown at a new V/div or offset reads the same volts:
	// a knob turn on a deep stopped record must not re-measure it.
	if c.m != nil && content != 0 && c.content == content && c.probe == probe && c.ss == ss && c.cpl == cpl {
		return c.m
	}
	if c.m != nil && c.vpc == vpc && c.off == off && c.ss == ss && c.cpl == cpl {
		if seq == c.seq || time.Since(c.at) < measRefresh {
			return c.m
		}
	}
	m := measure.ComputeAcquisition(sig, q, vpc, off, ss, cpl, guard)
	c.seq, c.content, c.at, c.vpc, c.off, c.ss, c.probe, c.cpl, c.m = seq, content, time.Now(), vpc, off, ss, probe, cpl, m
	return m
}

// timedSlots hold each channel's envelope timing with the measurement it was
// taken on: while measFor answers with the same measurement (the same frame,
// or the readouts' refresh not yet due) the timing is that one's too. Taken
// again for every readout of every picture it was a tenth of the core.
var timedSlots [MaxChans]struct {
	base, timed  *measure.Result
	vpc, off, ss float64
}

// timedFor is take(base), or the one taken on the same base and scale.
// TRLC-LINKS: REQ-DSO-021
func timedFor(ch int, base *measure.Result, cache bool, vpc, off, ss float64, take func(*measure.Result) *measure.Result) *measure.Result {
	if !cache || base == nil || ch < 0 || ch >= MaxChans {
		return take(base)
	}
	measMu.Lock()
	defer measMu.Unlock()
	t := &timedSlots[ch]
	if t.base != base || t.vpc != vpc || t.off != off || t.ss != ss {
		t.base, t.timed, t.vpc, t.off, t.ss = base, take(base), vpc, off, ss
	}
	return t.timed
}

// chanMeasure is one channel's calibrated measurement of the frame, as the
// readouts show it (the probe-scaled V/div, the software coupling, the
// display invert); nil without the channel.
// TRLC-LINKS: REQ-DSO-021
func chanMeasure(f *sample.Frame, hud HUD, ch int) *measure.Result {
	valid := frameValid(f)
	if ch < 0 || ch >= MaxChans || len(f.Codes8(ch)) < valid {
		return nil
	}
	sig := f.Codes8(ch)[:valid]
	c := hud.Ch[ch]
	vdiv, probe, off, cpl := c.VdivV, c.Probe, c.OffV, c.Cpl
	if probe < 1 {
		probe = 1
	}
	if cpl != dsp.CoupleDC { // match the displayed (coupled) trace
		sig = dsp.CoupleDisplay(sig, cpl)
		off = 0
	}
	vpc := vdiv * float64(zoomOf(hud, ch)) / codesPerDiv(f) * probe
	q := frameQ(f, ch)
	if a, b := measure.Range(f); b-a < len(sig) { // a roll view's captured stretch
		sig = sig[a:b]
		if len(q) >= b {
			q = q[a:b]
		} else {
			q = nil
		}
	}
	m := measFor(ch, sig, q, f.Cond.Guard, f.Seq, f.ContentID, vpc, off*probe, hud.SampleS, probe, cpl)
	if f.Content == sample.PeakPairs {
		m = timedFor(ch, m, f.Seq != 0, vpc, off*probe, hud.SampleS, func(m *measure.Result) *measure.Result {
			return m.WithEnvelopeTiming(sig, vpc, off*probe, hud.SampleS).WithEdgeDetail(vpc, off*probe, measure.EdgeWindows(f, ch)...)
		})
	}
	// After the timing: the invert swaps rise and fall and complements the
	// duty, which the envelope's timing would otherwise put back.
	if c.Inv {
		m = m.Inverted() // measure what the inverted trace shows
	}
	return m
}

// measRow is one readout: its short name and value text.
type measRow struct{ name, value string }

// measParamRow reads one parameter ("pkpk", "freq", ...) of a measurement,
// ok false when the measurement has none. The names, the order and the
// percent figures are the MEASURE pane's and the web's, so one parameter reads
// the same everywhere.
// TRLC-LINKS: REQ-DSO-021, REQ-DSO-217
func measParamRow(m *measure.Result, id string) (measRow, bool) {
	pct := func(v float64) string { return strconv.FormatFloat(v, 'g', 3, 64) + "%" }
	switch id {
	case "pkpk":
		return measRow{"Vpp", fmtVolt(m.Vpp)}, true
	case "max":
		return measRow{"Vmax", fmtVolt(m.Vmax)}, true
	case "min":
		return measRow{"Vmin", fmtVolt(m.Vmin)}, true
	case "ampl":
		return measRow{"Vampl", fmtVolt(m.Vampl)}, true
	case "top":
		return measRow{"Vtop", fmtVolt(m.Vtop)}, true
	case "base":
		return measRow{"Vbase", fmtVolt(m.Vbase)}, true
	case "rms": // an envelope of a signal faster than its buckets holds no RMS or mean
		return measRow{"Vrms", fmtVolt(m.Vrms)}, !m.EnvAliased
	case "acrms":
		return measRow{"ACrms", fmtVolt(m.VrmsAC)}, !m.EnvAliased
	case "mean":
		return measRow{"Vmean", fmtVolt(m.Vmean)}, !m.EnvAliased
	case "freq":
		return measRow{"Freq", fmtFreq(m.Freq)}, m.HasTiming
	case "period":
		return measRow{"Period", fmtTdiv(m.Period)}, m.HasTiming
	case "duty":
		return measRow{"Duty", pct(m.Duty)}, m.HasTiming
	case "rise":
		return measRow{"Rise", fmtTdiv(m.RiseS)}, m.RiseS > 0
	case "fall":
		return measRow{"Fall", fmtTdiv(m.FallS)}, m.FallS > 0
	case "pwid":
		return measRow{"+Width", fmtTdiv(m.PosWidthS)}, m.PosWidthS > 0
	case "nwid":
		return measRow{"-Width", fmtTdiv(m.NegWidthS)}, m.NegWidthS > 0
	case "ovsh": // an envelope alone holds no edge shape (the MEASURE pane's "---")
		return measRow{"Overshoot", pct(m.Overshoot)}, m.EdgeShape || m.HasTiming && !m.EnvTiming
	case "prsh":
		return measRow{"Preshoot", pct(m.Preshoot)}, m.EdgeShape || m.HasTiming && !m.EnvTiming
	}
	return measRow{}, false
}

// allMeasParams is every parameter, in the readout box's order: the web's.
var allMeasParams = []string{
	"pkpk", "max", "min", "mean", "rms", "freq", "period", "duty",
	"acrms", "top", "base", "ampl", "rise", "fall", "pwid", "nwid", "ovsh", "prsh",
}

// drawMeasPanel draws the MEASURE readout boxes: per shown channel, a box of
// its chosen parameters (every parameter when nothing is chosen), via the
// shared measure package so the values match the web exactly.
// TRLC-LINKS: REQ-DSO-021, REQ-DSO-217
func drawMeasPanel(sf Canvas, p plotArea, f *sample.Frame, hud HUD) {
	if f == nil || f.Len() == 0 {
		return
	}
	x := 4
	for ch := range hud.numChans() {
		if !hud.shown(ch) {
			continue
		}
		params := allMeasParams
		if hud.MeasItems != nil {
			params = nil
			for _, it := range hud.MeasItems {
				if len(it) >= 2 && it[0] == byte('1'+ch) && it[1] == ':' { // "<ch>:<param>"
					params = append(params, it[2:])
				}
			}
			if len(params) == 0 {
				continue
			}
		}
		if measBox(sf, p, f, hud, ch, x, params) {
			x += 116
		}
	}
}

// measBox renders one channel's readouts at left edge x.
// TRLC-LINKS: REQ-DSO-021
func measBox(sf Canvas, p plotArea, f *sample.Frame, hud HUD, ch, x int, params []string) bool {
	m := chanMeasure(f, hud, ch)
	if m == nil {
		return false
	}
	rows := make([]measRow, 0, len(params))
	for _, id := range params {
		r, ok := measParamRow(m, id)
		if !ok {
			if hud.MeasItems == nil {
				continue // the full box lists only what the signal has
			}
			r.value = "---"
		}
		rows = append(rows, r)
	}
	col, name := chanColor(ch), chanName(ch)
	y0, w := p.y0+22, 112
	h := 14 + len(rows)*10
	fillRect(sf, x, y0, w, h, colBoxBg)
	vline(sf, x, y0, y0+h-1, colGrid)
	vline(sf, x+w-1, y0, y0+h-1, colGrid)
	DrawText(sf, x+3, y0+2, name+" MEAS", col, 1)
	for i, r := range rows {
		yy := y0 + 12 + i*10
		DrawText(sf, x+3, yy, r.name, colInfo, 1)
		DrawTextRight(sf, x+w-3, yy, r.value, colInfo, 1)
	}
	return true
}

// edgeArrow draws a small filled arrow with its tip at (x, y), pointing
// along (dx, dy): one of left, right, up, down.
// TRLC-LINKS: REQ-DSO-021, REQ-DSO-217
func edgeArrow(sf Canvas, x, y, dx, dy int, c Color) {
	for d := range 7 {
		for w := -d / 2; w <= d/2; w++ {
			sf.SetPixel(x-dx*d+w*absI(dy), y-dy*d+w*absI(dx), c)
		}
	}
}

// drawCursors draws the on-screen X (time) or Y (volts) cursor pair and a Δ
// readout. Time Δ uses the labelled t/div × 10 divisions; volts Δ uses each
// shown channel's probe-scaled V/div × 8 divisions.
// TRLC-LINKS: REQ-DSO-021
func drawCursors(sf Canvas, p plotArea, hud HUD) {
	if !hud.CurOn {
		return
	}
	dash := func(vertical bool, at int, active bool) {
		// The inactive cursor is dim, not the grid colour: in colGrid it
		// vanishes into the graticule.
		col := colDim
		if active {
			col = colTrig
		}
		if vertical {
			for y := p.top; y <= p.bot; y += 2 {
				sf.SetPixel(at, y, col)
			}
		} else {
			for x := 0; x < p.w; x += 2 {
				sf.SetPixel(x, at, col)
			}
		}
	}
	// A cursor stays on its signal time or level: beyond the screen it shows
	// as an arrow at the side it is beyond, with its letter.
	off := func(vertical bool, f float64, k int) bool {
		col := colDim
		if hud.CurSel == k {
			col = colTrig
		}
		lab := string(rune('A' + k)) //nolint:gosec // k is a cursor index (0 or 1)
		switch {
		case vertical && f < 0:
			y := p.top + 30 + 14*k
			edgeArrow(sf, 0, y, -1, 0, col)
			DrawText(sf, 9, y-3, lab, col, 1)
		case vertical && f > 1:
			y := p.top + 30 + 14*k
			edgeArrow(sf, p.w-1, y, 1, 0, col)
			DrawText(sf, p.w-15, y-3, lab, col, 1)
		case !vertical && f < 0:
			x := p.w/2 + 60 + 20*k
			edgeArrow(sf, x, p.y0, 0, -1, col)
			DrawText(sf, x+8, p.y0+2, lab, col, 1)
		case !vertical && f > 1:
			x, y := p.w/2+60+20*k, min(p.last(), p.m.H-12) // above the bottom readout row
			edgeArrow(sf, x, y, 0, 1, col)
			DrawText(sf, x+8, y-8, lab, col, 1)
		default:
			return false
		}
		return true
	}
	var label string
	if hud.CurType == 0 { // X (time) cursors
		for k := range 2 {
			if !off(true, hud.CurX[k], k) {
				dash(true, int(hud.CurX[k]*float64(p.w-1)), hud.CurSel == k)
			}
		}
		label = timeCursorLabel(hud, p.m.DivsH)
	} else { // Y (volts) cursors — report ΔV for BOTH channels (web parity)
		for k := range 2 {
			if !off(false, hud.CurY[k], k) {
				dash(false, p.top+int(hud.CurY[k]*float64(p.bot-p.top)), hud.CurSel == k)
			}
		}
		label = voltCursorLabel(hud, p.m.DivsV)
	}
	w := len(label)*6 + 6
	x := p.w/2 - w/2 // top-centre, clear of the MEAS boxes (left) and the right column
	if hud.ShowMeas && !hud.UI.Layout.HideMeas {
		boxes := 0
		for ch := range hud.numChans() {
			if hud.shown(ch) {
				boxes++
			}
		}
		x = max(x, 4+boxes*116+4) // more boxes than the left half holds
	}
	fillRect(sf, x, p.y0+22, w, 12, colBoxBg)
	DrawText(sf, x+3, p.y0+24, label, colTrig, 1)
}

// timeCursorLabel is the X cursors' readout: their time apart, and its
// frequency.
// TRLC-LINKS: REQ-DSO-021
func timeCursorLabel(hud HUD, divs int) string {
	span := hud.TdivS * float64(divs)
	if hud.Zoom > 1 {
		span /= float64(hud.Zoom)
	}
	dt := math.Abs(hud.CurX[0]-hud.CurX[1]) * span
	label := "dt " + fmtTdiv(dt)
	if dt > 0 {
		label += "  1/dt " + fmtFreq(1/dt)
	}
	return label
}

// voltCursorLabel is the Y cursors' readout: their voltage apart on each
// shown channel (web parity).
// TRLC-LINKS: REQ-DSO-021
func voltCursorLabel(hud HUD, divs int) string {
	var label string
	frac := math.Abs(hud.CurY[0] - hud.CurY[1])
	for ch := range hud.numChans() {
		if !hud.shown(ch) {
			continue
		}
		if label != "" {
			label += "  "
		}
		probe := max(hud.Ch[ch].Probe, 1)
		label += "dV" + strconv.Itoa(ch+1) + " " + fmtVolt(frac*hud.Ch[ch].VdivV*probe*float64(divs)) // the plot's vertical divisions
	}
	return label
}

// drawAutosetBanner overlays a centred "AUTOSET…" progress banner while the
// sweep runs, with the cancel hint (a second AUTO press stops it).
// TRLC-LINKS: REQ-DSO-021
func drawAutosetBanner(sf Canvas, p plotArea, msg string) {
	if msg == "" {
		msg = "AUTOSET..."
	}
	const bw, bh = 300, 44
	x0, y0 := (p.w-bw)/2, (p.m.H-bh)/2
	fillRect(sf, x0, y0, bw, bh, colMenuBg)
	strokeRect(sf, x0, y0, bw, bh, colTrig)
	DrawText(sf, x0+(bw-TextWidth(msg, 2))/2, y0+6, msg, colTrig, 2)
	// Only the working banner is cancelable; a result note (e.g. "no signal")
	// just clears on its own.
	hint := "AUTO again to cancel"
	if msg != "AUTOSET..." {
		hint = "check the probe / scale"
	}
	DrawText(sf, x0+(bw-TextWidth(hint, 1))/2, y0+30, hint, colDim, 1)
}

// drawNotice is a short notice over the plot: what a tool just did (a
// workspace's entry behaviour, a zoom, PRINT's saved file name).
// TRLC-LINKS: REQ-DSO-021, REQ-DSO-211
func drawNotice(sf Canvas, p plotArea, msg string) {
	maxC := (p.w - 40) / 6
	if len(msg) > maxC {
		msg = msg[:maxC-2] + ".."
	}
	bw, bh := TextWidth(msg, 1)+16, 20
	x0, y0 := (p.w-bw)/2, (p.m.H-bh)/2
	fillRect(sf, x0, y0, bw, bh, colMenuBg)
	strokeRect(sf, x0, y0, bw, bh, colInfo)
	DrawText(sf, x0+8, y0+6, msg, colInfo, 1)
}

// drawRemote is the one-line note of a change made from the phone remote,
// at the top of the plot so it does not cover the trace's middle.
// TRLC-LINKS: REQ-DSO-212
func drawRemote(sf Canvas, p plotArea, msg string) {
	maxC := (p.w - 40) / 6
	if len(msg) > maxC {
		msg = msg[:maxC-2] + ".."
	}
	bw, bh := TextWidth(msg, 1)+12, 14
	x0, y0 := (p.w-bw)/2, p.y0+36
	fillRect(sf, x0, y0, bw, bh, colMenuBg)
	strokeRect(sf, x0, y0, bw, bh, colSR)
	DrawText(sf, x0+6, y0+4, msg, colSR, 1)
}

// drawMarkers overlays the trigger level (horizontal line + right handle), the
// trigger position (top pointer), and the per-channel ground/offset arrows on
// the left edge — the same references the web canvas shows (spec 07 §6).
// TRLC-LINKS: REQ-DSO-021
func drawMarkers(sf Canvas, p plotArea, f *sample.Frame, hud HUD) {
	px := func(x, y int, c Color) {
		if x >= 0 && x < p.w && y >= p.y0 && y < p.m.H {
			sf.SetPixel(x, y, c)
		}
	}
	// Trigger LEVEL: dashed horizontal line + right-edge arrow at the level code.
	ly := p.sy(sample.Mid8 + hud.TrigLvlDiv*p.cpd)
	if ly >= p.y0+2 && ly < p.m.H {
		for x := 0; x < p.w; x += 7 {
			px(x, ly, colTrig)
			px(x+1, ly, colTrig)
		}
		for dy := -4; dy <= 4; dy++ {
			for dx := range 5 - absI(dy) {
				px(p.w-1-dx, ly+dy, colTrig)
			}
		}
	}
	// Trigger POSITION: downward pointer at the top (none while a zoom's
	// window leaves the trigger off the screen).
	tx := trigX(p, f, hud)
	for dy := 0; dy <= 7 && tx >= 0; dy++ {
		hw := (7 - dy) / 2
		for dx := -hw; dx <= hw; dx++ {
			px(tx+dx, p.y0+dy+2, colTrig)
		}
	}
	// Per-channel GROUND (0 V) arrows on the left edge: code = Mid8 + offV·cpd/Vdiv.
	ground := func(vdiv, offV float64, col Color) {
		if vdiv <= 0 {
			return
		}
		gy := p.sy(sample.Mid8 + offV*p.cpd/vdiv)
		for dx := range 7 {
			for dy := -(6 - dx); dy <= 6-dx; dy++ {
				px(dx, gy+dy, col)
			}
		}
	}
	for ch := range hud.numChans() {
		ground(hud.Ch[ch].VdivV, hud.Ch[ch].OffV, chanColor(ch))
	}
}

// trigX is the trigger position's column, -1 when the screen does not show
// it (trigFrac).
// TRLC-LINKS: REQ-DSO-021
func trigX(p plotArea, f *sample.Frame, hud HUD) int {
	frac, ok := trigFrac(f, hud)
	if !ok {
		return -1
	}
	return min(int(frac*float64(p.w)), p.w-1)
}

// trigFrac is where across the screen the trigger shows: the trigger
// position, or zoomed, where the trace's window (traceWindow, centred on the
// trigger plus the pan) puts it; false when that is off the screen.
// TRLC-LINKS: REQ-DSO-021
func trigFrac(f *sample.Frame, hud HUD) (float64, bool) {
	frac := hud.TrigPosFrac
	if !(frac >= 0 && frac <= 1) {
		frac = 0.5
	}
	if hud.Zoom > 1 && f != nil && f.Codes8Len() > 0 {
		valid := frameValid(f)
		win, xc, posFrac := traceWindow(f, hud, valid)
		anchor := f.TriggerAt
		if anchor < 0 {
			anchor = float64(valid) / 2
		}
		frac = (anchor - (xc - float64(win)*posFrac)) / float64(win)
	}
	return frac, frac >= 0 && frac <= 1
}

// TRLC-LINKS: REQ-DSO-021
func drawHUD(sf Canvas, f *sample.Frame, hud HUD) {
	sm := metricsOf(sf)
	// The top bar: the channels' labels a ChanLabelDX apart (room for two at
	// least), then the timebase (x tx), its zoom tag and the math legend, the
	// trigger readout right-aligned.
	n := hud.numChans()
	tx := topBarTimeX(sm, hud)
	touch := hud.UI.Layout.Touch // the readout bar above carries the top bar's readouts
	if f != nil && f.Cond.NoiseGain > 1 {
		label := fmt.Sprintf("Ideal +%.2f bits (not ENOB)", math.Log2(f.Cond.NoiseGain))
		if f.Cond.PassbandHz > 0 {
			label += "  pass " + fmtFreq(f.Cond.PassbandHz) + fmt.Sprintf("  FPGA /%d", f.AcqDecim)
		}
		DrawText(sf, tx, 14, label, colInfo, 1)
	}
	if touch {
		drawBottomReadouts(sf, f, hud)
		return
	}
	for ch := range n {
		c := hud.Ch[ch]
		DrawText(sf, 4+ch*sm.ChanLabelDX, 2, chanName(ch)+" "+vdivLabel(c.VdivV, c.Probe)+cplTag(c.Cpl), chanColor(ch), 1)
	}
	if f != nil && f.Len() > 0 { // clipping warning — railed traces make readings suspect
		clip := frameClipped(f)
		for ch := range n {
			if clip[ch] {
				DrawText(sf, 4+ch*sm.ChanLabelDX, 14, "CLIP", colTrig, 1)
			}
		}
	}
	// The horizontal axis is time only in Y-T; in FFT it's frequency and in X-Y
	// it's C1 voltage, so "M <t/div>" would be misleading there. But X-Y/FFT
	// fall through to Y-T on an envelope frame, so only label them when the
	// alternate view actually renders (matches the Render() branch condition).
	altView := f != nil && !f.Flags.Has(sample.Envelope) && f.Codes8Len() > 0
	switch {
	case hud.ViewMode == 1 && altView:
		DrawText(sf, tx, 2, "X-Y", colMath, 1)
	case hud.ViewMode == 2 && altView:
		DrawText(sf, tx, 2, "FFT", colMath, 1)
	case hud.ViewMode == 3:
		DrawText(sf, tx, 2, "BODE", colMath, 1)
	case hud.ViewMode == 4:
		DrawText(sf, tx, 2, "SPGM", colMath, 1)
	default:
		DrawText(sf, tx, 2, "M "+fmtTdiv(hud.TdivS), colInfo, 1)
		if hud.Zoom > 1 { // horizontal magnification tag
			DrawText(sf, tx+62, 2, fmt.Sprintf("x%d", hud.Zoom), colTrig, 1) // "Z50x" read as 250x
		}
	}
	// Math legend so the purple trace is identified (and not confused with a ref).
	if hud.MathMode != 0 && hud.ViewMode == 0 {
		if name := mathName(hud); name != "" {
			DrawText(sf, tx+100, 2, "M:"+name, colMath, 1)
		}
	}

	DrawTextRight(sf, sm.W-4, 2, trigReadout(hud), colTrig, 1)

	// Device URL (post-takeover support: "what do I browse to?"). Right-aligned
	// on the top bar in the dead zone between the math legend (ends ≤ ~345) and
	// the trigger readout (starts ≥ ~670) — clear of the trace band, readouts
	// and the menu panel in every view. No network → nothing.
	DrawStatusSlot(sf, hud)
	drawBottomReadouts(sf, f, hud)
}

// drawBottomReadouts is the bottom quick readouts: each shown channel's
// peak-to-peak and frequency.
// TRLC-LINKS: REQ-DSO-021
func drawBottomReadouts(sf Canvas, f *sample.Frame, hud HUD) {
	sm := metricsOf(sf)
	n := hud.numChans()
	if f == nil || f.Len() == 0 || hud.UI.Layout.HideBottom {
		return
	}
	// Bottom quick-readout: calibrated Vpp + frequency per channel, via the
	// shared measure package (real probe-scaled V/div + software coupling) so it
	// agrees with the MEASURE readouts and the web.
	for ch := range n {
		if !hud.shown(ch) { // a hidden channel has no readout
			continue
		}
		m := chanMeasure(f, hud, ch)
		if m == nil {
			continue
		}
		col, name, x := chanColor(ch), chanName(ch), 4+ch*sm.ReadoutDX
		s := name + " Vpp " + fmtVolt(m.Vpp)
		if m.HasTiming {
			s += "  f " + fmtFreq(m.Freq)
		}
		DrawText(sf, x, sm.H-9, s, col, 1)
	}
}

// topBarTimeX is where the top bar's timebase starts: after the channels'
// labels, room for two at least.
// TRLC-LINKS: REQ-DSO-021
func topBarTimeX(sm *Metrics, hud HUD) int { return 4 + max(hud.numChans(), 2)*sm.ChanLabelDX + 12 }

// trigReadout is the top bar's trigger readout: source, edge, level (or the
// qualified trigger's tag) and the acquisition state.
// TRLC-LINKS: REQ-DSO-021
func trigReadout(hud HUD) string {
	what, state := trigParts(hud)
	return "T " + what + " " + state
}

// stateStop is the trigger readout's state while the acquisition is
// stopped.
const stateStop = "STOP"

// trigParts are the trigger readout's two parts: what triggers ("C1 ^
// 200mV", or the qualified trigger's tag) and the acquisition state.
// TRLC-LINKS: REQ-DSO-021
func trigParts(hud HUD) (what, state string) {
	edge := "^"
	if !hud.TrigRising {
		edge = "v"
	}
	state = "AUTO"
	if hud.Norm {
		state = "NORM"
	}
	switch {
	case hud.Single && hud.Running:
		state = "SNGL" // armed, waiting for the single trigger
	case !hud.Running && hud.Single:
		state = stateStop // (should not persist: single clears on capture)
	case !hud.Running:
		state = stateStop
	case hud.Roll:
		state = "ROLL" // untriggered scrolling view (slow timebase)
	case hud.Waiting:
		state = "WAIT" // the held frame's T'D is stale
	case hud.Trigd:
		state = "T'D"
	case hud.Norm:
		state = "WAIT"
	}
	if tag := trigTag(hud); tag != "" {
		return tag, state
	}
	return fmt.Sprintf("C%d %s %s", hud.TrigSrc+1, edge, fmtVolt(hud.TrigLvlV)), state
}

// TRLC-LINKS: REQ-DSO-021
func frameQ(f *sample.Frame, ch int) []uint16 { return f.Precision(ch) }

// zoomOf is a channel's display zoom: codes sit on the analog range, a
// zoomed detent's volts per code is VdivV·zoom over its codes a division.
// TRLC-LINKS: REQ-DSO-021
func zoomOf(hud HUD, ch int) int { return max(hud.Ch[ch].Zoom, 1) }

// clipCache holds the last published frame's rail state: scanning a stopped
// 1 M-sample record on every repaint takes as long as drawing it. A published
// frame's Seq is unique; Seq 0 is never cached.
var clipCache struct {
	sync.Mutex
	seq   uint64
	valid int
	clip  [MaxChans]bool
}

// frameClipped reports whether each channel's codes touch a rail.
// TRLC-LINKS: REQ-DSO-021
func frameClipped(f *sample.Frame) [MaxChans]bool {
	valid := frameValid(f) // clamp to len(C1) — f.Valid can exceed the slice
	clipCache.Lock()
	defer clipCache.Unlock()
	if f.Seq != 0 && clipCache.seq == f.Seq && clipCache.valid == valid {
		return clipCache.clip
	}
	var clip [MaxChans]bool
	for ch := range min(len(f.Traces), MaxChans) {
		clip[ch] = len(f.Codes8(ch)) >= valid && measure.Clipped(f.Codes8(ch)[:valid])
	}
	clipCache.seq, clipCache.valid, clipCache.clip = f.Seq, valid, clip
	return clip
}

// DrawStatusSlot draws the top bar's status slot (between the math legend and
// the trigger readout): the loading indicator while a frame is being read or
// processed, else the device URL.
// TRLC-LINKS: REQ-DSO-168
func DrawStatusSlot(sf Canvas, hud HUD) {
	if txt := statusSlotText(hud); txt != "" {
		c := colDim
		if hud.Busy != "" {
			c = colTrig
		}
		DrawTextRight(sf, metricsOf(sf).StatusSlotX, 2, txt, c, 1)
	}
}

// RedrawStatusSlot clears the slot (as wide as its widest text) and draws it
// again, so the LCD loop can turn the spinner without re-rendering a deep
// frame — which would steal the CPU from the very readout being shown.
// TRLC-LINKS: REQ-DSO-168
func RedrawStatusSlot(sf Canvas, hud HUD) {
	idle := hud
	idle.Busy = ""
	busy := hud
	busy.Busy, busy.BusyPct = "x", 100
	w := max(TextWidth(statusSlotText(idle), 1), TextWidth(statusSlotText(busy), 1))
	fillRect(sf, metricsOf(sf).StatusSlotX-w, 0, w, StatusSlotRows, colBG)
	DrawStatusSlot(sf, hud)
}

// TRLC-LINKS: REQ-DSO-168
func statusSlotText(hud HUD) string {
	if hud.Busy == "" {
		return hud.URL
	}
	spin := `|/-\`[hud.BusyPhase&3 : hud.BusyPhase&3+1]
	if hud.BusyPct >= 0 {
		return fmt.Sprintf("LOADING %2.0f%% %s", hud.BusyPct, spin)
	}
	return "LOADING " + spin
}

// colorOf maps a wsview colour to the palette.
// TRLC-LINKS: REQ-DSO-211
func colorOf(c wsview.Col) Color {
	switch c {
	case wsview.Dim:
		return colDim
	case wsview.Amber:
		return colTrig
	case wsview.Stop:
		return colStale
	case wsview.Run:
		return colOK
	case wsview.Warn:
		return colWarn
	case wsview.C1:
		return colC1
	case wsview.C2:
		return colC2
	case wsview.C3:
		return chanColor(2)
	case wsview.C4:
		return chanColor(3)
	case wsview.Math:
		return colMath
	case wsview.SR:
		return colSR
	case wsview.Mask:
		return colMask
	case wsview.Screen:
		return colBG
	case wsview.Cursor:
		return colCursor
	case wsview.Text:
	}
	return colInfo
}

// trigTag names a trigger that is no plain edge for the readout: the serial
// match, the pulse or slope condition, the video line, a zone trigger ("" for
// an edge). Without it the readout said "T C1 ^ 1.37V" on the owner's serial
// trigger on AA, as for any edge.
// TRLC-LINKS: REQ-DSO-021
func trigTag(hud HUD) string {
	cond := func(c int, minNs, maxNs float64) string {
		switch c {
		case 1:
			return "<" + fmtTdiv(minNs*1e-9)
		case 2:
			return ">" + fmtTdiv(maxNs*1e-9)
		case 3:
			return fmtTdiv(minNs*1e-9) + "-" + fmtTdiv(maxNs*1e-9)
		}
		return "any"
	}
	src := fmt.Sprintf(" C%d", hud.TrigSrc+1)
	q := hud.TrigQual
	switch {
	case hud.SerialArmed:
		m := hud.SerialMatch
		if len(m) > 11 {
			m = m[:10] + ".."
		}
		if m == "" {
			m = "any"
		}
		return "SER " + m
	case hud.ZoneTrig:
		return "ZONE" + src
	case hud.TrigType == 1:
		return "PULSE " + cond(q.PulseCond, q.PulseMinNs, q.PulseMaxNs) + src
	case hud.TrigType == 2:
		return "SLOPE " + cond(q.SlopeCond, q.SlopeMinNs, q.SlopeMaxNs) + src
	case hud.TrigType == 3:
		return fmt.Sprintf("VIDEO L%d%s", q.VideoLine, src)
	}
	return ""
}
