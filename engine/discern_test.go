// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"testing"
)

// TRLC-LINKS: REQ-SDS-011
func TestMidLevel(t *testing.T) {
	if got := MidLevel(nil); got != 128 {
		t.Errorf("midLevel(nil) = %d, want 128", got)
	}
	if got := MidLevel([]uint8{10, 200}); got != 105 {
		t.Errorf("midLevel = %d, want 105", got)
	}
}

// TRLC-LINKS: REQ-SDS-009, REQ-SDS-011
func TestPtp(t *testing.T) {
	lo, hi, p := Ptp([]uint8{100, 50, 220, 128})
	if lo != 50 || hi != 220 || p != 170 {
		t.Errorf("ptp = %d/%d/%d, want 50/220/170", lo, hi, p)
	}
	if _, _, p := Ptp(nil); p != 0 {
		t.Errorf("ptp(nil) = %d, want 0", p)
	}
}
