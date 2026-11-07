package render

import "testing"

// A touch in the softkey column presses the nearest F-key; elsewhere none.
// TRLC-LINKS: REQ-DSO-212
func TestTouchKey(t *testing.T) {
	m := &Metrics1024x600
	for i, y := range m.SoftkeyY {
		if got, want := TouchKey(m, m.W-10, y+5), [...]string{"f1", "f2", "f3", "f4", "f5"}[i]; got != want {
			t.Errorf("row %d: %q, want %q", i, got, want)
		}
	}
	if got := TouchKey(m, 500, 300); got != "" {
		t.Fatalf("the plot: %q", got)
	}
	if got := TouchKey(m, m.W, 300); got != "" {
		t.Fatalf("off the screen: %q", got)
	}
}
