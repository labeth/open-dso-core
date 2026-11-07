// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import "testing"

// The top-bar render: a URL paints dim text in the reserved top-bar span; an
// empty URL paints nothing there (the no-network case renders nothing).
// TRLC-LINKS: REQ-SDS-021
func TestRenderDeviceURL(t *testing.T) {
	region := func(m *Image565) int { return countColorIn(m, colDim, 380, 0, 670, 12) }

	h := defaultHUD()
	h.URL = "http://192.168.1.209:8080"
	with := NewImage565(m800)
	Render(with, testFrame(2048), h, true)
	if region(with) == 0 {
		t.Error("URL set but nothing drawn on the top bar")
	}

	h.URL = ""
	without := NewImage565(m800)
	Render(without, testFrame(2048), h, true)
	if n := region(without); n != 0 {
		t.Errorf("no-network HUD painted %d URL pixels, want none", n)
	}

	// The URL must not collide with the right-aligned trigger readout even at
	// its widest, nor with the math legend: check its extent stays in-lane.
	h.URL = "http://255.255.255.255:8080" // widest possible
	h.MathMode = 4                        // "M:C1xC2" legend at x=300
	wide := NewImage565(m800)
	Render(wide, testFrame(2048), h, true)
	if countColorIn(wide, colDim, 0, 0, 380, 12) != 0 {
		t.Error("widest URL leaked left of x=380 into the legend zone")
	}
	if countColorIn(wide, colDim, 665, 0, W, 12) != 0 {
		t.Error("URL leaked right of x=664 into the trigger readout zone")
	}
}
