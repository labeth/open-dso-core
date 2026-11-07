package hal

import "github.com/labeth/open-dso-core/sample"

// Content and the other types here alias the frame's vocabulary, which lives
// in package sample: the HAL's Readout fills it and every consumer reads it;
// the aliases let board and server code name it through the HAL.
type (
	Content     = sample.Content
	ContentSet  = sample.ContentSet
	ContentFlag = sample.ContentFlag
	CodeFormat  = sample.CodeFormat
	Scale       = sample.Scale
	RecordID    = sample.RecordID
	Status      = sample.Status
	Counters    = sample.Counters
	Role        = sample.Role
)

// The window roles, as in package sample.
const (
	Main    = sample.Main
	Zoom    = sample.Zoom
	Edge    = sample.Edge
	EdgeOpp = sample.EdgeOpp
)

// RoleView is the n-th magnified view's detail window.
// TRLC-LINKS: REQ-DSO-1001
func RoleView(n int) Role { return sample.RoleView(n) }

// The content kinds, as in package sample.
const (
	RawFullRate   = sample.RawFullRate
	RawDecimated  = sample.RawDecimated
	PeakPairs     = sample.PeakPairs
	Resampled     = sample.Resampled
	DisplayDomain = sample.DisplayDomain
)
