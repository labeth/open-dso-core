package hal

import "context"

// Streamer is the optional gap-free stream capability: roll, sound and
// decoded streams through a lease/release ring, so the hot path never
// allocates. A loss is reported as a fault, never as a silent gap.
type Streamer interface {
	Kinds() []Payload         // ANY: which payloads the current image set can stream
	Start(s StreamSpec) error // OWNER
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
