package measure

import (
	"math"
	"reflect"
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// An 8-bit trace measures exactly as ComputeAcquisition; the same signal as
// 12-bit words left-aligned in 16 bits measures as its Q8.8 precision words.
func TestComputeTraceFormats(t *testing.T) {
	n := 4000
	codes, q := make([]uint8, n), make([]uint16, n)
	for i := range n {
		v := 128 + 60*math.Sin(2*math.Pi*float64(i)/400)
		codes[i], q[i] = uint8(math.Round(v)), uint16(math.Round(v*256))
	}
	f8 := sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	t8 := &sample.Trace{Format: f8, Codes8: codes, Precision: q, Valid: n, CodesPerDiv: 25}
	for _, cpl := range []int{0, 1, 2} {
		got := ComputeTrace(t8, 100, 3900, 0.04, 0.1, 1e-6, cpl, 4)
		want := ComputeAcquisition(codes[100:3900], q[100:3900], 0.04, 0.1, 1e-6, cpl, 4)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("8-bit coupling %d: %+v, want %+v", cpl, got, want)
		}
	}
	f12 := sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000}
	t12 := &sample.Trace{Format: f12, Codes16: q, Valid: n, CodesPerDiv: 25 * 256}
	got := ComputeTrace(t12, 100, 3900, 0.04, 0.1, 1e-6, 0, 4)
	want := ComputeAcquisition(codes[100:3900], q[100:3900], 0.04, 0.1, 1e-6, 0, 4)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("12-bit: %+v, want %+v", got, want)
	}
	if r := ComputeTrace(t12, 10, 5, 0.04, 0, 1e-6, 0, 0); r != nil {
		t.Errorf("an empty span measured %+v", r)
	}
}
