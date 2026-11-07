// Package fanout fans the engine's single-consumer frame arena out to
// multiple readers (the display renderer, the web server). The arena's
// Consume contract permits ONE consumer (its read slot is private to it);
// the fan-out is that consumer, and everyone else works on its deep copy
// under an RWMutex.
//
// The fan-out is generic over the frame type only so that a scope whose
// engine still publishes its own frame type can use it; the family's frame
// is sample.Frame (NewFrames).
// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE
package fanout

import (
	"sync"
	"time"

	"github.com/labeth/open-dso-core/sample"
)

// Source is the single-consumer frame producer (the engine).
// TRLC-LINKS: REQ-DSO-020
type Source[F any] interface {
	Consume() (*F, bool)
}

// Fanout holds the newest engine frame for every reader (display, web) and
// wakes those waiting for a fresh one.
// TRLC-LINKS: REQ-DSO-020
type Fanout[F any] struct {
	mu     sync.RWMutex
	latest F
	has    bool
	wake   chan struct{} // closed+replaced under mu on every fresh snapshot
	copyFn func(dst, src *F)
	seqFn  func(*F) uint64
}

// New returns an empty fan-out. copyFrame deep-copies a frame into another
// reusing the destination's buffers; seq returns a frame's publish sequence
// (0: nothing published).
// TRLC-LINKS: REQ-DSO-020
func New[F any](copyFrame func(dst, src *F), seq func(*F) uint64) *Fanout[F] {
	return &Fanout[F]{wake: make(chan struct{}), copyFn: copyFrame, seqFn: seq}
}

// NewFrames returns an empty fan-out of sample.Frame.
// TRLC-LINKS: REQ-DSO-020
func NewFrames() *Fanout[sample.Frame] {
	return New((*sample.Frame).CopyFrom, func(f *sample.Frame) uint64 { return f.Seq })
}

// Run snapshots fresh frames at up to 100 Hz so the live preview is not
// limited by the display cadence. Blocks; run as a goroutine.
// TRLC-LINKS: REQ-DSO-020
func (fo *Fanout[F]) Run(src Source[F], stop <-chan struct{}) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			f, fresh := src.Consume()
			if f == nil || !fresh {
				continue
			}
			fo.mu.Lock()
			fo.copyFn(&fo.latest, f)
			fo.has = true
			close(fo.wake)                // wake WaitNext parkers; close never blocks,
			fo.wake = make(chan struct{}) // so a stuck waiter can't stall this tick
			fo.mu.Unlock()
		}
	}
}

// WaitNext parks until the fan-out snapshots a frame with Seq != last,
// returning the new Seq, or until timeout elapses, returning whatever Seq is
// current (possibly last, possibly 0 if nothing has been published). No lock
// is held while parked, and Run can never block on a parker (it only closes
// the wake channel). The timer is created once, not per wake iteration.
// TRLC-LINKS: REQ-DSO-020
func (fo *Fanout[F]) WaitNext(last uint64, timeout time.Duration) uint64 {
	// A newer frame already there needs no timer.
	fo.mu.RLock()
	if fo.has {
		if seq := fo.seqFn(&fo.latest); seq != 0 && seq != last {
			fo.mu.RUnlock()
			return seq
		}
	}
	fo.mu.RUnlock()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		fo.mu.RLock()
		var seq uint64
		if fo.has {
			seq = fo.seqFn(&fo.latest)
		}
		wake := fo.wake
		fo.mu.RUnlock()
		if seq != 0 && seq != last {
			return seq
		}
		select {
		case <-wake:
		case <-deadline.C:
			return seq
		}
	}
}

// WithFrame runs fn on the latest snapshot under the read lock; fn must not
// retain the pointer, and must be quick: while it runs the next snapshot
// waits, and every reader after it (the display, the web page's status)
// waits on that. Long work copies the frame and runs outside. fn receives
// nil when nothing has been published yet.
// TRLC-LINKS: REQ-DSO-020
func (fo *Fanout[F]) WithFrame(fn func(*F)) {
	fo.mu.RLock()
	defer fo.mu.RUnlock()
	if !fo.has {
		fn(nil)
		return
	}
	fn(&fo.latest)
}
