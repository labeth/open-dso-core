// ENGMODEL-OWNER-UNIT: FU-OTA-AGENT

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/supervisor/fdinherit"
	"github.com/labeth/open-dso-core/supervisor/watchdog"
)

// TakeoverOpts are the takeover RPC's arguments.
// TRLC-LINKS: REQ-SDS-027
type TakeoverOpts struct {
	DryRun bool `json:"dry_run"`
	// Force skips the board's stop and idle-confirm gates. Only for a unit
	// whose vendor app is already dead/hung and unreachable. The engine may
	// be mid-frame: expect a possible wedge; have the power plug ready.
	Force bool `json:"force"`
}

// TakeoverResult is a takeover's outcome: each step it took, the vendor
// processes it found, and why it stopped when it did not finish.
// TRLC-LINKS: REQ-SDS-027
type TakeoverResult struct {
	OK         bool               `json:"ok"`
	Steps      []string           `json:"steps"`
	Candidates []fdinherit.Holder `json:"candidates"`
	Err        string             `json:"err,omitempty"`
	// Killed is how many vendor processes the board's Takeover killed.
	Killed int `json:"-"`
}

// Step records one step.
// TRLC-LINKS: REQ-SDS-027
func (r *TakeoverResult) Step(format string, args ...any) {
	r.Steps = append(r.Steps, fmt.Sprintf(format, args...))
}

// Summary is the steps on one line, for the log.
// TRLC-LINKS: REQ-SDS-027
func (r *TakeoverResult) Summary() string { return strings.Join(r.Steps, "; ") }

// Takeover is the inherit-then-kill sequence. The board lands the vendor app
// (its gates, the vendor processes, an idle landing: the engine must never be
// killed mid-frame), then:
//
//  1. taken_over=true is persisted BEFORE the kill: a crashed agent's
//     successor must re-acquire the watchdog immediately, or the SoC
//     warm-resets and the update path is lost until a power cycle
//  2. the board stops the vendor app for good (hal.SupervisorBoard.Takeover)
//  3. the watchdog is acquired and petted (retrying in the background)
//
// After this returns the supervisor loop launches the app slot (if any).
// TRLC-LINKS: REQ-SDS-027
func (a *Agent) Takeover(opts TakeoverOpts) *TakeoverResult {
	a.tkMu.Lock()
	defer a.tkMu.Unlock()
	res := &TakeoverResult{}

	if a.st.get().TakenOver {
		res.OK = true
		res.Step("already taken over (idempotent)")
		// Make sure the watchdog side is running (restart path does this too).
		go a.keepWatchdog()
		return res
	}

	if !a.board.Land(res, opts) {
		return res
	}

	// Point of no return: persist first (see doc comment).
	if err := a.st.update(func(s *State) { s.TakenOver = true }); err != nil {
		res.Err = "persist state: " + err.Error()
		return res
	}
	res.Step("state persisted: taken_over=true")

	if err := a.board.Takeover(WithResult(context.Background(), res)); err != nil {
		res.Step("vendor takeover: %v", err)
	}

	// Watchdog: from this moment nothing else pets it.
	if err := a.wd.Acquire(15*time.Second, a.cfg.WdPet); err == nil {
		res.Step("watchdog acquired, petting every %s", a.cfg.WdPet)
	} else if !errors.Is(err, watchdog.ErrNone) {
		res.Step("watchdog acquire failed (%v) — background retry armed", err)
		go a.keepWatchdog()
	}

	a.event("takeover.done", map[string]any{"killed": res.Killed})
	res.OK = true
	res.Step("takeover complete — supervisor will launch the app slot if present")
	return res
}

// reclaimBus re-drives the board's stop and kill when a vendor process is
// found still owning the instrument after the device is already marked
// taken_over (a crash in the persist→kill window, or an init-respawned vendor
// app). Serialized with Takeover via tkMu so the two never race.
// TRLC-LINKS: REQ-SDS-027
func (a *Agent) reclaimBus() {
	a.tkMu.Lock()
	defer a.tkMu.Unlock()
	res := &TakeoverResult{Candidates: a.board.VendorHolders()}
	if len(res.Candidates) == 0 {
		return
	}
	a.board.Reclaim(res)
	a.event("bus.reclaimed", map[string]any{"summary": res.Summary()})
}

// Probe is the read-only device fingerprint, for the `agent probe`
// subcommand and the probe RPC.
// TRLC-LINKS: REQ-SDS-114
func (a *Agent) Probe(deep bool) any { return a.board.Probe(deep, a.st.get().TakenOver) }

// Logf writes a line to the agent's log, for a board's own steps.
// TRLC-LINKS: REQ-SDS-122
func (a *Agent) Logf(format string, args ...any) { a.log.Printf(format, args...) }

// Event sends a lifecycle event (logged, and pushed over NATS when linked),
// for a board's own steps.
// TRLC-LINKS: REQ-SDS-123
func (a *Agent) Event(kind string, detail map[string]any) { a.event(kind, detail) }
