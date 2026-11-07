// ENGMODEL-OWNER-UNIT: FU-DSO-STREAMWIRE

package streamwire

import "time"

// State is where a decoded stream stands.
type State int

// The states of a stream.
const (
	Idle      State = iota // no stream
	Streaming              // lines are arriving
	Triggered              // the trigger fired; the record is on the display
	Stopped                // stopped by the operator
	Failed                 // the stream could not start or ended in a fault
	Starting               // the capture is being set up (an image load can take seconds)
)

// String names the state for the display.
// TRLC-LINKS: REQ-DSO-018
func (s State) String() string {
	return [...]string{"IDLE", "STREAM", "TRIG'D", "STOP", "FAIL", "START"}[s]
}

// View is a snapshot of the stream's state and counters: what the display
// and the web page show about it.
type View struct {
	State           State
	Label           string // protocol and settings
	Lines           int    // transcript lines held
	Events, Units   uint64
	Lost            uint64
	Errors          uint64
	UnitsPerSecond  float64
	Elapsed         time.Duration
	Err             string
	Note            string // while starting: why it takes seconds (an FPGA image to load)
	RecordAvailable bool
	TriggerLine     int // transcript line holding the trigger; -1 unknown
	// Mode says how the stream runs: gap-free from the FPGA's decoded events
	// or from software decode of the continuous stream, or captures with the
	// share of signal time read (the source's DecodedStreamMode).
	Mode string
}
