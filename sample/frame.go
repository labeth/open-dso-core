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

// The trigger kinds; the zero value is none (an untriggered roll or stream).
const (
	HardwareNode    TrigKind = iota + 1 // a hardware trigger node fired
	SoftwareRefined                     // software placed it on the captured data
	Forced                              // a forced acquisition: no trigger condition met
	StreamStop                          // where a stream was stopped
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

// StreamPos places a stream window on one time axis with its neighbours.
type StreamPos struct {
	Seq     uint64  // monotonic window counter
	WindowS float64 // this window's captured duration
	GapS    float64 // measured blackout before this window
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
// TRLC-LINKS: REQ-SDS-007, REQ-SDS-009
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
	Match     *Match
	Cond      Conditioning
	Stream    *StreamPos
	Seq       uint64 // advances only on a publish
	// ContentID is the same for frames that show the same conditioned record
	// (a stopped record re-shown at a new scale): consumers may reuse results
	// across them. 0 = unknown, never shared.
	ContentID uint64
	Status    Status
	Snapshot  Settings
}

// Trace is one channel. Exactly one of Codes8 and Codes16 holds the samples,
// as Format.Storage says; PeakPairs frames use the Min/Max pair of the same
// storage instead. Precision is an optional finer word stream beside 8-bit
// codes (fixed point with PrecisionFrac fraction bits), flagged on the frame.
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
	Valid         int    // the valid prefix of every slice above
	Scale         Scale
	CodesPerDiv   float64
}

// Word returns sample i as a stored word, from Codes8 or Codes16.
func (t *Trace) Word(i int) uint32 {
	if t.Format.Storage == U16 {
		return uint32(t.Codes16[i])
	}
	return uint32(t.Codes8[i])
}

// Volts returns sample i in volts at the probe tip.
func (t *Trace) Volts(i int) float64 { return t.Scale.Volts(float64(t.Word(i))) }

// CopyFrom deep-copies src into f, reusing f's buffers: a consumer's copy for
// long work outside the publisher's lock. Pointer fields are copied as new
// values, so the copy shares nothing with src.
// TRLC-LINKS: REQ-SDS-020
func (f *Frame) CopyFrom(src *Frame) {
	traces := f.Traces
	match, stream := f.Match, f.Stream
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
	f.Stream = copyPtr(stream, src.Stream)
}

// copyFrom copies src's valid prefix into t's storage.
func (t *Trace) copyFrom(src *Trace) {
	c8, c16 := t.Codes8, t.Codes16
	n8, x8, n16, x16 := t.Min8, t.Max8, t.Min16, t.Max16
	p, h := t.Precision, t.Held
	*t = *src
	t.Codes8 = reuse(c8, prefix(src.Codes8, src.Valid))
	t.Codes16 = reuse(c16, prefix(src.Codes16, src.Valid))
	t.Min8 = reuse(n8, prefix(src.Min8, src.Valid))
	t.Max8 = reuse(x8, prefix(src.Max8, src.Valid))
	t.Min16 = reuse(n16, prefix(src.Min16, src.Valid))
	t.Max16 = reuse(x16, prefix(src.Max16, src.Valid))
	t.Precision = reuse(p, prefix(src.Precision, src.Valid))
	t.Held = reuse(h, prefix(src.Held, src.Valid))
}

// prefix is s[:n], or all of s when it is shorter.
func prefix[T any](s []T, n int) []T { return s[:min(n, len(s))] }

// reuse copies src into buf's storage, growing it only when it is short.
// An absent src leaves an empty slice that keeps its storage for the next copy.
func reuse[T any](buf, src []T) []T { return append(buf[:0], src...) }

// copyPtr copies *src into dst's storage, or returns nil for a nil src.
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
