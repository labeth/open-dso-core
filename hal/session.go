package hal

import "context"

// Session is optional on a board (SessionBoard): stream sessions the
// board's own fabric runs - a decoded stream its hardware decoders and its
// software decode of its own stream images make together, a web page's
// decoded capture, a page decoding the board's digital stream itself - that
// take the owner's passes while they run (ADR-HAL-BOARD-SESSIONS). Their
// controller side (start, read, stop, end) is the board's own interface,
// reached through its extensions and the core's decoded-stream view; every
// request a controller makes waits in the session until the owner serves
// it. A board without one has the core's decoded stream over its Streamer
// (or none).
type Session interface {
	// Attach gives the session the owner it runs on. OWNER, once, before
	// the first Serve.
	Attach(h SessionHost)
	// Serve runs the requests waiting for the owner and moves the running
	// session's data, at every commit point (each pass and each poll of a
	// capture's wait). OWNER.
	Serve()
	// Step takes the pass while a session holds the acquisition: false when
	// none does and the pass goes on. OWNER.
	Step(ctx context.Context) bool
	// Active reports a session holding the acquisition: a capture waiting
	// leaves it to the session. OWNER.
	Active() bool
	// TakeRecord reports, once, a session that ended on a record the board
	// now holds as its stored record (Readout shows it whole as the stopped
	// record). OWNER.
	TakeRecord() bool
	// Close ends a running session as the owner stops. OWNER.
	Close()
}

// SessionHost is the owner a Session runs on: its run state and its own
// streams.
type SessionHost interface {
	// SetRunning starts or stops the run (a session is not RUN; a session
	// a reader left starts it again). OWNER.
	SetRunning(on bool)
	// YieldStreams ends the owner's own streams (the audio stream) before
	// a session takes the board's stream path. OWNER.
	YieldStreams()
}

// SessionBoard is a board with stream sessions of its own.
type SessionBoard interface {
	Session() (Session, bool) // ANY
}

// SessionOf is b's stream sessions; false when b has none.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func SessionOf(b Board) (Session, bool) {
	if sb, ok := b.(SessionBoard); ok {
		return sb.Session()
	}
	return nil, false
}
