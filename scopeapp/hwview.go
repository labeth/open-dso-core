// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"context"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
)

// viewMode is the UI model's X-Y view (render.HUD.ViewMode).
const viewXY = 1

// viewRetries is how often a refused view is tried again before the loop
// waits for the UI to change it, and viewRetryWait the pause between: a
// view can meet a board that has not yet taken the state it was built
// from (a zoom window wider than the old timebase allows), which the next
// commit fixes.
const (
	viewRetries   = 3
	viewRetryWait = 500 * time.Millisecond
)

// viewKey is everything a renderer view is built from; equal keys are
// equal views.
type viewKey struct {
	mode          hal.DisplayMode
	held          bool
	start, span   float64 // the main window, only on a held view (0: the acquisition's)
	zoom          bool
	zStart, zSpan float64
}

// hwView drives a hardware waveform renderer from the display loop: it
// builds the view the UI shows (the main window, the zoom window with each
// channel's gain, the display mode, held while stopped) and hands each new
// one to the owner, which configures the renderer (Renderer.Configure is
// OWNER work) between captures.
type hwView struct {
	model *instrument.Model
	r     hal.Renderer
	nch   int
	divs  float64 // the graticule's divisions across
	logf  func(format string, a ...any)

	mu      sync.Mutex
	last    viewKey   // the view last posted
	pending bool      // posted, not yet run by the owner
	ok      bool      // the last one was taken
	tries   int       // refusals of the last one
	retryAt time.Time // when a refused one may go again
}

// newHWView is the renderer feed of env's board; nil when the board has no
// hardware renderer or the scope has its own owner.
// TRLC-LINKS: REQ-DSO-168
func newHWView(env *Env) *hwView {
	d := env.Board.Display()
	if d == nil || env.Model == nil {
		return nil
	}
	r, ok := d.Renderer()
	if !ok || r == nil {
		return nil
	}
	p := env.Board.Profile()
	divs := float64(p.Display.DivsH)
	if divs <= 0 {
		divs = 10
	}
	// The board starts drawing the acquisition's window in Y-T, unzoomed:
	// that view needs no configuring.
	return &hwView{model: env.Model, r: r, nch: p.Channels, divs: divs, logf: env.Logf, ok: true}
}

// keyOf is the view the UI shows over state st.
// TRLC-LINKS: REQ-DSO-168
func (h *hwView) keyOf(st instrument.State, hud *render.HUD) viewKey {
	var k viewKey
	if hud.ViewMode == viewXY {
		k.mode = hal.DisplayXY
	}
	span := st.TimeDivS * h.divs
	if !st.Running {
		k.held, k.start, k.span = true, st.DelayS-span/2, span
	}
	if hud.Zoom > 1 && k.mode == hal.DisplayYT && span > 0 {
		// The zoom window is centred on the trigger, panned by ZoomOff
		// across the record (the main window), as the CPU-drawn zoom.
		k.zoom = true
		k.zSpan = span / float64(hud.Zoom)
		k.zStart = hud.ZoomOff*span - k.zSpan/2
	}
	return k
}

// view is the renderer's view of key k.
// TRLC-LINKS: REQ-DSO-168
func (h *hwView) view(k viewKey) hal.View {
	v := hal.View{Mode: k.mode, Held: k.held, StartS: k.start, SpanS: k.span}
	if k.zoom {
		gain := make([]float64, h.nch)
		for i := range gain {
			gain[i] = 1 // the UI has no vertical zoom: the zoom window keeps the main view's scale
		}
		v.Zoom = &hal.View{StartS: k.zStart, SpanS: k.zSpan, Gain: gain}
	}
	return v
}

// tick posts the view the UI shows now when it changed, or when a refused
// one is due another try.
// TRLC-LINKS: REQ-DSO-168
func (h *hwView) tick(hud *render.HUD, now time.Time) {
	k := h.keyOf(h.model.State(), hud)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending {
		return
	}
	if k == h.last {
		if h.ok || h.tries >= viewRetries || now.Before(h.retryAt) {
			return
		}
	} else {
		h.tries = 0
	}
	h.last, h.pending = k, true
	v := h.view(k)
	h.model.Post(func(context.Context) {
		err := h.r.Configure(v)
		h.mu.Lock()
		defer h.mu.Unlock()
		h.pending, h.ok = false, err == nil
		if err != nil {
			if h.tries == 0 {
				h.logf("display: renderer view: %v", err)
			}
			h.tries++
			h.retryAt = time.Now().Add(viewRetryWait)
		}
	})
}
