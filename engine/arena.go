// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"sync"

	"github.com/labeth/open-dso-core/sample"
)

// Arena is the triple buffer between the engine owner and consumers
// (spec 01 §2): write = producer's private drain target, ready = most recent
// published, read = consumer's private slot. Double-buffering tears against
// the immediate-re-arm invariant; three slots are required.
// TRLC-LINKS: REQ-SDS-007
type Arena struct {
	mu    sync.Mutex
	write *sample.Frame
	ready *sample.Frame
	read  *sample.Frame
	dirty bool
}

// NewArena is an arena of three frames mk makes: the producer's frame
// format and buffer sizes are its own.
// TRLC-LINKS: REQ-SDS-007
func NewArena(mk func() *sample.Frame) *Arena {
	return &Arena{write: mk(), ready: mk(), read: mk()}
}

// Write returns the producer's private slot. Owner-only.
// TRLC-LINKS: REQ-SDS-007
func (a *Arena) Write() *sample.Frame { return a.write }

// Publish swaps the drained write slot into ready. The mutex guards only the
// pointer swap — never held across bus access. If the consumer hasn't taken
// the previous frame it is overwritten (drop-newest backpressure).
// TRLC-LINKS: REQ-SDS-007
func (a *Arena) Publish() {
	a.mu.Lock()
	a.write, a.ready = a.ready, a.write
	a.dirty = true
	a.mu.Unlock()
}

// Consume returns the newest published frame. fresh=false means nothing new
// was published since the last call — the caller re-presents the held frame
// (a quiet NORM display, not an error).
// TRLC-LINKS: REQ-SDS-007
func (a *Arena) Consume() (f *sample.Frame, fresh bool) {
	a.mu.Lock()
	fresh = a.dirty
	if fresh {
		a.read, a.ready = a.ready, a.read
		a.dirty = false
	}
	f = a.read
	a.mu.Unlock()
	return f, fresh
}
