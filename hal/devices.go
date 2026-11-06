package hal

import (
	"context"
	"image"
	"image/color"
	"time"
)

// Images is the optional FPGA image capability: feature sets, not file names.
type Images interface {
	Provides() []ImageSet // ANY: only images whose Needs are fitted
	Current() ImageSet    // ANY
	// Load loads the cheapest image providing every feature, or returns a
	// *Refusal when it would drop the running trigger or a needed part is
	// absent. LONG.
	Load(ctx context.Context, features []string) error
}

// ImageSet is one loadable image as the core sees it.
type ImageSet struct {
	Features   []string      // "general", "stack", "bits-stream", ...
	Triggers   TrigCaps      // the nodes this image implements
	Needs      []string      // parts it needs fitted ("sram"); offered only then
	ReloadCost time.Duration // what a switch to it stalls acquisition for
}

// FrontEnd sets the analog path. A frame's Scale is not read from here: it
// travels with the frame.
type FrontEnd interface {
	// Set stages c, snapped to what the hardware can do, or returns a
	// *Refusal ("2 mV/div at 10x: below the 20 mV tip floor"). COMMIT.
	Set(ch int, c Channel) (Channel, error)
	Get(ch int) Channel                // ANY: what is applied
	Ladder(ch int) []Step              // ANY: for the current identity, probe and rate route
	OffsetGrid(ch int) Grid            // ANY: one offset step, in volts at the tip
	Couplings(ch int) []CouplingOption // ANY
	Calibration() CalSource            // ANY
}

// Channel is one channel's vertical setting, at the probe tip.
type Channel struct {
	On, Invert bool
	VoltsDiv   float64
	OffsetV    float64
	SkewS      float64 // deskew: this channel's time shift against the others
	Coupling   Coupling
	BWLimit    float64 // Hz; 0: full bandwidth
	Probe      float64 // attenuation: 1, 10, ...
	Impedance  float64 // ohms: 1e6 or 50
}

// Coupling is the input coupling.
type Coupling int

// The couplings.
const (
	DC Coupling = iota
	AC
	GND
)

// CouplingOption is one coupling a channel offers.
type CouplingOption struct {
	Coupling Coupling
	Software bool // done in software on the samples, not by a relay
}

// Step is one V/div step of a channel's ladder.
type Step struct {
	VoltsDiv float64
	Zoom     float64 // >1: a display zoom of a coarser analog range
}

// CalSource names the calibration in use; the FrontEnd applies it.
type CalSource struct {
	Source string // "vendor", "open-selfcal", "none"
	Valid  bool
}

// Display presents what the core rendered and configures a hardware
// waveform renderer where the scope has one.
type Display interface {
	Spec() DisplaySpec             // ANY
	Framebuffer() Framebuffer      // ANY: nil without a CPU framebuffer
	Renderer() (Renderer, bool)    // ANY: the hardware waveform renderer
	Capture(dst *image.RGBA) error // RENDER: the composed screen
	Backlight(level float64) error // OWNER: 0 off, 1 full
}

// Framebuffer is a CPU-drawn surface.
type Framebuffer interface {
	Back() []byte   // RENDER: the back buffer, in DisplaySpec.Format
	Stride() int    // ANY: bytes per row
	Present() error // RENDER: copy or flip the back buffer to the panel
}

// Renderer is a hardware waveform renderer. It is driven from the owner, not
// the render goroutine: it shares the board's bus with acquisition, its
// timebase is written in the acquisition commit, and it must be idle inside
// the frame cycle.
type Renderer interface {
	Configure(v View) error           // OWNER: what to draw
	SetAppearance(a Appearance) error // OWNER: how to draw it
	Area() image.Rectangle            // ANY: where it draws; the UI keeps it clear or transparent
	Colors(ch []color.RGBA) error     // OWNER: trace colours; a *Refusal when fixed
}

// View is what a hardware renderer draws: the window the core would draw.
type View struct {
	Channels  []int
	Selected  []int     // channels in the user's selection order, newest first: drawing priority
	OffsetDiv []float64 // per channel, in divisions
	StartS    float64   // window start relative to the trigger
	SpanS     float64   // window width
	Zoom      *View     // nil without a zoom window
	XY        bool
	Held      bool // stopped: re-draw the stored record
}

// Appearance is what a hardware renderer can be told.
type Appearance struct {
	Intensity    float64
	ColorGrade   bool
	PersistenceS float64 // <0: infinite
	Vectors      bool
}

// Input delivers key, knob and touch events and drives the panel's lamps.
type Input interface {
	Events() <-chan InputEvent  // ANY; the board documents which goroutine feeds it
	SetLamps(on []string) error // COMMIT: the lit lamps by core name; the board maps them
	Keys() []string             // ANY: the keys this panel has
	Knobs() []string            // ANY
}

// InputEvent is one key, knob step or touch, in core names.
type InputEvent struct {
	Source         InputSource
	Kind           InputKind
	Key            string // "run", "ch1", "f1", ...
	Knob           string
	Steps          int // signed knob steps
	TouchX, TouchY int
	Gesture        string // "tap", "drag", "pinch"
	Down           bool
}

// InputSource is where an event came from.
type InputSource int

// The sources.
const (
	FromPanel InputSource = iota
	FromTouch
	FromRemote
)

// InputKind is the kind of event.
type InputKind int

// The kinds. Overload is a synthetic event a board raises when an input's
// protection trips.
const (
	KeyEvent InputKind = iota
	KnobEvent
	TouchEvent
	Overload
)

// Storage is where settings, captures and calibration live on this scope.
type Storage interface {
	Settings() KV                      // ANY
	Setups() (KV, bool)                // ANY: numbered setup slots
	Captures() (dir string, err error) // ANY: "" when the scope has nowhere to put them
	Calibration() CalSource            // ANY
}

// KV is a small store with a size budget. Every method is ANY.
type KV interface {
	Get(key string) ([]byte, bool)
	Put(key string, val []byte) error // a *Refusal past Budget
	Budget() int                      // bytes; 0: unbounded
}
