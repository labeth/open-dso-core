// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import "testing"

// TestWaveRepeatsTheLine: a bus's waves sample its lines' levels, repeat
// after the period, and every protocol's Make holds its roles.
// TRLC-LINKS: REQ-DSO-018
func TestWaveRepeatsTheLine(t *testing.T) {
	b, err := Make(Spec{Proto: "i2c", Set: map[string]string{"rate": "400k"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	ws := b.Waves(1e-4)
	for k, l := range b.Lines {
		for _, s := range l.Steps {
			for _, tm := range []float64{s.At, s.At + ws[k].Period} {
				if got := ws[k].At(tm + 1e-12); got != l.Fraction(l.Level(s.At)) {
					t.Fatalf("line %s at %g: %g, want %d", b.Roles[k], tm, got, l.Level(s.At))
				}
			}
		}
	}
	if got := ws[1].At(b.Period + 5e-5); got != 1 {
		t.Errorf("SDA idles high after the bus: %g", got)
	}
	for _, p := range Protocols() {
		for _, s := range Variants(p.Proto) {
			b, err := Make(s, 0)
			if err != nil || len(b.Lines) != len(p.Roles) || b.Period <= 0 {
				t.Errorf("%s: %v, %d lines for %v, period %g", s, err, len(b.Lines), p.Roles, b.Period)
			}
		}
	}
	if _, err := Make(Spec{Proto: "uart", Set: map[string]string{"nosuch": "1"}}, 0); err == nil {
		t.Error("an unknown setting was accepted")
	}
}
