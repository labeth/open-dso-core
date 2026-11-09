// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"github.com/labeth/open-dso-core/engine"
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
