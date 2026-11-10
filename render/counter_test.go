// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER
package render

import (
	"testing"

	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// TRLC-LINKS: REQ-DSO-021, REQ-DSO-299
func TestTouchHardwareCounterReadout(t *testing.T) {
	h := defaultHUD()
	h.NumChans = 4
	h.UI.Layout.Touch = true
	h.UI.WS = wsview.WS(0)
	a, b := NewImage565(&Metrics1024x600), NewImage565(&Metrics1024x600)
	touchDraw{sf: a}.topBar(nil, h, true)
	h.Counter = "Counter C1 25.00012 kHz"
	touchDraw{sf: b}.topBar(nil, h, true)
	if countColorIn(b, colInfo, 742, 20, 930, 32) <= countColorIn(a, colInfo, 742, 20, 930, 32) {
		t.Fatal("hardware counter not visible in touch readout bar")
	}
}
