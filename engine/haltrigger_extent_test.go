// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// extentFrame is a frame of channel 0 whose samples are codes, at 25
// codes a division of 0.1 V.
func extentFrame(codes []uint8) *sample.Frame {
	return &sample.Frame{Traces: []sample.Trace{{
		Ch: 0, Valid: len(codes), Codes8: codes, CodesPerDiv: 25,
		Format: sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128},
		Scale:  sample.Scale{Zero: 128, PerCode: 0.004},
	}}}
}

// A record of a line at rest inside the swing learned before (a sparse
// bus between its frames) keeps the extent, so a trigger placed on it is
// not refused for no swing; a line that went flat elsewhere, or a
// different V/div, is learned again.
// TRLC-LINKS: REQ-DSO-011
func TestExtentKeepsThroughAnIdleRecord(t *testing.T) {
	c := &HALCycle{ext: make([]extent, 1), placed: make([]bool, 1)}
	busy := make([]uint8, 200)
	for i := range busy {
		busy[i] = 28
		if i%20 < 10 {
			busy[i] = 228
		}
	}
	c.learn(extentFrame(busy))
	if _, why := c.swing(0); why != "" {
		t.Fatalf("the bus: %s", why)
	}
	idle := make([]uint8, 200)
	for i := range idle {
		idle[i] = 28
	}
	c.learn(extentFrame(idle))
	if _, why := c.swing(0); why != "" {
		t.Fatalf("an idle record of the bus: %s", why)
	}
	gone := make([]uint8, 200) // flat beyond the bus's high level
	for i := range gone {
		gone[i] = 252
	}
	c.learn(extentFrame(gone))
	if _, why := c.swing(0); why == "" {
		t.Fatal("a line at a level the bus never had kept the bus's swing")
	}
}
