package engine

import (
	"context"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
)

// beatBoard is the fake board whose LONG work beats the owner's heartbeat
// (hal.BeatBoard), with a session, its platform's service logged beside
// the session's.
type beatBoard struct {
	*halfake.Board
	s    *logSession
	beat func()
	log  *[]string
}

func (b *beatBoard) SetBeat(fn func())            { b.beat = fn }
func (b *beatBoard) Session() (hal.Session, bool) { return b.s, true }
func (b *beatBoard) Platform() hal.Platform       { return logPlatform{b.Board.Platform(), b.log} }
func (b *beatBoard) longWork(steps int, hang bool) {
	for range steps {
		if hang {
			continue // a step that never ends beats no more
		}
		b.beat()
	}
}

// logPlatform logs its service.
type logPlatform struct {
	hal.Platform
	log *[]string
}

func (p logPlatform) Service() { *p.log = append(*p.log, "platform"); p.Platform.Service() }

// logSession is a session that logs its service and beats its host in a
// long step.
type logSession struct {
	fakeSession
	log *[]string
}

func (s *logSession) Serve() { *s.log = append(*s.log, "session") }

// The HAL cycle's heartbeat moves with what progresses: every commit point
// (a pass, a wait's poll), the loop's beats, a board's LONG work between
// its steps (handed over at the open) and a session's long step through its
// host; LONG work stuck in a step beats no more, so a wedge stops it. The
// commit point serves the board's sessions before the board's own
// housekeeping.
// TRLC-LINKS: REQ-DSO-294, REQ-DSO-001, REQ-DSO-018
func TestHALCycleHeartbeat(t *testing.T) {
	var log []string
	b := &beatBoard{Board: halfake.New(halfake.Config{}), log: &log}
	b.s = &logSession{log: &log}
	m := instrument.New(loopState(hal.NormalSweep), nil)
	_, c := NewHALLoop(HALConfig{Board: b, Instrument: m})
	c.Open()
	if b.beat == nil {
		t.Fatal("the cycle handed the board no heartbeat")
	}
	n := c.Beats()
	c.Serve()
	if c.Beats() != n+1 || len(log) != 2 || log[0] != "session" || log[1] != "platform" {
		t.Fatalf("a commit point: beats %d → %d, served %v", n, c.Beats(), log)
	}
	c.Beat()
	if c.Beats() != n+2 {
		t.Fatal("the loop's beat did not count")
	}
	b.longWork(5, false)
	if c.Beats() != n+7 {
		t.Fatalf("LONG work's five steps beat %d times", c.Beats()-n-2)
	}
	b.longWork(5, true)
	if c.Beats() != n+7 {
		t.Fatal("LONG work stuck in a step beat")
	}
	h, ok := b.s.host.(interface{ Beat() })
	if !ok {
		t.Fatal("a session's host has no heartbeat")
	}
	h.Beat()
	if c.Beats() != n+8 {
		t.Fatal("a session's beat did not count")
	}
	// the published frames are not the heartbeat: a NORMAL wait publishes none
	if c.Plan(context.Background()); m.Snapshot().Published != 0 {
		t.Fatal("a frame was published")
	}
}
