// ENGMODEL-OWNER-UNIT: FU-DSO-DECODEDLINES

package decodedlines

// The kinds of decoded event.
const (
	EventStart uint8 = iota + 1
	EventData
	EventEnd
	EventError
	EventLoss
	EventTrigger
	EventRetained
)

// Event is one decoded event as a stream delivers it, from a hardware
// decoder or a software decode of the samples. Sample is an absolute sample
// ordinal within Epoch. Sequence counts all emitted events including loss
// markers. Record is meaningful only with RecordValid. Value preserves full
// decoder words (including ARINC's 19-bit data field). For EventLoss, Count
// is the number of lost events; zero means unknown count. For
// EventRetained, Sample is the first retained sample and Count its length.
// Channels is a bit mask: bit 0 C1, bit 1 C2. Protocol is the hardware
// decoder's identity; zero is reserved for transport-wide loss and
// retained-record metadata.
type Event struct {
	Epoch, Sequence uint32
	Sample          uint64
	Record, Value   uint32
	Count           uint32
	Protocol, Kind  uint8
	Channels        uint8
	Valid           bool
	RecordValid     bool
}
