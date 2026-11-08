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
	// SelfCal runs the scope's self-calibration and stores its result
	// where the next bring-up finds it. The core runs it with acquisition
	// stopped: it reprograms the front end and timebase, the stored record
	// is lost and the next Arm is a commit arm. progress gets the done
	// fraction (0 … 1); it may be nil. A channel whose calibration fails
	// keeps its previous tables and is listed in the report; a scope
	// without a self-calibration returns a *Refusal. LONG, OWNER.
	SelfCal(ctx context.Context, progress func(done float64)) (CalReport, error)
}

// CalReport is a self-calibration's outcome.
type CalReport struct {
	Failed []string  // one reason per failed channel ("C2: offset search did not converge")
	Stored string    // where the result was written; "" when nothing was
	Source CalSource // the calibration in use afterwards
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
	Source string // the board's name for the data: "vendor", "open-selfcal", "file", "defaults", "none"
	// Author is who produced the data: "vendor" (factory or the vendor
	// firmware), "open" (this firmware's self-calibration), "" when the
	// data does not record it (a vendor-format file either could have
	// written).
	Author string
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
//
// Configure and SetAppearance stage: what they set reaches the screen at
// the next Acquirer.Arm. While stopped, a Configure with Held redraws the
// stored record at once; an appearance change made while stopped shows
// from the next run.
type Renderer interface {
	Configure(v View) error           // OWNER: what to draw
	SetAppearance(a Appearance) error // OWNER: how to draw it
	Area() image.Rectangle            // ANY: where it draws; the UI keeps it clear or transparent
	Colors(ch []color.RGBA) error     // OWNER: trace colours; a *Refusal when fixed
}

// View is what a hardware renderer draws: the window the core would draw.
// Per-channel slices are indexed by channel number; a missing entry has
// the zero value's meaning.
type View struct {
	Mode     DisplayMode
	Channels []int
	Selected []int // channels in the user's selection order, newest first: drawing priority
	// OffsetDiv is where each channel's centre line sits, in divisions
	// above the screen's centre. In the main view a renderer whose vertical
	// placement follows the front end's offset ignores it. In the zoom view
	// it is where each channel's zoom window centres, in main-screen
	// divisions above the main screen's centre.
	OffsetDiv []float64
	// Gain is each channel's vertical magnification against the main view
	// (0 or missing: 1): the zoom view's vertical zoom. In the main view it
	// stays 1: a digital step of the ladder (Step.Zoom) is already in the
	// frames.
	Gain []float64
	// StartS is the window's start relative to the trigger and SpanS its
	// width; a main view with SpanS 0 is the acquisition's window.
	StartS float64
	SpanS  float64
	Zoom   *View // nil without a zoom window; a zoom view has no zoom and no mode of its own
	// Held: stopped, redraw the stored record at this view's window with
	// the staged vertical settings, at once rather than at the next Arm.
	// Refused with no stored record or while acquiring; a held record is
	// redrawn in the display mode it was taken in.
	Held bool
}

// DisplayMode is how a view lays out its channels.
type DisplayMode int

// The display modes; the zero value is YT.
const (
	DisplayYT    DisplayMode = iota // amplitude against time
	DisplayXY                       // one channel against another
	DisplayYTXY                     // both: the YT window and an XY plot beside it
	DisplaySplit                    // YT with each channel in a grid of its own
)

// Appearance is what a hardware renderer can be told.
type Appearance struct {
	Intensity    float64
	ColorGrade   bool
	PersistenceS float64 // <0: infinite
	Vectors      bool
}

// Input delivers key, knob and touch events and drives the panel's lamps,
// beeper and touch switch.
type Input interface {
	Events() <-chan InputEvent  // ANY; the board documents which goroutine feeds it
	SetLamps(on []string) error // COMMIT: the lit lamps by core name; the board maps them
	Keys() []string             // ANY: the keys this panel has
	Knobs() []string            // ANY
	// SetSound switches the clicks a board sounds itself on keys and
	// touches; a *Refusal without a beeper. ANY.
	SetSound(on bool) error
	// Beep sounds the beeper for d (an alert the core raises); a *Refusal
	// without a beeper. ANY: it does not wait for the sound to end.
	Beep(d time.Duration) error
	// SetTouch switches the touch screen's input on or off (off: no touch
	// events); a *Refusal without touch. OWNER.
	SetTouch(on bool) error
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
