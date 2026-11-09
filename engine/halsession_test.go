package engine

import (
	"context"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
)

// fakeSession is a board session the test starts and ends by hand.
type fakeSession struct {
	host          hal.SessionHost
	active        bool
	record        bool
	serves, steps int
	closed        bool
}

func (s *fakeSession) Attach(h hal.SessionHost)  { s.host = h }
func (s *fakeSession) Serve()                    { s.serves++ }
func (s *fakeSession) Step(context.Context) bool { s.steps++; return s.active }
func (s *fakeSession) Active() bool              { return s.active }
func (s *fakeSession) Close()                    { s.closed = true }
func (s *fakeSession) TakeRecord() (taken bool)  { taken, s.record = s.record, false; return }

// sessionBoard is the fake board with a session of its own.
type sessionBoard struct {
	*halfake.Board
	s *fakeSession
}

func (b sessionBoard) Session() (hal.Session, bool) { return b.s, true }

// A board's session runs on the HAL cycle's owner: attached at the open,
// served at each commit point, given the pass while it holds the
// acquisition (a waiting capture leaves it), with the settings made
// meanwhile committed as they come and the whole state again once it ends;
// its stop goes through the instrument; the record it ended on is read
// whole; the cycle's close ends it.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func TestHALCycleRunsTheBoardsSession(t *testing.T) {
	fs := &fakeSession{}
	b := sessionBoard{halfake.New(halfake.Config{}), fs}
	m := instrument.New(loopState(hal.Auto), nil)
	_, c := NewHALLoop(HALConfig{Board: b, Instrument: m})
	c.Open()
	if fs.host == nil {
		t.Fatal("the cycle did not attach itself to the session")
	}
	ctx := context.Background()
	c.Serve()
	if fs.serves != 1 {
		t.Fatalf("serves %d after one commit point", fs.serves)
	}
	if c.Claim(ctx) || c.Watch(false) == Leave {
		t.Fatal("an idle session took the pass")
	}
	if _, ok := c.Plan(ctx); !ok {
		t.Fatal("no capture planned")
	}
	fs.active = true
	if c.Watch(false) != Leave {
		t.Fatal("a waiting capture did not leave the acquisition to the session")
	}
	fs.host.SetRunning(false) // a session is not RUN
	if !c.Claim(ctx) || fs.steps != 1 {
		t.Fatal("the active session did not take the pass")
	}
	if m.State().Running || c.Running() {
		t.Fatal("the session's stop was not committed while it runs")
	}
	if err := m.Change(func(s *instrument.State) error { s.TimeDivS *= 2; return nil }); err != nil {
		t.Fatal(err)
	}
	c.Claim(ctx)
	if c.cur.TimeDivS != m.State().TimeDivS {
		t.Fatalf("a setting made while the session runs was not committed (tdiv %g)", c.cur.TimeDivS)
	}
	fs.active, fs.record = false, true
	if c.Claim(ctx) {
		t.Fatal("the ended session still takes the pass")
	}
	if c.gen != 0 || !c.stopped.due {
		t.Fatalf("after the session: gen %d (want a fresh commit), stopped record due %v", c.gen, c.stopped.due)
	}
	c.Close()
	if !fs.closed {
		t.Fatal("the cycle's close did not end the session")
	}
}
