// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// The stopped record: on a board whose readout places its whole stored
// record (hal.ScreenReader with ScreenRequest.Record), a stop shows that
// record read whole, as the SDS1000CML+ does (ADR-RUN-CONTROL-CML-RULES):
// a finished SINGLE shows its live frame and then its full record, a STOP
// lets the capture it lands in finish (forced once stopWait has passed
// without a trigger) and shows that record whole, and an average stays
// shown. While stopped, a committed setting shows the held record again
// at it, without reading the board. A board that cannot place its record
// keeps the last live frame shown.

// stopWait is how long a capture a STOP lands in may still wait for its
// trigger before it is forced: a stop on a triggering signal shows a
// triggered record.
const stopWait = 100 * time.Millisecond

// stoppedRecord is the cycle's stopped record.
type stoppedRecord struct {
	due     bool          // the last live frame's record awaits its whole read
	held    *sample.Frame // the stopped record read whole, shown again at a new setting
	replay  bool          // this pass shows the held record again
	content uint64        // the ContentIDs the cycle has given (newContent)
	stopAt  time.Time     // when a STOP landed in the waiting capture; zero: none
	cond    heldCond      // the held record as read, and the filter its shown copy has (haleres.go)
}

// recordReader is the board's readout as a ScreenReader, when it places
// the whole stored record.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1003
func (c *HALCycle) recordReader() (hal.ScreenReader, bool) {
	sr, ok := c.b.Readout().(hal.ScreenReader)
	return sr, ok
}

// keepsRecords reports that a stop shows the whole stored record in the
// committed state: the board places it and no average is shown.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (c *HALCycle) keepsRecords() bool {
	_, ok := c.recordReader()
	return ok && c.cur.Mode != hal.Average
}

// stopOnly reports that the wanted state s differs from the committed one
// only by a stopped run: the capture in flight finishes and shows whole.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (c *HALCycle) stopOnly(s instrument.State) bool {
	if s.Running || !c.cur.Running {
		return false
	}
	s.Running, s.Single = c.cur.Running, c.cur.Single
	return s.Equal(c.cur)
}

// stoppedPlan is Plan's capture while stopped: the stored record read
// whole when its read is due, the held record again after a setting
// committed while stopped (wasRunning: the commit was the stop itself),
// else none.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (c *HALCycle) stoppedPlan(committed, wasRunning bool) (Capture, bool) {
	r := &c.stopped
	r.replay = false
	if r.due {
		return Capture{Replay: true, Normal: true, Norm: true}, true
	}
	if committed && !wasRunning && r.held != nil {
		r.replay = true
		return Capture{Replay: true, Normal: true, Norm: true}, true
	}
	return Capture{}, false
}

// livePlan forgets the stopped record: a capture is armed over it.
// TRLC-LINKS: REQ-DSO-008
func (c *HALCycle) livePlan() {
	c.stopped = stoppedRecord{content: c.stopped.content}
}

// watchStop lets the waiting capture a STOP lands in finish when the
// board keeps records; true: the wait goes on.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (c *HALCycle) watchStop() bool {
	if !c.keepsRecords() || c.inst.HasWork() || c.inst.RecordNeed() != c.need || c.replace {
		return false
	}
	s, _ := c.inst.Wanted()
	if !c.stopOnly(s) {
		return false
	}
	if c.stopped.stopAt.IsZero() {
		c.stopped.stopAt = time.Now()
	}
	return true
}

// stopForceDue takes the forced trigger that ends a capture a STOP landed
// in once it has waited stopWait.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) stopForceDue() bool {
	r := &c.stopped
	if r.stopAt.IsZero() || time.Since(r.stopAt) < stopWait {
		return false
	}
	r.stopAt = time.Time{}
	return true
}

// stoppedFrame is the frame a stopped pass shows: the held record again,
// or the stored record read whole (held for the next setting). false:
// nothing to show (the read failed; the last frame stays shown).
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1003
func (c *HALCycle) stoppedFrame(ctx context.Context) (*sample.Frame, Verdict, bool) {
	r := &c.stopped
	if r.replay {
		g := *c.reconditioned(r) // a published frame never changes: the copy is numbered anew
		return &g, Verdict{FullRecall: true}, true
	}
	r.due = false
	release := c.quietRead()
	f, ok := c.readWhole(ctx)
	release()
	if !ok {
		return nil, Verdict{}, false
	}
	c.placeEdge(f, true)
	r.cond = heldCond{raw: f}
	if c.eresLen > 1 {
		raw := &sample.Frame{}
		raw.CopyFrom(f)
		r.cond = heldCond{raw: raw, length: c.eresLen}
		c.condition(f)
	}
	if f.ContentID == 0 {
		f.ContentID = r.newContent()
	}
	r.held = f
	return f, Verdict{FullRecall: true}, true
}

// readWhole reads the board's whole stored record of every enabled
// channel into one frame.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-1003
func (c *HALCycle) readWhole(ctx context.Context) (*sample.Frame, bool) {
	sr, _ := c.recordReader()
	w, err := sr.ScreenWindow(hal.ScreenRequest{Record: true})
	if err != nil {
		c.cfg.Logf("stopped record: %v", err)
		return nil, false
	}
	f, err := c.read(ctx, w)
	if err != nil {
		c.cfg.Logf("stopped record: %v", err)
		return nil, false
	}
	return f, true
}

// cycleContent marks the ContentIDs the cycle gives, apart from a board's.
const cycleContent = 1 << 63

// newContent is a ContentID the cycle gives a record it read whole or
// filtered again: never one a board gave.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-012
func (r *stoppedRecord) newContent() uint64 {
	r.content++
	return cycleContent | r.content
}
