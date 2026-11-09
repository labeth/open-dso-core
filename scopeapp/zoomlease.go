// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/sample"
)

// zoomLeaseFor is how long one picture's zoom lease holds: the display
// renews it every picture, so a zoom left or a view changed ends it within
// a second.
const zoomLeaseFor = time.Second

// viewLeaser is an engine that reads a stretch of its live frames at full
// detail for a surface (HALScope's engine over the instrument model).
type viewLeaser interface {
	LeaseViewWindow(sf engine.ViewSurface, a, b float64, cols int, d time.Duration)
}

// zoomViewWindow is the stretch of live envelope frame f the display's
// zoom shows, in screen widths from the trigger, while zoomed in Y-T (X-Y
// draws no zoomed stretch, and its reads cost frames).
// TRLC-LINKS: REQ-DSO-021
func zoomViewWindow(hud render.HUD, f *sample.Frame) (a, b float64, ok bool) {
	if hud.Zoom <= 1 || hud.ViewMode != 0 || f == nil || f.Content != sample.PeakPairs || f.WinPoints <= 0 || f.Len() <= 0 {
		return 0, 0, false
	}
	c := hud.ZoomOff * float64(f.Len()) / float64(min(f.WinPoints, f.Len()))
	h := 0.5 / float64(hud.Zoom)
	return c - h, c + h, true
}

// zoomLease asks the engine for the zoomed stretch of the live envelope at
// full detail (the frame's view window) while the display's Y-T is zoomed
// in, as the scope's own screen does over its engine.
// TRLC-LINKS: REQ-DSO-021
func (d *display) zoomLease(f *sample.Frame, hud *render.HUD) {
	if d.lease == nil {
		return
	}
	if a, b, ok := zoomViewWindow(*hud, f); ok {
		d.lease.LeaseViewWindow(engine.ViewScope, a, b, render.PlotColumns(metricsFor(d.env.Board.Profile().Display), hud.UI.Layout), zoomLeaseFor)
	}
}
