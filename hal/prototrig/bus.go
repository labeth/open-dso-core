package prototrig

import "github.com/labeth/open-dso-core/hal"

// Bus is a trigger on any bus the core decodes, asked as the decoder
// registry's request: a board whose receivers match decoded units (sliced
// symbol classes, sequences of units, frame starts, protocol receivers it
// chooses by the request) takes it whole, and picks the receiver itself.
//
// The request's slicing thresholds are codes of the decoder's 8-bit view;
// a trigger's levels are volts, so Levels replaces them: Levels[0] slices
// Request.ChA and Levels[1] Request.ChB, in volts at the tips, wherever
// Request.HaveThr and HaveThrB say a threshold is set (a board converts
// them with its front end, as every trigger level).
type Bus struct {
	hal.ProtoTriggerType
	Request SerialParams
	Levels  [2]float64
	// ARINCSlicer is ARINC 429's slicer learned from a recent record:
	// the levels leaving HI, leaving LO, entering HI and entering LO, in
	// volts at Request.ChA's tip; nil while none is known.
	ARINCSlicer []float64
}

// Protocol is "bus".
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-1001
func (Bus) Protocol() string { return "bus" }

// Equal reports that b and o are the same trigger: the request, the levels
// and the slicer.
// TRLC-LINKS: REQ-DSO-013
func (b *Bus) Equal(o *Bus) bool {
	return b.Levels == o.Levels && sliceEqual(b.ARINCSlicer, o.ARINCSlicer) && b.Request.Equal(&o.Request)
}
