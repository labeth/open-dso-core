package scopeapp

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
)

// fakeRenderer records the views it is configured with.
type fakeRenderer struct {
	mu     sync.Mutex
	views  []hal.View
	refuse error
}

func (r *fakeRenderer) Configure(v hal.View) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.views = append(r.views, v)
	return r.refuse
}

func (r *fakeRenderer) SetAppearance(hal.Appearance) error { return nil }
func (r *fakeRenderer) Area() image.Rectangle              { return image.Rect(100, 60, 700, 420) }
func (r *fakeRenderer) Colors([]color.RGBA) error          { return nil }

// last is the newest view, false before one.
func (r *fakeRenderer) last() (hal.View, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.views) == 0 {
		return hal.View{}, false
	}
	return r.views[len(r.views)-1], true
}

// planeBlue is the colour the fake board's read-back shows its plane in.
var planeBlue = color.RGBA{B: 0xff, A: 0xff}

// hwDisplay is an ARGB framebuffer over a hardware renderer whose read-back
// shows the plane blue.
type hwDisplay struct {
	r    *fakeRenderer
	back []byte
}

func (d *hwDisplay) Spec() hal.DisplaySpec          { return hwSpec }
func (d *hwDisplay) Framebuffer() hal.Framebuffer   { return d }
func (d *hwDisplay) Renderer() (hal.Renderer, bool) { return d.r, true }
func (d *hwDisplay) Backlight(float64) error        { return nil }
func (d *hwDisplay) Back() []byte                   { return d.back }
func (d *hwDisplay) Stride() int                    { return 4 * hwSpec.W }
func (d *hwDisplay) Present() error                 { return nil }
func (d *hwDisplay) Capture(dst *image.RGBA) error {
	for y := dst.Rect.Min.Y; y < dst.Rect.Max.Y; y++ {
		for x := dst.Rect.Min.X; x < dst.Rect.Max.X; x++ {
			dst.SetRGBA(x, y, planeBlue)
		}
	}
	return nil
}

var hwSpec = hal.DisplaySpec{W: 800, H: 480, Format: hal.ARGB8888, HWRenderer: true, DivsH: 10, DivsV: 8}

// hwBoard is the fake board with a hardware renderer under an ARGB
// framebuffer.
type hwBoard struct {
	*halfake.Board
	d *hwDisplay
}

func (b hwBoard) Profile() hal.Profile {
	p := b.Board.Profile()
	p.Display = hwSpec
	return p
}

func (b hwBoard) Display() hal.Display { return b.d }

func newHWBoard() (hwBoard, *fakeRenderer) {
	r := &fakeRenderer{}
	return hwBoard{Board: halfake.New(halfake.Config{Channels: 4}), d: &hwDisplay{r: r, back: make([]byte, 4*hwSpec.W*hwSpec.H)}}, r
}

// near reports a and b equal to a part in a million.
func near(a, b float64) bool { return math.Abs(a-b) <= 1e-6*math.Max(math.Abs(a), math.Abs(b)) }

// The UI's zoom and timebase drive the hardware renderer: the display loop
// hands the owner the zoom window (the main window over the zoom factor,
// each channel at unity gain), follows a timebase change, draws the held
// record while stopped and leaves the zoom off again.
// TRLC-LINKS: REQ-DSO-168
func TestRendererFollowsTheUIsView(t *testing.T) {
	b, r := newHWBoard()
	env, stop := run(t, b)
	defer stop()
	if res := env.UI.SheetSet("time.zoom", nil, "10x"); !res.OK {
		t.Fatalf("zoom: %+v", res)
	}
	zoomed := func(span float64) func() bool {
		return func() bool {
			v, ok := r.last()
			return ok && v.Zoom != nil && near(v.Zoom.SpanS, span) && near(v.Zoom.StartS, -span/2) && len(v.Zoom.Gain) == 4 && v.Zoom.Gain[3] == 1
		}
	}
	waitFor(t, "the renderer's zoom window at 1 ms across", zoomed(1e-3)) // 1 ms/div × 10 div / 10
	if err := env.Model.Change(func(s *instrument.State) error { s.TimeDivS = 1e-6; return nil }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the zoom window following the timebase", zoomed(1e-6))
	if err := env.Model.Change(func(s *instrument.State) error { s.Running = false; return nil }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a held view while stopped", func() bool {
		v, ok := r.last()
		return ok && v.Held && near(v.SpanS, 10e-6) && near(v.StartS, -5e-6)
	})
	if res := env.UI.SheetSet("time.zoom", nil, "Off"); !res.OK {
		t.Fatalf("zoom off: %+v", res)
	}
	waitFor(t, "the zoom window gone", func() bool { v, ok := r.last(); return ok && v.Zoom == nil })
}

// The screen of a scope with a hardware renderer is the board's read-back
// with the UI layer over it, at the display's native size: the plane shows
// through the renderer's area, the UI covers the rest.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-168
func TestScreenOverAHardwareRenderer(t *testing.T) {
	b, r := newHWBoard()
	env, stop := run(t, b)
	defer stop()
	waitFor(t, "the display loop serving captures", env.display.capturing.Load)
	img, err := png.Decode(bytes.NewReader(env.ScreenPNG()))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, hwSpec.W, hwSpec.H) {
		t.Fatalf("the screen is %v", img.Bounds())
	}
	blue := func(rect image.Rectangle) (n, all int) {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				all++
				if r, g, b, _ := img.At(x, y).RGBA(); r == 0 && g == 0 && b == 0xffff {
					n++
				}
			}
		}
		return n, all
	}
	if n, all := blue(r.Area()); n < all*9/10 {
		t.Errorf("%d of %d pixels of the renderer's area show the plane", n, all)
	}
	if n, all := blue(image.Rect(0, 0, hwSpec.W, r.Area().Min.Y)); n > all/10 {
		t.Errorf("%d of %d pixels above the plane show it through the UI", n, all)
	}
}

// A refused view is tried again a few times, then left until the UI
// changes it; an unchanged view is not posted again.
// TRLC-LINKS: REQ-DSO-168
func TestRefusedViewRetries(t *testing.T) {
	m := instrument.New(instrument.State{TimeDivS: 1e-3, Running: true}, nil)
	r := &fakeRenderer{refuse: errors.New("no")}
	h := &hwView{model: m, r: r, nch: 2, divs: 10, logf: t.Logf, ok: true}
	serve := func() {
		for _, w := range m.TakeWork() {
			w(context.Background())
		}
	}
	hud := render.HUD{Zoom: 2}
	now := time.Now()
	for range 10 {
		h.tick(&hud, now)
		serve()
		now = now.Add(viewRetryWait)
	}
	if got := len(r.views); got != viewRetries {
		t.Fatalf("%d tries of a refused view, want %d", got, viewRetries)
	}
	r.refuse = nil
	hud.Zoom = 5
	h.tick(&hud, now)
	serve()
	h.tick(&hud, now)
	serve()
	if got := len(r.views); got != viewRetries+1 {
		t.Fatalf("%d views after a change taken once, want %d", got, viewRetries+1)
	}
}
