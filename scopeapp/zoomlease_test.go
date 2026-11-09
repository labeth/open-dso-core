// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"math"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/uimodel/panel"
)

// The display's zoom leases its stretch of a live envelope at full detail
// in Y-T only: X-Y draws no zoomed stretch, and the reads cost frames.
// TRLC-LINKS: REQ-DSO-021
func TestZoomViewWindowOnlyInYT(t *testing.T) {
	f := &sample.Frame{WinPoints: 1024, Content: sample.PeakPairs, Traces: []sample.Trace{{Valid: 4096}, {Ch: 1, Valid: 4096}}}
	hud := render.HUD{Zoom: 10, ZoomOff: 0.01}
	a, b, ok := zoomViewWindow(hud, f)
	if !ok || math.Abs(a-(0.04-0.05)) > 1e-12 || math.Abs(b-(0.04+0.05)) > 1e-12 {
		t.Fatalf("Y-T zoomed: lease %v %g..%g, want -0.01..0.09 screen widths", ok, a, b)
	}
	hud.ViewMode = 1 // X-Y
	if _, _, ok := zoomViewWindow(hud, f); ok {
		t.Fatal("X-Y leased a zoomed stretch it does not draw")
	}
	hud.ViewMode, hud.Zoom = 0, 1
	if _, _, ok := zoomViewWindow(hud, f); ok {
		t.Fatal("not zoomed, yet leased")
	}
	hud.Zoom = 10
	if _, _, ok := zoomViewWindow(hud, &sample.Frame{WinPoints: 1024, Content: sample.RawFullRate, Traces: f.Traces}); ok {
		t.Fatal("a frame of samples leased a view window it already holds")
	}
}

// leases records the view windows asked for.
type leases struct {
	sf   []engine.ViewSurface
	cols []int
	d    []time.Duration
}

func (l *leases) LeaseViewWindow(sf engine.ViewSurface, _, _ float64, cols int, d time.Duration) {
	l.sf, l.cols, l.d = append(l.sf, sf), append(l.cols, cols), append(l.d, d)
}

// A zoomed picture leases the scope surface's view window over the plot's
// columns for a second; a picture without a leasing engine asks nothing.
// TRLC-LINKS: REQ-DSO-021
func TestDisplayZoomLease(t *testing.T) {
	env := &Env{Board: halfake.New(halfake.Config{})}
	l := &leases{}
	d := &display{env: env, lease: l}
	f := &sample.Frame{WinPoints: 1024, Content: sample.PeakPairs, Traces: []sample.Trace{{Valid: 4096}, {Ch: 1, Valid: 4096}}}
	hud := render.HUD{Zoom: 4}
	d.zoomLease(f, &hud)
	want := render.PlotColumns(metricsFor(env.Board.Profile().Display), hud.UI.Layout)
	if len(l.sf) != 1 || l.sf[0] != engine.ViewScope || l.cols[0] != want || l.d[0] != zoomLeaseFor {
		t.Fatalf("leases %+v, want one of the scope surface over %d columns", l, want)
	}
	hud.Zoom = 1
	d.zoomLease(f, &hud)
	if len(l.sf) != 1 {
		t.Fatal("leased without a zoom")
	}
	(&display{env: env}).zoomLease(f, &render.HUD{Zoom: 4}) // no engine to lease from
}

// The core's UI over a HAL scope leases from the HAL scope's engine.
// TRLC-LINKS: REQ-DSO-021
func TestHALScopeEngineLeasesViews(t *testing.T) {
	b := halfake.New(halfake.Config{})
	m := instrument.New(instrument.State{Channels: make([]hal.Channel, b.Profile().Channels)}, nil)
	if _, ok := panel.NewHALScope(m, b, "test").Engine().(viewLeaser); !ok {
		t.Fatal("the HAL scope's engine leases no view window")
	}
}
