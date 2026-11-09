package siggen

import (
	"testing"

	"github.com/labeth/open-dso-core/decode/synth"
)

// A line of a generator's bus (bus.PROTO.ROLE) carries that line at the
// levels asked, repeating every bus period and a tenth of it idle, in step
// with the bus's other lines; a protocol, line or setting the generator
// has not is refused.
// TRLC-LINKS: REQ-DSO-298
func TestSynthBusLine(t *testing.T) {
	s, err := Parse("bus.i2c.sda,0,3.3,1.65,rate=400k")
	if err != nil {
		t.Fatal(err)
	}
	b, err := synth.Make(synth.Spec{Proto: "i2c", Set: map[string]string{"rate": "400k"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := 1 / (b.Period * 1.1); s.Freq < want*0.999 || s.Freq > want*1.001 || !s.IsLine() {
		t.Fatalf("repeats at %g Hz, want %g; a line %v", s.Freq, want, s.IsLine())
	}
	sda := b.Lines[1].Wave(b.Period * 1.1)
	for _, at := range []float64{0, b.Period / 3, b.Period / 2, b.Period * 1.05} {
		want := 1.65 - 1.65 + 3.3*sda.At(at)
		if v := s.V(at + b.Period*1.1); v < want-0.01 || v > want+0.01 {
			t.Fatalf("at %g s: %g V, want %g", at, v, want)
		}
	}
	for _, bad := range []string{"bus.nope.x,0,3.3,1.65", "bus.i2c.tx,0,3.3,1.65", "bus.i2c.sda,0,3.3,1.65,speed=9"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("%s parsed", bad)
		}
	}
}
