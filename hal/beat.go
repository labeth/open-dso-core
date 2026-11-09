package hal

// BeatBoard is optional on a Board: LONG work the board runs for the owner
// (an image load, a converter recovery, a stacking session, a stream's
// start on its own image) takes seconds, so it beats the owner's heartbeat
// between its steps. The owner hands its heartbeat over once, before its
// first pass; a step that hangs beats no more, so a supervisor watching
// the heartbeat sees a wedge.
type BeatBoard interface {
	SetBeat(beat func()) // OWNER, once
}

// HandBeat hands b the owner's heartbeat beat when b beats inside its
// LONG work (BeatBoard).
// TRLC-LINKS: REQ-DSO-294
func HandBeat(b Board, beat func()) {
	if bb, ok := b.(BeatBoard); ok {
		bb.SetBeat(beat)
	}
}
