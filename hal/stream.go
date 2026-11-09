package hal

import "context"

// Streamer is the optional gap-free stream capability: roll, sound and
// decoded streams through a lease/release ring, so the hot path never
// allocates. A loss is reported as a fault, never as a silent gap.
//
// A stream is the one place a board may run its own goroutine on the bus
// (an exception to the Service hook rule): the transfer engine that fills
// the ring cannot wait for the owner's commit points without losing
// blocks. The board's stream goroutine owns the transfer path between
// Start and Stop and nothing else; while it runs the board refuses
// Acquirer.Arm and Readout.Read with the reason rather than sharing the
// path, Platform.Service still runs on the owner, and Stop returns only
// after the goroutine has let go of the bus. Start commits what the stream
// needs from the staged plan itself; a board whose plan only a stream can
// serve (a roll plan on a scope that rolls by streaming) refuses Arm on it.
type Streamer interface {
	Kinds() []Payload // ANY: which payloads the current image set can stream
	// Start commits the staged plan's stream settings and starts the
	// board's stream goroutine. OWNER.
	Start(s StreamSpec) error
	// Lease blocks for the next filled block. ANY, one consumer.
	Lease(ctx context.Context) (StreamBlock, error)
	Release(b StreamBlock) // ANY
	Stop() error           // OWNER
	Faults() StreamFaults  // ANY
}

// CommitStreamer is optional on a Streamer: the board takes the front end
// and trigger committed while it streams (Set and Program stage as ever),
// writing the front end between its blocks or chunks and arming each chunk
// on the trigger staged then, and every block states the scale its codes
// were captured at (StreamBlock.Scales); data captured across a change of a
// channel's scale, or in the front end's settle after one, it drops or
// marks Held. The cycle then commits a setting while the stream goes on
// instead of starting it again. ANY.
type CommitStreamer interface {
	TakesCommits() bool
}

// RingRecorder is optional on a Streamer: the stream runs while the record
// memory keeps a ring of the same signal, and Freeze stops the ring at a
// stream position so Readout can show it as a record. OWNER.
type RingRecorder interface {
	Freeze(at uint64) (RecordID, error)
}

// Payload is what a stream block carries.
type Payload uint8

// The payloads.
const (
	PayloadCodes  Payload = iota + 1 // sample codes in the block's formats
	PayloadBits                      // packed logic words: 16 periods per uint16
	PayloadEvents                    // hardware-decoded events
	PayloadChunks                    // roll: captured chunks with held gaps between them
)

// StreamSpec is what to stream.
type StreamSpec struct {
	Payload  Payload
	Channels []int
	SampleS  float64 // between delivered samples (Codes, Bits)
	Decoder  string  // the hardware decoder for PayloadEvents: "uart", "spi", ...
}

// StreamBlock is a leased region of the stream ring.
//
// With several channels, PayloadCodes and PayloadChunks blocks hold equal
// runs per channel, channel-major in StreamSpec.Channels order: channel i's
// n samples are Words[i*n:(i+1)*n] (or Bytes), n = len/len(Formats); Held
// has n entries, one per sample time, shared by every channel. Run returns
// one channel's run.
type StreamBlock struct {
	Payload Payload
	Formats []CodeFormat // per channel, for PayloadCodes and PayloadChunks
	Bytes   []byte       // codes stored as 8-bit words
	Words   []uint16     // codes stored as 16-bit words, or PayloadBits
	Events  []Event      // PayloadEvents
	Held    []bool       // PayloadChunks: samples no capture covered
	Marks   []Mark
	First   uint64 // sequence number of the first item; a jump is a recorded fault
	Ring    int    // ring slot, for Release
	// SampleS is the time between the block's samples as the board
	// delivers them (PayloadCodes, PayloadChunks); 0: the StreamSpec's.
	SampleS float64
	// Scales are the volts of each channel's codes as its front end stood
	// when they were captured (PayloadCodes, PayloadChunks; per channel,
	// as Formats): a setting committed while the stream runs reaches the
	// blocks captured after the board applied it. nil, or a channel's zero
	// PerCode: the committed state's scale.
	Scales []Scale
}

// Mark is a position the board flagged in a stream: where a setting
// changed, a capture began, or a channel's codes crossed the StreamMarker's
// level (Kind MarkRise or MarkFall; At the newest such crossing, Count the
// crossings of that kind since the last mark of it).
type Mark struct {
	At      uint64 // item sequence number
	Kind    string
	Channel int // the channel a crossing was on
	Count   int // crossings this mark stands for (0: one)
}

// The kinds of a level-crossing mark.
const (
	MarkRise = "rise"
	MarkFall = "fall"
)

// StreamMarker is optional on a Streamer: the board's hardware flags where
// a channel's samples cross a level, at the full rate (a crossing the
// stream's decimation would blur), and the stream's blocks carry them as
// Marks. A board that lacks it marks nothing.
type StreamMarker interface {
	// SetMarks sets what the board marks; a running stream takes it at its
	// next block. ANY.
	SetMarks(m MarkSpec) error
	// Marking is what the running stream marks now, FromTrigger resolved;
	// on is false when the stream marks no crossings at all (an image
	// without the marks, no stream running). ANY.
	Marking() (m MarkSpec, on bool)
}

// MarkSpec is the level crossings a StreamMarker flags: on Channel, through
// Level with Hyst of hysteresis, both in the raw converter's codes
// (Profile.Format), rising, falling or both; neither marks nothing.
// FromTrigger marks the committed edge trigger's source, level and slope
// instead, as the trigger's comparator sees them (the other fields are
// ignored).
type MarkSpec struct {
	Channel     int
	Level, Hyst uint32
	Rise, Fall  bool
	FromTrigger bool
}

// StreamFaults are a stream's losses, counted since Start.
type StreamFaults struct {
	LostBlocks uint64
	SeqJumps   uint64
	Stalls     uint64
}

// Run returns channel i's run of a PayloadCodes or PayloadChunks block
// (i indexes Formats): bytes for codes stored as 8-bit words, words for
// 16-bit ones; both nil when i is out of range.
// TRLC-LINKS: REQ-DSO-1001
func (b StreamBlock) Run(i int) (bytes []byte, words []uint16) {
	nch := len(b.Formats)
	if i < 0 || i >= nch {
		return nil, nil
	}
	if b.Words != nil {
		n := len(b.Words) / nch
		return nil, b.Words[i*n : (i+1)*n]
	}
	n := len(b.Bytes) / nch
	return b.Bytes[i*n : (i+1)*n], nil
}
