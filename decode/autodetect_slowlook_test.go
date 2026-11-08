// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package decode

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/decode/synth"
)

// A 9600 Bd UART's text on a bus search's slow screens (32768 samples at
// 5 and 10 ms/div, wherever the screen starts) is no SENT: its pulses read
// as SENT nibbles put every third out of range, and a frame's CRC passing
// by chance among them named the bus SENT.
// TRLC-LINKS: REQ-DSO-018
func TestUARTOnASlowScreenIsNoSENT(t *testing.T) {
	b, err := synth.Make(synth.Spec{Proto: "uart", Set: map[string]string{"baud": "9600"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	per := b.Period * 1.1
	w := b.Lines[0].Wave(per)
	const n = 32768
	for _, tdiv := range []float64{5e-3, 10e-3} {
		dt := 10 * tdiv / n
		for off := range 8 {
			c1, c2 := make([]uint8, n), make([]uint8, n)
			for i := range c1 {
				c1[i], c2[i] = uint8(math.Round(56+144*w.At(per*float64(off)/8+float64(i)*dt))), 200
			}
			if r, _ := AutodetectFast(c1, c2, dt, "hex"); r.Proto == protoSENT {
				t.Errorf("%g s/div from %d/8: SENT %q", tdiv, off, r.Text)
			}
		}
	}
}
