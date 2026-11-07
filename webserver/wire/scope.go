// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

// Record is one whole sampled record (/api/record.bin): both channels'
// codes, their interval, and how a code reads as volts on each channel at
// the probe tip (volts = (code-128)*Vpc - Off).
type Record struct {
	Seq      uint64
	SampleS  float64
	TdivS    float64
	Vpc, Off [2]float64
	C1, C2   []byte
	// Chans are the record's channels when they are not exactly C1 and C2
	// of 8-bit codes: each channel's codes on the 8-bit view and its scale.
	// With Chans, C1, C2, Vpc and Off are unused.
	Chans []RecordChan
}

// RecordChan is one channel of a record: its codes on the 8-bit view
// (volts = (code-128)*Vpc - Off at the probe tip).
type RecordChan struct {
	Ch       int
	Codes    []byte
	Vpc, Off float64
}

// Zone is one zone-trigger rectangle: seconds from the trigger edge by
// display codes (0..255) on a channel.
type Zone struct {
	DtLoS, DtHiS   float64
	CodeLo, CodeHi int
	Avoid          bool // the frame must not enter the zone
	Ch             int
}

// ZoneV is a zone in the v2 shape: seconds from the trigger edge by volts
// at the probe tip on a channel.
type ZoneV struct {
	DtLoS, DtHiS float64
	VLo, VHi     float64
	Avoid        bool
	Ch           int
}

// Mask is an envelope mask: a frame fails where a sample of display column
// j leaves [Lo[j], Hi[j]] on channel Ch. With TdivS and SampleS it holds
// only for frames of that timebase, interval and kind (PeakDetect).
type Mask struct {
	Lo, Hi     []uint8
	WinCols    int
	Ch         int
	TdivS      float64
	SampleS    float64
	PeakDetect bool
}

// MaskFail is one captured failing frame: its codes, the first violation
// and when it was captured (AtNs, the scope's clock).
type MaskFail struct {
	C1, C2     []uint8
	Valid      int
	Seq        uint64
	EdgeX      float64
	SampleS    float64
	WinCols    int
	AtNs       int64
	FailCol    int
	FailCode   int
	FailSample int
}

// BodePoint is one point of the frequency-response curve.
type BodePoint struct {
	FreqHz, GainDB, PhaseDeg float64
}

// SoundInfo is the audio stream's state, as a page reads it.
type SoundInfo struct {
	State     string
	Active    bool
	RateHz    float64
	Next      uint64 // the next sample's ordinal
	Losses    int
	Marks     bool  // the stream carries the FPGA's edge marks
	MarkCh    int   // the marks' channel
	MarkLevel uint8 // their level, an 8-bit code
	Err       string
}

// SoundBlock is samples First..First+N-1 of each channel (Q8.8 codes), with
// the scales (volts at the tip), gaps and marks inside them.
type SoundBlock struct {
	First  uint64
	N      int
	Q1, Q2 []uint16
	RateHz float64
	Scales []SoundScale
	Gaps   []uint64
	Marks  []SoundMark
	Next   uint64 // the ring's next ordinal when read
}

// SoundTake is a frozen stretch of the stream and the trigger in it.
type SoundTake struct {
	SoundBlock
	ID         uint64
	Why        string // "stop", "single" or "normal"
	HasTrig    bool
	Trig       uint64 // the trigger's sample (stream ordinal)
	TrigCh     int
	TrigRising bool
	TrigLevel  uint8 // an 8-bit code
}

// WordChunk is a piece of the digital stream as the scope's ring holds it:
// Index the stream sample its first word is, Words its word count, Enc
// their coding, Break words lost before it, Check the scope would decode
// what it queued after it.
type WordChunk struct {
	Index uint64
	Words int
	Break bool
	Check bool
	Enc   []byte
}

// Words is a page's read of the digital stream: the stream's description
// (Info, the scope's), the chunks, those asked for that the ring no longer
// held, the seq to ask for next and the samples the scope holds without
// the page's frames.
type Words struct {
	Info    any
	Chunks  []WordChunk
	Dropped uint64
	Next    uint64
	Lag     uint64
}
