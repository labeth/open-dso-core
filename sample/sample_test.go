package sample

import (
	"strings"
	"testing"
)

// TRLC-LINKS: REQ-DSO-1003
func TestContentText(t *testing.T) {
	for c := RawFullRate; c <= DisplayDomain; c++ {
		b, err := c.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var back Content
		if err := back.UnmarshalText(b); err != nil || back != c {
			t.Errorf("%v round trip: %v %v", c, back, err)
		}
	}
	var c Content
	if err := c.UnmarshalText([]byte("q88")); err == nil {
		t.Error("unknown content accepted")
	}
	if Content(0).String() != "unknown" {
		t.Error("zero content has a name")
	}
}

// TRLC-LINKS: REQ-DSO-1003
func TestContentSet(t *testing.T) {
	s := ContentSetOf(RawFullRate, PeakPairs)
	if !s.Has(RawFullRate) || !s.Has(PeakPairs) || s.Has(Resampled) || s.Has(0) {
		t.Fatalf("set %v", s)
	}
	var back ContentSet
	if err := back.UnmarshalText([]byte(s.String())); err != nil || back != s {
		t.Errorf("round trip %v: %v %v", s, back, err)
	}
	if err := back.UnmarshalText([]byte("raw-full-rate,bogus")); err == nil {
		t.Error("bad member accepted")
	}
}

// TRLC-LINKS: REQ-DSO-1003
func TestCodeFormatValidate(t *testing.T) {
	ok := []CodeFormat{
		{Bits: 8, Storage: U8, Max: 255, Mid: 128},
		{Bits: 12, Storage: U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000},
		{Bits: 8, Storage: U16, Frac: 8, Max: 0xFFFF, Mid: 128 << 8},
	}
	for _, f := range ok {
		if err := f.Validate(); err != nil {
			t.Errorf("%+v: %v", f, err)
		}
	}
	bad := map[string]CodeFormat{
		"no storage":  {Bits: 8, Max: 255},
		"no bits":     {Storage: U8, Max: 255},
		"too wide":    {Bits: 12, Storage: U8, Max: 255},
		"max too big": {Bits: 8, Storage: U8, Max: 256},
		"mid outside": {Bits: 8, Storage: U8, Min: 10, Max: 255, Mid: 5},
	}
	for name, f := range bad {
		if err := f.Validate(); err == nil {
			t.Errorf("%s: %+v accepted", name, f)
		}
	}
}

// TRLC-LINKS: REQ-DSO-1003
func TestTraceVolts(t *testing.T) {
	tr8 := Trace{Format: CodeFormat{Bits: 8, Storage: U8}, Codes8: []uint8{128, 153}, Scale: Scale{Zero: 128, PerCode: 0.04}}
	if v := tr8.Volts(1); v < 0.999 || v > 1.001 {
		t.Errorf("8-bit volts %g", v)
	}
	tr16 := Trace{Format: CodeFormat{Bits: 12, Storage: U16, Shift: 4}, Codes16: []uint16{0x8000 + 7680}, Scale: Scale{Zero: 0x8000, PerCode: 1.0 / 7680, OffsetV: 0.5}}
	if v := tr16.Volts(0); v < 0.499 || v > 0.501 {
		t.Errorf("16-bit volts %g", v)
	}
}

// TRLC-LINKS: REQ-DSO-1003
func TestCopyFromOwnsStorage(t *testing.T) {
	src := &Frame{
		Seq:   7,
		Match: &Match{EndS: 1e-6},
		Traces: []Trace{
			{Ch: 0, Valid: 2, Codes8: []uint8{1, 2, 3}, Precision: []uint16{256, 512, 768}},
			{Ch: 1, Valid: 2, Codes16: []uint16{10, 20, 30}},
		},
	}
	var dst Frame
	dst.CopyFrom(src)
	src.Traces[0].Codes8[0] = 99
	src.Traces[0].Precision[0] = 0
	src.Traces[1].Codes16[0] = 0
	src.Match.EndS = 0
	if dst.Seq != 7 || dst.Traces[0].Codes8[0] != 1 || dst.Traces[0].Precision[0] != 256 || dst.Traces[1].Codes16[0] != 10 || dst.Match.EndS != 1e-6 {
		t.Fatalf("copy aliases its source: %+v", dst)
	}
	if len(dst.Traces[0].Codes8) != 2 {
		t.Errorf("copied %d codes, want the valid 2", len(dst.Traces[0].Codes8))
	}
	// A second copy reuses the first one's buffers and drops what src lacks.
	buf := &dst.Traces[0].Codes8[0]
	src2 := &Frame{Traces: []Trace{{Valid: 1, Codes8: []uint8{5}}}}
	dst.CopyFrom(src2)
	if &dst.Traces[0].Codes8[0] != buf {
		t.Error("buffer not reused")
	}
	if len(dst.Traces) != 1 || len(dst.Traces[0].Precision) != 0 || dst.Match != nil {
		t.Errorf("stale data after copy: %+v", dst)
	}
}

// TRLC-LINKS: REQ-DSO-1003
func TestStorageText(t *testing.T) {
	var s StorageKind
	if err := s.UnmarshalText([]byte("u16")); err != nil || s != U16 || s.Bits() != 16 {
		t.Fatal(s, err)
	}
	if b, _ := U8.MarshalText(); !strings.EqualFold(string(b), "u8") {
		t.Error(string(b))
	}
}

// TRLC-LINKS: REQ-DSO-1003
func BenchmarkCopyFrom(b *testing.B) {
	src := &Frame{Traces: []Trace{
		{Valid: 20480, Codes8: make([]uint8, 20480), Precision: make([]uint16, 20480)},
		{Valid: 20480, Codes8: make([]uint8, 20480), Precision: make([]uint16, 20480)},
	}}
	var dst Frame
	for b.Loop() {
		dst.CopyFrom(src)
	}
}

// An envelope keeps its own length, the detail windows' frames are shared,
// and the accessors tolerate a missing trace.
// TRLC-LINKS: REQ-DSO-1003
func TestCopyFromEnvelopeAndDetails(t *testing.T) {
	edge := &Frame{SampleS: 2e-9}
	src := &Frame{
		Flags:   Envelope,
		Traces:  []Trace{{Valid: 2, Codes8: []uint8{1, 2}, Min8: []uint8{1, 2, 3}, Max8: []uint8{4, 5, 6}}},
		Details: []Detail{{Role: Edge, Read: 3, Frame: edge}},
	}
	var dst Frame
	dst.CopyFrom(src)
	if len(dst.Min8(0)) != 3 || dst.Max8(0)[2] != 6 {
		t.Errorf("envelope cut to the codes' length: %v %v", dst.Min8(0), dst.Max8(0))
	}
	src.Details[0].Read = 9
	if d := dst.Detail(Edge); d == nil || d.Read != 3 || d.Frame != edge {
		t.Errorf("detail %+v", d)
	}
	if dst.Detail(EdgeOpp) != nil || dst.Codes8(1) != nil || dst.Precision(1) != nil || dst.Held(4) != nil {
		t.Error("an absent trace or detail is not nil")
	}
	if !dst.Flags.Has(Envelope) || dst.Len() != 2 {
		t.Errorf("flags %v len %d", dst.Flags, dst.Len())
	}
	dst.SetLen(1)
	dst.Flags.Set(Envelope, false)
	if dst.Traces[0].Valid != 1 || dst.Flags.Has(Envelope) {
		t.Error("SetLen or Flags.Set")
	}
}

// TRLC-LINKS: REQ-DSO-1003
func TestTriggerName(t *testing.T) {
	for _, c := range []struct {
		f    Frame
		want string
	}{
		{Frame{TrigKind: HardwareNode, TrigNode: "edge"}, "hardware-edge"},
		{Frame{TrigKind: Forced}, "forced"},
		{Frame{TrigKind: Untriggered}, "untriggered"},
		{Frame{TrigKind: StreamStop}, "stream"},
		{Frame{TrigKind: Untriggered, Flags: Roll}, "roll"},
		{Frame{}, ""},
	} {
		if got := c.f.TriggerName(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.f, got, c.want)
		}
	}
}

// The 8-bit view maps an 8-bit code to itself and a left-aligned 12-bit
// word to its high byte, each format's mid to 128.
// TRLC-LINKS: REQ-DSO-1003
func TestEightBitView(t *testing.T) {
	f8 := CodeFormat{Bits: 8, Storage: U8, Max: 255, Mid: 128}
	f12 := CodeFormat{Bits: 12, Storage: U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000}
	r12 := CodeFormat{Bits: 12, Storage: U16, Max: 4095, Mid: 2048}
	for _, c := range []struct {
		f    CodeFormat
		w    uint32
		q    uint16
		c8   uint8
		unit float64
	}{
		{f8, 128, 0x8000, 128, 1}, {f8, 0, 0, 0, 1}, {f8, 255, 0xFF00, 255, 1}, {f8, 153, 153 << 8, 153, 1},
		{f12, 0x8000, 0x8000, 128, 256}, {f12, 0x9980, 0x9980, 154, 256}, {f12, 0xFFF0, 0xFFF0, 255, 256},
		{r12, 2048, 0x8000, 128, 16}, {r12, 2048 + 16*25, 0x8000 + 25<<8, 153, 16}, {r12, 0, 0, 0, 16},
	} {
		if q, c8, u := c.f.Q88(c.w), c.f.Code8(c.w), c.f.Unit8(); q != c.q || c8 != c.c8 || u != c.unit {
			t.Errorf("%+v word %#x: q88 %#x code %d unit %v, want %#x %d %v", c.f, c.w, q, c8, u, c.q, c.c8, c.unit)
		}
	}
}

// A 12-bit trace's 8-bit view holds its high bytes, and its codes per
// division on that view are the words' over 256.
// TRLC-LINKS: REQ-DSO-1003
func TestTraceView8(t *testing.T) {
	t8 := &Trace{Format: CodeFormat{Bits: 8, Storage: U8, Max: 255, Mid: 128}, Codes8: []uint8{1, 2, 3}, Valid: 2, CodesPerDiv: 25}
	if v := t8.View8(); len(v) != 2 || &v[0] != &t8.Codes8[0] || t8.CodesPerDiv8() != 25 {
		t.Errorf("8-bit view %v, %v codes a division", v, t8.CodesPerDiv8())
	}
	t12 := &Trace{Format: CodeFormat{Bits: 12, Storage: U16, Shift: 4, Max: 0xFFF0, Mid: 0x8000}, Codes16: []uint16{0x0100, 0x8000, 0xFFF0}, Valid: 3, CodesPerDiv: 7680}
	if v := t12.View8(); v[0] != 1 || v[1] != 128 || v[2] != 255 || t12.CodesPerDiv8() != 30 {
		t.Errorf("12-bit view %v, %v codes a division", v, t12.CodesPerDiv8())
	}
}
