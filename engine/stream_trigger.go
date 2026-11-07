// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import "errors"

// StreamTrigger is what ends a scope-side decoded stream (ADR-PANEL-DECODED-STREAM).
type StreamTrigger int

const (
	// StreamManual streams until the operator stops it.
	StreamManual StreamTrigger = iota
	// StreamOnError triggers on any decode error the hardware decoder flags.
	StreamOnError
	// StreamOnValue triggers on one decoded unit equal to the chosen value.
	StreamOnValue
)

// ErrLevelsLearning is a stream refused while the ARINC 429 slicer levels
// are still being learned from a sampled record (a moment after the decoder
// settings name ARINC 429).
var ErrLevelsLearning = errors.New("the ARINC 429 levels are not learned yet")
