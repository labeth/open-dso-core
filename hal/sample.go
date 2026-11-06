package hal

import "github.com/labeth/open-dso-core/sample"

// The frame's vocabulary lives in package sample, which the HAL's Readout
// fills and every consumer reads; these aliases let board and server code
// name it through the HAL.
type (
	Content     = sample.Content
	ContentSet  = sample.ContentSet
	ContentFlag = sample.ContentFlag
	CodeFormat  = sample.CodeFormat
	Scale       = sample.Scale
	RecordID    = sample.RecordID
	Status      = sample.Status
	Counters    = sample.Counters
)

// The content kinds, as in package sample.
const (
	RawFullRate   = sample.RawFullRate
	RawDecimated  = sample.RawDecimated
	PeakPairs     = sample.PeakPairs
	Resampled     = sample.Resampled
	DisplayDomain = sample.DisplayDomain
)
