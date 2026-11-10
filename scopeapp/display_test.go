// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP
package scopeapp

import (
	"testing"

	"github.com/labeth/open-dso-core/render"
)

// A hardware counter remains live even when the displayed capture is held.
// TRLC-LINKS: REQ-DSO-021
func TestCounterRepaintsHeldDisplay(t *testing.T) {
	h := render.HUD{Counter: "Counter C1 1 kHz"}
	before := renderSigOf(nil, h, false)
	h.Counter = "Counter C1 2 kHz"
	if before == renderSigOf(nil, h, false) {
		t.Fatal("counter change did not repaint a stopped display")
	}
}
