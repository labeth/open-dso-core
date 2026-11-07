package sample

import (
	"math"
	"testing"
)

// A word that is a code or a Q8.8 step comes back from volts exactly, at
// every V/div and offset a front end gives, so a threshold half way between
// two codes rounds the same way after the trip.
// TRLC-LINKS: REQ-DSO-1003
func TestScaleWordRoundTrip(t *testing.T) {
	for _, vdiv := range []float64{1e-3, 2e-3, 5e-3, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 20, 50, 100} {
		for _, off := range []float64{0, 0.37, -1.3, 3 * vdiv, -9.99 * vdiv, 10 * vdiv, 123.456} {
			s := Scale{Zero: 128, PerCode: vdiv / 25, OffsetV: off}
			for w := -512.0; w <= 768; w += 1.0 / 256 {
				if got := s.Word(s.Volts(w)); got != w {
					t.Fatalf("vdiv %g off %g: word %v came back %v", vdiv, off, w, got)
				}
			}
			if r := math.Round(s.Word(s.Volts(100.5))); r != 101 {
				t.Fatalf("vdiv %g off %g: 100.5 rounds to %v after the trip", vdiv, off, r)
			}
		}
	}
	s := Scale{Zero: 128, PerCode: 0.04}
	for _, w := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := s.Word(s.Volts(w)); !(got == w || math.IsNaN(got) && math.IsNaN(w)) {
			t.Fatalf("%v came back %v", w, got)
		}
	}
	if w := 100.3; math.Abs(s.Word(s.Volts(w))-w) > 1e-9 {
		t.Fatalf("100.3 came back %v", s.Word(s.Volts(w)))
	}
}
