// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"

	"github.com/labeth/open-dso-core/hal"
)

// A board's own stream sessions (hal.Session, ADR-HAL-BOARD-SESSIONS): the
// SDS1000CML+'s decoded streams, which its hardware decoders and its
// software decode of its stream images make together, run on the cycle's
// owner as they ran on the scope's own. The cycle attaches itself as the
// sessions' owner when it opens, serves their requests at every commit
// point, gives them the pass while one holds the acquisition (ahead of the
// audio stream and the roll view, which end first) and commits the settings
// made meanwhile as they come, leaves a waiting capture to a session that
// took the acquisition, and shows the record a session ended on whole as
// the stopped record.

// sessionHost is the cycle as a board session's owner.
type sessionHost struct{ c *HALCycle }

var _ hal.SessionHost = sessionHost{}

// SetRunning starts or stops the run through the instrument, as a
// controller's RUN or STOP.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func (h sessionHost) SetRunning(on bool) { h.c.inst.SetRunning(on) }

// YieldStreams ends the audio stream before a session takes the stream
// path; it starts again once the session ends.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018, REQ-DSO-265
func (h sessionHost) YieldStreams() {
	if h.c.snd.running {
		h.c.soundStop()
	}
}

// openSessions attaches the cycle to the board's sessions.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func (c *HALCycle) openSessions() {
	if s, ok := hal.SessionOf(c.b); ok {
		c.sess = s
		s.Attach(sessionHost{c})
	}
}

// serveSessions serves the sessions' requests at a commit point.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func (c *HALCycle) serveSessions() {
	if c.sess != nil {
		c.sess.Serve()
	}
}

// sessionActive reports a session holding the acquisition.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func (c *HALCycle) sessionActive() bool { return c.sess != nil && c.sess.Active() }

// sessionStep gives the pass to a session that holds the acquisition (the
// cycle's own streams end first); a record a session ended on is read
// whole on the next stopped pass. false: the pass goes on.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func (c *HALCycle) sessionStep(ctx context.Context) bool {
	if c.sess == nil {
		return false
	}
	if c.sess.TakeRecord() {
		c.stopped = stoppedRecord{content: c.stopped.content, due: true}
		c.sessShown = true
	}
	if !c.sess.Active() {
		if c.sessOn {
			c.sessOn, c.gen = false, 0 // the whole state is committed again for the captures
		}
		return false
	}
	c.sessOn = true
	if c.roll.running {
		c.rollStop()
	}
	if c.snd.running {
		c.soundStop()
	}
	// A setting made while a session runs is committed as it comes, as the
	// SDS1000CML+'s owner applies its settings while its streams run: staged
	// on the board, the front end's relays written at the commit point with
	// nothing armed (ADR-HAL-FRONTEND-IDLE-COMMIT), no capture armed.
	if s, gen := c.inst.Wanted(); gen != c.gen {
		c.commitState(s, gen)
	}
	return c.sess.Step(ctx)
}

// sessionHoldsImage reports the board's image stays for a session: one
// holds it (starting, or started again soon), or the stopped record shown
// is the one a session ended on (a load would drop it, and the stream
// started again needs that image back).
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018, REQ-DSO-228
func (c *HALCycle) sessionHoldsImage() bool {
	if c.sess == nil {
		return false
	}
	return c.sess.Active() || c.sess.HoldsImage() || (c.sessShown && !c.running)
}
