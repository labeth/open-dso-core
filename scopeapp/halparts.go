package scopeapp

import (
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/webserver"
)

// halSerial is the HAL path's protocol trigger as the web takes it: the
// instrument state's, which the owner programs as the board's protocol
// trigger.
type halSerial struct{ m *instrument.Model }

// SerialConfig is the request as staged.
// TRLC-LINKS: REQ-DSO-162, REQ-DSO-013
func (h halSerial) SerialConfig() (engine.SerialParams, bool) { return h.m.SerialConfig(), true }

// SetSerialParams stages a request.
// TRLC-LINKS: REQ-DSO-162, REQ-DSO-013
func (h halSerial) SetSerialParams(p engine.SerialParams) { h.m.SetSerialParams(p) }

// SerialMode is 1 while the serial trigger is armed.
// TRLC-LINKS: REQ-DSO-162, REQ-DSO-013
func (h halSerial) SerialMode() int { return h.m.State().SerialMode }

// halLeases are a page's leases on the HAL path: the UI model's engine
// takes each as the panel's own (panel.HALScope).
type halLeases struct{ eng any }

// Lease renews or ends one lease; false for a kind the engine has none of.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-010
func (l halLeases) Lease(n webserver.Need) bool {
	lease := func(fn func(time.Duration)) bool { fn(n.Hold); return true }
	switch e := l.eng; n.Kind {
	case webserver.NeedRaw:
		if x, ok := e.(interface{ LeaseRawFor(string, time.Duration) }); ok {
			return lease(func(d time.Duration) { x.LeaseRawFor("web", d) })
		}
	case webserver.NeedSpectrum:
		if x, ok := e.(interface{ LeaseSpectrumView(time.Duration) }); ok {
			return lease(x.LeaseSpectrumView)
		}
	case webserver.NeedSamples:
		if x, ok := e.(interface{ LeaseSampleView(time.Duration) }); ok {
			return lease(x.LeaseSampleView)
		}
	case webserver.NeedSearch:
		if x, ok := e.(interface{ LeaseSearchView(time.Duration) }); ok {
			return lease(x.LeaseSearchView)
		}
	case webserver.NeedView:
		if x, ok := e.(interface {
			LeaseViewWindow(engine.ViewSurface, float64, float64, int, time.Duration)
		}); ok {
			x.LeaseViewWindow(engine.ViewWeb, n.View.A, n.View.B, n.View.Cols, n.Hold)
			return true
		}
	}
	return false
}
