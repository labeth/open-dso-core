package wave

import (
	"encoding/binary"
	"math"
	"testing"
)

// The descriptor puts every field at its LeCroy offset.
// TRLC-LINKS: REQ-DSO-024
func TestDescOffsets(t *testing.T) {
	d := Desc{Template: "WAVEACE", CommType: 1, CommOrder: 1, WaveArray1: 40, InstrumentName: "X",
		InstrumentNumber: 0xCDAB, WaveArrayCount: 20, PointsPerScreen: 18, LastValid: 19, FirstPoint: 3,
		Sparsing: 2, SegmentIndex: -1, SweepsPerAcq: 7, PointsPerPair: 1, VerticalGain: .5, VerticalOffset: .25,
		MaxValue: 7680, NominalBits: 16, NomSubarrayCount: -1, HorizInterval: 1e-9, HorizOffset: -1e-6,
		PixelOffset: -1e-6, VertUnit: "V", HorUnit: "S", HorizUncertainty: 1e-9, RISSweeps: 1, Timebase: 9,
		VertCoupling: 1, ProbeAtt: 10, FixedVertGain: 4, BandwidthLimit: 1, VerticalVernier: 1, AcqVerticalOffset: .25, WaveSource: 2}
	b := d.Encode()
	le := binary.LittleEndian
	f32 := func(o int) float32 { return math.Float32frombits(le.Uint32(b[o:])) }
	checks := []struct {
		off  int
		got  any
		want any
	}{
		{0, string(b[0:8]), "WAVEDESC"}, {16, string(b[16:23]), "WAVEACE"}, {32, le.Uint16(b[32:]), uint16(1)},
		{36, le.Uint32(b[36:]), uint32(346)}, {60, le.Uint32(b[60:]), uint32(40)}, {92, le.Uint32(b[92:]), uint32(0xCDAB)},
		{120, le.Uint32(b[120:]), uint32(18)}, {140, int32(le.Uint32(b[140:])), int32(-1)}, {144, le.Uint32(b[144:]), uint32(7)},
		{164, f32(164), float32(7680)}, {172, le.Uint16(b[172:]), uint16(16)}, {176, f32(176), float32(1e-9)},
		{180, math.Float64frombits(le.Uint64(b[180:])), -1e-6}, {196, string(b[196:197]), "V"}, {244, string(b[244:245]), "S"},
		{324, le.Uint16(b[324:]), uint16(9)}, {328, f32(328), float32(10)}, {340, f32(340), float32(.25)}, {344, le.Uint16(b[344:]), uint16(2)},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("offset %d: %v want %v", c.off, c.got, c.want)
		}
	}
	if len(b) != DescSize {
		t.Fatalf("len %d", len(b))
	}
}

// Blocks carry their lengths; windows clip to the record and the limit.
// TRLC-LINKS: REQ-DSO-024
func TestBlocksAndWindow(t *testing.T) {
	if got := string(Block9("C1:WF DAT2,", []byte("ab"))); got != "C1:WF DAT2,#9000000002ab" {
		t.Fatal(got)
	}
	if got := string(Block("", make([]byte, 1000))[:6]); got != "#41000" {
		t.Fatal(got)
	}
	for _, c := range []struct {
		w                Window
		n                int
		first, step, cnt int
		ok               bool
	}{
		{Window{}, 100, 0, 1, 100, true},
		{Window{First: 3, Sparse: 4, Points: 10}, 100, 3, 4, 10, true},
		{Window{First: 3, Sparse: 4, Points: 1000}, 100, 3, 4, 25, true},
		{Window{First: 100}, 100, 0, 0, 0, false},
		{Window{Points: 0, Sparse: 2, Max: 30}, 100, 0, 2, 15, true},
		{Window{Points: 20, Sparse: 2, Max: 30}, 100, 0, 2, 15, true},
	} {
		f, s, n, ok := c.w.Span(c.n)
		if f != c.first || s != c.step || n != c.cnt || ok != c.ok {
			t.Errorf("%+v over %d: %d %d %d %v", c.w, c.n, f, s, n, ok)
		}
	}
	if got := Signed8([]uint8{128, 0, 255}, 0, 1, 3); got[0] != 0 || got[1] != 0x80 || got[2] != 0x7f {
		t.Fatal(got)
	}
	if got := Signed16([]uint16{0x8000, 0x8123}, 0, 1, 2, true); got[0] != 0 || got[2] != 0x01 || got[3] != 0x23 {
		t.Fatalf("% x", got)
	}
	if got := High8([]uint16{0x8000, 0xF800}, 0, 1, 2); got[0] != 0 || got[1] != 0x78 {
		t.Fatalf("% x", got)
	}
}
