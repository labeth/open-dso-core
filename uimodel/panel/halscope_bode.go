// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/engine/fanout"
	"github.com/labeth/open-dso-core/sample"
)

// SetBodeMode arms or disarms the frequency response on channels refCh and
// dutCh (any of the scope's).
// TRLC-LINKS: REQ-DSO-066
func (e halEngine) SetBodeMode(on bool, refCh, dutCh int) { e.bode.SetBodeMode(on, refCh, dutCh) }

// ClearBode empties the frequency response's curve.
// TRLC-LINKS: REQ-DSO-066
func (e halEngine) ClearBode() { e.bode.ClearBode() }

// BodePoints is the frequency response's curve.
// TRLC-LINKS: REQ-DSO-066
func (e halEngine) BodePoints() []engine.BodePoint { return e.bode.BodePoints() }

// Bode is the frequency response the panel and the web arm (the web
// scope's Bode part).
// TRLC-LINKS: REQ-DSO-066
func (s *HALScope) Bode() *engine.Bode { return s.bode }

// Watch measures the frequency response on each frame frames publishes
// while it is armed, until stop: the owner loop has no Bode of its own, so
// it runs beside it on the published frames' 8-bit view.
// TRLC-LINKS: REQ-DSO-066
func (s *HALScope) Watch(frames *fanout.Fanout[sample.Frame], stop <-chan struct{}) {
	var last uint64
	vdiv := func(ch int) float64 { // at the probe tip, as the gain compares
		if c, ok := s.channel(ch); ok {
			return c.VoltsDiv
		}
		return 1
	}
	for {
		select {
		case <-stop:
			return
		default:
		}
		seq := frames.WaitNext(last, 200*time.Millisecond)
		if seq == last || !s.bode.Armed() {
			last = seq
			continue
		}
		last = seq
		frames.WithFrame(func(f *sample.Frame) {
			if f != nil {
				s.bode.Frame(sample.View8Frame(f), vdiv)
			}
		})
	}
}
