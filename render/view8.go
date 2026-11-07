// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import (
	"sync"

	"github.com/labeth/open-dso-core/sample"
)

// format8 is the 8-bit view's own code format.
var format8 = sample.CodeFormat{Bits: 8, Storage: sample.U8, Min: 0, Max: 255, Mid: sample.Mid8}

// view8Cache keeps the last wide frame's 8-bit view: the display redraws the
// same frame as the HUD changes around it.
var view8Cache struct {
	sync.Mutex
	src  *sample.Frame
	seq  uint64
	view *sample.Frame
}

// view8 is frame f on the 8-bit view the renderer draws (SPLIT-LEARNINGS:
// the 8-bit view plus the trace's own codes a division): an 8-bit frame as
// it is; a frame of wider words copied with each trace's codes mapped onto
// the view, its words kept as Q8.8 precision for the measurements, and its
// codes a division restated on the view.
// TRLC-LINKS: REQ-SDS-021
func view8(f *sample.Frame) *sample.Frame {
	if f == nil || !wide(f) {
		return f
	}
	view8Cache.Lock()
	defer view8Cache.Unlock()
	if f.Seq != 0 && view8Cache.src == f && view8Cache.seq == f.Seq {
		return view8Cache.view
	}
	v := *f
	v.Traces = make([]sample.Trace, len(f.Traces))
	for i := range f.Traces {
		v.Traces[i] = trace8(&f.Traces[i])
	}
	v.Flags.Set(sample.Precision, true)
	view8Cache.src, view8Cache.seq, view8Cache.view = f, f.Seq, &v
	return &v
}

// wide reports whether any of f's traces stores words wider than 8 bits.
// TRLC-LINKS: REQ-SDS-021
func wide(f *sample.Frame) bool {
	for i := range f.Traces {
		if f.Traces[i].Format.Storage == sample.U16 {
			return true
		}
	}
	return false
}

// trace8 is trace t on the 8-bit view.
// TRLC-LINKS: REQ-SDS-021
func trace8(t *sample.Trace) sample.Trace {
	if t.Format.Storage != sample.U16 {
		return *t
	}
	o := *t
	o.Codes8, o.Codes16, o.Min16, o.Max16 = t.View8(), nil, nil, nil
	o.Precision, o.PrecisionFrac = make([]uint16, len(o.Codes8)), 8
	for i, w := range t.Codes16[:len(o.Codes8)] {
		o.Precision[i] = t.Format.Q88(uint32(w))
	}
	o.Min8, o.Max8 = codes8(t.Format, t.Min16), codes8(t.Format, t.Max16)
	o.CodesPerDiv, o.Format = t.CodesPerDiv8(), format8
	return o
}

// codes8 is words w of format f on the 8-bit view; nil for none.
// TRLC-LINKS: REQ-SDS-021
func codes8(f sample.CodeFormat, w []uint16) []uint8 {
	if w == nil {
		return nil
	}
	out := make([]uint8, len(w))
	for i, v := range w {
		out[i] = f.Code8(uint32(v))
	}
	return out
}
