package sample

import "sync"

// Format8 is the 8-bit view's own code format: an 8-bit converter's codes
// as they are, centred at Mid8.
var Format8 = CodeFormat{Bits: 8, Storage: U8, Min: 0, Max: 255, Mid: Mid8}

// Wide reports whether any of f's traces stores words wider than 8 bits.
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) Wide() bool {
	for i := range f.Traces {
		if f.Traces[i].Format.Storage == U16 {
			return true
		}
	}
	for i := range f.Details {
		if d := f.Details[i].Frame; d != nil && d.Wide() {
			return true
		}
	}
	return false
}

// View8Frame is frame f on the 8-bit view the product's kernels read: an
// 8-bit frame as it is (no copy); a frame of wider words copied with each
// trace's codes mapped onto the view (CodeFormat.Code8), its words kept as
// Q8.8 precision so measurements keep the converter's resolution, its codes
// a division restated on the view, and its detail windows on the view too.
// The copy shares nothing with f.
// TRLC-LINKS: REQ-DSO-1003
func View8Frame(f *Frame) *Frame {
	if f == nil || !f.Wide() {
		return f
	}
	v := *f
	v.Traces = make([]Trace, len(f.Traces))
	for i := range f.Traces {
		v.Traces[i] = f.Traces[i].trace8()
	}
	if f.Details != nil {
		v.Details = make([]Detail, len(f.Details))
		for i, d := range f.Details {
			d.Frame = View8Frame(d.Frame)
			v.Details[i] = d
		}
	}
	if f.Match != nil {
		m := *f.Match
		v.Match = &m
	}
	v.Flags.Set(Precision, true)
	return &v
}

// trace8 is t on the 8-bit view: Codes8 the view's codes, Precision the
// Q8.8 words (a trace's own precision words beside 8-bit codes are kept).
// TRLC-LINKS: REQ-DSO-1003
func (t *Trace) trace8() Trace {
	if t.Format.Storage != U16 {
		return *t
	}
	o := *t
	o.Codes8, o.Codes16, o.Min16, o.Max16 = t.View8(), nil, nil, nil
	o.Precision, o.PrecisionFrac = make([]uint16, len(o.Codes8)), 8
	for i, w := range t.Codes16[:len(o.Codes8)] {
		o.Precision[i] = t.Format.Q88(uint32(w))
	}
	o.Min8, o.Max8 = codes8(t.Format, t.Min16), codes8(t.Format, t.Max16)
	if t.Held != nil {
		o.Held = append([]bool(nil), t.Held...)
	}
	o.CodesPerDiv, o.Format = t.CodesPerDiv8(), Format8
	return o
}

// codes8 is words w of format f on the 8-bit view; nil for none.
// TRLC-LINKS: REQ-DSO-1003
func codes8(f CodeFormat, w []uint16) []uint8 {
	if w == nil {
		return nil
	}
	out := make([]uint8, len(w))
	for i, v := range w {
		out[i] = f.Code8(uint32(v))
	}
	return out
}

// View8Cache keeps the last wide frame's 8-bit view, for a consumer that
// reads the same published frame more than once (a display redrawing it as
// the HUD changes around it, a UI model analysing it and drawing it).
// The view it returns is shared and read-only.
type View8Cache struct {
	mu   sync.Mutex
	src  *Frame
	seq  uint64
	view *Frame
}

// Of is f on the 8-bit view (View8Frame), the cached copy while f is the
// same published frame (same pointer and a non-zero Seq).
// TRLC-LINKS: REQ-DSO-1003
func (c *View8Cache) Of(f *Frame) *Frame {
	if f == nil || !f.Wide() {
		return f
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if f.Seq != 0 && c.src == f && c.seq == f.Seq {
		return c.view
	}
	c.src, c.seq, c.view = f, f.Seq, View8Frame(f)
	return c.view
}
