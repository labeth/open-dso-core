// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/shape"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// FrameSource is the engine's frame fan-out: the newest published frame for
// every reader and a wait for the next one.
type FrameSource interface {
	// WithFrame runs fn on the newest frame (nil before the first) under
	// the fan-out's read lock; fn must not keep it.
	WithFrame(fn func(*sample.Frame))
	// WaitNext parks until a frame other than last has published, or
	// timeout, and returns the newest frame's seq.
	WaitNext(last uint64, timeout time.Duration) uint64
}

// Config is what the web scope is built over. Board, Instrument and Frames
// carry every shared part; the rest are optional parts a scope has, or
// hooks that stand in for state its engine keeps outside the instrument
// model.
type Config struct {
	// Board is the scope's HAL board: its profile, front end, trigger,
	// storage and scope-only routes. Nil only for a scope whose Status,
	// Controls and Scales come from its own engine (and which serves no
	// caps).
	Board hal.Board
	// Instrument is the instrument state every controller stages: the
	// shared /api/set and status read and change it. Nil with Controls.
	Instrument *instrument.Model
	// Frames is the engine's fan-out.
	Frames FrameSource
	// Version is the firmware's version as the page shows it.
	Version string
	// Probes are the probe factors a channel offers (default 1, 10, 100),
	// BWLimits the bandwidth limits by name (default "full"): the HAL
	// front end takes any value and refuses at commit, so the lists the
	// page offers are the scope's data.
	Probes   []float64
	BWLimits []string
	// CapsEdit, when set, completes the capabilities document with what the
	// scope's own engine serves beyond the HAL's projection (the trigger
	// types it programs, its acquisition modes); the generation follows.
	CapsEdit func(c *wire.Caps)

	// UI is the scope's UI model (the panel, the sheets and the phone
	// remote); it may also implement webserver.Refs and the decoded
	// stream's webserver.Stream methods.
	UI webserver.UIModel
	// Setups is the setup store; nil: the board's Storage setups over the
	// instrument state, if the board has them.
	Setups webserver.Setups
	// Screen renders the device screen as a PNG; nil: none.
	Screen func() []byte
	// Routes are the scope's own routes beside Board.Extensions'.
	Routes []webserver.Route

	// Parts are the core engine's parts the scope's engine runs.
	Parts Parts

	// Status is the scope's own /api/status document in place of the
	// shared one: a scope whose engine keeps counters and state the
	// instrument model does not hold yet (its v1 dialect).
	Status func() any
	// Controls is the scope's own /api/set in place of the instrument
	// model's: a scope whose engine keeps run control or levels outside
	// the instrument model.
	Controls webserver.Settings
	// Scales is how the v1 wire reads the codes of a two-channel 8-bit
	// scope whose frames do not carry their scale yet; nil: the frames'
	// traces and the instrument state.
	Scales Scales
	// Quiet brackets the serialisation of a reply on a scope whose
	// acquisition a CPU burst disturbs (Budget.QuietAcquire).
	Quiet webserver.Quiet
}

// Scales is how a two-channel 8-bit scope's v1 wire reads its codes and
// where it shows the trigger, from its own engine's state.
type Scales interface {
	// Scales are each channel's offset volts and volts a code, at the
	// probe tip, for codes about 128.
	Scales() (off, vpc [2]float64)
	// Coupling is channel ch's display coupling, done in software on the
	// codes: 0 DC (none), 1 AC, 2 GND.
	Coupling(ch int) int
	// Probe is channel ch's probe factor.
	Probe(ch int) float64
	// Inverted is each channel's display inversion.
	Inverted() [2]bool
	// Position is the trigger's place on the screen (0..1), whether the
	// scope runs and whether a single shot is armed (running free, the
	// measurements may lag a frame for 100 ms).
	Position() (posFrac float64, running, single bool)
}

// Scope is the web scope: webserver.Scope over a Config.
type Scope struct {
	cfg  Config
	meas shape.MeasCache
	caps capsCache
	fps  fpsMeter
}

var _ webserver.Scope = (*Scope)(nil)

// New is the web scope over cfg. It serves no capabilities document: a
// page then keeps the two-channel 8-bit constants it was written for
// (WithCaps serves one).
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func New(cfg Config) *Scope {
	if len(cfg.Probes) == 0 {
		cfg.Probes = []float64{1, 10, 100}
	}
	if len(cfg.BWLimits) == 0 {
		cfg.BWLimits = []string{"full"}
	}
	return &Scope{cfg: cfg}
}

// WithCaps is the scope serving its capabilities document projected from
// the board (webserver.CapsSource): the page reads its lists, channels and
// code format from it, and api and caps_gen lead the status. Without a
// board it is the scope as it is.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) WithCaps() webserver.Scope {
	if s.cfg.Board == nil {
		return s
	}
	return capsScope{s}
}

// capsScope is a scope that serves its capabilities.
type capsScope struct{ *Scope }

// Caps is the capabilities document built from the HAL.
// TRLC-LINKS: REQ-DSO-164
func (c capsScope) Caps() wire.Caps { return c.capsDoc() }

// CapsGen moves whenever the document does.
// TRLC-LINKS: REQ-DSO-164
func (c capsScope) CapsGen() uint64 { return c.capsGen() }

var _ webserver.CapsSource = capsScope{}

// profile is the board's profile; the zero profile without a board.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) profile() hal.Profile {
	if s.cfg.Board == nil {
		return hal.Profile{}
	}
	return s.cfg.Board.Profile()
}

// v1Wire reports whether frame wire v1 can carry this scope's frames: two
// channels of 8-bit codes (a scope without a board is one).
// TRLC-LINKS: REQ-DSO-163
func (s *Scope) v1Wire() bool {
	if s.cfg.Board == nil {
		return true
	}
	p := s.profile()
	return p.Channels == 2 && p.Format.Storage == sample.U8
}

// Status is the scope's status document.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) Status() any {
	if s.cfg.Status != nil {
		return s.cfg.Status()
	}
	return s.status()
}

// Settings applies /api/set's controls.
// TRLC-LINKS: REQ-DSO-162
func (s *Scope) Settings() webserver.Settings {
	if s.cfg.Controls != nil {
		return s.cfg.Controls
	}
	return controls{s}
}

// UI is the scope's UI model.
// TRLC-LINKS: REQ-DSO-162
func (s *Scope) UI() (webserver.UIModel, bool) { return s.cfg.UI, s.cfg.UI != nil }

// Refs is the UI model's saved references.
// TRLC-LINKS: REQ-DSO-138
func (s *Scope) Refs() (webserver.Refs, bool) {
	r, ok := s.cfg.UI.(webserver.Refs)
	return r, ok
}

// Stream is the UI model's decoded stream transcript.
// TRLC-LINKS: REQ-DSO-018
func (s *Scope) Stream() (webserver.Stream, bool) {
	if s.cfg.UI == nil {
		return nil, false
	}
	return stream{s.cfg.UI}, true
}

// Screen is the device screen.
// TRLC-LINKS: REQ-DSO-161
func (s *Scope) Screen() (webserver.Screen, bool) {
	return screenPNG(s.cfg.Screen), s.cfg.Screen != nil
}

// screenPNG is a screen renderer as webserver.Screen.
type screenPNG func() []byte

// TRLC-LINKS: REQ-DSO-161
func (f screenPNG) PNG() []byte { return f() }

// Extensions are the board's scope-only routes and the scope's own.
// TRLC-LINKS: REQ-DSO-161
func (s *Scope) Extensions() []webserver.Route {
	var out []webserver.Route
	if s.cfg.Board != nil {
		for _, x := range s.cfg.Board.Extensions() {
			for _, r := range x.Routes {
				out = append(out, webserver.Route{Pattern: r.Pattern, Feature: x.Feature, Handler: r.Handler})
			}
		}
	}
	return append(out, s.cfg.Routes...)
}
