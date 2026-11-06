package sample

import (
	"strings"
	"testing"
)

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

func TestStorageText(t *testing.T) {
	var s StorageKind
	if err := s.UnmarshalText([]byte("u16")); err != nil || s != U16 || s.Bits() != 16 {
		t.Fatal(s, err)
	}
	if b, _ := U8.MarshalText(); !strings.EqualFold(string(b), "u8") {
		t.Error(string(b))
	}
}

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
