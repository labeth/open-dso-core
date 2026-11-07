// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"testing"
)

// TRLC-LINKS: REQ-DSO-014
func TestBuildMaskFromEnvelope(t *testing.T) {
	win := 100
	lo := make([]uint8, win)
	hi := make([]uint8, win)
	for i := range lo {
		lo[i], hi[i] = 100, 120
	}
	// a step at column 50
	for i := 50; i < win; i++ {
		lo[i], hi[i] = 180, 200
	}
	m := BuildMaskFromEnvelope(lo, hi, win, 2, 5, 0)
	if m == nil {
		t.Fatal("nil mask")
	}
	if m.Lo[10] != 95 || m.Hi[10] != 125 {
		t.Errorf("flat region dilation: [%d,%d]", m.Lo[10], m.Hi[10])
	}
	// near the step, horizontal dilation must open the envelope across it
	if m.Lo[49] != 95 || m.Hi[49] != 205 {
		t.Errorf("step-adjacent dilation: [%d,%d]", m.Lo[49], m.Hi[49])
	}
	if BuildMaskFromEnvelope(lo, hi, win+1, 1, 1, 0) != nil {
		t.Error("length mismatch must return nil")
	}
}
