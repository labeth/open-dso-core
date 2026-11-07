// ENGMODEL-OWNER-UNIT: FU-OTA-AGENT

package agent

import (
	"context"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/supervisor/fdinherit"
)

// Board is what the supervisor needs from the scope it runs on. Beside
// hal.SupervisorBoard it names what a takeover and the status report need:
// the boot descriptors, the vendor processes that own the instrument, and
// the takeover's steps before its point of no return. The persisted
// taken_over flag, the watchdog keeper and the order of the takeover's steps
// stay here, so every board gets the same crash-safe sequence.
//
// hal.SupervisorBoard.Takeover is the step past the point of no return: it
// stops the vendor app for good (the descriptors it held stay with the
// supervisor), recording what it did on the TakeoverResult that ResultOf
// finds in its context.
// TRLC-LINKS: REQ-SDS-122, REQ-SDS-027
type Board interface {
	hal.SupervisorBoard

	// BootFDs names the descriptors the supervisor inherited from the boot
	// chain and keeps for the app, -1 for one it does not hold; status and
	// probe report them as inherited_fds. Empty on a board that has none.
	BootFDs() map[string]int

	// VendorHolders lists the vendor processes that still own the
	// instrument. The supervisor never launches the app while one remains
	// (two owners of one bus wedge some scopes until a power cycle).
	VendorHolders() []fdinherit.Holder

	// Land runs the takeover's steps before its point of no return: the
	// board's own gates, the vendor processes found (res.Candidates), and an
	// idle landing of the vendor app. It returns false when the takeover must
	// stop: with res.Err set on a refusal, with res.OK set on a dry run.
	Land(res *TakeoverResult, opts TakeoverOpts) bool

	// Reclaim re-drives the vendor app's stop and kill when vendor holders
	// are found again after the takeover (a crash between the persist and
	// the kill, or an init-respawned vendor app).
	Reclaim(res *TakeoverResult)

	// Probe is the read-only device fingerprint the probe RPC returns;
	// deep asks for the board's opt-in bus reads. takenOver is the
	// supervisor's persisted state.
	Probe(deep, takenOver bool) any
}

// Attacher is a Board that logs and sends events through the agent (Logf,
// Event): New hands it the agent it serves.
type Attacher interface {
	Attach(a *Agent)
}

// resultKey carries the takeover's result to Board.Takeover.
type resultKey struct{}

// WithResult is ctx carrying res for the board's Takeover to record on.
// TRLC-LINKS: REQ-SDS-027
func WithResult(ctx context.Context, res *TakeoverResult) context.Context {
	return context.WithValue(ctx, resultKey{}, res)
}

// ResultOf is the takeover result ctx carries, or a throwaway one: a board's
// Takeover records its steps without checking who asked.
// TRLC-LINKS: REQ-SDS-027
func ResultOf(ctx context.Context) *TakeoverResult {
	if r, ok := ctx.Value(resultKey{}).(*TakeoverResult); ok && r != nil {
		return r
	}
	return &TakeoverResult{}
}
