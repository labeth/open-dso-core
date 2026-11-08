// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import "testing"

// TestStepsMatchSamples: every bus's steps (what a signal source samples
// itself, Make at rate 0) are its samples (Make at a rate): a level a
// generator took from the samples written so far, and so not from the
// steps, opened a Miller message on the wrong level. A sample at an edge
// may fall either side of it.
// TRLC-LINKS: REQ-DSO-018
func TestStepsMatchSamples(t *testing.T) {
	for _, p := range Protocols() {
		for _, s := range Variants(p.Proto) {
			b0, err := Make(s, 0)
			if err != nil {
				t.Fatal(err)
			}
			sr := b0.NaturalSR * 4
			b, err := Make(s, sr)
			if err != nil {
				t.Fatal(err)
			}
			for li, l := range b0.Lines {
				w := l.Wave(b0.Period)
				bits := b.Lines[li].Bits
				bad := 0
				for i := range bits {
					if w.At((float64(i)+0.5)/sr) != b.Lines[li].Fraction(bits[i]) {
						bad++
					}
				}
				if bad > len(l.Steps)+2 {
					t.Errorf("%s line %d: %d of %d samples differ, the line has %d steps", s, li, bad, len(bits), len(l.Steps))
				}
			}
		}
	}
}
