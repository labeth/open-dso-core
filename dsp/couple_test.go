// ENGMODEL-OWNER-UNIT: FU-APP-DSP

package dsp

import (
	"math/rand"
	"slices"
	"testing"
)

// TRLC-LINKS: REQ-SDS-096
func TestCoupleDisplay(t *testing.T) {
	sig := []uint8{100, 200, 100, 200} // mean 150
	// DC passes through unchanged (same backing array).
	if got := CoupleDisplay(sig, CoupleDC); &got[0] != &sig[0] {
		t.Fatal("DC coupling copied/altered the signal")
	}
	// AC removes DC: mean → 128, so 100→78, 200→178 (shift −22).
	ac := CoupleDisplay(sig, CoupleAC)
	if ac[0] != 78 || ac[1] != 178 {
		t.Fatalf("AC = %v, want [78 178 ...]", ac[:2])
	}
	// GND is a flat mid-scale ground trace.
	for _, v := range CoupleDisplay(sig, CoupleGND) {
		if v != 128 {
			t.Fatalf("GND produced %d, want flat 128", v)
		}
	}
}

// TRLC-LINKS: REQ-SDS-096
func TestRemoveDC(t *testing.T) {
	sig := make([]uint8, 100)
	for i := range sig {
		sig[i] = 200 // flat DC level well above centre
	}
	out := RemoveDC(sig)
	if out[50] != 128 {
		t.Fatalf("flat 200 recentred to %d, want 128", out[50])
	}
	if sig[0] != 200 {
		t.Fatal("RemoveDC mutated its input")
	}
	// Clamps at the rails rather than wrapping.
	// (mean 64: a shift of +64 takes 255 past the top rail, to 255, not 63)
	if got := RemoveDC([]uint8{0, 0, 0, 255}); !slices.Equal(got, []uint8{64, 64, 64, 255}) {
		t.Fatalf("RemoveDC at the rail: %v", got)
	}
}

// benchRecord is a captured-looking record: a square wave with noise.
// TRLC-LINKS: REQ-SDS-096
func benchRecord(n int) []uint8 {
	r := rand.New(rand.NewSource(1)) //nolint:gosec // a fixed test pattern
	s := make([]uint8, n)
	for i := range s {
		v := 60
		if i/500%2 == 1 {
			v = 200
		}
		s[i] = uint8(v + r.Intn(9) - 4) //nolint:gosec // 56..204
	}
	return s
}

// TRLC-LINKS: REQ-SDS-096
func BenchmarkRemoveDC(b *testing.B) {
	s := benchRecord(1 << 16)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		RemoveDC(s)
	}
}

// RemoveDC and CoupleDisplayBy keep the clamp's result for every code and
// shift.
// TRLC-LINKS: REQ-SDS-096
func TestShiftedIsTheClamp(t *testing.T) {
	sig := make([]uint8, 256)
	for i := range sig {
		sig[i] = uint8(i) //nolint:gosec // 0..255
	}
	for shift := -300; shift <= 300; shift++ {
		got := shifted(sig, shift)
		for v, g := range got {
			if want := uint8(min(max(v+shift, 0), 255)); g != want { //nolint:gosec // clamped
				t.Fatalf("shift %d code %d: %d, want %d", shift, v, g, want)
			}
		}
	}
	s := benchRecord(5000)
	got := RemoveDC(s)
	var sum int
	for _, v := range s {
		sum += int(v)
	}
	if mean := float64(sum) / float64(len(s)); got[0] != uint8(int(s[0])+128-int(mean+0.5)) { //nolint:gosec // in range
		t.Fatalf("RemoveDC moved code %d to %d (mean %.2f)", s[0], got[0], mean)
	}
}
