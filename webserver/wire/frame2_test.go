// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// cmlLike is a two-channel 8-bit frame with Q8.8 precision words beside
// its codes, 25 codes a division about 128.
// TRLC-LINKS: REQ-DSO-163
func cmlLike(precision bool) *sample.Frame {
	f := &sample.Frame{Content: sample.RawFullRate, Seq: 9, TriggerAt: 1.5, TrigKind: sample.HardwareNode}
	if precision {
		f.Flags |= sample.Precision
	}
	for ch := range 2 {
		f.Traces = append(f.Traces, sample.Trace{
			Ch: ch, Format: sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128},
			Codes8: []uint8{128, 129, 130}, Precision: []uint16{128 << 8, 129<<8 + 128, 130 << 8}, PrecisionFrac: 8,
			Valid: 3, Scale: sample.Scale{Zero: 128, PerCode: 0.04, OffsetV: 0.1}, CodesPerDiv: 25,
		})
	}
	return f
}

// A precision frame serves its words with the scale in their units: the
// same volts and divisions as the codes.
// TRLC-LINKS: REQ-DSO-163
func TestFromSamplePrecision(t *testing.T) {
	var f Frame
	f.FromSample(cmlLike(true))
	tr := f.Traces[0]
	if tr.Fmt.Storage != "u16" || tr.Fmt.Frac != 8 || tr.Fmt.Mid != 128<<8 || len(tr.Codes.U16) != 3 || len(tr.Codes.U8) != 0 {
		t.Fatalf("precision trace: %+v", tr)
	}
	volts := func(w float64) float64 { return (w-tr.Scale.Zero)*tr.Scale.PerCodeV - tr.Scale.OffV }
	if got, want := volts(float64(tr.Codes.U16[1])), (129.5-128)*0.04-0.1; got-want > 1e-12 || want-got > 1e-12 {
		t.Fatalf("volts %g, want %g", got, want)
	}
	if tr.Scale.CodesPerDiv != 25*256 {
		t.Fatalf("codes per div %g", tr.Scale.CodesPerDiv)
	}
	f.FromSample(cmlLike(false))
	if tr := f.Traces[1]; tr.Fmt.Storage != "u8" || len(tr.Codes.U8) != 3 || tr.Scale.CodesPerDiv != 25 {
		t.Fatalf("code trace: %+v", tr)
	}
	if f.Header.TrigKind != "hardware-node" || f.Header.Content != "raw-full-rate" || f.Header.TriggerAt != 1.5 {
		t.Fatalf("header: %+v", f.Header)
	}
}

// The encoder pads the header to 8 bytes, places every segment at an
// 8-byte offset and refuses words that contradict their format.
// TRLC-LINKS: REQ-DSO-163
func TestEncodeFrame2Layout(t *testing.T) {
	var f Frame
	f.FromSample(cmlLike(false))
	f.Traces[0].View = Samples{U8: []uint8{1, 2}}
	b, err := EncodeFrame2(&f)
	if err != nil {
		t.Fatal(err)
	}
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	var h FrameHeader
	if err := json.Unmarshal(b[8:8+n], &h); err != nil {
		t.Fatal(err)
	}
	start := (8 + n + 7) &^ 7
	want := []Seg{{0, RoleCodes, "u8", 3, 0}, {0, RoleView, "u8", 2, 8}, {1, RoleCodes, "u8", 3, 16}}
	if len(h.Segs) != len(want) {
		t.Fatalf("segs %+v", h.Segs)
	}
	for i, s := range want {
		if h.Segs[i] != s {
			t.Fatalf("seg %d: %+v, want %+v", i, h.Segs[i], s)
		}
	}
	if len(b) != start+16+8 || b[start] != 128 || b[start+8] != 1 || b[start+18] != 130 {
		t.Fatalf("payload % x", b[start:])
	}
	f.Traces[1].Codes = Samples{U16: []uint16{1}}
	if _, err := EncodeFrame2(&f); err == nil {
		t.Fatal("u16 words in a u8 trace encoded")
	}
}

// A v2 frame decodes back to the words and scales it was encoded from.
// TRLC-LINKS: REQ-DSO-163
func TestDecodeFrame2(t *testing.T) {
	var f Frame
	f.FromSample(cmlLike(true))
	b, err := EncodeFrame2(&f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeFrame2(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Traces) != len(f.Traces) || got.Header.Seq != f.Header.Seq {
		t.Fatalf("%d traces seq %d", len(got.Traces), got.Header.Seq)
	}
	for i := range f.Traces {
		w, g := f.Traces[i], got.Traces[i]
		if g.Ch != w.Ch || g.Codes.Len() != w.Codes.Len() || g.Scale != w.Scale {
			t.Fatalf("trace %d: %+v", i, g.TraceHeader)
		}
		for k := range w.Codes.Len() {
			if g.Volts(g.Codes, k) != w.Volts(w.Codes, k) {
				t.Fatalf("trace %d word %d", i, k)
			}
		}
	}
	if _, err := DecodeFrame2(b[:9]); err == nil {
		t.Fatal("a cut frame decoded")
	}
}
