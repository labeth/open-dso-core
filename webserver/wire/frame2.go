// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/labeth/open-dso-core/measure"
	"github.com/labeth/open-dso-core/sample"
)

// Frame wire v2 (/api/frame.bin?v=2), all integers little-endian:
//
//	[0:4]   "DSF2"
//	[4:8]   u32 H = JSON header byte length
//	[8:8+H] UTF-8 JSON header (FrameHeader), its traces and segs filled
//	pad     zero bytes to a multiple of 8 from the message start
//	payload the segments of header.segs; each starts at its at (bytes from
//	        the payload start, a multiple of 8) and holds n words of its
//	        storage, so a client views it in place as a typed array
//
// v1 starts with 0xF5, so one reader tells the two apart by the first byte.
// Any number of channels and any code format travel the same way: a trace
// says how its stored words read as volts (scale) and as divisions
// (codes_per_div), and the payload carries the words as the scope stored
// them, with no conversion.
var FrameMagic2 = [4]byte{'D', 'S', 'F', '2'}

// The segment roles of a v2 frame.
const (
	RoleCodes = "codes" // the trace's samples
	RoleMin   = "min"   // peak pairs: each bucket's lowest word
	RoleMax   = "max"   // peak pairs: each bucket's highest word
	RoleView  = "view"  // the leased view window at full detail
	RoleRaw   = "raw"   // the un-windowed record (?raw=1)
)

// CodeFormat is sample.CodeFormat as the wire names it: how a trace's
// stored words map to the converter's codes.
type CodeFormat struct {
	Bits    uint8  `json:"bits"`
	Storage string `json:"storage"` // "u8" or "u16"
	Shift   uint8  `json:"shift"`   // left alignment of the code in its word
	Frac    uint8  `json:"frac"`    // fraction bits below one code
	Mid     uint32 `json:"mid"`
	Min     uint32 `json:"min"`
	Max     uint32 `json:"max"`
}

// FormatOf is f as the wire names it.
// TRLC-LINKS: REQ-DSO-163
func FormatOf(f sample.CodeFormat) CodeFormat {
	st := ""
	if b, err := f.Storage.MarshalText(); err == nil {
		st = string(b)
	}
	return CodeFormat{Bits: f.Bits, Storage: st, Shift: f.Shift, Frac: f.Frac, Mid: f.Mid, Min: f.Min, Max: f.Max}
}

// TraceScale reads a trace's stored words, at the probe tip:
// volts = (word − Zero)·PerCodeV − OffV, and on the screen
// divisions = (word − Zero)/CodesPerDiv + the channel's offset in divisions.
// Every value is in stored-word units, fraction bits included.
type TraceScale struct {
	Zero        float64 `json:"zero"`
	PerCodeV    float64 `json:"per_code_v"`
	OffV        float64 `json:"off_v"`
	CodesPerDiv float64 `json:"codes_per_div"`
}

// TraceHeader is one channel's entry in a v2 frame header.
type TraceHeader struct {
	Ch       int             `json:"ch"`
	Fmt      CodeFormat      `json:"fmt"`
	Scale    TraceScale      `json:"scale"`
	Coupling string          `json:"coupling,omitempty"`
	Clip     bool            `json:"clip,omitempty"` // railed against full scale: readings suspect
	Head     int             `json:"head,omitempty"` // columns before the record (no samples served there)
	Tail     int             `json:"tail,omitempty"` // columns after it
	Meas     *measure.Result `json:"meas,omitempty"`
}

// Seg places one payload segment.
type Seg struct {
	Ch      int    `json:"ch"`
	Role    string `json:"role"`
	Storage string `json:"storage"`
	N       int    `json:"n"`  // words
	At      int    `json:"at"` // bytes from the payload start
}

// FrameHeader is a v2 frame's JSON header: every v1 field that is not per
// channel, what the samples are, and the traces and their segments. The
// encoder fills Traces and Segs from the frame's traces.
type FrameHeader struct {
	Seq       uint64 `json:"seq"`
	Unchanged bool   `json:"unchanged,omitempty"`
	Raw       bool   `json:"raw,omitempty"` // the raw shape: segments of role raw

	Content   string   `json:"content,omitempty"` // sample.Content's name
	Flags     []string `json:"flags,omitempty"`   // sample.ContentFlag names
	SampleS   float64  `json:"sample_s,omitempty"`
	CaptureS  float64  `json:"capture_s,omitempty"`
	AcqDecim  int      `json:"acq_decim,omitempty"`
	TriggerAt float64  `json:"trigger_at"`
	TrigKind  string   `json:"trig_kind,omitempty"`

	TdivS      float64  `json:"tdiv_s"`
	DisplayedS float64  `json:"displayed_sdiv_s"`
	Cols       int      `json:"cols"`
	ColSpanS   float64  `json:"col_span_s"`
	DtS        float64  `json:"dt_s,omitempty"`
	Depth      int      `json:"depth,omitempty"`
	CaptureDep int      `json:"capture_depth,omitempty"`
	WinCols    int      `json:"win_cols,omitempty"`
	EdgeX      float64  `json:"edge_x"`
	EdgeFrac   float64  `json:"edge_frac"`
	WinFrac    float64  `json:"win_frac"`
	Held       [][2]int `json:"held,omitempty"`
	ViewA      float64  `json:"view_a,omitempty"`
	ViewB      float64  `json:"view_b,omitempty"`
	ViewN      int      `json:"view_n,omitempty"`
	StreamSeq  uint64   `json:"stream_seq,omitempty"`
	WindowNs   int64    `json:"window_ns,omitempty"`
	GapNs      int64    `json:"gap_ns,omitempty"`
	MatchEndS  *float64 `json:"match_end_s,omitempty"`
	Ptp        int      `json:"ptp"`
	Interp     bool     `json:"interp"`
	Norm       bool     `json:"norm"`
	Trigd      bool     `json:"trigd"`
	Coherent   bool     `json:"coherent"`
	Degraded   bool     `json:"degraded,omitempty"`

	Filter         string  `json:"filter,omitempty"`
	FilterGuard    int     `json:"filter_guard,omitempty"`
	BandwidthHz    float64 `json:"bandwidth_hz,omitempty"`
	PassbandHz     float64 `json:"passband_hz,omitempty"`
	NoiseGainIdeal float64 `json:"noise_gain_ideal,omitempty"`
	MeasSeq        uint64  `json:"meas_seq,omitempty"`
	EdgeRead       uint64  `json:"edge_read,omitempty"`

	Traces []TraceHeader `json:"traces,omitempty"`
	Segs   []Seg         `json:"segs,omitempty"`
}

// Samples is one segment's words: U8 or U16, as the trace's storage says.
type Samples struct {
	U8  []uint8
	U16 []uint16
}

// Len is the segment's word count.
// TRLC-LINKS: REQ-DSO-163
func (s Samples) Len() int { return max(len(s.U8), len(s.U16)) }

// storage names the segment's word type; "" when it holds none.
// TRLC-LINKS: REQ-DSO-163
func (s Samples) storage() string {
	switch {
	case len(s.U16) > 0:
		return "u16"
	case len(s.U8) > 0:
		return "u8"
	}
	return ""
}

// FrameTrace is one channel of a v2 frame: its header entry and its
// segments' words. A peak-pairs trace fills Min and Max; a raw frame's
// trace fills Codes, sent as role raw.
type FrameTrace struct {
	TraceHeader
	Codes, Min, Max, View Samples
}

// Frame is a published frame as v2 serves it. A scope fills it (FromSample
// does the parts a sample.Frame holds; the shaping fields of Header are
// the scope's), and EncodeFrame2 lays it out. It owns its memory; the
// server reuses one across requests.
type Frame struct {
	Header FrameHeader
	Traces []FrameTrace
}

// Reset empties f for reuse, keeping its trace storage.
// TRLC-LINKS: REQ-DSO-163
func (f *Frame) Reset() {
	traces := f.Traces[:0]
	*f = Frame{Traces: traces}
}

// flagNames are sample.ContentFlag's bits by name, low bit first.
var flagNames = [...]string{"ordered", "interp", "roll", "held", "segmented", "precision"}

// trigKindNames are sample.TrigKind's names.
var trigKindNames = [...]string{"", "hardware-node", "software-refined", "forced", "stream-stop"}

// FromSample fills f's header and traces from a published sample frame:
// what the samples are, the timing, the trigger, the state and every
// trace's words, format and scale. A trace with precision words on a frame
// flagged Precision is served as those words (u16 with their fraction
// bits), its scale stretched to match. The words are copied (f owns them).
// The page's shaping fields (cols, col_span_s, edge_frac, win_frac, view)
// are the scope's to set after it.
// TRLC-LINKS: REQ-DSO-163
func (f *Frame) FromSample(src *sample.Frame) {
	h := &f.Header
	h.Seq = src.Seq
	h.Content = ""
	if src.Content != 0 {
		h.Content = src.Content.String()
	}
	h.Flags = h.Flags[:0]
	for i, n := range flagNames {
		if src.Flags&(1<<i) != 0 {
			h.Flags = append(h.Flags, n)
		}
	}
	if len(h.Flags) == 0 {
		h.Flags = nil
	}
	h.SampleS, h.CaptureS, h.AcqDecim = src.SampleS, src.CaptureS, src.AcqDecim
	h.TriggerAt = src.TriggerAt
	h.TrigKind = ""
	if int(src.TrigKind) < len(trigKindNames) {
		h.TrigKind = trigKindNames[src.TrigKind]
	}
	h.CaptureDep, h.WinCols = src.Depth, src.WinPoints
	h.TdivS, h.DisplayedS, h.Norm = src.Snapshot.TdivS, src.Snapshot.DisplayedS, src.Snapshot.Norm
	h.Trigd, h.Coherent, h.Degraded = src.Status.Triggered, src.Status.Coherent, src.Status.Degraded
	h.Interp = src.Flags&sample.Interp != 0
	h.Filter, h.FilterGuard = src.Cond.Filter, src.Cond.Guard
	h.BandwidthHz, h.PassbandHz, h.NoiseGainIdeal = src.Cond.BandwidthHz, src.Cond.PassbandHz, src.Cond.NoiseGain
	// A frame outside a decoded stream carries the zero StreamPos.
	h.StreamSeq = src.Stream.Seq
	h.WindowNs = int64(src.Stream.WindowS * 1e9)
	h.GapNs = int64(src.Stream.GapS * 1e9)
	h.MatchEndS = nil
	if src.Match != nil {
		end := src.Match.EndS
		h.MatchEndS = &end
	}
	precision := src.Flags&sample.Precision != 0
	f.Traces = f.Traces[:0]
	for i := range src.Traces {
		t := &src.Traces[i]
		if i < cap(f.Traces) {
			f.Traces = f.Traces[:i+1]
		} else {
			f.Traces = append(f.Traces, FrameTrace{})
		}
		out := &f.Traces[i]
		out.TraceHeader = TraceHeader{
			Ch: t.Ch, Fmt: FormatOf(t.Format),
			Scale: TraceScale{Zero: t.Scale.Zero, PerCodeV: t.Scale.PerCode, OffV: t.Scale.OffsetV, CodesPerDiv: t.CodesPerDiv},
		}
		n := t.Valid
		out.Codes = Samples{U8: reuse(out.Codes.U8, t.Codes8, n), U16: reuse(out.Codes.U16, t.Codes16, n)}
		out.Min = Samples{U8: reuse(out.Min.U8, t.Min8, n), U16: reuse(out.Min.U16, t.Min16, n)}
		out.Max = Samples{U8: reuse(out.Max.U8, t.Max8, n), U16: reuse(out.Max.U16, t.Max16, n)}
		out.View = Samples{U8: out.View.U8[:0], U16: out.View.U16[:0]}
		if precision && len(t.Precision) > 0 && t.PrecisionFrac > 0 {
			k := float64(uint32(1) << t.PrecisionFrac)
			fr := t.PrecisionFrac
			out.Fmt = CodeFormat{
				Bits: t.Format.Bits, Storage: "u16", Shift: t.Format.Shift, Frac: t.Format.Frac + fr,
				Mid: t.Format.Mid << fr, Min: t.Format.Min << fr, Max: t.Format.Max << fr,
			}
			out.Scale.Zero *= k
			out.Scale.PerCodeV /= k
			out.Scale.CodesPerDiv *= k
			out.Codes = Samples{U8: out.Codes.U8[:0], U16: reuse(out.Codes.U16, t.Precision, n)}
		}
	}
}

// reuse copies src's first n words into buf's storage.
// TRLC-LINKS: REQ-DSO-163
func reuse[T uint8 | uint16](buf, src []T, n int) []T {
	return append(buf[:0], src[:min(n, len(src))]...)
}

// EncodeFrame2 lays f out as wire v2. It fills f.Header.Traces and Segs. A
// trace whose segments disagree with its format's storage is an error (the
// page would read its words with the wrong width).
// TRLC-LINKS: REQ-DSO-163
func EncodeFrame2(f *Frame) ([]byte, error) {
	h := &f.Header
	h.Traces, h.Segs = h.Traces[:0], h.Segs[:0]
	payload := 0
	if !h.Unchanged {
		for i := range f.Traces {
			t := &f.Traces[i]
			h.Traces = append(h.Traces, t.TraceHeader)
			codes := RoleCodes
			if h.Raw {
				codes = RoleRaw
			}
			for _, s := range [...]struct {
				role string
				w    Samples
			}{{codes, t.Codes}, {RoleMin, t.Min}, {RoleMax, t.Max}, {RoleView, t.View}} {
				st := s.w.storage()
				if st == "" {
					continue
				}
				if st != t.Fmt.Storage {
					return nil, fmt.Errorf("wire: channel %d %s words are %s, its format says %q", t.Ch, s.role, st, t.Fmt.Storage)
				}
				n := s.w.Len()
				h.Segs = append(h.Segs, Seg{Ch: t.Ch, Role: s.role, Storage: st, N: n, At: payload})
				payload = align8(payload + n*storageBytes(st))
			}
		}
	}
	if len(h.Traces) == 0 {
		h.Traces = nil
	}
	if len(h.Segs) == 0 {
		h.Segs = nil
	}
	hj, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	start := align8(8 + len(hj))
	buf := make([]byte, start+payload)
	copy(buf, FrameMagic2[:])
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(hj))) //nolint:gosec // G115: a JSON header of a few KB
	copy(buf[8:], hj)
	if h.Unchanged {
		return buf, nil
	}
	k := 0
	for i := range f.Traces {
		t := &f.Traces[i]
		for _, w := range [...]Samples{t.Codes, t.Min, t.Max, t.View} {
			if w.storage() == "" {
				continue
			}
			at := start + h.Segs[k].At
			if len(w.U16) > 0 {
				copy(buf[at:], LEBytes(w.U16))
			} else {
				copy(buf[at:], w.U8)
			}
			k++
		}
	}
	return buf, nil
}

// align8 rounds n up to a multiple of 8.
// TRLC-LINKS: REQ-DSO-163
func align8(n int) int { return (n + 7) &^ 7 }

// storageBytes is a stored word's size.
// TRLC-LINKS: REQ-DSO-163
func storageBytes(st string) int {
	if st == "u16" {
		return 2
	}
	return 1
}
