// Package agenttest is a board for tests of the supervisor's transports and
// host tools: a scope with no vendor app, no boot descriptors and no
// watchdog, whose app slots are in a directory of the test's choosing.
// ENGMODEL-OWNER-UNIT: FU-OTA-AGENT
package agenttest

import (
	"context"
	"errors"
	"os"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/supervisor/agent"
	"github.com/labeth/open-dso-core/supervisor/fdinherit"
)

// Board is an agent.Board with nothing to take over.
// TRLC-LINKS: REQ-SDS-122
type Board struct {
	Slots string // the app slots' root
}

var _ agent.Board = (*Board)(nil)

// Watchdog reports none.
// TRLC-LINKS: REQ-SDS-086
func (*Board) Watchdog() (hal.Watchdog, bool) { return nil, false }

// Takeover has no vendor app to stop.
// TRLC-LINKS: REQ-SDS-027
func (*Board) Takeover(ctx context.Context) error {
	agent.ResultOf(ctx).Step("no vendor app to stop")
	return nil
}

// LaunchEnv hands the app nothing.
// TRLC-LINKS: REQ-SDS-028
func (*Board) LaunchEnv() ([]string, []*os.File) { return nil, nil }

// Alive reports whether pid runs.
// TRLC-LINKS: REQ-SDS-028
func (*Board) Alive(pid int) bool { return fdinherit.Alive(pid) }

// SlotsDir is Slots.
// TRLC-LINKS: REQ-SDS-028
func (b *Board) SlotsDir() string { return b.Slots }

// Reboot refuses: a test host is not rebooted.
// TRLC-LINKS: REQ-SDS-121
func (*Board) Reboot(string) error { return errors.New("agenttest: no reboot") }

// Heartbeat has nobody to tell.
// TRLC-LINKS: REQ-SDS-122
func (*Board) Heartbeat() error { return nil }

// BootFDs is none.
// TRLC-LINKS: REQ-SDS-114
func (*Board) BootFDs() map[string]int { return map[string]int{} }

// VendorHolders is none.
// TRLC-LINKS: REQ-SDS-027
func (*Board) VendorHolders() []fdinherit.Holder { return nil }

// Land passes, or stops a dry run.
// TRLC-LINKS: REQ-SDS-027
func (*Board) Land(res *agent.TakeoverResult, opts agent.TakeoverOpts) bool {
	if opts.DryRun {
		res.OK = true
		res.Step("dry-run: stopping here (no STOP sent, nothing killed)")
		return false
	}
	return true
}

// Reclaim has nothing to reclaim.
// TRLC-LINKS: REQ-SDS-027
func (*Board) Reclaim(*agent.TakeoverResult) {}

// Probe reports the arguments it was given.
// TRLC-LINKS: REQ-SDS-114
func (*Board) Probe(deep, takenOver bool) any {
	return map[string]bool{"deep": deep, "taken_over": takenOver}
}
