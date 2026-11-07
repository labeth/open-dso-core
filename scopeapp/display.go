// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"math"
	"os"
	"reflect"
	"time"

	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/streamwire"
	"github.com/labeth/open-dso-core/uimodel/panel"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// Display is how the scope's own display is drawn: the screen the loop
// draws on, what it asks of the acquisition, and the heads-up state it
// draws. Every field may be nil.
type Display struct {
	// Open brings the screen up, on the display goroutine; an error leaves
	// the scope running without its display. nil: the board's framebuffer
	// (hal.Display), none when it has none.
	Open func() (Screen, error)
	// Engine is what the loop asks of the acquisition; nil: an owner that
	// shares no core with the display (a 20th of a second between
	// pictures, nothing paced).
	Engine DisplayEngine
	// HUD is the heads-up state of the next picture; nil: the UI model's
	// HUD over the instrument state.
	HUD func() render.HUD
	// OnFrame sees each picture's frame and HUD under the fan-out's lock
	// (a scope's zoomed view lease).
	OnFrame func(f *sample.Frame, hud *render.HUD)
	// URL is the address the busy indicator shows.
	URL func() string
	// Canvas is a fresh canvas the size of the screen, for a picture drawn
	// headless (the web's screen.png, SCPI's screen dump); nil: one at the
	// profile's display.
	Canvas func() render.Canvas
}

// Screen is a display the loop draws on: a back buffer, an afterglow layer
// (nil: none), and the copy to the panel.
type Screen interface {
	Canvas() render.Canvas
	Layer() render.Layer
	Present()               // the whole back buffer
	PresentRows(y0, y1 int) // rows y0 … y1-1 only
	SetLight(on bool) error // the backlight
}

// DisplayEngine is what the display loop asks of an acquisition engine
// that shares the core with it.
type DisplayEngine interface {
	// RenderPeriod is the display's tick.
	RenderPeriod() time.Duration
	// SetDisplayPace asks each capture to take at least d, so a display
	// falling behind gets the core.
	SetDisplayPace(d time.Duration)
	// QuietRLock holds off the engine's load-sensitive windows (arm and
	// drain) while a picture is drawn; QuietRUnlock ends it.
	QuietRLock()
	QuietRUnlock()
	// NoteLCDFrame counts a frame the display put up.
	NoteLCDFrame()
}

// freeEngine is an owner that shares no core with the display.
type freeEngine struct{}

// TRLC-LINKS: REQ-DSO-168
func (freeEngine) RenderPeriod() time.Duration { return hudPeriod }

// TRLC-LINKS: REQ-DSO-168
func (freeEngine) SetDisplayPace(time.Duration) {}

// TRLC-LINKS: REQ-DSO-168
func (freeEngine) QuietRLock() {}

// TRLC-LINKS: REQ-DSO-168
func (freeEngine) QuietRUnlock() {}

// TRLC-LINKS: REQ-DSO-168
func (freeEngine) NoteLCDFrame() {}

// hudPeriod is the display's tick over a hardware waveform renderer: the
// renderer draws the traces at its own rate, so the readouts, menus and
// markers need no more.
const hudPeriod = 50 * time.Millisecond

// display is the display loop's state shared with the headless picture.
type display struct {
	env   *Env
	cfg   Display
	eng   DisplayEngine
	spect *render.Spectrogram // the one waterfall every picture of it blits
	chans int                 // the board's channels (a profile is built per call on some boards)
}

// newDisplay is the display of env with cfg.
// TRLC-LINKS: REQ-DSO-168
func newDisplay(env *Env, cfg Display) *display {
	d := &display{env: env, cfg: cfg, eng: cfg.Engine, spect: render.NewSpectrogram(), chans: env.Board.Profile().Channels}
	if d.eng == nil {
		d.eng = freeEngine{}
	}
	return d
}

// hud is the next picture's heads-up state.
// TRLC-LINKS: REQ-DSO-168
func (d *display) hud() render.HUD {
	var hud render.HUD
	switch {
	case d.cfg.HUD != nil:
		hud = d.cfg.HUD()
	case d.env.UI != nil:
		hud = d.env.UI.HUD(d.chans)
	}
	if hud.Spect == nil {
		hud.Spect = d.spect
	}
	return hud
}

// url is the address on the busy indicator.
// TRLC-LINKS: REQ-DSO-168
func (d *display) url() string {
	if d.cfg.URL != nil {
		return d.cfg.URL()
	}
	return ""
}

// canvas is a fresh canvas for a headless picture.
// TRLC-LINKS: REQ-DSO-168
func (d *display) canvas() render.Canvas {
	if d.cfg.Canvas != nil {
		return d.cfg.Canvas()
	}
	m := metricsFor(d.env.Board.Profile().Display)
	if d.env.Board.Profile().Display.Format == hal.RGB565 {
		return render.NewImage565(m)
	}
	return render.NewImage32(m, hal.ARGB8888)
}

// Render draws the display's picture into dst headless (the web's
// screen.png, SCPI's screen dump, PRINT): the workspace's analysis of the
// frame first, so the pane describes the frame drawn.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-211
func (d *display) Render(dst render.Canvas) {
	fo := d.env.Frames
	if pc := d.env.UI; pc != nil {
		fo.WithFrame(pc.OnFrame)
	}
	hud := d.hud()
	fo.WithFrame(func(f *sample.Frame) {
		if f != nil {
			hud.SampleS = f.SampleS
			hud.Trigd = f.Status.Triggered
		}
		render.Render(dst, f, hud, true)
	})
}

// metricsFor is the layout table of a display spec.
// TRLC-LINKS: REQ-DSO-168
func metricsFor(s hal.DisplaySpec) *render.Metrics {
	switch {
	case s.W >= 1280:
		return &render.Metrics1280x800
	case s.W >= 1024:
		return &render.Metrics1024x600
	}
	return &render.Metrics800x480
}

// fbScreen is the board's framebuffer as the loop's screen: the picture
// drawn into a canvas of the display's format (the waveform plane's area
// left transparent over a hardware renderer), copied row by row.
type fbScreen struct {
	d      hal.Display
	fb     hal.Framebuffer
	canvas render.Canvas
	pix    []byte // the canvas's pixels
	stride int    // its bytes a row
	rows   int
}

// openBoardScreen is the board's framebuffer; an error when it has none.
// TRLC-LINKS: REQ-DSO-168
func openBoardScreen(b hal.Board) (Screen, error) {
	d := b.Display()
	if d == nil || d.Framebuffer() == nil {
		return nil, hal.Refuse("display", "the board has no framebuffer")
	}
	spec := b.Profile().Display
	m := metricsFor(spec)
	s := &fbScreen{d: d, fb: d.Framebuffer(), rows: m.H}
	if spec.Format == hal.RGB565 {
		img := render.NewImage565(m)
		s.canvas, s.pix, s.stride = img, img.Pix, 2*m.W
		return s, nil
	}
	img := render.NewImage32(m, hal.ARGB8888)
	if r, ok := d.Renderer(); ok {
		img.SetUnder(r.Area())
	}
	s.canvas, s.pix, s.stride = img, img.Pix, 4*m.W
	return s, nil
}

// TRLC-LINKS: REQ-DSO-168
func (s *fbScreen) Canvas() render.Canvas { return s.canvas }

// Layer is none: a board's framebuffer draws no afterglow (over a hardware
// renderer the traces are the plane's).
// TRLC-LINKS: REQ-DSO-168
func (s *fbScreen) Layer() render.Layer { return nil }

// TRLC-LINKS: REQ-DSO-168
func (s *fbScreen) Present() { s.PresentRows(0, s.rows) }

// TRLC-LINKS: REQ-DSO-168
func (s *fbScreen) PresentRows(y0, y1 int) {
	copyRows(s.fb.Back(), s.fb.Stride(), s.pix, s.stride, y0, y1)
	_ = s.fb.Present() // a failed copy shows at the next picture
}

// TRLC-LINKS: REQ-DSO-168
func (s *fbScreen) SetLight(on bool) error {
	if on {
		return s.d.Backlight(1)
	}
	return s.d.Backlight(0)
}

// copyRows copies rows y0 … y1-1 of src (srcStride bytes a row) into a
// framebuffer of dstStride bytes a row.
// TRLC-LINKS: REQ-DSO-168
func copyRows(dst []byte, dstStride int, src []byte, srcStride, y0, y1 int) {
	for y := max(y0, 0); y < y1 && (y+1)*srcStride <= len(src) && y*dstStride < len(dst); y++ {
		copy(dst[y*dstStride:min(len(dst), (y+1)*dstStride)], src[y*srcStride:(y+1)*srcStride])
	}
}

// renderSig is a scalar fingerprint of everything the static (Y-T, X-Y,
// FFT) views draw. When it is unchanged tick to tick the display is
// identical, so the loop skips the rasterize and copy, the heavy part that
// otherwise ran on every tick of a held frame. Time-evolving views
// (persistence, Bode, spectrogram, super-res review, an active mask or zone
// test) bypass it and always repaint. All fields are comparable so ==
// decides.
// TRLC-LINKS: REQ-DSO-168
type renderSig struct {
	streamOn, streamList                            bool
	streamState, streamTop, streamTotal, streamTrig int
	streamSlot                                      int64
	seq                                             uint64
	waitS                                           int // a waiting trigger's seconds (the screen counts them)
	view, math, dec, zoom                           int
	curType, curSel                                 int
	ui                                              uint64 // the workspace UI (uiSig)
	url                                             string
	tdiv, zoomOff                                   float64
	c1v, c2v, off1, off2                            float64
	trig                                            float64
	curX, curY                                      [2]float64
	probe1, probe2                                  float64
	cpl1, cpl2                                      int
	inv1, inv2                                      bool
	showMeas, persist                               bool
	running, single, norm                           bool
	trigd, live                                     bool
	showC1, showC2                                  bool
	curOn                                           bool
	ref0, ref1                                      bool
	// extra prints the remaining HUD state a held display shows: the
	// autoset banner, the notices, the trigger readout, decode parameters
	// (a softkey that changes only a value, e.g. TRIGGER Type Pulse->Slope
	// while stopped, must not leave the old value on screen), and the
	// channels past C2.
	extra uint64
}

// statusSlot is what the top bar's status slot shows (render.DrawStatusSlot).
type statusSlot struct {
	busy       string
	pct, phase int
}

// uiSig fingerprints the workspace UI for the repaint check. Pointers are
// followed (fmt would print their addresses, new on every snapshot); the
// eye's density grid is left out: it changes only with a new frame, and the
// signature has the frame's Seq.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-211
func uiSig(v wsview.View) uint64 {
	if v.Eye != nil {
		e := *v.Eye
		e.Grid = nil
		v.Eye = &e
	}
	h := sigOffset
	h.add(reflect.ValueOf(&v).Elem())
	return uint64(h)
}

// waitSecond is the whole seconds a waiting trigger has waited (the
// screen counts them), 0 when nothing waits.
// TRLC-LINKS: REQ-DSO-168
func waitSecond(hud render.HUD) int {
	if !hud.Waiting {
		return 0
	}
	return int(hud.WaitS)
}

// TRLC-LINKS: REQ-DSO-168
func renderSigOf(f *sample.Frame, hud render.HUD, live bool) renderSig {
	var seq uint64
	if f != nil {
		seq = f.Seq
	}
	return renderSig{
		seq:  seq,
		view: hud.ViewMode, math: hud.MathMode, dec: hud.DecProto, zoom: hud.Zoom,
		curType: hud.CurType, curSel: hud.CurSel, ui: uiSig(hud.UI),
		url:  hud.URL, // an IP appearing/changing must repaint a held display
		tdiv: hud.TdivS, zoomOff: hud.ZoomOff,
		c1v: hud.Ch[0].VdivV, c2v: hud.Ch[1].VdivV, off1: hud.Ch[0].OffV, off2: hud.Ch[1].OffV,
		trig: hud.TrigLvlDiv, curX: hud.CurX, curY: hud.CurY,
		probe1: hud.Ch[0].Probe, probe2: hud.Ch[1].Probe, cpl1: hud.Ch[0].Cpl, cpl2: hud.Ch[1].Cpl,
		inv1: hud.Ch[0].Inv, inv2: hud.Ch[1].Inv, // an INVS flip must repaint a held display
		showMeas: hud.ShowMeas, persist: hud.Persist,
		running: hud.Running, single: hud.Single, norm: hud.Norm,
		trigd: hud.Trigd, live: live, showC1: hud.Ch[0].Show, showC2: hud.Ch[1].Show,
		waitS: waitSecond(hud),
		curOn: hud.CurOn,
		ref0:  hud.RefShow[0], ref1: hud.RefShow[1],
		// Decoded stream: its state and the list's page repaint the
		// display; while streaming, a quarter-second slot paces the
		// transcript at 4 Hz so drawing leaves the CPU to the event pump.
		streamOn: hud.StreamOn, streamState: int(hud.Stream.State), streamList: hud.StreamList,
		streamTop: hud.StreamPage.Top, streamTotal: hud.StreamPage.Total, streamTrig: hud.StreamPage.TriggerLine,
		streamSlot: StreamSlot(hud),
		extra:      hudExtraSig(&hud),
	}
}

// hudExtraSig fingerprints the rest of the HUD a held display shows,
// value by value: printing them with fmt every tick allocated a string.
// TRLC-LINKS: REQ-DSO-168
func hudExtraSig(hud *render.HUD) uint64 {
	h := sigOffset
	flag := func(b bool) {
		if b {
			h.byte1(1)
		} else {
			h.byte1(0)
		}
	}
	num := func(v int) { h.word(uint64(v)) } //nolint:gosec // G115: a hash input: the bits count, not the sign
	flt := func(v float64) { h.word(math.Float64bits(v)) }
	flag(hud.AutosetBusy)
	h.str(hud.AutosetMsg)
	h.str(hud.Remote)
	num(len(hud.MeasItems))
	for _, m := range hud.MeasItems {
		h.str(m)
	}
	num(hud.FFTSrc)
	num(hud.TrigSrc)
	flag(hud.TrigRising)
	flt(hud.TrigLvlV)
	flt(hud.TrigPosFrac)
	flag(hud.Waiting)
	num(hud.TrigType)
	q := hud.TrigQual // a copy: a field's address would move the whole HUD to the heap
	h.add(reflect.ValueOf(&q).Elem())
	flag(hud.SerialArmed)
	h.str(hud.SerialMatch)
	flag(hud.ZoneTrig)
	num(hud.Ch[0].Zoom)
	num(hud.Ch[1].Zoom)
	flag(hud.ChanExplicit)
	num(hud.DecBaud)
	num(hud.DecChA)
	num(hud.DecChB)
	flag(hud.DecCPOL)
	flag(hud.DecCPHA)
	num(hud.DecFormat)
	h.str(hud.MaskMsg)
	h.str(hud.SRStatus)
	h.str(hud.Notice)
	m := hud.Match
	h.add(reflect.ValueOf(&m).Elem())
	// The channels past C2 (a two-channel scope's are zero).
	if hud.NumChans > 2 {
		ch := hud.Ch
		h.add(reflect.ValueOf(&ch).Elem())
	}
	return uint64(h)
}

// run drives the display at the engine's render cadence until stop. It is
// optional: on any bring-up failure the scope keeps running headless.
// TRLC-LINKS: REQ-DSO-168
func (d *display) run(stop <-chan struct{}, scr Screen) { //nolint:gocyclo // the display loop: one tick's render decisions, read top to bottom in the order they apply
	env, e, fo := d.env, d.eng, d.env.Frames
	back, persistLayer := scr.Canvas(), scr.Layer()
	draw := func(f *sample.Frame, hud render.HUD, live bool) {
		if persistLayer != nil {
			render.Render(back, f, hud, live, persistLayer)
			return
		}
		render.Render(back, f, hud, live)
	}
	var sgSeq uint64 // last frame pushed into the waterfall
	sgSrc := -1      // the channel the waterfall shows (it restarts on a change)
	var lastSeq uint64
	var lastPresented uint64
	var lastFresh time.Time
	var lastSig renderSig
	var lastSlot statusSlot
	haveSig := false
	// The screen saver (ADR-SCREEN-SAVER): what the display shows, when the
	// saver began (its drift is the time since), when it last moved, and
	// the light.
	mode := panel.ScreenOn
	var saverT0, saverAt time.Time
	lit := true
	idle := false // the last tick drew nothing and saw no new frame
	var busyDrawAt time.Time
	var renderDue time.Time // when the next full picture may start
	var govAt time.Time     // when gov last looked
	gov := newDisplayGov()
	var pictures, govPictures uint64 // full pictures drawn, and as gov last counted
	var cpu cpuMeter
	wasBusy := false
	for {
		select {
		case <-stop:
			return
		default:
		}
		switch {
		case wasBusy:
			time.Sleep(displayIdleWait) // busy mode: only its own few draws a second
		case idle:
			// Nothing moved: wait for the next frame, or displayIdleWait for
			// the UI's own changes. Checking at the render cadence built the
			// whole HUD hundreds of times a second on a still screen.
			fo.WaitNext(lastSeq, max(e.RenderPeriod(), displayIdleWait))
		default:
			time.Sleep(e.RenderPeriod()) // the display's cadence
			// never faster than the panel refreshes, nor than the engine's
			// frame rate allows (displayGov): a picture costs the engine
			// frames (polled: one long sleep overshoots on a slow kernel's
			// timer)
			now := time.Now()
			if dt := now.Sub(govAt); dt >= govEvery {
				ws := env.Web
				page := ws != nil && ws.FrameClient(pageGone)
				rate := float64(pictures-govPictures) / dt.Seconds()
				if govAt.IsZero() {
					rate = 0
				}
				govAt, govPictures = now, pictures
				gov.step(env.Controls.Snapshot(), rate, cpu.busy(), page, mode == panel.ScreenOn && !wasBusy)
				framecalc.SetSpectrumHold(gov.hold)
				e.SetDisplayPace(gov.pace)
			}
			if mode == panel.ScreenOn && now.Before(renderDue) {
				continue
			}
		}
		seenSeq := lastSeq
		// Busy mode: while the engine reads a full record or loads an
		// image, and while AUTO searches, the display draws only what shows
		// the work going on (busyDraw), a few times a second; one full
		// redraw follows.
		pc := env.UI
		st := env.Controls.Snapshot()
		if bm := busyMode(st, pc != nil && pc.AutosetBusy(), pc != nil && pc.StreamMode()); bm != busyNone && mode == panel.ScreenOn { //nolint:nestif // the busy screen's draw, in place of the workspace's
			if now := time.Now(); now.Sub(busyDrawAt) >= busyEvery || !wasBusy {
				busyDrawAt, wasBusy = now, true
				e.QuietRLock()
				if busyDraw(bm, st, back, d.url()) {
					scr.PresentRows(0, render.StatusSlotRows)
				} else {
					hud := d.hud()
					fo.WithFrame(func(f *sample.Frame) {
						draw(f, hud, true)
						if f != nil && f.Seq != lastPresented {
							lastSeq, lastFresh, lastPresented = f.Seq, now, f.Seq
							e.NoteLCDFrame()
						}
					})
					scr.Present()
				}
				e.QuietRUnlock()
			}
			continue
		}
		if wasBusy {
			wasBusy, haveSig = false, false // the work is done: one full redraw
		}
		// Render only outside the engine's load-sensitive windows (arm
		// settle and drain): a concurrent render burst there corrupts a
		// capture on a single core.
		e.QuietRLock()
		if pc != nil {
			fo.WithFrame(pc.OnFrame) // the workspace pane's analysis, once per new frame
			if s := pc.Screen(); s != mode {
				mode = s
				saverT0, saverAt = time.Now(), time.Time{}
				haveSig = false // back on, the workspace repaints in full
			}
		}
		hud := d.hud()
		present := false
		fo.WithFrame(func(f *sample.Frame) {
			// The publish rate is slower than the render tick, so "fresh"
			// is a short window, not a per-tick flag: otherwise the
			// liveness strip flickers on every held tick.
			if f != nil && f.Seq != lastSeq {
				lastSeq = f.Seq
				lastFresh = time.Now()
			}
			if f != nil { //nolint:nestif // the frame's fields copied into the HUD only with a frame
				hud.Trigd = f.Status.Triggered // the on-screen TRIG'd indicator
				hud.SampleS = f.SampleS
				if d.cfg.OnFrame != nil {
					d.cfg.OnFrame(f, &hud)
				}
				// Spectrogram: each fresh per-sample frame's spectrum of the
				// FFT source is a new waterfall row while the SPECTROGRAM
				// view shows; a new source starts a new waterfall.
				if hud.ViewMode == 4 && !f.Flags.Has(sample.Envelope) && len(f.Codes8(0)) > 0 && (f.Seq != sgSeq || hud.FFTSrc&1 != sgSrc) {
					if hud.FFTSrc&1 != sgSrc {
						d.spect.Clear()
						sgSrc = hud.FFTSrc & 1
					}
					sgSeq = f.Seq
					effNyq := 0.0
					if f.SampleS > 0 {
						effNyq = 0.5 / f.SampleS
					}
					d.spect.Push(f, sgSrc, effNyq)
				}
			}
			// The UI model's own tick keeps the lamps and the remote's
			// snapshot in step; doing it here too doubles their cost.
			live := f != nil && time.Since(lastFresh) < 300*time.Millisecond
			// Asleep, the scope keeps acquiring and analysing; only the
			// workspace's render stops.
			switch mode {
			case panel.ScreenOff: // black once, then the light goes off
				if lit {
					back.Fill(0)
					present = true
				}
				return
			case panel.ScreenSaver:
				if now := time.Now(); now.Sub(saverAt) >= render.SaverStep {
					saverAt = now
					render.DrawSaver(back, f, hud, int(now.Sub(saverT0)/render.SaverStep))
					present = true
				}
				return
			case panel.ScreenOn: // the workspace renders, below
			}
			// Repaint only when the picture changed.
			force := forceRepaint(hud)
			sig := renderSigOf(f, hud, live)
			slot := statusSlot{hud.Busy, int(hud.BusyPct), hud.BusyPhase}
			// Stopped and the engine is re-showing the held record at the
			// new view: drawing the old frame at the new scale first only
			// delays it.
			stale := haveSig && hud.ViewPending && f != nil && f.Seq == lastSig.seq
			if haveSig && (stale || !force && sig == lastSig) {
				// Only the loading indicator moved: redraw its slot alone.
				if slot != lastSlot {
					lastSlot = slot
					render.RedrawStatusSlot(back, hud)
					present = true
				}
				return // nothing else visible changed: skip the rasterize
			}
			lastSig, haveSig, lastSlot = sig, true, slot
			// the next picture is due a period after this one was: a start
			// late by the poll or the engine's quiet window is not added to
			// the next, and a long pause does not bring a burst
			now := time.Now()
			renderDue = renderDue.Add(gov.period)
			if renderDue.Before(now) {
				renderDue = now.Add(gov.period / 2)
			}
			if hud.StreamOn && hud.StreamList && renderDue.Before(now.Add(streamPeriod)) {
				renderDue = now.Add(streamPeriod) // the transcript's text: a few pictures a second read it
			}
			pictures++
			draw(f, hud, live)
			present = true
		})
		if present {
			scr.Present()
			if mode == panel.ScreenOn && lastSeq != lastPresented {
				e.NoteLCDFrame()
				lastPresented = lastSeq
			}
		}
		// The light follows the picture: off once the black frame is up,
		// on once the next picture is.
		if want := mode != panel.ScreenOff; want != lit && (present || !want) {
			if err := scr.SetLight(want); err != nil {
				env.Logf("display: backlight: %v", err)
			}
			lit = want
		}
		idle = !present && lastSeq == seenSeq
		e.QuietRUnlock()
	}
}

// soundView is the SOUND view's ViewMode (the UI model's viewSound).
const soundView = 5

// forceRepaint reports a picture that changes without a frame or a UI
// change the render signature sees, which bypasses the repaint cache:
// afterglow decay, the Bode sweep, the spectrogram waterfall, super-res
// review, a running mask/zone test whose live counters advance faster
// than the published frame seq. The SOUND view is not one: its pane is in
// the signature and changes with each of the sound service's readings;
// forced, it was redrawn as fast as the core allowed and the audio
// stream's listeners ran dry.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-273
func forceRepaint(hud render.HUD) bool {
	return hud.Persist || (hud.ViewMode >= 3 && hud.ViewMode != soundView) || hud.SRActive ||
		hud.MaskMode > 0 || hud.ZoneMode > 0
}

// displayIdleWait bounds how long a still display waits for something to
// change that brings no frame (a menu, a notice, the loading indicator).
const displayIdleWait = 20 * time.Millisecond

// panelPeriod is the panel's refresh period (the SDS1000CML+'s 800x480 at
// 52 Hz): the loop puts up at most one picture a refresh.
const panelPeriod = time.Second / 52

// The display shares the core with the engine on a one-core board: a
// picture (render and copy, ~6 ms on the SDS1000CML+; in SPECTRUM two
// 8192-point spectra more, ~25) costs it frames, measured 53 fps with the
// panel at 51 and 64 with it at 32. Who gets the core depends on who is
// looking (displayGov.step).
const (
	engineFPS     = 61
	engineRoom    = 64
	slowestPeriod = time.Second / 20 // the panel looks live down to 20 pictures a second
	longestHold   = slowestPeriod
	longestPace   = time.Second / 20 // the engine runs at 20 frames a second or more
	coreBusy      = 0.90
	coreWaiting   = 0.85
	govStep       = 2 * time.Millisecond
	holdStep      = 10 * time.Millisecond
	paceStep      = time.Millisecond
	govEvery      = 250 * time.Millisecond
	// pageGone is how long after its last frame request a page counts as
	// gone (it polls at least every 1.5 s while it shows the scope).
	pageGone = 3 * time.Second
)

// displayGov is the display's share of the core: its period, how long it
// keeps a spectrum (framecalc.SetSpectrumHold) and the least time it asks
// an engine capture to take (DisplayEngine.SetDisplayPace).
type displayGov struct {
	period, hold, pace time.Duration
	lcd                float64 // pictures a second, smoothed
}

// newDisplayGov is the governor at the panel's rate, the engine unpaced.
// TRLC-LINKS: REQ-DSO-168
func newDisplayGov() *displayGov { return &displayGov{period: panelPeriod} }

// step looks once at the engine, the pictures drawn a second since the
// last look (lcd), the core's busy fraction, whether a web page shows the
// scope and whether the display draws the engine's frames at all (the
// screen on, not in its busy mode): asleep or busy it draws few or none on
// purpose, and that is no reason to pace the engine.
//
// With no page the panel is the only view and gets the core: pictures at
// the panel's rate, a new spectrum each. A picture needs a new frame, so
// while the display falls behind the engine's frames on a busy core the
// engine is paced (never under 20 frames a second) until it keeps up.
//
// With a page the engine's rate is what the page sees: while the engine is
// under engineFPS on a busy core the display's period grows, then it keeps
// each spectrum for later pictures; with room above engineRoom, or the
// core waiting (a record that takes longer than a 60th of a second to
// come), they come back. Never below 20 pictures a second, though: under
// that the panel stops looking live, worth more than the engine's frames,
// and a display still under it paces the engine.
// TRLC-LINKS: REQ-DSO-168
func (g *displayGov) step(st instrument.Stats, lcd, busy float64, page, drawing bool) {
	g.lcd = (g.lcd + lcd) / 2
	if !st.Running {
		*g = displayGov{period: panelPeriod}
		return
	}
	if !drawing {
		g.pace = 0
		return
	}
	busyCore := busy >= coreBusy
	if !page {
		g.period, g.hold = panelPeriod, 0
		switch {
		case busyCore && g.lcd < min(st.FPS, 52)-2:
			g.pace = min(g.pace+paceStep, longestPace)
		case g.lcd >= st.FPS-1 || busy < coreWaiting:
			g.pace = max(g.pace-paceStep, 0)
		}
		return
	}
	switch {
	case st.FPS < engineFPS && busyCore:
		if g.period < slowestPeriod {
			g.period = min(g.period+govStep, slowestPeriod)
		} else {
			g.hold = min(g.hold+holdStep, longestHold)
		}
	case st.FPS >= engineRoom || busy < coreWaiting:
		if g.hold > 0 {
			g.hold = max(g.hold-holdStep, 0)
		} else {
			g.period = max(g.period-govStep, panelPeriod)
		}
	}
	// paced only while the display falls behind the frames it is given: a
	// paced engine's frames bound the display too
	switch {
	case g.lcd < 19.5 && g.lcd < st.FPS-2 && g.period >= slowestPeriod && busyCore:
		g.pace = min(g.pace+paceStep, longestPace)
	case g.lcd > 21 || g.lcd >= st.FPS-1:
		g.pace = max(g.pace-paceStep, 0)
	}
}

// cpuMeter reads the cores' busy fraction from /proc/stat.
type cpuMeter struct{ total, idle uint64 }

// busy is the fraction of the time not idle (nor waiting on I/O) since the
// last call; 1 when /proc/stat cannot be read, so the governor acts on the
// frame rate alone.
// TRLC-LINKS: REQ-DSO-168
func (m *cpuMeter) busy() float64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 1
	}
	total, idle, ok := cpuTimes(b)
	if !ok {
		return 1
	}
	dt, di := total-m.total, idle-m.idle
	m.total, m.idle = total, idle
	if dt == 0 || dt < di {
		return 1
	}
	return 1 - float64(di)/float64(dt)
}

// cpuTimes sums the first "cpu" line of /proc/stat: all its times, and the
// idle and iowait ones.
// TRLC-LINKS: REQ-DSO-168
func cpuTimes(b []byte) (total, idle uint64, ok bool) {
	if len(b) < 4 || string(b[:4]) != "cpu " {
		return 0, 0, false
	}
	field, v, in := 0, uint64(0), false
	for _, c := range b[4:] {
		if c >= '0' && c <= '9' {
			v, in = v*10+uint64(c-'0'), true
			continue
		}
		if in {
			total += v
			if field == 3 || field == 4 {
				idle += v
			}
			field++
			v, in = 0, false
		}
		if c == '\n' {
			break
		}
	}
	return total, idle, field >= 5
}

// busyEvery is the busy mode's drawing period: the work shown going on a
// few times a second, the core left to the work.
const busyEvery = 250 * time.Millisecond

// streamPeriod is the least time between pictures while the decoded
// stream's transcript fills the screen: its lines scroll at text speed,
// and the full rate cost the core a tenth for nothing seen.
const streamPeriod = time.Second / 10

// The display's busy modes (busyMode).
const (
	busyNone    = iota
	busyLoad    // a full-record read or an image switch: the loading indicator alone
	busyAutoset // AUTO's search: the whole screen, with its banner, a few times a second
)

// busyMode is the display's busy mode: the engine's loading work (shown
// after the indicator's grace, so a quick read never enters it), else
// AUTO's search. A decoded stream is never one: its live view is the point.
// TRLC-LINKS: REQ-DSO-168
func busyMode(st instrument.Stats, autoset, streaming bool) int {
	switch {
	case streaming:
		return busyNone
	case st.Busy != "" && st.BusyMs >= 150:
		return busyLoad
	case autoset:
		return busyAutoset
	}
	return busyNone
}

// busyDraw draws the loading indicator alone into back and reports true
// for busyLoad; for busyAutoset it draws nothing and reports false (the
// caller renders the whole screen).
// TRLC-LINKS: REQ-DSO-168
func busyDraw(mode int, st instrument.Stats, back render.Canvas, url string) bool {
	if mode != busyLoad {
		return false
	}
	render.RedrawStatusSlot(back, render.HUD{Busy: st.Busy, BusyPct: st.BusyPct, BusyPhase: int(st.BusyMs / 250), URL: url})
	return true
}

// StreamSlot is a quarter-second counter while a decoded stream runs (0
// otherwise): the display's 4 Hz transcript refresh.
// TRLC-LINKS: REQ-DSO-018
func StreamSlot(hud render.HUD) int64 {
	if hud.StreamOn && (hud.Stream.State == streamwire.Streaming || hud.Stream.State == streamwire.Starting) {
		return time.Now().UnixMilli() / 250
	}
	return 0
}
