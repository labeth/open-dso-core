package sample

// RecordID names one stored acquisition record, so a re-read while stopped
// and an event joined to a record find the same one.
type RecordID struct{ Epoch, Seq uint32 }

// Counters are hardware counters latched with a record, if the board has any.
type Counters struct {
	FreqHz float64
	Total  uint64
	Valid  bool
}

// Status is the acquisition state a record was taken with.
type Status struct {
	Triggered, Done, Forced bool
	// Coherent: the record is one capture end to end. Degraded: a known
	// defect survived the board's retries (the SDS1000CML+'s dead tail).
	Coherent, Degraded bool
	Counters           Counters
	Record             RecordID
}

// TrigKind is how a frame's trigger position was found.
type TrigKind uint8

// The trigger kinds; the zero value is not stated.
const (
	HardwareNode    TrigKind = iota + 1 // a hardware trigger node fired
	SoftwareRefined                     // software placed it on the captured data
	Forced                              // a forced acquisition: no trigger condition met
	StreamStop                          // where a stream was stopped
	Untriggered                         // nothing could trigger: a free-running capture or a roll view
)

// Match is where a protocol trigger's match ended.
type Match struct {
	EndS float64 // seconds after TriggerAt
}

// Conditioning is the digital filtering applied to a frame's precision words.
type Conditioning struct {
	Filter      string
	Guard       int     // boundary samples excluded from conditioned measurements
	BandwidthHz float64 // qualified flat passband; 0: no digital bandwidth limit
	PassbandHz  float64
	NoiseGain   float64 // ideal noise gain of the filter
}

// StreamPos places a capture window on one time axis with its neighbours.
// A board that measures them gives every record its duration and the gap
// before it; Seq numbers the windows of a stream (0: not a stream window).
type StreamPos struct {
	Seq     uint64  // monotonic window counter
	WindowS float64 // this window's captured duration
	GapS    float64 // measured blackout before this window
}

// Detail is a further window of a frame's record, read beside it at more
// detail: the full-rate stretch around the trigger edge, a magnified view's
// stretch. Its frame is read-only once published, so copies share it.
type Detail struct {
	Role Role
	// From, To are where the window lies among the frame's samples
	// (fractional indices); 0, 0 when the board does not place it.
	From, To float64
	// Read numbers the read the window came from: a window carried to the
	// frames between reads keeps its number, so statistics count it once.
	Read  uint64
	Frame *Frame
}

// Settings is the configuration a frame was drained with, so a setting
// changed mid-flight cannot tear the served frame.
type Settings struct {
	TdivS      float64 // acquisition time per division
	DisplayedS float64 // displayed time per division
	Norm       bool    // normal (not auto) sweep
}

// Frame is one published acquisition: its traces and everything a consumer
// needs to show or measure them. Frames are reused in place, so a producer
// sets or clears every field each time.
// TRLC-LINKS: REQ-DSO-007, REQ-DSO-009
type Frame struct {
	Traces    []Trace
	Content   Content
	Flags     ContentFlag
	SampleS   float64 // between delivered samples
	CaptureS  float64 // the converters' sample interval for this record
	AcqDecim  int     // hardware decimation before storage; 1 = full rate
	Depth     int     // samples per channel in the record
	WinPoints int     // delivered samples spanning the 10-division screen
	TriggerAt float64 // delivered-sample index with any fine (sub-sample) position folded in; -1: a flat rail
	TrigKind  TrigKind
	TrigNode  string // the hardware node that fired, by name (edge, zone, a protocol)
	Match     *Match
	Cond      Conditioning
	Stream    StreamPos
	Seq       uint64 // advances only on a publish
	// ContentID is the same for frames that show the same conditioned record
	// (a stopped record re-shown at a new scale): consumers may reuse results
	// across them. 0 = unknown, never shared.
	ContentID uint64
	Status    Status
	Snapshot  Settings
	// SourcePtp is the peak-to-peak, in codes, of the channel the trigger
	// watches: whether there is a signal to trigger on.
	SourcePtp int
	Details   []Detail
}

// TriggerName names how the frame's trigger was found, as the web page and
// the logs show it: "hardware-edge", "forced", "untriggered", "roll".
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) TriggerName() string {
	switch {
	case f.Flags.Has(Roll):
		return "roll"
	case f.TrigKind == HardwareNode:
		return "hardware-" + f.TrigNode
	case f.TrigKind == SoftwareRefined:
		return "software-" + f.TrigNode
	case f.TrigKind == Forced:
		return "forced"
	case f.TrigKind == StreamStop:
		return "stream"
	case f.TrigKind == Untriggered:
		return "untriggered"
	}
	return ""
}

// Len is the delivered sample count: the valid prefix the traces share.
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) Len() int {
	if len(f.Traces) == 0 {
		return 0
	}
	return f.Traces[0].Valid
}

// SetLen sets every trace's valid prefix to n.
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) SetLen(n int) {
	for i := range f.Traces {
		f.Traces[i].Valid = n
	}
}

// Codes8 is trace ch's 8-bit codes, nil when the frame has no such trace.
// The slices returned by these accessors are the trace's own, with its full
// length; the samples are their first Len().
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) Codes8(ch int) []uint8 {
	if ch >= len(f.Traces) {
		return nil
	}
	return f.Traces[ch].Codes8
}

// Precision is trace ch's precision words, nil when absent.
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) Precision(ch int) []uint16 {
	if ch >= len(f.Traces) {
		return nil
	}
	return f.Traces[ch].Precision
}

// Min8 is trace ch's 8-bit display envelope minima, nil when absent.
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) Min8(ch int) []uint8 {
	if ch >= len(f.Traces) {
		return nil
	}
	return f.Traces[ch].Min8
}

// Max8 is trace ch's 8-bit display envelope maxima, nil when absent.
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) Max8(ch int) []uint8 {
	if ch >= len(f.Traces) {
		return nil
	}
	return f.Traces[ch].Max8
}

// Held is trace ch's held-sample marks (Trace.Held), nil when absent.
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) Held(ch int) []bool {
	if ch >= len(f.Traces) {
		return nil
	}
	return f.Traces[ch].Held
}

// Detail returns the frame's detail window of role r, or nil.
// TRLC-LINKS: REQ-DSO-1003
func (f *Frame) Detail(r Role) *Detail {
	for i := range f.Details {
		if f.Details[i].Role == r {
			return &f.Details[i]
		}
	}
	return nil
}

// Trace is one channel. Exactly one of Codes8 and Codes16 holds the samples,
// as Format.Storage says; a PeakPairs frame holds each bucket's two extremes
// there, interleaved. Precision is an optional finer word stream beside
// 8-bit codes (fixed point with PrecisionFrac fraction bits), flagged on the
// frame. Min/Max hold a display envelope of their own length, one (min, max)
// per screen column, on an Envelope frame.
type Trace struct {
	Ch            int
	Format        CodeFormat
	Codes8        []uint8
	Codes16       []uint16
	Min8, Max8    []uint8
	Min16, Max16  []uint16
	Precision     []uint16
	PrecisionFrac uint8
	Held          []bool // roll: samples no capture covered
	Valid         int    // the valid prefix of the codes, precision words and held marks
	Scale         Scale
	CodesPerDiv   float64
}

// Word returns sample i as a stored word, from Codes8 or Codes16.
// TRLC-LINKS: REQ-DSO-1003
func (t *Trace) Word(i int) uint32 {
	if t.Format.Storage == U16 {
		return uint32(t.Codes16[i])
	}
	return uint32(t.Codes8[i])
}

// View8 is samples [0, Valid) on the 8-bit view (CodeFormat.Code8): 8-bit
// codes as they are, no copy; wider words mapped into a new slice.
// TRLC-LINKS: REQ-DSO-1003
func (t *Trace) View8() []uint8 {
	if t.Format.Storage != U16 {
		return t.Codes8[:min(t.Valid, len(t.Codes8))]
	}
	words := t.Codes16[:min(t.Valid, len(t.Codes16))]
	out := make([]uint8, len(words))
	for i, w := range words {
		out[i] = t.Format.Code8(uint32(w))
	}
	return out
}

// CodesPerDiv8 is the trace's codes per division on the 8-bit view.
// TRLC-LINKS: REQ-DSO-1003
func (t *Trace) CodesPerDiv8() float64 { return t.CodesPerDiv / t.Format.Unit8() }

// Volts returns sample i in volts at the probe tip.
// TRLC-LINKS: REQ-DSO-1003
func (t *Trace) Volts(i int) float64 { return t.Scale.Volts(float64(t.Word(i))) }

// CopyFrom deep-copies src into f, reusing f's buffers: a consumer's copy for
// long work outside the publisher's lock. Pointer fields are copied as new
// values, so the copy shares nothing with src but the detail windows' frames,
// which are read-only.
// TRLC-LINKS: REQ-DSO-020
func (f *Frame) CopyFrom(src *Frame) {
	traces, details := f.Traces, f.Details
	match := f.Match
	*f = *src
	f.Traces = traces[:0]
	for i := range src.Traces {
		if i < cap(traces) {
			f.Traces = f.Traces[:i+1]
		} else {
			f.Traces = append(f.Traces, Trace{})
		}
		f.Traces[i].copyFrom(&src.Traces[i])
	}
	f.Match = copyPtr(match, src.Match)
	f.Details = reuse(details, src.Details)
}

// copyFrom copies src's valid prefix into t's storage.
// TRLC-LINKS: REQ-DSO-1003
func (t *Trace) copyFrom(src *Trace) {
	c8, c16 := t.Codes8, t.Codes16
	n8, x8, n16, x16 := t.Min8, t.Max8, t.Min16, t.Max16
	p, h := t.Precision, t.Held
	*t = *src
	t.Codes8 = reuse(c8, prefix(src.Codes8, src.Valid))
	t.Codes16 = reuse(c16, prefix(src.Codes16, src.Valid))
	t.Min8 = reuse(n8, src.Min8)
	t.Max8 = reuse(x8, src.Max8)
	t.Min16 = reuse(n16, src.Min16)
	t.Max16 = reuse(x16, src.Max16)
	t.Precision = reuse(p, prefix(src.Precision, src.Valid))
	t.Held = reuse(h, prefix(src.Held, src.Valid))
}

// prefix is s[:n], or all of s when it is shorter.
// TRLC-LINKS: REQ-DSO-1003
func prefix[T any](s []T, n int) []T { return s[:min(n, len(s))] }

// reuse copies src into buf's storage, growing it only when it is short.
// An absent src leaves an empty slice that keeps its storage for the next copy.
// TRLC-LINKS: REQ-DSO-1003
func reuse[T any](buf, src []T) []T { return append(buf[:0], src...) }

// copyPtr copies *src into dst's storage, or returns nil for a nil src.
// TRLC-LINKS: REQ-DSO-1003
func copyPtr[T any](dst, src *T) *T {
	if src == nil {
		return nil
	}
	if dst == nil {
		dst = new(T)
	}
	*dst = *src
	return dst
}
