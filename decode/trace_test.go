package decode

import (
	"reflect"
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// A bus read as 12-bit words decodes as the same bus read as 8-bit codes.
func TestAutodetectTracesFormats(t *testing.T) {
	const n = 20000
	c1 := make([]uint8, n)
	bit := 0
	for i := range c1 { // a UART at 100 samples a bit: 'A' repeated
		frame := []int{0, 1, 0, 0, 0, 0, 0, 1, 0, 1, 1, 1}
		bit = frame[(i/100)%len(frame)]
		c1[i] = uint8(70 + 120*bit)
	}
	flat := make([]uint8, n)
	for i := range flat {
		flat[i] = 128
	}
	f8 := sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	f12 := sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Max: 0xFFF0, Mid: 0x8000}
	to16 := func(c []uint8) []uint16 {
		w := make([]uint16, len(c))
		for i, v := range c {
			w[i] = uint16(v) << 8
		}
		return w
	}
	a8 := &sample.Trace{Format: f8, Codes8: c1, Valid: n}
	b8 := &sample.Trace{Format: f8, Codes8: flat, Valid: n}
	a12 := &sample.Trace{Format: f12, Codes16: to16(c1), Valid: n}
	b12 := &sample.Trace{Format: f12, Codes16: to16(flat), Valid: n}
	if got := Codes8(a12); !reflect.DeepEqual(got, c1) {
		t.Fatal("the 8-bit view of left-aligned words is not their high byte")
	}
	want := Autodetect(c1, flat, 1e-6, "hex")
	if got := AutodetectTraces(a8, b8, 1e-6, "hex"); !reflect.DeepEqual(got, want) {
		t.Errorf("8-bit traces: %+v, want %+v", got.Proto, want.Proto)
	}
	if got := AutodetectTraces(a12, b12, 1e-6, "hex"); !reflect.DeepEqual(got, want) {
		t.Errorf("12-bit traces: %+v, want %+v", got.Proto, want.Proto)
	}
}
