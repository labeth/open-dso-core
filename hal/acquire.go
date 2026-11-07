package hal

import (
	"context"
	"time"
)

// Request is what the user asked for, in instrument terms.
type Request struct {
	Channels    []int
	TimePerDivS float64
	DelayS      float64 // horizontal position: the screen centre's time after the trigger
	Depth       int     // samples per channel; 0: auto
	Mode        AcqMode
	PretrigFrac float64 // fraction of the record before the trigger
	// PretrigRoom is acquired samples the record holds before the trigger
	// beyond PretrigFrac's share (bounded by the record): room for a
	// trigger point placed back in software, such as a protocol match's
	// start the hardware fires at the end of.
	PretrigRoom int
	Segments    int // 0 or 1: one record
	Need        []Feature
}

// Feature names a core consumer and the content it accepts, so the engine
// refuses it with the reason instead of feeding it the wrong kind.
type Feature struct {
	Name     string     // "eye", "superres", "measure", ...
	Accepts  ContentSet // content kinds it is correct on
	MinDepth int        // samples per channel below which it refuses
}

// AcqMode is the acquisition mode.
type AcqMode int

// The acquisition modes.
const (
	Normal AcqMode = iota
	PeakDetect
	Average
	HighRes
	RollMode
	Stream // gap-free; needs a Streamer
)

// Plan is what the board will do for a Request, in portable terms.
type Plan struct {
	Request    Request // what the plan was made for: Configure stages its channels and delay
	Mode       AcqMode
	Content    ContentSet // what Readout can deliver from records of this plan
	SampleS    float64    // acquisition sample interval
	Depth      int        // acquired samples per channel
	AcqDecim   int        // hardware decimation before storage; 1 = full rate
	Pick       bool       // decimate by picking one sample per bucket, not by filtering
	ETSFactor  int        // equivalent-time interleave; 1 = real time
	Segments   int
	WinSamples int // acquired samples spanning the screen's divisions
	Precision  PrecisionSpec
	Note       string // why the plan differs from the request, for the user
}

// PrecisionSpec is a finer conditioned word stream the board computes beside
// its codes (the SDS1000CML+'s Q8.8 precision filter on decimated records).
// The zero value is none.
type PrecisionSpec struct {
	RateHz     float64 // output rate of the precision words
	Guard      int     // boundary samples the filter cannot condition
	PassbandHz float64
}

// Planner turns requests into plans. A feature in Request.Need the plan
// cannot serve is a *Refusal.
type Planner interface {
	Plan(r Request) (Plan, error) // ANY: pure, no hardware access
}

// Acquirer runs acquisitions into the board's record memory.
type Acquirer interface {
	// Configure stages plan p; the trigger is staged only by Trigger.Program.
	// What is staged stays staged across arms: a plain re-arm commits it
	// again without another Configure, and a Configure of the plan already
	// staged changes nothing (the next Arm is no commit arm for it), so an
	// owner may configure at every pass or only on a change. COMMIT.
	Configure(p Plan) error
	// Arm commits the staged FrontEnd, Plan and Trigger to the hardware, in
	// that order, and starts an acquisition. The board tells the two kinds
	// of arm apart from its own state: a commit arm (something staged differs
	// from what is applied, or the first arm after Stop or bring-up) runs
	// whatever a settings change needs (a start delay, a pending converter
	// calibration); the plain per-frame re-arm does not. OWNER.
	Arm() error
	// Wait blocks until a record is ready (nil), the timeout passes
	// (ErrTimeout) or ctx ends. A ready record is already halted and valid:
	// a board whose hardware needs a stop request before readout makes it
	// inside Wait, so Readout may run as soon as Wait returns. A zero
	// timeout polls once, so an owner can serve its commit points and
	// AUTO's forced trigger between polls; on ErrTimeout the Status still
	// says whether the hardware has triggered (a triggered record only
	// fills its post-trigger part, which AUTO must not force). OWNER.
	Wait(ctx context.Context, timeout time.Duration) (Status, error)
	Force() error // OWNER: the AUTO sweep's forced trigger
	// Stop is the user's STOP: no further acquisition until the next Arm,
	// which the board treats as a commit arm. A stored record stays
	// readable. OWNER.
	Stop() error
	State() AcqState // ANY
}

// AcqState is where the acquisition is.
type AcqState int

// The acquisition states.
const (
	Idle    AcqState = iota // nothing armed; a stored record may be read
	Armed                   // waiting for a trigger
	Ready                   // a record is halted and may be read
	Reading                 // Readout is running
)
