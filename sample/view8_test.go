package sample

import "testing"

// A frame of the channels switched on (C1, C3) is read by channel: C3's
// trace at index 2, C2 an empty trace of the frame's length; 12-bit words
// on the 8-bit view with their Q8.8 words beside.
// TRLC-LINKS: REQ-DSO-1003
func TestView8FramePlacesTracesByChannel(t *testing.T) {
	fm := CodeFormat{Bits: 12, Storage: U16, Shift: 4, Max: 0xFFF0, Mid: 0x8000}
	f := &Frame{Traces: []Trace{
		{Ch: 0, Format: fm, Codes16: []uint16{0x8000, 0x8100}, Valid: 2, CodesPerDiv: 7680},
		{Ch: 2, Format: fm, Codes16: []uint16{0x9000, 0x7000}, Valid: 2, CodesPerDiv: 7680},
	}}
	v := View8Frame(f)
	if len(v.Traces) != 3 || v.Len() != 2 || len(v.Codes8(1)) != 0 || v.Traces[1].Valid != 2 {
		t.Fatalf("traces %d, len %d, C2 %v", len(v.Traces), v.Len(), v.Codes8(1))
	}
	if c := v.Codes8(2); len(c) != 2 || c[0] != 144 || c[1] != 112 || v.Precision(2)[0] != 0x9000 {
		t.Fatalf("C3 %v %v", c, v.Precision(2))
	}
	if cpd := v.Traces[2].CodesPerDiv; cpd != 30 {
		t.Fatalf("C3 codes a division %g", cpd)
	}
	eight := &Frame{Traces: []Trace{{Ch: 0, Format: Format8}, {Ch: 1, Format: Format8}}}
	if View8Frame(eight) != eight {
		t.Fatal("an 8-bit frame in place was copied")
	}
	fixture := &Frame{Traces: []Trace{{}, {}}} // no channels named: left as it is
	if View8Frame(fixture) != fixture {
		t.Fatal("a frame naming no channels was copied")
	}
}
