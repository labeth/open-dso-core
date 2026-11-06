package hal

import (
	"context"
	"os"
)

// SupervisorBoard is what the core supervisor (A/B slots, health, rollback,
// RPC) needs from a board. The supervisor runs in the scope's separate
// supervisor module, so the app never links it.
type SupervisorBoard interface {
	Watchdog() (Watchdog, bool)
	Takeover(ctx context.Context) error          // stop the vendor app, keep the descriptors it held
	LaunchEnv() (env []string, files []*os.File) // what the app inherits
	Alive(pid int) bool
	SlotsDir() string // the A/B slots
	Reboot(reason string) error
	Heartbeat() error // a service that expects one; nil when none does
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
