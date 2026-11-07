package hal

import (
	"context"
	"os"
	"time"
)

// SupervisorBoard is what the core supervisor (A/B slots, health, rollback,
// RPC) needs from a board. The supervisor runs in the scope's separate
// supervisor module, so the app never links it.
//
// A board may also implement HealthChecker (a scope without a watchdog)
// and CensusBoard (its diagnostics); the supervisor finds them by type
// assertion.
type SupervisorBoard interface {
	Watchdog() (Watchdog, bool)
	Takeover(ctx context.Context) error          // stop the vendor app, keep the descriptors it held
	LaunchEnv() (env []string, files []*os.File) // what the app inherits
	Alive(pid int) bool
	SlotsDir() string // the A/B slots
	// Reboot is the last resort. It may return a *Refusal under the
	// board's reboot policy (a scope without a watchdog bounds reboots
	// without a healthy app in between, then leaves the unit to the vendor
	// firmware); the supervisor then stops restarting and reports it.
	Reboot(reason string) error
	Heartbeat() error // a service that expects one; nil when none does
}

// HealthChecker is the software health contract of a scope without a
// usable watchdog, where a hung app is found only by its health token.
type HealthChecker interface {
	// Healthy reports whether the app pid runs and, once it has written
	// its health token, keeps it fresh: no older than the board's maximum
	// age at now. Before the first write it is liveness only.
	Healthy(pid int, now time.Time) bool
	// Confirmed records that the app proved healthy for the supervisor's
	// stable time: the reboot policy's count starts again.
	Confirmed() error
}

// CensusBoard is a supervisor board that takes the unit's census. The
// census lives on the supervisor side so a scope's app module need not
// require its supervisor module; the app reaches it through the
// supervisor.
type CensusBoard interface {
	Census() Census
}

// Watchdog is a hardware watchdog.
type Watchdog interface {
	Open() error
	Pet() error
	Close() error
}

// Census is the board's diagnostics: which parts are fitted and working.
type Census interface {
	// Run is LONG; writes=false is the read-only first contact.
	Run(ctx context.Context, writes bool) ([]Verdict, error)
}

// Verdict is one part's judgement.
type Verdict struct {
	Part    string
	Verdict string // "present", "absent", "partial", "other"
	Detail  string
}

// Stacker is the optional hardware super-resolution stacker.
type Stacker interface {
	Stack(n int) error // OWNER: accumulate n records
}

// Segments is optional segmented memory in hardware.
type Segments interface {
	Max(depth int) int                     // ANY: segments that fit at this depth
	Timestamps(dst []float64) (int, error) // OWNER: seconds from the first segment
}

// Counter is an optional free-running hardware counter or totalizer.
type Counter interface {
	Read() (Counters, error) // OWNER
}

// LogicProbe is an optional digital probe on the scope's own input.
type LogicProbe interface {
	Lines() int                            // ANY: digital lines, also trigger Sources
	ReadDigital(dst []uint16) (int, error) // OWNER
}

// Generator is an optional signal source the scope drives.
type Generator interface {
	Waveforms() []string // ANY: "sine", "square", ...
	// Apply sets the output, or returns a *Refusal. OWNER.
	Apply(g GeneratorSetting) error
}

// GeneratorSetting is a generator's output.
type GeneratorSetting struct {
	On         bool
	Waveform   string
	FreqHz     float64
	AmplitudeV float64 // peak to peak
	OffsetV    float64
}
