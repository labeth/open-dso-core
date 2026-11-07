// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"testing"

	"github.com/labeth/open-dso-core/decode"
)

// TRLC-LINKS: REQ-SDS-013
func TestMatchBytesUnit(t *testing.T) {
	// contiguous bytes ~10 samples wide, abutting (like a continuous UART stream)
	spans := []decode.Span{
		{Kind: "data", Val: 0x11, I0: 10, I1: 19},
		{Kind: "data", Val: 0x22, I0: 20, I1: 29},
		{Kind: "data", Val: 0x33, I0: 30, I1: 39},
	}
	if ok, a := matchBytes(spans, []int{0x22}); !ok || a != 20 {
		t.Fatalf("single byte: ok=%v a=%d want 20", ok, a)
	}
	if ok, a := matchBytes(spans, []int{0x22, 0x33}); !ok || a != 20 {
		t.Fatalf("contiguous sequence anchors on first byte: ok=%v a=%d want 20", ok, a)
	}
	if ok, _ := matchBytes(spans, []int{0x33, 0x22}); ok {
		t.Fatal("out-of-order sequence must not match")
	}
	if ok, a := matchBytes(spans, nil); !ok || a != 10 {
		t.Fatalf("empty pattern → first byte: ok=%v a=%d", ok, a)
	}
	if ok, _ := matchBytes(nil, nil); ok {
		t.Fatal("no bytes at all must not match")
	}

	// error bytes are NOT matchable data (must not fire on corrupted traffic)
	errSpans := []decode.Span{{Kind: "frame-error", Val: 0x55, I0: 5, I1: 14}, {Kind: "parity-error", Val: 0x33, I0: 15, I1: 24}}
	if ok, _ := matchBytes(errSpans, []int{0x55}); ok {
		t.Fatal("frame-error byte must NOT satisfy a data trigger")
	}
	if ok, _ := matchBytes(errSpans, nil); ok {
		t.Fatal("empty pattern must NOT fire on a record of only error bytes")
	}
	for _, kind := range []string{"gap", "frame-error", "parity-error"} {
		broken := []decode.Span{
			{Kind: "data", Val: 0x41, I0: 0, I1: 9},
			{Kind: kind, I0: 10, I1: 19},
			{Kind: "data", Val: 0x42, I0: 20, I1: 29},
			{Kind: "data", Val: 0x43, I0: 30, I1: 39},
		}
		if ok, _ := matchBytes(broken, []int{0x41, 0x42}); ok {
			t.Fatalf("pattern bridged %s", kind)
		}
		if ok, anchor := matchBytes(broken, []int{0x42, 0x43}); !ok || anchor != 20 {
			t.Fatalf("valid sequence after %s rejected: %v %d", kind, ok, anchor)
		}
	}

	// CONTIGUITY: two matching bytes separated by a large idle gap must NOT form
	// a sequence (different transmissions), but MUST still match individually.
	gapped := []decode.Span{
		{Kind: "data", Val: 0x41, I0: 100, I1: 190},   // width ~90
		{Kind: "data", Val: 0x42, I0: 9000, I1: 9090}, // thousands of samples later
	}
	if ok, _ := matchBytes(gapped, []int{0x41, 0x42}); ok {
		t.Fatal("a 2-byte pattern must NOT bridge a large idle gap")
	}
	if ok, a := matchBytes(gapped, []int{0x42}); !ok || a != 9000 {
		t.Fatalf("single byte across a gap still matches: ok=%v a=%d", ok, a)
	}
}

// TRLC-LINKS: REQ-SDS-013
func TestIndexSeqUnit(t *testing.T) {
	hay := []int{1, 2, 3, 2, 3, 4}
	if indexSeq(hay, []int{2, 3, 4}) != 3 {
		t.Fatal("should find at 3")
	}
	if indexSeq(hay, []int{9}) != -1 {
		t.Fatal("absent → -1")
	}
	if indexSeq(hay, nil) != 0 {
		t.Fatal("empty needle → 0")
	}
	if indexSeq([]int{1}, []int{1, 2}) != -1 {
		t.Fatal("needle longer than hay → -1")
	}
}

// TRLC-LINKS: REQ-SDS-013
func TestSerialRegistryCapabilities(t *testing.T) {
	ds := SerialDecoderList()
	if len(ds) < 10 {
		t.Fatalf("lost existing decoders: %d", len(ds))
	}
	for i, d := range ds {
		if i > 0 && d.ID <= ds[i-1].ID {
			t.Fatal("capabilities not sorted")
		}
		if d.Decode == nil || d.Match == nil || d.Name == "" {
			t.Fatal("incomplete decoder")
		}
	}
	if err := RegisterSerialDecoder(ds[0]); err == nil {
		t.Fatal("replaced saved protocol ID")
	}
	if err := RegisterSerialDecoder(SerialDecoder{}); err == nil {
		t.Fatal("accepted invalid decoder")
	}
}
