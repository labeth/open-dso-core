package panel

import (
	"context"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/superres"
)

type stackBoard struct {
	*halfake.Board
	runs int
}

func (b *stackBoard) FPGAStack(context.Context, superres.StackRequest) (superres.StackResult, error) {
	b.runs++
	return superres.StackResult{Records: 7}, nil
}

type holdCycle struct {
	fixedCycle
	held []bool
}

func (c *holdCycle) HoldImage(on bool) { c.held = append(c.held, on) }

// A HAL scope stacks on a board that stacks as LONG work on the owner, and
// says it cannot on one that does not; the qualifier image hold reaches the
// cycle.
// TRLC-LINKS: REQ-DSO-141, REQ-DSO-223
func TestHALScopeStacksOnTheBoard(t *testing.T) {
	m := instrument.New(instrument.State{Channels: make([]hal.Channel, 2)}, nil)
	sb := &stackBoard{Board: halfake.New(halfake.Config{})}
	cyc := &holdCycle{}
	e := NewHALScope(m, sb, "test").WithCycle(cyc).Engine()
	st, ok := e.(interface{ FPGAStacks() bool })
	fs, okS := e.(fpgaStacker)
	h, okH := e.(interface{ HoldQualifyImage(bool) })
	if !ok || !okS || !okH || !st.FPGAStacks() {
		t.Fatal("the board stacks")
	}
	done := make(chan superres.StackResult, 1)
	go func() {
		r, _ := fs.FPGAStack(context.Background(), superres.StackRequest{Records: 7})
		done <- r
	}()
	for {
		if w := m.TakeWork(); len(w) > 0 {
			w[0](context.Background())
			break
		}
	}
	if r := <-done; r.Records != 7 || sb.runs != 1 {
		t.Fatalf("%+v runs %d", r, sb.runs)
	}
	h.HoldQualifyImage(true)
	if len(cyc.held) != 1 || !cyc.held[0] {
		t.Fatal(cyc.held)
	}
	plain := NewHALScope(m, halfake.New(halfake.Config{}), "test").Engine()
	ps, ok := plain.(interface{ FPGAStacks() bool })
	pf, okF := plain.(fpgaStacker)
	if !ok || !okF || ps.FPGAStacks() {
		t.Fatal("a board without a stacker stacks")
	}
	if _, err := pf.FPGAStack(context.Background(), superres.StackRequest{}); err == nil {
		t.Fatal("a stack on a board without one")
	}
}

type rollStreamBoard struct {
	*halfake.Board
	on bool
}

func (b *rollStreamBoard) SetRollStream(on bool) { b.on = on }
func (b *rollStreamBoard) RollStream() bool      { return b.on }

// The roll-stream preference is the board's: a HAL scope forwards it and
// reports it in its snapshot; over a board that rolls one way only the
// panel says so.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-212
func TestHALScopeRollStreamIsTheBoards(t *testing.T) {
	m := instrument.New(instrument.State{Channels: make([]hal.Channel, 2)}, nil)
	rb := &rollStreamBoard{Board: halfake.New(halfake.Config{})}
	c := &Controller{eng: NewHALScope(m, rb, "test").Engine()}
	if err := c.setRollStream(true); err != nil || !rb.on || !c.eng.Snapshot().RollStream {
		t.Fatalf("roll stream on: %v, board %v, snapshot %v", err, rb.on, c.eng.Snapshot().RollStream)
	}
	if err := c.setRollStream(false); err != nil || rb.on {
		t.Fatalf("roll stream off: %v, board %v", err, rb.on)
	}
	plain := &Controller{eng: NewHALScope(m, halfake.New(halfake.Config{}), "test").Engine()}
	if err := plain.setRollStream(true); err == nil {
		t.Fatal("a board without a roll stream took the preference")
	}
}
