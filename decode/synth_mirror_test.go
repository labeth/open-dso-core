// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package decode

import (
	"slices"
	"testing"

	"github.com/labeth/open-dso-core/decode/synth"
)

// A bus whose truth says it reads the same with its lines the other way
// round (synth.Truth.Mirrored: a quadrature encoder, whose direction is
// which line is called A) decodes to that truth with its lines swapped.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-250
func TestSynthMirroredBuses(t *testing.T) {
	for _, s := range []synth.Spec{{Proto: "quad"}, {Proto: "quad", Payload: []int{1, 1, 0, 1, 0, 0, 1, 1}}} {
		b, err := synth.Make(s, 0)
		if err != nil {
			t.Fatal(err)
		}
		if b, err = synth.Make(s, b.NaturalSR); err != nil {
			t.Fatal(err)
		}
		a, c := b.Lines[0].Codes(), b.Lines[1].Codes()
		if r := Quadrature(a, c, 1/b.NaturalSR, ClockedCfg{}); !slices.Equal(r.Bytes, b.Truth.Bytes) {
			t.Errorf("%s: A then B % x, want % x", s, r.Bytes, b.Truth.Bytes)
		}
		if r := Quadrature(c, a, 1/b.NaturalSR, ClockedCfg{}); !slices.Equal(r.Bytes, b.Truth.Mirrored) {
			t.Errorf("%s: B then A % x, want % x", s, r.Bytes, b.Truth.Mirrored)
		}
	}
}
