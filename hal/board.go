package hal

import (
	"context"
	"encoding/json"
	"net/http"
)

// Board is everything a scope provides. A scope's main builds one and hands
// it to the core, which owns argv, the worker re-exec, the web server, SCPI,
// the renderer and the supervisor's health token.
type Board interface {
	Profile() Profile // ANY: loaded from profile.yaml, overlaid by identity
	Platform() Platform
	Planner() Planner
	Acquirer() Acquirer
	Readout() Readout
	Trigger() Trigger
	FrontEnd() FrontEnd
	Display() Display
	Input() Input
	Storage() Storage
	Census() Census

	// Optional capabilities, by typed accessor. ok=false: absent, and every
	// feature that needs it is refused with the profile's reason. ANY.
	Streamer() (Streamer, bool)
	Images() (Images, bool)
	Stacker() (Stacker, bool)
	Segments() (Segments, bool)
	Counter() (Counter, bool)
	LogicProbe() (LogicProbe, bool)
	Generator() (Generator, bool) // a signal source the scope drives
	Extensions() []Extension      // ANY: scope-only web routes and SCPI headers
}

// Platform is bring-up, the hand-over from the supervisor and the board's
// housekeeping.
type Platform interface {
	// BringUp loads the base image, checks the unit's identity and returns
	// it; an identity no profile variant matches is a *Refusal before any
	// image load. OWNER, once.
	BringUp(ctx context.Context) (Identity, error)
	// Handover is what the supervisor gave this process. ANY.
	Handover() Handover
	// Service runs board housekeeping at each commit point and on every
	// Readout yield. OWNER.
	Service()
	// Worker is the board's acquisition-worker entry when the app re-execs
	// itself; nil when the board has none. ANY.
	Worker() func() int
	// Reboot is the supervisor's last resort. OWNER.
	Reboot(reason string) error
	// Diag is the board's counters for the status "diag" object. ANY.
	Diag() json.RawMessage
}

// Handover is what the supervisor gave the app process.
type Handover struct {
	FDs       map[string]int // inherited descriptors by role: "gpmc", "fpga_key"
	HealthTok string         // where the core's health writer writes
	StateDir  string         // settings and setups
}

// Extension is a scope-only feature: web routes and SCPI headers, named as
// one feature so the capabilities list it.
type Extension struct {
	Feature string
	Routes  []Route
	SCPI    []SCPIHeader
}

// Route is one scope-only web route.
type Route struct {
	Pattern string // a net/http ServeMux pattern: "POST /api/fpga/stack"
	Handler http.Handler
}

// SCPIHeader is one scope-only SCPI command or query.
type SCPIHeader struct {
	Header string // the long form, as the dialect names it
	// Handle runs the command with its argument text; a query returns its
	// reply without the terminator.
	Handle func(args string) (reply []byte, err error)
}
