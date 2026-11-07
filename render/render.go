// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/labeth/open-dso-core/dsp"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/streamwire"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// Colour palette (spec 07 §2.2). The col* variables in palette.go are tokens
// of open-dso-core/design's shared palette, the single source of truth for the
// web UI too, so the two surfaces cannot diverge (they did: the trigger colour
// was red on web but green here). Edit open-dso-core/design/tokens.json and run
// `go generate ./design` there.

// HUD is the UI-state snapshot the overlay renders alongside the frozen
// frame (spec 07 §6). It carries no capture state.
// TRLC-LINKS: REQ-SDS-021
type HUD struct {
	// Ch are the channels' settings, C1 first; NumChans of them exist (0 or
	// 1: C1 alone).
	Ch         [MaxChans]Channel
	NumChans   int
	TdivS      float64
	TrigSrc    int
	TrigRising bool
	TrigLvlDiv float64
	TrigLvlV   float64 // trigger level at the probe tip, volts
	// The trigger the FPGA decides when it is no plain edge: its type
	// (engine Stats.TrigType), the qualifier's figures, a serial match armed
	// (its units as text) and a zone trigger; the readout names it.
	TrigType      int
	TrigQual      TrigQual
	SerialArmed   bool
	SerialMatch   string
	ZoneTrig      bool
	Waiting       bool            // a trigger the FPGA decides armed, running, nothing published lately
	WaitS         float64         // seconds since the last frame came (running)
	StreamOn      bool            // DECODE Auto in Stream mode
	Stream        streamwire.View // its state and counters
	StreamList    bool            // the transcript fills the screen
	StreamPage    streamwire.Page // the transcript lines to show
	Running, Norm bool
	RecordS       float64 // the capture in progress fills this long a record (0 unknown)
	Single        bool
	Trigd         bool
	SampleS       float64 // per-sample seconds (frequency readout)
	TrigPosFrac   float64 // horizontal trigger position, 0.5 = centre
	// ChanExplicit marks the channels' Show as the user's choice (the panel
	// set them), so all off draws none. Without it, all off means "unset"
	// and every channel is drawn.
	ChanExplicit bool
	ShowMeas     bool // on-device MEASURE readout boxes
	// MeasItems are the chosen readouts, "1:pkpk" (channel and parameter);
	// nil shows every parameter of each shown channel.
	MeasItems        []string
	ViewMode         int       // 0=Y-T 1=X-Y 2=FFT 3=BODE 4=SPECTROGRAM
	MathMode         int       // 0 = off, 1 = C1+C2, 2 = C1-C2, 3 = C2-C1, 4 = C1×C2
	FFTSrc           int       // the FFT source channel (drawn on top; the SPECTRUM marker's)
	AutosetBusy      bool      // autoset sweep running → show a cancelable banner
	AutosetMsg       string    // banner text while AutosetBusy
	Notice           string    // a short notice (PRINT, workspace tools), "" when none
	Remote           string    // a remote change's one-line note, "" when none
	Match            MatchView // serial trigger match editor (full screen while open)
	Busy             string    // a frame is being read/processed (loading indicator); "" idle
	BusyPct          float64   // its progress 0..100, <0 unknown
	BusyPhase        int       // spinner step, advanced by the caller while busy
	ViewPending      bool      // stopped: the shown frame is about to be replaced at the new view
	Roll             bool      // the live view is rolling (slow timebase, AUTO)
	Zoom             int       // horizontal magnification (1 = none)
	ZoomOff          float64   // zoom-window pan offset (fraction of the record)
	Persist          bool      // display persistence (afterglow)
	DecProto         int       // framecalc's numbers: 0=off,2=UART,3=I2C,4=SPI ... 11=FlexRay, 12=LIN ... 31=Wiegand ... 53=IR multi (1, Auto, is a detection, never decoded)
	DecBaud          int
	DecChA, DecChB   int // channel roles (0=C1,1=C2)
	DecCPOL, DecCPHA bool
	DecLSB           bool          // SPI least significant bit first
	DecParity        string        // Modbus RTU's framing ("": found by the decoder)
	DecDelayNs       float64       // SPI's data delay in use after the sampling edge (ns)
	DecFormat        int           // byte display: 0=hex,1=ascii,2=both
	RefC1, RefC2     [2][]uint8    // saved reference waveforms (REF A/B), one code per column; nil if unset
	RefVdiv, RefOff  [2][2]float64 // [slot][channel] analog V/div and offset volts at the save
	RefProbe         [2][2]float64 // [slot][channel] the probe factor at the save (0: 1)
	RefShow          [2]bool

	// On-screen cursors: two X (time) and two Y (volts), positions as screen
	// fractions. CurType 0=X 1=Y; CurSel 0=A 1=B (the active one is highlighted).
	CurOn   bool
	CurType int
	CurSel  int
	CurX    [2]float64
	CurY    [2]float64

	// URL is the device's control-page address ("http://<ip>:8080", from
	// the board) shown on the top bar so a taken-over scope tells the user
	// where its web UI lives. Empty (no network) renders nothing.
	URL string

	// UI is the task-workspace UI (ADR-TASK-WORKSPACES): the layout, the
	// pictogram rail, the workspace's pane, a settings sheet or the byte
	// picker. The zero View draws no UI over a full-screen plot.
	UI wsview.View

	// Super-res stack-and-crunch overlay (reference-locked). SRActive shows the
	// status HUD; SRFocus 3 (review) replaces the live trace with the stacked mean;
	// SRFocus 1/2 overlay the gate markers (active edge highlighted) for editing.
	SRActive  bool
	SRFocus   int // 0=watch, 1=gate-start, 2=gate-end, 3=review
	SRStatus  string
	SRBits    float64
	SRMean    []float32 // align-channel review trace on the L·K fine grid; -1 = gap (nil unless review)
	SRMean2   []float32 // the OTHER channel's review trace (stacked X-Y / dual FFT); -1 = gap
	SRAlign   int       // which physical channel SRMean is (0=C1,1=C2); SRMean2 is the other
	SRSampleS float64   // the stack's raw per-sample seconds (fine dt = SRSampleS/SRk)
	SRk       int
	SRGateLo  int
	SRGateHi  int
	SRN       int
	SRWinLo   int // the selected span the review renders — the frozen view, unchanged
	SRWinHi   int
	SRPeriod  int // >0: the stack is one period; tile it across the span

	// Zone trigger + mask testing overlay (engine-side capture qualification;
	// parity with the web card). Zones render edge-anchored; the mask renders
	// as its envelope boundary lines when the window geometry matches.
	ZoneMode       int
	Zones          []Zone
	MaskMode       int // 0 off, 1 test, 2 stop-on-fail
	MaskLo, MaskHi []uint8
	MaskWin        int
	MaskPass       int64
	MaskFail       int64
	MaskSkip       int64
	MaskStopped    bool
	MaskMsg        string // panel build/status line ("" when idle)
	ZoneSkip       int64  // zone-armed publishes that were untestable (env/roll)
	// The mask's timebase and sample interval, to draw it through time over a
	// frame sampled otherwise.
	MaskTdivS, MaskSampleS float64

	// FRA / Bode plot (bode.go): the accumulated response curve, rendered when
	// ViewMode == 3. Parallel arrays sorted ascending by frequency.
	BodeFreq  []float64
	BodeGain  []float64
	BodePhase []float64
	// BodeRef, BodeDut are the response's reference and DUT channels (0 = C1).
	BodeRef, BodeDut int

	// Spectrogram ("FFT over time", spectrogram.go): the scrolling waterfall
	// image, accumulated by the LCD loop and blitted when ViewMode == 4.
	Spect *Spectrogram
}

// MaxChans is the most channels a HUD describes.
const MaxChans = 8

// Channel is one channel's display settings.
type Channel struct {
	VdivV float64
	// Zoom magnifies the channel's codes about mid-scale: the SDS1000CML+'s
	// 2 and 5 mV detents are ×5/×2 display zooms of the 10 mV analog range
	// (0 means 1).
	Zoom  int
	Probe float64 // probe attenuation (1/10/100); 0 treated as ×1
	Cpl   int     // input coupling (dsp.CoupleDC/CoupleAC/CoupleGND)
	Inv   bool    // display-level trace invert (SCPI Cn:INVS)
	OffV  float64 // applied vertical offset volts (ground markers)
	Show  bool    // display enable
}

// TrigQual is the pulse and slope qualifiers the trigger readout names
// (levels as fractions of the span, widths and times in ns, conditions
// any/<min/>max/in): the engine's own report, field for field.
type TrigQual struct {
	VideoStd   int     `json:"video_std"`
	VideoLine  int     `json:"video_line"`
	VideoNeg   bool    `json:"video_neg"`
	PulseLvl   float64 `json:"pulse_lvl"`
	PulseMinNs float64 `json:"pulse_min_ns"`
	PulseMaxNs float64 `json:"pulse_max_ns"`
	PulseCond  int     `json:"pulse_cond"`
	SlopeLo    float64 `json:"slope_lo"`
	SlopeHi    float64 `json:"slope_hi"`
	SlopeMinNs float64 `json:"slope_min_ns"`
	SlopeMaxNs float64 `json:"slope_max_ns"`
	SlopeCond  int     `json:"slope_cond"`
}

// Zone is one qualification rectangle the zone overlay draws. Times are
// seconds relative to the trigger edge; codes are display codes (0..255).
type Zone struct {
	DtLoS, DtHiS   float64
	CodeLo, CodeHi int
	Avoid          bool // true: the frame must not enter the zone
	Ch             int  // 0 = C1, 1 = C2
}

// drawGraticule draws the plot's 10 x 8 divisions, centre axes brighter.
// TRLC-LINKS: REQ-SDS-021
func drawGraticule(sf Canvas, p plotArea) {
	for c := range p.m.DivsH + 1 {
		x := p.gridX(c)
		col := colGrid
		if c == p.m.DivsH/2 {
			col = colAxis
		}
		vline(sf, x, p.y0, p.last(), col)
	}
	for r := range p.m.DivsV + 1 {
		y := p.gridY(r)
		col := colGrid
		if r == p.m.DivsV/2 {
			col = colAxis
		}
		hline(sf, 0, p.w-1, y, col)
	}
}

// hline draws row y from x0 to x1 inclusive.
// TRLC-LINKS: REQ-SDS-021
func hline(sf Canvas, x0, x1, y int, c Color) {
	if fillSpan(sf, x0, y, x1+1, y+1, c) {
		return
	}
	for x := x0; x <= x1; x++ {
		sf.SetPixel(x, y, c)
	}
}

// drawLine is a Bresenham segment (spec 07 §3.5).
// TRLC-LINKS: REQ-SDS-021
func drawLine(sf Canvas, x0, y0, x1, y1 int, c Color) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	// On screen end to end: write the pixels without the interface call and
	// per-pixel clip (a sparse trace is one segment per column).
	if sm := sf.Metrics(); uint(x0) < uint(sm.W) && uint(x1) < uint(sm.W) && uint(y0) < uint(sm.H) && uint(y1) < uint(sm.H) {
		switch m := sf.(type) {
		case *Image565:
			linePx(m.pix, sm.W, x0, y0, x1, y1, pack565(c))
			return
		case *Image32:
			linePx(m.pix, sm.W, x0, y0, x1, y1, m.native(c))
			return
		}
	}
	for {
		sf.SetPixel(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// linePx is drawLine on a page w pixels wide that holds both ends.
// TRLC-LINKS: REQ-SDS-021
func linePx[P pixel](px []P, w, x0, y0, x1, y1 int, c P) {
	dx, dy := absI(x1-x0), absI(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	step := sy * w
	o := y0*w + x0
	for {
		px[o] = c
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
			o += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
			o += step
		}
	}
}

// traceID names the samples a trace draws (frame Seq plus how the channel's
// codes were derived), so a redraw of the same frame reuses the dense trace's
// column extremes. A zero seq never caches.
type traceID struct {
	seq     uint64
	ch, cpl int
	inv     bool
}

// drawTraceID maps the record window onto plot p: nearest sample when
// interp is false, linear interpolation of REAL samples when true — never
// sinc, never a segment across a skipped column.
// TRLC-LINKS: REQ-SDS-021
func drawTraceID(sf Canvas, p plotArea, id traceID, sig []uint8, win int, xc float64, interp bool, col Color, posFrac float64, zoom ...int) {
	drawTraceHeld(sf, p, id, sig, nil, win, xc, interp, col, posFrac, zoom...)
}

// heldColor is a trace colour dimmed to about 40 %: the stretches of a roll
// view no capture covered, drawn at the last value held.
// TRLC-LINKS: REQ-SDS-021
func heldColor(f hal.PixelFormat, col Color) Color {
	r, g, b := atPrecision(f, col).channels()
	if f == hal.RGB565 { // RGB565's own steps
		return RGB(r>>3*2/5<<3, g>>2*2/5<<2, b>>3*2/5<<3)
	}
	return RGB(r*2/5, g*2/5, b*2/5)
}

// codeAt is sig's code at record position pos (n = len(sig) > 0): linearly
// interpolated between the neighbours, or the nearest-below sample, with pos
// held to the record.
// TRLC-LINKS: REQ-SDS-021
func codeAt(sig []uint8, pos float64, interp bool) float64 {
	n := len(sig)
	if !interp {
		i := int(pos)
		if pos < 0 {
			i = 0
		} else if i > n-1 {
			i = n - 1
		}
		return float64(sig[i])
	}
	if pos < 0 {
		pos = 0
	} else if pos > float64(n-1) {
		pos = float64(n - 1)
	}
	i := int(pos)
	v := float64(sig[i])
	if i+1 < n {
		frac := pos - float64(i)
		v = v*(1-frac) + float64(sig[i+1])*frac
	}
	return v
}

// drawTraceHeld is drawTraceID with held marking samples (len(sig), or nil)
// that no capture covered: those are drawn in heldColor, so a roll view's
// held stretches do not read as signal.
// TRLC-LINKS: REQ-SDS-021
func drawTraceHeld(sf Canvas, p plotArea, id traceID, sig []uint8, held []bool, win int, xc float64, interp bool, col Color, posFrac float64, zoom ...int) {
	if len(held) != len(sig) {
		held = nil
	}
	z := 1.0
	if len(zoom) > 0 && zoom[0] > 1 {
		z = float64(zoom[0])
	}
	n := len(sig)
	if n == 0 {
		return
	}
	if win > n {
		win = n
	}
	if !(posFrac >= 0 && posFrac <= 1) {
		posFrac = 0.5
	}
	// Anchor at posFrac; do NOT clamp `left` — extend the nearest rail off the
	// record (repeat-nearest), identical to web window() so LCD == web. Keeps the
	// anchor exactly at posFrac even when the record has no mid crossing.
	left := xc - float64(win)*posFrac
	if !interp && float64(win) > 1.5*float64(p.w) {
		drawDenseTrace(sf, p, id, sig, held, left, win, col, z)
		return
	}
	prevX, prevY := -1, 0
	for x := range p.w {
		pos := left + float64(x)*float64(win)/float64(p.w)
		y := p.sy(128 + (codeAt(sig, pos, interp)-128)*z)
		c := col
		if held != nil && held[min(max(int(pos), 0), n-1)] {
			c = heldColor(sf.Format(), col)
		}
		if prevX >= 0 {
			drawLine(sf, prevX, prevY, x, y, c)
		} else {
			sf.SetPixel(x, y, c)
		}
		prevX, prevY = x, y
	}
}

// drawAcquiring notes that the capture for the current timebase is still
// filling its record.
// TRLC-LINKS: REQ-SDS-021
func drawAcquiring(sf Canvas, p plotArea, hud HUD) {
	lines := acquiringLines(hud)
	y := (p.top+p.bot)/2 - 8 - 7*(len(lines)-1)
	DrawText(sf, (p.w-TextWidth(lines[0], 2))/2, y, lines[0], colDim, 2)
	for i, l := range lines[1:] {
		DrawText(sf, (p.w-TextWidth(l, 1))/2, y+24+14*i, l, colDim, 1)
	}
}

// acquiringLines is what the screen says while no frame of the timebase
// on screen has come: a record filling, or, with a trigger armed that the
// FPGA decides and nothing come for a while, the trigger it waits for and
// how long, and after a few seconds what to try. A capture of a slow
// timebase takes its record's time; one that waits for a trigger takes as
// long as the signal does to hold it, and a screen that only said
// ACQUIRING did not tell the two apart.
// TRLC-LINKS: REQ-SDS-021, REQ-SDS-206
func acquiringLines(hud HUD) []string {
	if !hud.Waiting {
		msg := "ACQUIRING"
		if hud.RecordS >= 1 {
			msg += "  " + fmtTdiv(hud.RecordS) + " record"
		}
		return []string{msg}
	}
	what := fmt.Sprintf("C%d %s %s edge", hud.TrigSrc+1, map[bool]string{true: "rising", false: "falling"}[hud.TrigRising], fmtVolt(hud.TrigLvlV))
	hint := "No edge crosses the level: set it inside the signal (Trig Menu) or press AUTO."
	switch {
	case hud.SerialArmed:
		m := hud.SerialMatch
		if m == "" {
			m = "any unit"
		}
		what = "serial match " + m
		hint = "No match in the signal at this timebase yet: press AUTO to find the bus again."
	case hud.ZoneTrig:
		what = "zones on C" + strconv.Itoa(hud.TrigSrc+1)
		hint = "No edge passes every zone: check the zones (COMPARE) or the level."
	case hud.TrigType != 0:
		what = trigTag(hud)
		hint = "Nothing in the signal meets the condition: check it in the Trig Menu."
	}
	lines := []string{"WAITING FOR TRIGGER", fmt.Sprintf("the FPGA's %s, %.0f s", what, hud.WaitS)}
	if hud.WaitS >= waitHintS {
		lines = append(lines, hint)
	}
	return lines
}

// waitHintS is how long a trigger waits before the screen suggests what to
// try.
const waitHintS = 5

// drawDenseTrace draws a record with more samples than columns as each
// column's min..max, joined to its neighbour. One point per column drops
// everything between: a stopped 1 M-sample record at 100 us/div would show one
// sample in 1250, so a captured glitch would not be drawn.
// Columns beyond the record repeat its nearest end, as drawTrace does.
// TRLC-LINKS: REQ-SDS-021
func drawDenseTrace(sf Canvas, p plotArea, id traceID, sig []uint8, held []bool, left float64, win int, col Color, z float64) {
	cols := denseColumns(id, sig, left, win, p.w)
	rows := p.codeRows(z)
	y := func(v uint8) int { return int(rows[v]) }
	var top, bot [MaxW]int16
	var cc [MaxW]Color
	w := min(p.w, MaxW)
	prevY := -1
	for x := range w {
		yTop, yBot := y(cols.mx[x]), y(cols.mn[x])
		if prevY >= 0 { // join: the span reaches the previous column's last value
			yTop, yBot = min(yTop, prevY), max(yBot, prevY)
		}
		c := col
		if held != nil && columnHeld(held, left, win, p.w, x) {
			c = heldColor(sf.Format(), col)
		}
		yTop, yBot = max(yTop, 0), min(yBot, p.m.H-1)
		top[x], bot[x], cc[x] = int16(yTop), int16(yBot), c
		prevY = y(cols.last[x])
	}
	fillSpans(sf, top[:w], bot[:w], cc[:w])
}

// spanBlock is how many columns fillSpans fills as one block.
const spanBlock = 128

// fillSpans draws column x from row top[x] to bot[x] in colour cc[x]. A fast
// signal's columns all span most of the trace: written down each column a
// pixel lies a row (1600 bytes) from the last, and the cache misses cost an
// LCD frame 14 ms on the scope. The rows all of a block's columns cover in one
// colour are copied a row at a time instead (3 ms), the ragged ends drawn a
// column at a time.
// TRLC-LINKS: REQ-SDS-021
func fillSpans(sf Canvas, top, bot []int16, cc []Color) {
	switch m := sf.(type) {
	case *Image565:
		var nc [MaxW]uint16
		for i, c := range cc[:min(len(cc), MaxW)] {
			nc[i] = pack565(c)
		}
		fillSpansPx(m.pix, m.m.W, m.m.H, top, bot, nc[:len(cc)])
	case *Image32:
		var nc [MaxW]uint32
		for i, c := range cc[:min(len(cc), MaxW)] {
			nc[i] = m.native(c)
		}
		fillSpansPx(m.pix, m.m.W, m.m.H, top, bot, nc[:len(cc)])
	default:
		for x := range top {
			vline(sf, x, int(top[x]), int(bot[x]), cc[x])
		}
	}
}

// fillSpansPx is fillSpans on a w×h page of pixels px, the colours in its
// format.
// TRLC-LINKS: REQ-SDS-021
func fillSpansPx[P pixel](px []P, w, h int, top, bot []int16, cc []P) {
	// Spans arrive clipped to [0,h) by drawDenseTrace; clip again for any
	// other caller so vlinePx may assume it.
	vl := func(x, y0, y1 int, c P) {
		if x < w {
			vlinePx(px, w, x, max(y0, 0), min(y1, h-1), c)
		}
	}
	var line [spanBlock]P // a row of the block's colour
	var lineC P
	lined := false
	for x0 := 0; x0 < len(top); x0 += spanBlock {
		x1 := min(x0+spanBlock, len(top))
		cTop, cBot, c := int16(0), int16(h-1), cc[x0] //nolint:gosec // a screen height fits int16
		for x := x0; x < x1; x++ {
			cTop, cBot = max(cTop, top[x]), min(cBot, bot[x])
			if cc[x] != c {
				cBot = -1
			}
		}
		if cBot-cTop < 8 { // little in common
			if denseBlock(px, w, h, top[x0:x1], bot[x0:x1], cc[x0:x1], x0) {
				continue
			}
			for x := x0; x < x1; x++ { // a column at a time
				vl(x, int(top[x]), int(bot[x]), cc[x])
			}
			continue
		}
		if !lined || lineC != c {
			for i := range line {
				line[i] = c
			}
			lineC, lined = c, true
		}
		for r := int(cTop); r <= int(cBot); r++ {
			copy(px[r*w+x0:r*w+x1], line[:x1-x0])
		}
		for x := x0; x < x1; x++ {
			vl(x, int(top[x]), int(cTop)-1, c)
			vl(x, int(cBot)+1, int(bot[x]), c)
		}
	}
}

// denseBlock fills a block of columns starting at x0 a row at a time when
// they cover most of the rows between their extremes (a fast signal's ragged
// columns), and reports whether it did: a row costs a compare a column but
// its stores are sequential, where a column's each wait on a cache line.
// TRLC-LINKS: REQ-SDS-021
func denseBlock[P pixel](px []P, w, h int, top, bot []int16, cc []P, x0 int) bool {
	nb := len(top)
	if nb > spanBlock || x0+nb > w {
		return false
	}
	var t0, ht [spanBlock]int32
	rTop, rBot, cover := int32(h), int32(-1), 0 //nolint:gosec // a screen height fits int32
	for i := range nb {
		a, b := max(int32(top[i]), 0), min(int32(bot[i]), int32(h-1)) //nolint:gosec // a screen height fits int32
		if a > b {                                                    // empty: a first row no row reaches
			t0[i], ht[i] = 1<<20, 0
			continue
		}
		t0[i], ht[i] = a, b-a
		rTop, rBot = min(rTop, a), max(rBot, b)
		cover += int(b - a + 1)
	}
	if rBot < rTop || 2*cover < int(rBot-rTop+1)*nb {
		return false
	}
	for r := rTop; r <= rBot; r++ {
		spanRow(px[int(r)*w+x0:int(r)*w+x0+nb], t0[:nb], ht[:nb], cc[:nb], r)
	}
	return true
}

// spanRow writes row r's pixels of the columns covering it. Out of line so
// the tables are registers, not reloaded from the caller's frame.
// TRLC-LINKS: REQ-SDS-021
//
//go:noinline
func spanRow[P pixel](row []P, t0, ht []int32, cc []P, r int32) {
	t0, ht, cc = t0[:len(row)], ht[:len(row)], cc[:len(row)]
	for i := range row {
		if uint32(r-t0[i]) <= uint32(ht[i]) { //nolint:gosec // unsigned: 0 <= r-t0 <= ht in one compare
			row[i] = cc[i]
		}
	}
}

// floorInt is int(math.Floor(v)) for the sample positions of a plot column:
// math.Floor is a software routine on this ARM and these run per column per
// frame. Out of int32 range, or NaN, it defers to math.Floor.
// TRLC-LINKS: REQ-SDS-021
func floorInt(v float64) int {
	if !(v > -1<<31 && v < 1<<31) {
		return int(math.Floor(v))
	}
	i := int(v)
	if float64(i) > v {
		i--
	}
	return i
}

// columnHeld reports whether every sample of plot column x is held (or off
// the record past a held end).
// TRLC-LINKS: REQ-SDS-021
func columnHeld(held []bool, left float64, win, w, x int) bool {
	n := len(held)
	a := floorInt(left + float64(x)*float64(win)/float64(w))
	b := floorInt(left + float64(x+1)*float64(win)/float64(w))
	a, b = min(max(a, 0), n-1), min(max(b, a+1), n)
	for i := a; i < b; i++ {
		if !held[i] {
			return false
		}
	}
	return true
}

// denseCols are a dense trace's per-column extremes and last sample.
type denseCols struct {
	key          denseKey
	mn, mx, last [MaxW]uint8
}

type denseKey struct {
	id     traceID
	n, win int
	cols   int
	left   float64
	ptr    *uint8    // the samples' buffer…
	probe  [16]uint8 // …and a spread of them, against a reused Seq
}

// denseCache keeps the column extremes of the last traces drawn (one slot per
// channel role): scanning a deep record is most of an LCD frame's cost, and
// the same frame is redrawn as the HUD changes around it.
var denseCache struct {
	sync.Mutex
	slot [zoomTraceID + MaxChans]denseCols
}

// TRLC-LINKS: REQ-SDS-021
func denseColumns(id traceID, sig []uint8, left float64, win, ncols int) *denseCols {
	ncols = min(max(ncols, 1), MaxW)
	key := denseKey{id: id, n: len(sig), win: win, cols: ncols, left: left, ptr: &sig[0]}
	for i := range key.probe {
		key.probe[i] = sig[i*(len(sig)-1)/(len(key.probe)-1)]
	}
	var c *denseCols
	if id.seq != 0 {
		denseCache.Lock()
		defer denseCache.Unlock()
		c = &denseCache.slot[id.ch%len(denseCache.slot)]
		if c.key == key {
			return c
		}
	} else {
		c = new(denseCols)
	}
	n := len(sig)
	step := float64(win) / float64(ncols)
	// From a non-negative left every position is non-negative, so the
	// floor is the conversion's truncation: no range test a column (a float
	// compare stalls this core's pipeline).
	trunc := left >= 0 && left+float64(ncols+1)*step < 1<<31
	next := floorInt(left)
	for x := range ncols {
		// Column x ends where x+1 starts: one floor a column.
		lo := next
		if pos := left + float64(x+1)*step; trunc {
			next = int(pos)
		} else {
			next = floorInt(pos)
		}
		hi := next - 1
		lo, hi = min(max(lo, 0), n-1), min(max(hi, 0), n-1)
		mn, mx := sig[lo], sig[lo]
		for _, v := range sig[lo : hi+1] {
			if v < mn {
				mn = v
			} else if v > mx {
				mx = v
			}
		}
		c.mn[x], c.mx[x], c.last[x] = mn, mx, sig[hi]
	}
	c.key = key
	if id.seq == 0 {
		c.key = denseKey{}
	}
	return c
}

// drawEnvelope fills each column min→max (spec 07 §4): every pixel lies
// between a real captured min and max, magnified about mid-scale by the
// channel's display zoom like drawTrace.
// TRLC-LINKS: REQ-SDS-021
func drawEnvelope(sf Canvas, p plotArea, mn, mx []uint8, cols int, col Color, zoom int) {
	z := float64(max(zoom, 1))
	var top, bot [MaxW]int16
	var cc [MaxW]Color
	w := min(p.w, MaxW)
	rows := p.codeRows(z)
	for x := range w {
		top[x], bot[x], cc[x] = 1, 0, col // nothing drawn
		c := x * cols / p.w
		if c >= cols || c >= len(mn) || c >= len(mx) {
			continue
		}
		yTop, yBot := rows[mx[c]], rows[mn[c]]
		if yTop > yBot {
			yTop, yBot = yBot, yTop
		}
		top[x], bot[x] = yTop, yBot
	}
	fillSpans(sf, top[:w], bot[:w], cc[:w])
}

// codesPerDiv is frame f's codes a division on the 8-bit view: its first
// trace's, or an 8-bit converter's when it states none.
// TRLC-LINKS: REQ-SDS-021
func codesPerDiv(f *sample.Frame) float64 {
	if f != nil && len(f.Traces) > 0 && f.Traces[0].CodesPerDiv > 0 {
		return f.Traces[0].CodesPerDiv8()
	}
	return defaultCodesPerDiv
}

// frameValid clamps a frame's valid-sample count into range (shared by the
// alternate-view renderers below).
// TRLC-LINKS: REQ-SDS-021
func frameValid(f *sample.Frame) int {
	valid := min(max(f.Len(), 1), len(f.Codes8(0)))
	return valid
}

// coupledDisplay applies the software coupling model for a channel (mirrors the
// Y-T path so the alternate views see the same trace).
// TRLC-LINKS: REQ-SDS-021
func coupledDisplay(sig []uint8, cpl int) []uint8 {
	if cpl != dsp.CoupleDC {
		return dsp.CoupleDisplay(sig, cpl)
	}
	return sig
}

// invertCodes mirrors a code slice about the display centre (127.5): the
// display-level INVS transform v' = 255 − v, matching the web draw path
// byte-for-byte. Returns a fresh slice — the input may be the shared fan-out
// frame. Applied to the RENDERED Y-T trace/envelope only: measurements,
// decode, math, X-Y/FFT and mask/zone tests keep the true captured polarity
// (see the Cn:INVS handler for the rationale).
// TRLC-LINKS: REQ-SDS-021
func invertCodes(sig []uint8) []uint8 {
	out := make([]uint8, len(sig))
	for i, v := range sig {
		out[i] = 255 - v
	}
	return out
}

// ---- value formatting (spec 07 §6.2): 3 sig figs, ASCII suffixes ----

// siUnit is one prefix row for siScale (high→low).
// TRLC-LINKS: REQ-SDS-021
type siUnit struct {
	scale  float64
	suffix string
}

var (
	voltUnits = []siUnit{{1, "V"}, {1e-3, "mV"}}
	freqUnits = []siUnit{{1e6, "MHz"}, {1e3, "kHz"}, {1, "Hz"}}
	timeUnits = []siUnit{{1, "s"}, {1e-3, "ms"}, {1e-6, "us"}, {1e-9, "ns"}}
)

// fillRect paints a solid rectangle (clipped to the surface).
// TRLC-LINKS: REQ-SDS-021
func fillRect(sf Canvas, x, y, w, h int, c Color) {
	if fillSpan(sf, x, y, x+w, y+h, c) {
		return
	}
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			sf.SetPixel(xx, yy, c)
		}
	}
}

// strokeRect draws a 1 px rectangle outline.
// TRLC-LINKS: REQ-SDS-021
func strokeRect(sf Canvas, x, y, w, h int, c Color) {
	if w <= 0 || h <= 0 {
		return
	}
	hline(sf, x, x+w-1, y, c)
	hline(sf, x, x+w-1, y+h-1, c)
	vline(sf, x, y, y+h-1, c)
	vline(sf, x+w-1, y, y+h-1, c)
}

// Render draws one complete frame into the back buffer (spec 07 §3.2):
// fill → graticule → trace/envelope → liveness strip → readouts → the
// workspace UI (the pane, the rail, a sheet or the picker). Never blanks on
// a held frame; the strip goes red instead.
// TRLC-LINKS: REQ-SDS-021, REQ-SDS-211
func Render(sf Canvas, f *sample.Frame, hud HUD, live bool, persist ...Layer) {
	f = view8(f)
	sf.Fill(colBG)
	ui := &hud.UI
	sheet := ui.Sheet != nil
	sm := metricsOf(sf)
	p := plotOf(sm, ui.Layout)
	p.cpd = codesPerDiv(f)
	traces := !sheet && !ui.Layout.HideTraces
	if traces && decodeStrip(hud) {
		p = p.aboveStrip()
	}
	// Over a hardware waveform renderer the plane under the canvas shows the
	// graticule and the waveforms: its area stays transparent under the
	// markers, readouts and UI drawn over it.
	under := underOf(sf)
	hw := !under.Empty()
	if hw {
		fillRect(sf, under.Min.X, under.Min.Y, under.Dx(), under.Dy(), Transparent)
	}
	if traces && !hw {
		drawGraticule(sf, p)
	}
	// Running, a frame captured at another timebase is not this view: drawn
	// under the new label it misreads by the ratio, and at 10 s/div the next
	// frame takes minutes to fill. Show the graticule and
	// say a record is filling instead, as a scope clears on a timebase change.
	acquiring := hud.Running && f != nil && f.Snapshot.TdivS > 0 && hud.TdivS > 0 &&
		math.Abs(f.Snapshot.TdivS-hud.TdivS) > 1e-9*hud.TdivS
	if acquiring {
		f = nil
		if traces {
			defer drawAcquiring(sf, p, hud)
		}
	}

	// Persistence: when on (Y-T only, non-envelope), draw the traces onto a
	// separate layer that decays each frame, then composite it over the fresh
	// graticule — an afterglow that catches jitter/glitches. The layer is owned
	// by the caller (LCD loop); nil disables it (screenshot path, tests).
	var pb Layer
	if len(persist) > 0 {
		pb = persist[0]
	}
	persisting := traces && !hw && pb != nil && hud.Persist && hud.ViewMode == 0 && f != nil && !f.Flags.Has(sample.Envelope) && len(f.Codes8(0)) > 0
	traceTarget := sf
	if persisting {
		pb.FadeToBlack()
		traceTarget = pb
	}
	if traces && !hw {
		drawViews(sf, traceTarget, p, f, hud)
	}
	if persisting { // composite the decayed trace layer over the graticule
		if l, ok := sf.(Layer); ok {
			l.BlitBright(pb)
		}
	}

	// Liveness strip: green only for a fresh frame with real signal.
	strip := colStale
	if live && f != nil && f.SourcePtp >= 8 {
		strip = colOK
	}
	hline(sf, 0, sm.W-1, 0, strip)
	hline(sf, 0, sm.W-1, 1, strip)

	// Trigger/ground markers, the MEASURE readouts and cursors are time-domain
	// concepts — only overlay them in Y-T, not on the X-Y/FFT plots.
	yt := traces && hud.ViewMode == 0 && !streamFullScreen(hud)
	if yt {
		drawMarkers(sf, p, f, hud)
	}
	drawHUD(sf, f, hud)
	if yt && hud.ShowMeas && !ui.Layout.HideMeas {
		drawMeasPanel(sf, p, f, hud)
	}
	if yt {
		drawCursors(sf, p, hud)
	}
	if !sheet {
		if hud.SRActive {
			drawSuperresHUD(sf, p, hud)
		}
		drawMaskHUD(sf, hud)
	}
	drawUI(sf, f, hud, p)
	switch {
	case hud.AutosetBusy:
		drawAutosetBanner(sf, p, hud.AutosetMsg)
	case hud.Notice != "":
		drawNotice(sf, p, hud.Notice)
	}
	if hud.Remote != "" {
		drawRemote(sf, p, hud.Remote)
	}
}

// drawViews draws the plot's content for the view: the transcript, the
// super-res review, X-Y, FFT, Bode, spectrogram, or the Y-T traces with
// their overlays.
// TRLC-LINKS: REQ-SDS-021
func drawViews(sf, traceTarget Canvas, p plotArea, f *sample.Frame, hud HUD) {
	// X-Y / FFT only make sense on per-sample (native/decimated) frames — an
	// envelope/roll frame has no paired samples or usable spectrum, so fall
	// through to the Y-T envelope rendering (matches the web, which gates these).
	srReview := hud.SRFocus == 3 && len(hud.SRMean) > 0
	switch {
	case streamFullScreen(hud):
		drawStream(sf, p, hud)
	case srReview:
		// Review the super-resolved stack in the SELECTED view, so FFT and X-Y
		// operate on the crunched (extra-bit, K× fine-grid) trace — not just Y-T.
		// Bode (3) / Spectrogram (4) are sweep/time-evolving views with no meaning
		// for a frozen stack, so they fall back to the Y-T super-res trace.
		switch hud.ViewMode {
		case 1:
			drawSuperresXY(sf, p, hud)
		case 2:
			drawSuperresFFT(sf, p, hud)
		default:
			drawSuperresTrace(traceTarget, p, hud)
		}
	case hud.ViewMode == 1 && f != nil && !f.Flags.Has(sample.Envelope) && len(f.Codes8(0)) > 0:
		drawXY(sf, p, f, hud)
	case hud.ViewMode == 2 && f != nil && !f.Flags.Has(sample.Envelope) && len(f.Codes8(0)) > 0:
		drawFFT(sf, p, f, hud)
	case hud.ViewMode == 3:
		drawBode(sf, p, hud)
	case hud.ViewMode == 5:
		drawSoundTrace(sf, p, hud.UI.Sound)
	case hud.ViewMode == 4:
		drawSpectrogramMark(sf, p, hud.Spect, hud.UI.Layout.HideViewCaptions, hud.UI.Spec)
	case f != nil && len(f.Codes8(0)) > 0:
		drawYT(sf, traceTarget, p, f, hud)
	}
	if hud.StreamOn && !streamFullScreen(hud) && hud.Stream.State != streamwire.Idle {
		drawStreamStrip(sf, p, hud)
	}
}

// drawYT draws the Y-T view: the traces (or a live envelope's bands), C1
// on top, and their overlays.
// TRLC-LINKS: REQ-SDS-021
func drawYT(sf, traceTarget Canvas, p plotArea, f *sample.Frame, hud HUD) {
	valid := frameValid(f)
	if f.Flags.Has(sample.Envelope) && len(f.Min8(0)) > 0 {
		drawEnvelopes(sf, p, f, hud)
		return
	}
	win, xc, posFrac := traceWindow(f, hud, valid)
	drawZoneMask(traceTarget, p, f, hud, win, xc, posFrac)
	// Zoomed into a live envelope: the frame's view window holds the
	// zoomed part at full detail, where the screen's buckets are too coarse.
	view, vwin, vxc, zoomed := zoomView(f, hud, valid)
	for ch := hud.numChans() - 1; ch >= 0; ch-- {
		if hud.shown(ch) && len(f.Codes8(ch)) >= valid {
			drawChan(traceTarget, p, f, hud, ch, valid, ytWindow{win, xc, posFrac}, ytWindow{vwin, vxc, 0.5}, view, zoomed)
		}
	}
	if hud.MathMode != 0 {
		drawMath(traceTarget, p, f, hud, win, xc, posFrac)
	}
	drawRefs(traceTarget, p, hud)
	if hud.DecProto != 0 {
		drawDecode(sf, p, f, hud, win, xc, posFrac)
	}
}

// drawEnvelopes draws a live envelope frame's bands, C1 on top.
// TRLC-LINKS: REQ-SDS-021
func drawEnvelopes(sf Canvas, p plotArea, f *sample.Frame, hud HUD) {
	for ch := hud.numChans() - 1; ch >= 0; ch-- {
		if !hud.shown(ch) {
			continue
		}
		mn, mx := f.Min8(ch), f.Max8(ch)
		if hud.Ch[ch].Inv { // INVS mirrors the band: min'=inv(max), max'=inv(min)
			mn, mx = invertCodes(mx), invertCodes(mn)
		}
		drawEnvelope(sf, p, mn, mx, len(f.Min8(0)), chanColor(ch), hud.Ch[ch].Zoom)
	}
}

// drawChan draws channel ch's trace: coupled and inverted for the display,
// over the record window rec, or over zw from the view window when the
// screen is zoomed into a live envelope.
// TRLC-LINKS: REQ-SDS-021
func drawChan(traceTarget Canvas, p plotArea, f *sample.Frame, hud HUD, ch, valid int, rec, zw ytWindow, view *sample.Frame, zoomed bool) {
	c := hud.Ch[ch]
	codes := f.Codes8(ch)[:valid]
	if zoomed {
		codes = dsp.CoupleDisplayBy(view.Codes8(ch)[:view.Len()], codes, c.Cpl)
	} else if c.Cpl != dsp.CoupleDC { // software coupling: AC removes DC, GND grounds
		codes = dsp.CoupleDisplay(codes, c.Cpl)
	}
	if c.Inv { // display-level INVS: mirror the rendered trace only
		codes = invertCodes(codes)
	}
	if zoomed {
		drawTraceID(traceTarget, p, traceID{f.Seq, zoomTraceID + ch, c.Cpl, c.Inv}, codes, zw.win, zw.xc, false, chanColor(ch), zw.posFrac, c.Zoom)
	} else {
		drawTraceHeld(traceTarget, p, traceID{f.Seq, 1 + ch, c.Cpl, c.Inv}, codes, heldOf(f, valid), rec.win, rec.xc, f.Flags.Has(sample.Interp), chanColor(ch), rec.posFrac, c.Zoom)
	}
}

// zoomTraceID is the first trace ID of the channels' zoomed views (1 + ch
// for the record's).
const zoomTraceID = 1 + MaxChans

// ytWindow is a record window on the plot: its width in samples, centre and
// the anchor's place across the plot (traceWindow's three).
type ytWindow struct {
	win         int
	xc, posFrac float64
}

// traceWindow is the sample window the Y-T plot shows: win samples with the
// trigger crossing xc at posFrac of the screen; zoomed, win/zoom samples
// centred on the panned crossing.
// TRLC-LINKS: REQ-SDS-021
func traceWindow(f *sample.Frame, hud HUD, valid int) (win int, xc, posFrac float64) {
	win = f.WinPoints
	if win <= 0 || win > valid {
		win = valid
	}
	xc = f.TriggerAt
	if xc < 0 {
		// Match web window(): an uncentred frame (EdgeX=-1: flat, or forced
		// in AUTO) centres the screen on the record, the trigger point at its
		// position (at 10 % the record's middle as the trigger left the
		// screen's last 40 % past the record of a sampled 10ms/div frame).
		// NEVER fabricate an edge from a marginal/noise crossing (that would
		// jitter a flat line and diverge from web).
		xc = float64(valid)/2 + (hud.TrigPosFrac-0.5)*float64(win)
	}
	// Horizontal ZOOM: show win/zoom samples magnified across the screen,
	// panned by ZoomOff across the record (centred, so posFrac=0.5).
	posFrac = hud.TrigPosFrac
	if hud.Zoom > 1 {
		win /= hud.Zoom
		if win < 8 {
			win = 8
		}
		xc += hud.ZoomOff * float64(valid)
		posFrac = 0.5
	}
	return win, xc, posFrac
}

// TRLC-LINKS: REQ-SDS-021
func absI(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// shownChans is C1's and C2's display enable (shown).
// TRLC-LINKS: REQ-SDS-021
func (h HUD) shownChans() (bool, bool) { return h.shown(0), h.shown(1) }

// numChans is how many channels the HUD describes, C1 at least.
// TRLC-LINKS: REQ-SDS-021
func (h HUD) numChans() int { return min(max(h.NumChans, 1), MaxChans) }

// shown is channel ch's display enable. All off means "unset" (show every
// channel) unless the panel set the flags: then the user hid them all and
// none is drawn.
// TRLC-LINKS: REQ-SDS-021
func (h HUD) shown(ch int) bool {
	if ch < 0 || ch >= MaxChans {
		return false
	}
	if !h.ChanExplicit {
		set := false
		for _, c := range h.Ch {
			set = set || c.Show
		}
		if !set {
			return true
		}
	}
	return h.Ch[ch].Show
}

// heldOf is a roll frame's held samples over its valid prefix; nil otherwise.
// TRLC-LINKS: REQ-SDS-021
func heldOf(f *sample.Frame, valid int) []bool {
	if len(f.Held(0)) < valid {
		return nil
	}
	return f.Held(0)[:valid]
}

// zoomView is the frame's view window when it covers the zoomed part of a live
// envelope frame, and where it lies: vwin of its values span the plot, the
// zoom's centre at vxc.
// TRLC-LINKS: REQ-SDS-021
func zoomView(f *sample.Frame, hud HUD, valid int) (view *sample.Frame, vwin int, vxc float64, ok bool) {
	v := f.Detail(sample.RoleView(0))
	if hud.Zoom <= 1 || v == nil || f.Content != sample.PeakPairs || v.Frame.Len() < 2 || !(v.To > v.From) {
		return nil, 0, 0, false
	}
	win, xc, _ := traceWindow(f, hud, valid)
	za, zb := xc-float64(win)/2, xc+float64(win)/2
	if tol := float64(win) / 100; v.From > za+tol || v.To < zb-tol {
		return nil, 0, 0, false // a view of another stretch (the zoom just moved)
	}
	per := float64(v.Frame.Len()) / (v.To - v.From) // its values per frame value
	vwin = int(math.Round(float64(win) * per))
	return v.Frame, max(vwin, 2), (xc - v.From) * per, true
}
