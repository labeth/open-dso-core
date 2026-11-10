// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"testing"

	"github.com/labeth/open-dso-core/decode"
)

// TRLC-LINKS: REQ-DSO-013
func TestPacketTriggerBoundaries(t *testing.T) {
	for _, tc := range []struct {
		id       int
		boundary string
	}{
		{SerSENT, "sync"},
		{SerCAN, "sof"},
		{SerMIL1553, "start"},
		{SerARINC, "gap"},
		{SerUSB, "start"},
		{SerFlexRay, "start"},
	} {
		d, _ := LookupSerialDecoder(tc.id)
		t.Run(d.Name, func(t *testing.T) {
			a := decode.Span{Kind: "data", Val: 0x41, I0: 10, I1: 19}
			b := decode.Span{Kind: "data", Val: 0x42, I0: 20, I1: 29}
			boundary := decode.Span{Kind: tc.boundary}
			p := SerialParams{Bytes: []int{0x41, 0x42}}
			if ok, _ := d.Match([]decode.Span{boundary, a, b}, p); !ok {
				t.Fatal("clean packet rejected")
			}
			if ok, _ := d.Match([]decode.Span{boundary, a, boundary, b}, p); ok {
				t.Fatal("pattern crossed packet boundary")
			}
			for _, kind := range []string{"frame-error", "parity-error"} {
				errSpan := decode.Span{Kind: kind}
				for _, corrupt := range [][]decode.Span{{boundary, errSpan, a, b}, {boundary, a, b, errSpan}} {
					if ok, _ := d.Match(corrupt, p); ok {
						t.Fatal("corrupt packet matched")
					}
					if ok, _ := d.Match(corrupt, SerialParams{}); ok {
						t.Fatal("any-data trigger accepted corrupt packet")
					}
					if ok, _ := d.Match(append(corrupt, boundary, a, b), p); !ok {
						t.Fatal("clean later packet rejected")
					}
				}
			}
		})
	}
}
