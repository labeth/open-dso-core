// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"context"
	"errors"

	"github.com/labeth/open-dso-core/superres"
)

// BoardStacker is a board that stacks records in its FPGA against a
// template (the SDS1000CML+'s stacking image): a whole session, LONG work
// the owner runs with the acquisition stopped. OWNER, LONG.
type BoardStacker interface {
	FPGAStack(ctx context.Context, req superres.StackRequest) (superres.StackResult, error)
}

// RollStreamer is a board that rolls either way (the SDS1000CML+: short
// captures on any image, or gap-free on its stream image's peak words):
// the roll-stream preference is the board's setting, which its planner
// answers a roll request with. ANY.
type RollStreamer interface {
	SetRollStream(on bool)
	RollStream() bool
}

// imageHolder is the owner loop's cycle that holds the FPGA image
// (engine.HALCycle.HoldImage).
type imageHolder interface{ HoldImage(on bool) }

// errNoFPGAStack is a stacking session asked of a board that has none.
var errNoFPGAStack = errors.New("FPGA stacking unavailable")

// HoldQualifyImage asks the owner to stay on the image the trigger runs
// on while it serves the trigger (AUTO in BUS between the qualifier's idle
// triggers and edge-triggered looks): the board's image hold.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-223
func (e halEngine) HoldQualifyImage(on bool) {
	if h, ok := e.cycle.(imageHolder); ok {
		h.HoldImage(on)
	}
}

// SetRollStream sets the board's roll-stream preference (a board that
// rolls one way only keeps it).
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-212
func (e halEngine) SetRollStream(on bool) {
	if rs, ok := e.b.(RollStreamer); ok {
		rs.SetRollStream(on)
	}
}

// RollStreams reports a board with a roll-stream preference.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-212
func (e halEngine) RollStreams() bool {
	_, ok := e.b.(RollStreamer)
	return ok
}

// FPGAStacks reports a board that stacks records in its FPGA.
// TRLC-LINKS: REQ-DSO-141
func (e halEngine) FPGAStacks() bool {
	_, ok := e.b.(BoardStacker)
	return ok
}

// FPGAStack runs a stacking session on the board as LONG work on the
// owner (the acquisition stopped, every setting committed again after);
// a caller that gives up before the owner takes it runs none.
// TRLC-LINKS: REQ-DSO-141
func (e halEngine) FPGAStack(ctx context.Context, req superres.StackRequest) (superres.StackResult, error) {
	st, ok := e.b.(BoardStacker)
	if !ok {
		return superres.StackResult{}, errNoFPGAStack
	}
	type result struct {
		res superres.StackResult
		err error
	}
	done := make(chan result, 1)
	e.Post(func(context.Context) {
		if err := ctx.Err(); err != nil {
			done <- result{err: err}
			return
		}
		res, err := st.FPGAStack(ctx, req)
		done <- result{res, err}
	})
	select {
	case r := <-done:
		return r.res, r.err
	case <-ctx.Done():
		return superres.StackResult{}, ctx.Err()
	}
}
