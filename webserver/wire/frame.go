// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package wire

import (
	"encoding/binary"
	"encoding/json"

	"github.com/labeth/open-dso-core/measure"
)

// Binary frame layout served by /api/frame.bin (all integers little-endian):
//
//	[0]     u8  magic 0xF5 — bump on any layout change
//	[1]     u8  flags: bit0 unchanged, bit1 envelope, bit2 deep, bit3 empty,
//	            bit4 raw, bit5 precision
//	[2:4]   u16 reserved (0)
//	[4:8]   u32 H = JSON header byte length
//	[8:8+H]     UTF-8 JSON header: FrameReply with the array fields nilled,
//	            plus head/tail = the contiguous -1 margin counts
//	[8+H:]      payload, raw uint8 ADC codes:
//	            native: c1[cols] c2[cols]
//	            deep:   c1[cols-head-tail] c2[cols-head-tail] (cols == depth)
//	            env:    e1min[cols] e1max[cols] e2min[cols] e2max[cols]
//	            precision: q1[body] q2[body], u16 Q8.8 codes
//	            unchanged/empty: none
//	            then, when the header's view_n > 0 (a request that leased a
//	            view window, view=): vc1[view_n] vc2[view_n]
//
// The -1 sentinels in the int16 reply arrays are provably contiguous head/tail
// runs (the deep path serves the record verbatim; window() rail-extends off-record
// columns), so two counts replace the in-band sentinel and the payload narrows to uint8.
const BinMagic = 0xF5

// The flags byte's bits.
const (
	FlagUnchanged = 1 << 0
	FlagEnv       = 1 << 1
	FlagDeep      = 1 << 2
	FlagEmpty     = 1 << 3
	FlagRaw       = 1 << 4 // raw=1: un-windowed record, payload c1[cols] c2[cols]
	FlagPrecision = 1 << 5 // the payload is u16 Q8.8 words
)

// FrameReply is a published frame as /api/frame.bin serves it: the header
// fields and the per-channel arrays EncodeBinFrame lays out after it.
// TRLC-LINKS: REQ-SDS-163
type FrameReply struct {
	NoiseGainIdeal float64  `json:"noise_gain_ideal,omitempty"`
	PassbandHz     float64  `json:"passband_hz,omitempty"`
	Q1, Q2         []uint16 `json:"-"`
	FractionBits   uint8    `json:"fraction_bits,omitempty"`
	Decimation     uint32   `json:"decimation,omitempty"`
	BandwidthHz    float64  `json:"bandwidth_hz,omitempty"`
	FilterGuard    int      `json:"filter_guard,omitempty"`
	Filter         string   `json:"filter,omitempty"`
	CaptureDepth   int      `json:"capture_depth,omitempty"`
	TriggerKind    string   `json:"trigger_kind,omitempty"`
	Seq            uint64   `json:"seq"`
	Unchanged      bool     `json:"unchanged,omitempty"`
	C1             []int16  `json:"c1,omitempty"`
	C2             []int16  `json:"c2,omitempty"`
	IsEnv          bool     `json:"is_env,omitempty"`
	// PeakDetect: the samples are (min, max) pairs per bucket, sample_s apart -
	// extremes, not a time series; spectra and timing use the pair midpoints.
	PeakDetect bool `json:"peak_detect,omitempty"`
	// Ordered: a peak-detect frame whose pairs are in time order, so it decodes
	// as samples sample_s apart (the live envelope; engine orderEnvelope).
	Ordered bool `json:"ordered,omitempty"`
	// FullRate: the converters' own samples at the full rate (engine
	// Frame.FullRate), the only frames the eye folds.
	FullRate bool `json:"full_rate,omitempty"`
	// CaptureSampleS is the converters' sample interval when it differs from
	// sample_s (a peak-detect frame): the rate the scope really sampled at.
	CaptureSampleS float64 `json:"capture_sample_s,omitempty"`
	E1Min          []int16 `json:"e1min,omitempty"`
	E1Max          []int16 `json:"e1max,omitempty"`
	E2Min          []int16 `json:"e2min,omitempty"`
	E2Max          []int16 `json:"e2max,omitempty"`
	EdgeX          float64 `json:"edge_x"`
	// MatchEndS is where the hardware UART matcher received the matched
	// character's stop bit, seconds after the trigger point (engine
	// Frame.MatchEndS): the page's UART decode frames the characters back to
	// back with it from there. Absent without such a match.
	MatchEndS  *float64 `json:"match_end_s,omitempty"`
	Ptp        int      `json:"ptp"`
	TdivS      float64  `json:"tdiv_s"`
	DisplayedS float64  `json:"displayed_sdiv_s"`
	Interp     bool     `json:"interp"`
	Norm       bool     `json:"norm"`
	Trigd      bool     `json:"trigd"`
	Coherent   bool     `json:"coherent"`
	Degraded   bool     `json:"degraded,omitempty"` // native-fast dead tail survived retries: half-capture

	// Scale factors the client uses for cursors/FFT/XY/measurements.
	Cols     int     `json:"cols"`       // number of columns returned per trace
	ColSpanS float64 `json:"col_span_s"` // seconds spanned by the whole column array

	// DtS is the TRUE capture time per served point. It differs from
	// col_span_s/cols on the 1–200 ns/div bands, where the display window is
	// deliberately sized at the 1 ns nominal while the hardware samples every
	// 2 ns (spec 04 §6): col_span_s carries the nominal so the screen matches
	// the labelled tdiv, and anything reconstructing real time from the served
	// points (CSV/sigrok export, decode) must use dt_s instead. 0 on envelope/
	// roll frames, which aggregate many acquisitions and have no per-point time.
	DtS float64 `json:"dt_s,omitempty"`

	// Deep-memory (full=1 on decimated bands): the served array is the FULL
	// drained record, not the 10-div screen slice; the client windows it.
	Depth    int     `json:"depth,omitempty"` // full-record sample count served (0 = not deep)
	EdgeFrac float64 `json:"edge_frac"`       // trigger anchor as a fraction of the served array (-1 = free-run)
	WinFrac  float64 `json:"win_frac"`        // one 10-div screen as a fraction of the served array (1 = whole)
	// Held is a roll view's served sample ranges [a, b) no capture covered:
	// the last value held between chunks, drawn dimmed, no signal.
	Held [][2]int `json:"held,omitempty"`
	// The page's zoom into a live envelope at full detail (its view window,
	// leased with view=): ViewN values a channel, coupled as the trace is,
	// the first at ViewA and the last at ViewB of the served array (value i
	// of n at i/(n-1), as the page draws them). On the binary transport they
	// follow the payload, ViewN bytes a channel.
	ViewA float64 `json:"view_a,omitempty"`
	ViewB float64 `json:"view_b,omitempty"`
	ViewN int     `json:"view_n,omitempty"`
	VC1   []int16 `json:"vc1,omitempty"`
	VC2   []int16 `json:"vc2,omitempty"`

	// Stream/stitch mode: continuity so the client stitches windows on one axis.
	StreamSeq uint64  `json:"stream_seq,omitempty"` // monotonic window counter (0 = not a stream frame)
	WindowNs  int64   `json:"window_ns,omitempty"`  // this window's captured duration
	GapNs     int64   `json:"gap_ns,omitempty"`     // blackout (drain+re-arm) before this window
	Vpc1      float64 `json:"vpc1"`                 // volts per ADC code, CH1 (Vdiv/32)
	Vpc2      float64 `json:"vpc2"`                 // volts per ADC code, CH2
	Off1V     float64 `json:"off1_v"`               // applied offset volts (input-referred)
	Off2V     float64 `json:"off2_v"`

	M1 *measure.Result `json:"m1,omitempty"` // CH1 auto-measurements
	M2 *measure.Result `json:"m2,omitempty"` // CH2
	// MeasSeq is the frame M1 and M2 were measured on (reused for the frames
	// of the next 100 ms while running), EdgeRead that frame's edge-window
	// read (engine.Frame.EdgeRead): statistics count each reading once.
	MeasSeq  uint64 `json:"meas_seq,omitempty"`
	EdgeRead uint64 `json:"edge_read,omitempty"`

	Clip1 bool `json:"clip1,omitempty"` // CH1 railed against the ADC full scale
	Clip2 bool `json:"clip2,omitempty"` // CH2 railed — readings/measurements suspect

	// Binary-transport margin counts (set only on /api/frame.bin headers,
	// never on the JSON endpoint): the served arrays' contiguous -1 head/tail
	// runs, so the payload can carry raw uint8 codes with no in-band sentinel.
	Head int `json:"head,omitempty"`
	Tail int `json:"tail,omitempty"`

	// Raw-shape only (/api/frame.bin?raw=1): the per-sample capture interval.
	// The raw shape serves the un-windowed, un-interpolated record for the
	// super-resolution stacker; edge_x is then an absolute (sub-sample)
	// index into that record.
	SampleS float64 `json:"sample_s,omitempty"`
	WinCols int     `json:"win_cols,omitempty"`
}

// EncodeBinFrame serializes a built reply to the binary wire format. It runs
// after WithFrame returns — rep owns its data, so no lock is held here.
// TRLC-LINKS: REQ-SDS-163
func EncodeBinFrame(rep FrameReply) []byte {
	var flags byte
	precision := rep.FractionBits == 8 && len(rep.Q1) == len(rep.C1) && len(rep.Q2) == len(rep.C2) && len(rep.Q1) > 0
	var segs [][]int16
	head, tail := 0, 0
	switch {
	case rep.Unchanged:
		flags |= FlagUnchanged
	case rep.IsEnv:
		flags |= FlagEnv
		segs = [][]int16{rep.E1Min, rep.E1Max, rep.E2Min, rep.E2Max}
	default:
		if rep.Depth > 0 {
			flags |= FlagDeep
		}
		n := len(rep.C1)
		for head < n && rep.C1[head] < 0 {
			head++
		}
		if head == n { // whole array is margin (defensive: Valid<1 never publishes)
			flags |= FlagEmpty
		} else {
			for rep.C1[n-1-tail] < 0 {
				tail++
			}
			segs = [][]int16{rep.C1, rep.C2}
		}
	}
	if precision {
		flags |= FlagPrecision
	}
	hdr := rep
	hdr.C1, hdr.C2 = nil, nil
	hdr.E1Min, hdr.E1Max, hdr.E2Min, hdr.E2Max = nil, nil, nil, nil
	hdr.VC1, hdr.VC2 = nil, nil
	hdr.Head, hdr.Tail = head, tail
	view := !rep.Unchanged && !rep.IsEnv && rep.ViewN > 0 && len(rep.VC1) == rep.ViewN && len(rep.VC2) == rep.ViewN
	if !view {
		hdr.ViewA, hdr.ViewB, hdr.ViewN = 0, 0, 0
	}
	hj, err := json.Marshal(hdr)
	if err != nil { // unreachable with finite inputs; keep the wire well-formed
		flags = FlagUnchanged
		segs, head, tail = nil, 0, 0
		hj = []byte(`{"seq":0,"unchanged":true,"edge_x":-1}`)
	}
	segLen := 0
	if len(segs) > 0 {
		segLen = len(segs[0]) - head - tail
	}
	payloadBytes := segLen * len(segs)
	if precision {
		payloadBytes = 2 * (len(rep.Q1) + len(rep.Q2))
	}
	if view {
		payloadBytes += 2 * rep.ViewN
	}
	buf := make([]byte, 8, 8+len(hj)+payloadBytes)
	buf[0] = BinMagic
	buf[1] = flags
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(hj))) //nolint:gosec // G115: a JSON header of a few KB
	buf = append(buf, hj...)
	// The payload written in place: the precision words as their
	// little-endian bytes, the codes a byte each.
	at := len(buf)
	buf = buf[:at+payloadBytes]
	if precision {
		for _, seg := range [][]uint16{rep.Q1, rep.Q2} {
			at += copy(buf[at:], LEBytes(seg))
		}
	} else {
		for _, seg := range segs {
			out := buf[at : at+segLen]
			for i, v := range seg[head : head+segLen] {
				out[i] = uint8(v & 0xff) // a display code, 0..255
			}
			at += segLen
		}
	}
	if view {
		for _, seg := range [][]int16{rep.VC1, rep.VC2} {
			out := buf[at : at+len(seg)]
			for i, v := range seg {
				out[i] = uint8(v & 0xff) // a display code, 0..255
			}
			at += len(seg)
		}
	}
	return buf
}

// RawFrame is the raw shape (/api/frame.bin?raw=1) as a scope built it
// under its frame lock: the un-windowed, un-interpolated, pre-coupling
// record for the super-resolution stacker. Header carries sample_s and the
// sub-sample edge_x; Payload is c1[n] c2[n] codes, or u16 Q8.8 words with
// Precision. It owns its memory.
type RawFrame struct {
	Header           FrameReply
	Unchanged, Empty bool
	Precision        bool
	Payload          []byte
}

// EncodeRawFrame serializes a raw-shape frame: the frame layout's header
// with the raw flag, then the payload as built.
// TRLC-LINKS: REQ-SDS-163
func EncodeRawFrame(f RawFrame) []byte {
	var flags byte = FlagRaw
	if f.Unchanged {
		flags |= FlagUnchanged
	}
	if f.Empty {
		flags |= FlagEmpty
	}
	if f.Precision {
		flags |= FlagPrecision
	}
	payload := f.Payload
	hj, err := json.Marshal(f.Header)
	if err != nil { // unreachable with finite inputs
		hj, payload, flags = []byte(`{"seq":0,"unchanged":true,"edge_x":-1}`), nil, FlagRaw|FlagUnchanged
	}
	buf := make([]byte, 8, 8+len(hj)+len(payload))
	buf[0] = BinMagic
	buf[1] = flags
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(hj))) //nolint:gosec // G115: a JSON header of a few KB
	buf = append(buf, hj...)
	return append(buf, payload...)
}
