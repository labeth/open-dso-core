// Package simboard is a simulated board: every HAL facet over a scope's
// profile and a signal generator (package siggen), so the whole product
// (engine, UI model, renderer, web server, decoders, measurements) runs
// against it without hardware. It is what the browser demo runs on.
//
// It is not a hardware model. Records are sampled from the generator on
// demand, in the profile's code format, at the profile's sample rate and
// record depth: the front end is numbers (a V/div ladder, offset, coupling,
// probe, bandwidth limit, invert), the trigger is software (edge and pulse
// width on the sampled signal, with hysteresis and holdoff), and an
// acquisition takes the signal's own time, so a slow timebase waits as a
// scope does. Everything else is absent with a reason, as the HAL's
// absent-capability rules ask.
//
// Time is the board's clock since New: the generator's t is that clock, so
// a frame taken now shows the signal now. The trigger point is placed on a
// record sample, so a periodic signal stands still on the screen.
package simboard

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/siggen"
)

// Config shapes the board. Profile is required; every other field may be
// zero.
type Config struct {
	// Profile is the scope being simulated, already resolved for Identity.
	Profile  hal.Profile
	Identity hal.Identity
	// Signals is the generator the inputs sample; nil: a bank of one input
	// per channel, each at 0 V.
	Signals *siggen.Bank
	// CodesPerDiv is a division's height in stored words; 0: the format's
	// span over the screen's divisions.
	CodesPerDiv float64
	// LevelStepsPerDiv and OffsetStepsPerDiv are how finely the trigger
	// level and the offset step (one knob detent); 0: one code.
	LevelStepsPerDiv, OffsetStepsPerDiv float64
	// Ladder is the V/div ladder at 1x; nil: 1-2-5 from 2 mV to 10 V.
	Ladder []float64
	// Depth is the record depth the planner chooses when the request leaves
	// it to the scope (0: 4096), never more than the profile's.
	Depth int
	// FrameInterval is the least time between two records (a scope's
	// re-arm and readout time); 0: the profile's target frame rate, or a
	// 30th of a second.
	FrameInterval time.Duration
	// Stored is the stores' content at start: store name ("settings",
	// "setups") to key to value. Persist hears every Put, so a host can keep
	// the stores across a restart (a browser's local storage).
	Stored  map[string]map[string][]byte
	Persist func(store, key string, val []byte)
	// OnPresent hears every picture the display presents, as RGBA rows;
	// the slice is the board's and is reused, so the callee copies it.
	OnPresent func(rgba []byte, w, h int)
	// OnLamps hears the lit lamps each time they change.
	OnLamps func(on []string)
}

// Board is the simulated board. Its methods are safe for concurrent use.
type Board struct {
	cfg     Config
	profile hal.Profile
	sig     *siggen.Bank
	t0      time.Time
	cpd     float64 // codes per division

	mu       sync.Mutex
	staged   setup
	applied  setup
	stopped  bool
	state    hal.AcqState
	acq      acquisition // the acquisition in flight
	rec      record      // the stored record
	recSeq   uint32
	lastTrig float64 // signal time of the last trigger (holdoff)
	lastDone time.Time
	pace     time.Duration
	backl    float64
	lamps    []string
	sound    bool
	selfCals int
	waits    int // records completed, for Diag
	forced   int

	events   chan hal.InputEvent
	settings *KV
	setups   *KV
	screen   screen
}

// setup is what a commit stages: the channels, the plan and the trigger.
type setup struct {
	channels []hal.Channel
	plan     hal.Plan
	trigger  hal.TriggerSpec
}

// clone is s with its own channels.
// TRLC-LINKS: REQ-DSO-1001
func (s setup) clone() setup {
	s.channels = slices.Clone(s.channels)
	return s
}

// absent is the simulated board's reason for each optional part it lacks.
var absent = map[string]string{
	"streamer":    "the simulated board has no gap-free stream: it samples records only",
	"images":      "the simulated board has one fixed trigger engine in software",
	"stacker":     "the simulated board has no hardware stacker",
	"segments":    "the simulated board keeps one record",
	"logic-probe": "the simulated board has no digital input",
	"generator":   "the simulated board drives no signal source; its inputs come from the demo's generator",
}

// swNodes are the trigger nodes the software trigger runs.
var swNodes = map[hal.TrigNode]hal.NodeCaps{
	hal.NodeEdge:  {Slopes: []hal.Slope{hal.Rising, hal.Falling, hal.Either}},
	hal.NodePulse: {Slopes: []hal.Slope{hal.Rising, hal.Falling}, QualOps: []hal.QualOp{hal.Shorter, hal.Longer, hal.Inside, hal.Outside}, MinS: 1e-9, MaxS: 10},
}

// defaultLadder is 1-2-5 from 2 mV to 10 V a division.
var defaultLadder = []float64{0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10}

// New returns the board, ready to be brought up.
// TRLC-LINKS: REQ-DSO-1001
func New(cfg Config) (*Board, error) {
	p := cfg.Profile
	if p.Channels < 1 {
		return nil, fmt.Errorf("simboard: the profile has %d channels", p.Channels)
	}
	if err := p.Format.Validate(); err != nil {
		return nil, fmt.Errorf("simboard: %w", err)
	}
	if p.Display.DivsH == 0 {
		p.Display.DivsH = 10
	}
	if p.Display.DivsV == 0 {
		p.Display.DivsV = 8
	}
	if p.Display.W == 0 || p.Display.H == 0 {
		p.Display.W, p.Display.H = 800, 480
	}
	if p.Display.Format == 0 {
		p.Display.Format = hal.RGB565
	}
	if len(p.Rates) == 0 {
		p.Rates = []hal.Rate{{SamplesPerS: 1e9, MaxChannels: p.Channels}}
	}
	if p.MaxDepth == 0 {
		p.MaxDepth = 1 << 16
	}
	if p.Content == 0 {
		p.Content = sample.ContentSetOf(sample.RawFullRate, sample.RawDecimated, sample.PeakPairs)
	}
	// The core draws the traces: a scope's hardware renderer is not
	// simulated.
	p.Display.HWRenderer, p.Display.Area = false, hal.Rect{}
	p.Triggers = softwareCaps(p.Triggers)
	p.Absent = maps.Clone(p.Absent)
	if p.Absent == nil {
		p.Absent = map[string]string{}
	}
	maps.Copy(p.Absent, absent)
	delete(p.Absent, "counter")
	p.Options = hal.OptCounter
	if cfg.Identity.Model == "" {
		cfg.Identity = hal.Identity{Vendor: "open-dso", Model: "simulated", Serial: "SIM0"}
	}
	p.Identity = cfg.Identity
	if cfg.Signals == nil {
		cfg.Signals = siggen.NewBank(p.Channels)
	}
	if len(cfg.Ladder) == 0 {
		cfg.Ladder = defaultLadder
	}
	if cfg.Depth <= 0 {
		cfg.Depth = 4096
	}
	cfg.Depth = min(cfg.Depth, p.MaxDepth)
	b := &Board{
		cfg: cfg, profile: p, sig: cfg.Signals, t0: time.Now(),
		cpd:      cfg.CodesPerDiv,
		events:   make(chan hal.InputEvent, 256),
		settings: newKV("settings", 0, cfg.Stored["settings"], cfg.Persist),
		setups:   newKV("setups", 0, cfg.Stored["setups"], cfg.Persist),
		backl:    1,
	}
	if b.cpd <= 0 {
		b.cpd = float64(p.Format.Max-p.Format.Min) / float64(p.Display.DivsV)
	}
	for range p.Channels {
		b.staged.channels = append(b.staged.channels, hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6})
	}
	b.applied = b.staged.clone()
	b.screen.init(p.Display)
	return b, nil
}

// softwareCaps is caps narrowed to what the software trigger runs, on
// analog sources, one stage.
// TRLC-LINKS: REQ-DSO-1001
func softwareCaps(caps hal.TrigCaps) hal.TrigCaps {
	out := hal.TrigCaps{Nodes: map[hal.TrigNode]hal.NodeCaps{}, MaxStages: 1, Sources: []hal.SourceKind{hal.Analog}}
	for n, c := range swNodes {
		if len(caps.Nodes) == 0 || caps.Has(n) {
			out.Nodes[n] = c
		}
	}
	return out
}

// Signals is the generator the inputs sample.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Signals() *siggen.Bank { return b.sig }

// now is the board's clock: seconds since New, the generator's time.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) now() float64 { return time.Since(b.t0).Seconds() }

// Profile implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Profile() hal.Profile { return b.profile }

// Platform implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Platform() hal.Platform { return platform{b} }

// Planner implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Planner() hal.Planner { return planner{b} }

// Acquirer implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Acquirer() hal.Acquirer { return acquirer{b} }

// Readout implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Readout() hal.Readout { return readout{b} }

// Trigger implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Trigger() hal.Trigger { return trigger{b} }

// FrontEnd implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) FrontEnd() hal.FrontEnd { return frontEnd{b} }

// Display implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Display() hal.Display { return display{b} }

// Input implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Input() hal.Input { return input{b} }

// Storage implements hal.Board.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Storage() hal.Storage { return storage{b} }

var _ hal.CensusBoard = (*Board)(nil)

// Census implements hal.CensusBoard: every part is simulated.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Census() hal.Census { return census{} }

// Streamer implements hal.Board: absent.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Streamer() (hal.Streamer, bool) { return nil, false }

// Images implements hal.Board: absent.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Images() (hal.Images, bool) { return nil, false }

// Stacker implements hal.Board: absent.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Stacker() (hal.Stacker, bool) { return nil, false }

// Segments implements hal.Board: absent.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Segments() (hal.Segments, bool) { return nil, false }

// Counter implements hal.Board: the trigger source's repetition rate, as
// the generator knows it.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Counter() (hal.Counter, bool) { return counter{b}, true }

// LogicProbe implements hal.Board: absent.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) LogicProbe() (hal.LogicProbe, bool) { return nil, false }

// Generator implements hal.Board: absent (the inputs' generator is the
// bench's, not a source the scope drives).
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Generator() (hal.Generator, bool) { return nil, false }

// Extensions implements hal.Board: the bench's web routes (the generator,
// the front panel's keys and knobs, the screen).
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Extensions() []hal.Extension { return []hal.Extension{b.bench()} }

// SetPace asks every record to take at least d from the last one (a
// display that shares one thread with the acquisition); 0: the board's own
// frame interval.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) SetPace(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pace = d
}

// frameInterval is the least time between two records.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) frameInterval() time.Duration {
	d := b.cfg.FrameInterval
	if d <= 0 {
		fps := b.profile.Budget.TargetFPS
		if fps <= 0 {
			fps = 30
		}
		d = time.Second / time.Duration(fps)
	}
	return max(d, b.pace)
}
