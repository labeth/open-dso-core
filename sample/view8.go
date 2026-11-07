package sample

import "sync"

// Format8 is the 8-bit view's own code format: an 8-bit converter's codes
// as they are, centred at Mid8.
var Format8 = CodeFormat{Bits: 8, Storage: U8, Min: 0, Max: 255, Mid: Mid8}

// placed reports whether f's traces sit at their channel's index, or name
// no distinct channels to place by (traces that all say channel 0).
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) placed() bool {
	var seen uint64
	in := true
	for i := range f.Traces {
		ch := f.Traces[i].Ch
		if ch < 0 || ch >= 64 || seen&(1<<ch) != 0 {
			return true
		}
		seen |= 1 << ch
		in = in && ch == i
	}
	return in
}

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

// View8Frame is frame f on the 8-bit view the product's kernels read, its
// traces at their channel's index (Frame.Codes8(ch) is channel ch's): an
// 8-bit frame with every trace in its place as it is (no copy); otherwise
// a copy with each trace's codes mapped onto the view (CodeFormat.Code8),
// its words kept as Q8.8 precision so measurements keep the converter's
// resolution, its codes a division restated on the view, its detail
// windows on the view too, and a channel the frame does not carry (one
// switched off) an empty trace of the frame's length. The copy shares
// nothing with f.
// TRLC-LINKS: REQ-DSO-1003
func View8Frame(f *Frame) *Frame {
	if f == nil || !f.Wide() && f.placed() {
		return f
	}
	v := *f
	n := f.Len()
	if f.placed() {
		v.Traces = make([]Trace, len(f.Traces))
		for i := range f.Traces {
			v.Traces[i] = f.Traces[i].trace8()
		}
	} else {
		top := 0
		for i := range f.Traces {
			top = max(top, f.Traces[i].Ch+1)
		}
		v.Traces = make([]Trace, top)
		for ch := range v.Traces {
			v.Traces[ch] = Trace{Ch: ch, Format: Format8, Valid: n}
		}
		for i := range f.Traces {
			v.Traces[f.Traces[i].Ch] = f.Traces[i].trace8()
		}
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
	if f.Wide() {
		v.Flags.Set(Precision, true)
	}
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
	if f == nil || !f.Wide() && f.placed() {
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
