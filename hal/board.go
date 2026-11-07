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
//
// Its handlers run on the web server's and the SCPI transports'
// goroutines, so they never touch the hardware: a handler records a
// request and the board carries it out in Platform.Service (OWNER, short
// work only). Long work (a calibration, an image load, a census) is a HAL
// method the core schedules, never an extension that blocks Service.
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

// SCPIHeader is one scope-only SCPI header with its command form, its
// query form or both. ANY, like every Extension handler.
type SCPIHeader struct {
	Header string // the long form without "?", as the dialect names it
	// Command runs the header as a command with its argument text; nil
	// when the header has no command form.
	Command func(args string) error
	// Query runs the header as a query ("HEADER?") with its argument text
	// and returns the reply without the terminator; nil when the header
	// has no query form.
	Query func(args string) (reply []byte, err error)
}
