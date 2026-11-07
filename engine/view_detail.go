// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"sync/atomic"

	"github.com/labeth/open-dso-core/sample"
)

// ViewSurface is who asks for a view window.
type ViewSurface int

// The view surfaces, and their count.
const (
	ViewScope ViewSurface = iota // the scope's zoom
	ViewWeb                      // the web page's zoom
	ViewSurfaces
)

// ViewSlot is one surface's request: until when, its ends (screen widths
// from the trigger, float64 bits) and the display columns it is drawn over.
type ViewSlot struct {
	Lease atomic.Int64
	A, B  atomic.Uint64
	Cols  atomic.Int64
}

// viewMaxBuckets caps a view's buckets: a bucket a display column up to a
// browser canvas's width. A bucket costs the frame two bus reads (~7 us).
const viewMaxBuckets = 1024

// ViewBuckets is how many buckets a view of b-a screen widths shown over cols
// columns is read in; 0 when the screen's envelope (screen buckets across
// it) has a bucket at least every second column there already. Magnified
// further, a bucket spans many columns and its (min, max) pair draws as a
// slope: the 1 MHz square at 200us/div zoomed 50x read as a slow wave.
// TRLC-LINKS: REQ-SDS-021
func ViewBuckets(a, b float64, cols int, screen uint32) uint32 {
	if !(b > a) || cols < 2 || 2*float64(screen)*(b-a) >= float64(cols) {
		return 0
	}
	n := uint32(min(cols, viewMaxBuckets))
	return n + n&1 // the reducer takes buckets in pairs
}

// ViewPlan is how a view's stretch is read: buckets of bucket words (0: raw
// samples) over the record words [start, start+length), n values a channel.
type ViewPlan struct {
	Bucket, Start, Length, Buckets uint32
	N                              int
}

// EnvelopeValue maps a record word to the value index of an envelope frame
// read from word offset in buckets of bucket words: a value a half bucket.
// TRLC-LINKS: REQ-SDS-021
func EnvelopeValue(offset, bucket uint32) func(w uint32) float64 {
	return func(w uint32) float64 { return (float64(w) - float64(offset)) * 2 / float64(bucket) }
}

// ViewDetail is frame f's view window for surface s, or nil.
// TRLC-LINKS: REQ-SDS-010, REQ-SDS-021
func ViewDetail(f *sample.Frame, s ViewSurface) *sample.Detail {
	return f.Detail(sample.RoleView(int(s)))
}
