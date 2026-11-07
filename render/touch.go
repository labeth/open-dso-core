// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render

// keysColumnW is the softkey column's width: an open menu's choices, and
// under them the workspace rail.
const keysColumnW = 116

// TouchKey is the key a touch at (x, y) on screen m presses: F1..F5 in the
// softkey column on the right (a menu's choices or the workspace rail's),
// the slot whose row is nearest; "" elsewhere. A touch screen gives a
// panel without softkeys the same keys the display draws.
// TRLC-LINKS: REQ-DSO-212
func TouchKey(m *Metrics, x, y int) string {
	if x < m.W-keysColumnW || x >= m.W || y < 0 || y >= m.H {
		return ""
	}
	best, bd := 0, -1
	for i, ky := range m.SoftkeyY {
		d := y - ky
		if d < 0 {
			d = -d
		}
		if bd < 0 || d < bd {
			best, bd = i, d
		}
	}
	return [...]string{"f1", "f2", "f3", "f4", "f5"}[best]
}
