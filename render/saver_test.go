// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import "testing"

// The saver box stays on the screen and moves one pixel each way every step,
// bouncing off the edges.
// TRLC-LINKS: REQ-SDS-221
func TestSaverDrifts(t *testing.T) {
	px, py := SaverPos(0)
	if px != 0 || py != 0 {
		t.Fatalf("step 0 at (%d,%d)", px, py)
	}
	reached := [2]bool{}
	for step := 1; step < 20000; step++ {
		x, y := SaverPos(step)
		if x < 0 || y < 0 || x+saverW > W || y+saverBoxH > H {
			t.Fatalf("step %d off the screen at (%d,%d)", step, x, y)
		}
		if d := (x - px) * (y - py); d != 1 && d != -1 {
			t.Fatalf("step %d moved (%d,%d) to (%d,%d)", step, px, py, x, y)
		}
		reached[0] = reached[0] || x+saverW == W
		reached[1] = reached[1] || y+saverBoxH == H
		px, py = x, y
	}
	if !reached[0] || !reached[1] {
		t.Fatalf("the box never reached the right or bottom edge: %v", reached)
	}
}
