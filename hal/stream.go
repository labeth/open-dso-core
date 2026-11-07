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
}

// Mark is a position the board flagged in a stream: where a setting
// changed or a capture began.
type Mark struct {
	At   uint64 // item sequence number
	Kind string
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
