// Package halfake is a fake board that implements every required HAL
// interface in memory, for tests of core code that drives a hal.Board. It
// acquires a synthetic square wave per channel, in whatever code format its
// Config gives, so the same test can run against 8-bit and 16-bit traces.
//
// Every OWNER and COMMIT call is logged (Calls), so a test can check the
// owner's cycle and commit order. Optional capabilities are absent: each
// accessor returns false, as on a board without them, and the profile gives
// a reason for each. The fake has a beeper but no touch screen, and a
// self-calibration that changes nothing. It also implements
// hal.CensusBoard, the supervisor-side census, so census consumers have a
// fake to run against.
package halfake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// Config shapes the fake. The zero value is a two-channel 8-bit scope.
type Config struct {
	Channels int               // default 2
	Format   sample.CodeFormat // default 8-bit codes, mid 128
	Identity hal.Identity      // default vendor "fake", model "fake-scope"
	// PeriodSamples is the square wave's period in acquired samples
	// (default 100).
	PeriodSamples int
	// NotTaken makes BringUp answer hal.ErrNotTaken.
	NotTaken bool
}

// Board is the fake. Its methods are safe for concurrent use.
type Board struct {
	mu      sync.Mutex
	cfg     Config
	profile hal.Profile
	calls   []string

	staged, applied setup
	stopped         bool // a user STOP since the last Arm
	state           hal.AcqState
	rec             sample.RecordID
	lastStatus      sample.Status
	forceNext       bool
	waitErr         error
	armKinds        []string
	services        int
	presents        int
	backlight       float64
	lamps           []string
	reboots         []string
	sound           bool
	beeps           []time.Duration
	selfCals        int

	events   chan hal.InputEvent
	settings *KV
	setups   *KV
	fb       []byte
}

type setup struct {
	channels []hal.Channel
	plan     hal.Plan
	trigger  hal.TriggerSpec
}

// TRLC-LINKS: REQ-DSO-1001
func (s setup) clone() setup {
	s.channels = slices.Clone(s.channels)
	return s
}

// TRLC-LINKS: REQ-DSO-1001
func (s setup) equal(o setup) bool {
	return slices.Equal(s.channels, o.channels) && fmt.Sprint(s.plan) == fmt.Sprint(o.plan) && fmt.Sprint(s.trigger) == fmt.Sprint(o.trigger)
}

// absent is the fake profile's reason for each optional part.
var absent = map[string]string{
	"streamer": "the fake board does not stream", "images": "the fake board has one fixed image",
	"stacker": "the fake board has no stacker", "segments": "the fake board keeps one record",
	"counter": "the fake board has no hardware counter", "logic-probe": "the fake board has no digital input",
	"generator": "the fake board drives no signal source",
}

var ladder = []float64{0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10}

// New returns a fake board.
// TRLC-LINKS: REQ-DSO-1001
func New(cfg Config) *Board {
	if cfg.Channels == 0 {
		cfg.Channels = 2
	}
	if cfg.Format.Storage == 0 {
		cfg.Format = sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	}
	if cfg.Identity.Model == "" {
		cfg.Identity = hal.Identity{Vendor: "fake", Model: "fake-scope", Serial: "0"}
	}
	if cfg.PeriodSamples == 0 {
		cfg.PeriodSamples = 100
	}
	nodes := map[hal.TrigNode]hal.NodeCaps{
		hal.NodeEdge:  {Slopes: []hal.Slope{hal.Rising, hal.Falling, hal.Either}},
		hal.NodePulse: {QualOps: []hal.QualOp{hal.Shorter, hal.Longer}, MinS: 1e-8, MaxS: 10},
	}
	b := &Board{
		cfg: cfg,
		profile: hal.Profile{
			Identity:  cfg.Identity,
			Channels:  cfg.Channels,
			Format:    cfg.Format,
			Rates:     []hal.Rate{{SamplesPerS: 1e9, MaxChannels: cfg.Channels}},
			MaxDepth:  100000,
			Bandwidth: 100e6,
			Content:   sample.ContentSetOf(sample.RawFullRate, sample.PeakPairs),
			RawAccess: hal.RawAccess{MaxDepth: 100000},
			Triggers:  hal.TrigCaps{Nodes: nodes, MaxStages: 1, Sources: []hal.SourceKind{hal.Analog}},
			Display:   hal.DisplaySpec{W: 64, H: 48, Format: hal.RGB565, DivsH: 10, DivsV: 8},
			Inputs:    hal.InputSpec{Keys: []string{"run", "single", "auto"}, Knobs: []string{"tdiv", "triglevel"}, Lamps: []string{"run", "stop"}, Beeper: true},
			Absent:    absent,
			Stage:     2,
			Timebase:  hal.TimebaseSpec{TdivS: []float64{1e-9, 1e-6, 1e-3, 1}},
			Limits:    hal.Limits{WebClients: 4, SetupSlots: 4, RefSlots: 2, RecordMax: 100000},
		},
		events:   make(chan hal.InputEvent, 64),
		settings: NewKV(4096),
		setups:   NewKV(0),
	}
	b.fb = make([]byte, b.profile.Display.W*b.profile.Display.H*2)
	for range cfg.Channels {
		b.staged.channels = append(b.staged.channels, hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6})
	}
	b.applied = b.staged.clone()
	return b
}

// TRLC-LINKS: REQ-DSO-1001
func (b *Board) log(call string) {
	b.calls = append(b.calls, call)
}

// Calls returns the OWNER and COMMIT calls made so far, in order.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Calls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.calls)
}

// ResetCalls clears the call log.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) ResetCalls() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = nil
}

// ArmKinds returns "commit" or "rearm" for every Arm so far.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) ArmKinds() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.armKinds)
}

// SetWaitError makes every Wait return err (hal.ErrTimeout, a fault) until
// it is set back to nil.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) SetWaitError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.waitErr = err
}

// Inject queues an input event, as a board's panel would.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Inject(ev hal.InputEvent) { b.events <- ev }

// Lamps returns the lamps last lit.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Lamps() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.lamps)
}

// Services returns how often Platform.Service ran.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Services() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.services
}

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

// Census implements hal.CensusBoard.
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

// Counter implements hal.Board: absent.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Counter() (hal.Counter, bool) { return nil, false }

// LogicProbe implements hal.Board: absent.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) LogicProbe() (hal.LogicProbe, bool) { return nil, false }

// Generator implements hal.Board: absent.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Generator() (hal.Generator, bool) { return nil, false }

// Extensions implements hal.Board: none.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Extensions() []hal.Extension { return nil }

// codesPerDiv is the fake's vertical scale: the format's span over ten
// divisions, so a full-scale trace has a division of headroom each side.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) codesPerDiv() float64 {
	return float64(b.cfg.Format.Max-b.cfg.Format.Min) / 10
}

type platform struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (p platform) BringUp(ctx context.Context) (hal.Identity, error) {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	p.b.log("BringUp")
	if p.b.cfg.NotTaken {
		return hal.Identity{}, hal.ErrNotTaken
	}
	return p.b.cfg.Identity, ctx.Err()
}

// TRLC-LINKS: REQ-DSO-1001
func (p platform) Handover() hal.Handover { return hal.Handover{FDs: map[string]int{}} }

// TRLC-LINKS: REQ-DSO-1001
func (p platform) Service() {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	p.b.log("Service")
	p.b.services++
}

// TRLC-LINKS: REQ-DSO-1001
func (p platform) Worker() func() int { return nil }

// TRLC-LINKS: REQ-DSO-1001
func (p platform) Reboot(reason string) error {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	p.b.log("Reboot")
	p.b.reboots = append(p.b.reboots, reason)
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (p platform) Diag() json.RawMessage {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	out, _ := json.Marshal(map[string]int{"services": p.b.services, "records": int(p.b.rec.Seq)})
	return out
}

type planner struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (p planner) Plan(r hal.Request) (hal.Plan, error) {
	prof := p.b.profile
	switch {
	case r.Segments > 1:
		return hal.Plan{}, hal.Refuse("segments", "this board has no segmented memory")
	case r.Mode == hal.Stream:
		return hal.Plan{}, hal.Refuse("stream", "this board has no streamer")
	case r.TimePerDivS <= 0:
		return hal.Plan{}, hal.Refuse("timebase", "time/div must be positive")
	}
	span := r.TimePerDivS * float64(prof.Display.DivsH)
	depth := r.Depth
	if depth == 0 {
		depth = 1000
	}
	depth = min(depth, prof.MaxDepth)
	sampleS := max(span/float64(depth), 1/prof.Rates[0].SamplesPerS)
	win := int(math.Round(span / sampleS))
	depth = min(max(depth, win), prof.MaxDepth)
	for _, f := range r.Need {
		if f.Accepts&prof.Content == 0 {
			return hal.Plan{}, hal.Refuse(f.Name, "this board delivers none of the content it needs")
		}
		if f.MinDepth > depth {
			return hal.Plan{}, hal.Refuse(f.Name, fmt.Sprintf("needs %d samples, the record holds %d", f.MinDepth, depth))
		}
	}
	return hal.Plan{
		Request: r, Mode: r.Mode, Content: prof.Content,
		SampleS: sampleS, Depth: depth, AcqDecim: 1, ETSFactor: 1, Segments: 1, WinSamples: win,
	}, nil
}

type acquirer struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Configure(p hal.Plan) error {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	a.b.log("Configure")
	if p.Depth < 1 || p.SampleS <= 0 {
		return hal.Refuse("plan", "depth and sample interval must be positive")
	}
	a.b.staged.plan = p
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Arm() error {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	a.b.log("Arm")
	if a.b.staged.plan.Depth == 0 {
		return errors.New("halfake: arm before a plan was configured")
	}
	kind := "rearm"
	if a.b.stopped || !a.b.staged.equal(a.b.applied) {
		kind = "commit"
	}
	a.b.armKinds = append(a.b.armKinds, kind)
	a.b.applied = a.b.staged.clone()
	a.b.stopped = false
	a.b.state = hal.Armed
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Wait(ctx context.Context, _ time.Duration) (hal.Status, error) {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	a.b.log("Wait")
	if err := ctx.Err(); err != nil {
		return hal.Status{}, err
	}
	if a.b.state != hal.Armed {
		return hal.Status{}, errors.New("halfake: wait while not armed")
	}
	if a.b.waitErr != nil {
		return hal.Status{}, a.b.waitErr
	}
	a.b.rec.Seq++
	a.b.lastStatus = sample.Status{Triggered: !a.b.forceNext, Forced: a.b.forceNext, Done: true, Coherent: true, Record: a.b.rec}
	a.b.forceNext = false
	a.b.state = hal.Ready
	return a.b.lastStatus, nil
}

// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Force() error {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	a.b.log("Force")
	a.b.forceNext = true
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) Stop() error {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	a.b.log("Stop")
	a.b.stopped = true
	if a.b.state == hal.Armed {
		a.b.state = hal.Idle
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (a acquirer) State() hal.AcqState {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	return a.b.state
}

type readout struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (r readout) Limits() hal.ReadLimits {
	return hal.ReadLimits{MaxPoints: r.b.profile.MaxDepth, MaxWindows: 8, MaxSegments: 1, ChannelsPerWindow: r.b.cfg.Channels}
}

// TRLC-LINKS: REQ-DSO-1001
func (r readout) Read(ctx context.Context, req hal.ReadRequest, dst []*sample.Frame, yield func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.b.mu.Lock()
	r.b.log("Read")
	if r.b.state != hal.Ready && (r.b.state != hal.Idle || r.b.rec.Seq == 0) {
		r.b.mu.Unlock()
		return errors.New("halfake: read with no halted record")
	}
	if req.Record != (sample.RecordID{}) && req.Record != r.b.rec {
		r.b.mu.Unlock()
		return hal.Refuse("record", "the board keeps only the newest record")
	}
	if req.Segments != [2]int{} && req.Segments != [2]int{0, 1} {
		r.b.mu.Unlock()
		return hal.Refuse("segments", "this board has no segmented memory")
	}
	if len(dst) < len(req.Windows) {
		r.b.mu.Unlock()
		return fmt.Errorf("halfake: %d frames for %d windows", len(dst), len(req.Windows))
	}
	app := r.b.applied.clone()
	status := r.b.lastStatus
	r.b.state = hal.Reading
	r.b.mu.Unlock()
	defer func() {
		r.b.mu.Lock()
		r.b.state = hal.Ready
		r.b.mu.Unlock()
	}()

	for i, w := range req.Windows {
		if err := r.fill(w, app, status, dst[i], yield); err != nil {
			return err
		}
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (r readout) fill(w hal.Window, app setup, status sample.Status, f *sample.Frame, yield func()) error {
	want := w.Want
	if want == 0 {
		want = sample.RawFullRate
	}
	if !r.b.profile.Content.Has(want) {
		return hal.Refuse(want.String(), "this board delivers raw full-rate samples and peak pairs only")
	}
	span := w.Span
	if span <= 0 {
		span = int64(app.plan.WinSamples)
	}
	points := w.Points
	if points <= 0 {
		points = int(span)
	}
	if want == sample.RawFullRate && int64(points) != span {
		return hal.Refuse("raw-full-rate", "full-rate samples cannot be compressed: ask for peak pairs")
	}
	bucket := float64(span) / float64(points)
	chans := w.Channels
	if len(chans) == 0 {
		for c := range r.b.cfg.Channels {
			chans = append(chans, c)
		}
	}
	f.Content = want
	f.Flags = 0
	f.SampleS = app.plan.SampleS * bucket
	f.CaptureS = app.plan.SampleS
	f.AcqDecim = 1
	f.Depth = app.plan.Depth
	f.WinPoints = int(math.Round(float64(app.plan.WinSamples) / bucket))
	f.TriggerAt = float64(-w.StartSample) / bucket
	if want == sample.PeakPairs {
		// Each delivered value is half a bucket: a (min, max) pair per bucket.
		f.SampleS /= 2
		f.WinPoints *= 2
		f.TriggerAt *= 2
	}
	f.TrigKind = sample.HardwareNode
	if status.Forced {
		f.TrigKind = sample.Forced
	}
	f.Match, f.Stream, f.Details = nil, sample.StreamPos{}, f.Details[:0]
	f.Cond = sample.Conditioning{}
	f.ContentID = uint64(status.Record.Seq)
	f.Status = status
	if cap(f.Traces) < len(chans) {
		f.Traces = make([]sample.Trace, len(chans))
	}
	f.Traces = f.Traces[:len(chans)]
	for i, ch := range chans {
		if ch < 0 || ch >= r.b.cfg.Channels {
			return hal.Refuse(fmt.Sprintf("channel %d", ch+1), "this board has no such channel")
		}
		if i > 0 && yield != nil {
			yield()
		}
		r.trace(&f.Traces[i], ch, app.channels[ch], w.StartSample, bucket, points, want)
	}
	return nil
}

// trace fills t with channel ch's square wave: ±2 divisions, period
// PeriodSamples, rising at the trigger.
// TRLC-LINKS: REQ-DSO-1001
func (r readout) trace(t *sample.Trace, ch int, c hal.Channel, start int64, bucket float64, points int, want sample.Content) {
	fm := r.b.cfg.Format
	cpd := r.b.codesPerDiv()
	hi := uint32(float64(fm.Mid) + 2*cpd)
	lo := uint32(float64(fm.Mid) - 2*cpd)
	level := func(s int64) uint32 {
		p := int64(r.b.cfg.PeriodSamples)
		if ((s%p)+p)%p < p/2 {
			return hi
		}
		return lo
	}
	*t = sample.Trace{
		Ch: ch, Format: fm, Valid: points, CodesPerDiv: cpd,
		Scale:   sample.Scale{Zero: float64(fm.Mid), PerCode: c.VoltsDiv / cpd, OffsetV: c.OffsetV},
		Codes8:  t.Codes8[:0],
		Codes16: t.Codes16[:0],
		Min8:    t.Min8[:0], Max8: t.Max8[:0], Min16: t.Min16[:0], Max16: t.Max16[:0],
	}
	if want == sample.PeakPairs {
		t.Valid = 2 * points // a (min, max) pair per bucket
	}
	for i := range points {
		s0 := start + int64(float64(i)*bucket)
		s1 := start + int64(float64(i+1)*bucket)
		v := level(s0)
		vmin, vmax := v, v
		for s := s0 + 1; s < s1; s++ {
			x := level(s)
			vmin, vmax = min(vmin, x), max(vmax, x)
		}
		switch {
		case want == sample.PeakPairs && fm.Storage == sample.U16:
			t.Codes16 = append(t.Codes16, uint16(vmin), uint16(vmax)) //nolint:gosec // G115: hi and lo are mid-scale ±2 divisions, inside the format's code range
		case want == sample.PeakPairs:
			t.Codes8 = append(t.Codes8, uint8(vmin), uint8(vmax)) //nolint:gosec // G115: hi and lo are mid-scale ±2 divisions, inside the format's code range
		case fm.Storage == sample.U16:
			t.Codes16 = append(t.Codes16, uint16(v)) //nolint:gosec // G115: hi and lo are mid-scale ±2 divisions, inside the format's code range
		default:
			t.Codes8 = append(t.Codes8, uint8(v)) //nolint:gosec // G115: hi and lo are mid-scale ±2 divisions, inside the format's code range
		}
	}
}

// TRLC-LINKS: REQ-DSO-1001
func (r readout) Export(ctx context.Context, rec hal.RecordID, ch int, dst *sample.Trace) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.b.mu.Lock()
	app := r.b.applied.clone()
	cur := r.b.rec
	r.b.mu.Unlock()
	if rec != cur {
		return hal.Refuse("record", "the board keeps only the newest record")
	}
	if ch < 0 || ch >= r.b.cfg.Channels {
		return hal.Refuse(fmt.Sprintf("channel %d", ch+1), "this board has no such channel")
	}
	r.trace(dst, ch, app.channels[ch], -int64(app.plan.Depth/2), 1, app.plan.Depth, sample.RawFullRate)
	return nil
}

type trigger struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Program(s hal.TriggerSpec) error {
	t.b.mu.Lock()
	defer t.b.mu.Unlock()
	t.b.log("Program")
	caps := t.b.profile.Triggers
	if len(s.Stages) > caps.MaxStages {
		return hal.Refuse("trigger sequence", fmt.Sprintf("this board has %d stage(s)", caps.MaxStages))
	}
	for _, st := range s.Stages {
		if !caps.Has(st.Node) {
			return hal.Refuse("trigger "+st.Node.String(), "this board has no such trigger node")
		}
		if st.Source.Kind != hal.Analog || st.Source.Index < 0 || st.Source.Index >= t.b.cfg.Channels {
			return hal.Refuse("trigger source", "this board triggers on its analog channels only")
		}
		if len(st.Zones) != 0 && !caps.Has(hal.NodeZone) {
			return hal.Refuse("zone trigger", "this board has no zone qualifier")
		}
	}
	t.b.staged.trigger = s
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Caps() hal.TrigCaps { return t.b.profile.Triggers }

// TRLC-LINKS: REQ-DSO-1001
func (t trigger) LevelGrid(src hal.Source) hal.Grid {
	t.b.mu.Lock()
	defer t.b.mu.Unlock()
	if src.Kind != hal.Analog || src.Index < 0 || src.Index >= t.b.cfg.Channels {
		return hal.Grid{}
	}
	c := t.b.staged.channels[src.Index]
	half := 5 * c.VoltsDiv
	return hal.Grid{Step: c.VoltsDiv / t.b.codesPerDiv(), Min: -half - c.OffsetV, Max: half - c.OffsetV}
}

// Default is a rising stage at 0 V on an analog channel for the fake's
// nodes.
// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Default(node hal.TrigNode, src hal.Source) (hal.TrigStage, error) {
	if !t.b.profile.Triggers.Has(node) {
		return hal.TrigStage{}, hal.Refuse("trigger "+node.String(), "this board has no such trigger node")
	}
	if src.Kind != hal.Analog || src.Index < 0 || src.Index >= t.b.cfg.Channels {
		return hal.TrigStage{}, hal.Refuse("trigger source", "this board triggers on its analog channels only")
	}
	return hal.TrigStage{Node: node, Source: src, Levels: hal.Levels{Slope: hal.Rising}}, nil
}

// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Events(_ []hal.Event) (int, error) { return 0, nil }

// TRLC-LINKS: REQ-DSO-1001
func (t trigger) Matches() (hal.MatchStats, bool) { return hal.MatchStats{}, false }

type frontEnd struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Set(ch int, c hal.Channel) (hal.Channel, error) {
	fe.b.mu.Lock()
	defer fe.b.mu.Unlock()
	fe.b.log("FrontEnd.Set")
	if ch < 0 || ch >= fe.b.cfg.Channels {
		return hal.Channel{}, hal.Refuse(fmt.Sprintf("channel %d", ch+1), "this board has no such channel")
	}
	if c.Probe == 0 {
		c.Probe = 1
	}
	tip := c.VoltsDiv / c.Probe
	if tip < ladder[0]*(1-1e-9) || tip > ladder[len(ladder)-1]*(1+1e-9) {
		return hal.Channel{}, hal.Refuse(fmt.Sprintf("%g V/div at %gx", c.VoltsDiv, c.Probe), "outside the ladder")
	}
	best := ladder[0]
	for _, v := range ladder {
		if math.Abs(math.Log(v/tip)) < math.Abs(math.Log(best/tip)) {
			best = v
		}
	}
	c.VoltsDiv = best * c.Probe
	if c.Impedance == 0 {
		c.Impedance = 1e6
	}
	grid := hal.Grid{Step: c.VoltsDiv / fe.b.codesPerDiv(), Min: -10 * c.VoltsDiv, Max: 10 * c.VoltsDiv}
	c.OffsetV = grid.Snap(c.OffsetV)
	fe.b.staged.channels[ch] = c
	return c, nil
}

// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Get(ch int) hal.Channel {
	fe.b.mu.Lock()
	defer fe.b.mu.Unlock()
	if ch < 0 || ch >= fe.b.cfg.Channels {
		return hal.Channel{}
	}
	return fe.b.applied.channels[ch]
}

// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Ladder(ch int) []hal.Step {
	p := fe.Get(ch).Probe
	if p == 0 {
		p = 1
	}
	out := make([]hal.Step, len(ladder))
	for i, v := range ladder {
		out[i] = hal.Step{VoltsDiv: v * p, Zoom: 1}
	}
	return out
}

// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) OffsetGrid(ch int) hal.Grid {
	c := fe.Get(ch)
	return hal.Grid{Step: c.VoltsDiv / fe.b.codesPerDiv(), Min: -10 * c.VoltsDiv, Max: 10 * c.VoltsDiv}
}

// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Couplings(int) []hal.CouplingOption {
	return []hal.CouplingOption{{Coupling: hal.DC}, {Coupling: hal.AC, Software: true}, {Coupling: hal.GND}}
}

// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Calibration() hal.CalSource {
	fe.b.mu.Lock()
	defer fe.b.mu.Unlock()
	return fe.b.calSource()
}

// TRLC-LINKS: REQ-DSO-1001
func (b *Board) calSource() hal.CalSource {
	if b.selfCals > 0 {
		return hal.CalSource{Source: "fake-selfcal", Author: "open", Valid: true}
	}
	return hal.CalSource{Source: "none"}
}

// SelfCal reports progress in quarters and changes nothing but the
// calibration source. It refuses while armed, as a board must.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) SelfCal(ctx context.Context, progress func(float64)) (hal.CalReport, error) {
	fe.b.mu.Lock()
	fe.b.log("SelfCal")
	if fe.b.state == hal.Armed || fe.b.state == hal.Reading {
		fe.b.mu.Unlock()
		return hal.CalReport{}, hal.Refuse("self-calibration", "the acquisition runs: stop it first")
	}
	fe.b.mu.Unlock()
	for i := range 5 {
		if err := ctx.Err(); err != nil {
			return hal.CalReport{}, err
		}
		if progress != nil {
			progress(float64(i) / 4)
		}
	}
	fe.b.mu.Lock()
	defer fe.b.mu.Unlock()
	fe.b.selfCals++
	fe.b.state, fe.b.stopped = hal.Idle, true
	return hal.CalReport{Source: fe.b.calSource()}, nil
}

type display struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (d display) Spec() hal.DisplaySpec { return d.b.profile.Display }

// TRLC-LINKS: REQ-DSO-1001
func (d display) Framebuffer() hal.Framebuffer { return framebuffer(d) }

// TRLC-LINKS: REQ-DSO-1001
func (d display) Renderer() (hal.Renderer, bool) { return nil, false }

// TRLC-LINKS: REQ-DSO-1001
func (d display) Capture(dst *image.RGBA) error {
	spec := d.b.profile.Display
	if dst.Rect.Dx() < spec.W || dst.Rect.Dy() < spec.H {
		return fmt.Errorf("halfake: capture into %v, need %dx%d", dst.Rect, spec.W, spec.H)
	}
	d.b.mu.Lock()
	defer d.b.mu.Unlock()
	for y := range spec.H {
		for x := range spec.W {
			i := (y*spec.W + x) * 2
			v := uint16(d.b.fb[i]) | uint16(d.b.fb[i+1])<<8
			r, g, b := uint8(v>>11)<<3, uint8(v>>5&0x3f)<<2, uint8(v&0x1f)<<3
			dst.SetRGBA(dst.Rect.Min.X+x, dst.Rect.Min.Y+y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (d display) Backlight(level float64) error {
	d.b.mu.Lock()
	defer d.b.mu.Unlock()
	d.b.log("Backlight")
	if level < 0 || level > 1 {
		return hal.Refuse("backlight", "level is 0..1")
	}
	d.b.backlight = level
	return nil
}

type framebuffer struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (f framebuffer) Back() []byte { return f.b.fb }

// TRLC-LINKS: REQ-DSO-1001
func (f framebuffer) Stride() int { return f.b.profile.Display.W * 2 }

// TRLC-LINKS: REQ-DSO-1001
func (f framebuffer) Present() error {
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	f.b.presents++
	return nil
}

type input struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (in input) Events() <-chan hal.InputEvent { return in.b.events }

// TRLC-LINKS: REQ-DSO-1001
func (in input) Keys() []string { return in.b.profile.Inputs.Keys }

// TRLC-LINKS: REQ-DSO-1001
func (in input) Knobs() []string { return in.b.profile.Inputs.Knobs }

// TRLC-LINKS: REQ-DSO-1001
func (in input) SetLamps(on []string) error {
	in.b.mu.Lock()
	defer in.b.mu.Unlock()
	in.b.log("SetLamps")
	for _, l := range on {
		if !slices.Contains(in.b.profile.Inputs.Lamps, l) {
			return hal.Refuse("lamp "+l, "this panel has no such lamp")
		}
	}
	in.b.lamps = slices.Clone(on)
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (in input) SetSound(on bool) error {
	in.b.mu.Lock()
	defer in.b.mu.Unlock()
	in.b.sound = on
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (in input) Beep(d time.Duration) error {
	in.b.mu.Lock()
	defer in.b.mu.Unlock()
	in.b.beeps = append(in.b.beeps, d)
	return nil
}

// TRLC-LINKS: REQ-DSO-1001
func (in input) SetTouch(bool) error {
	return hal.Refuse("touch", "this board has no touch screen")
}

// Beeps returns the beeps sounded so far and whether key clicks are on.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Beeps() (beeps []time.Duration, sound bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.beeps), b.sound
}

type storage struct{ b *Board }

// TRLC-LINKS: REQ-DSO-1001
func (s storage) Settings() hal.KV { return s.b.settings }

// TRLC-LINKS: REQ-DSO-1001
func (s storage) Setups() (hal.KV, bool) { return s.b.setups, true }

// TRLC-LINKS: REQ-DSO-1001
func (s storage) Captures() (string, error) { return "", nil }

// TRLC-LINKS: REQ-DSO-1001
func (s storage) Calibration() hal.CalSource { return frontEnd(s).Calibration() }

type census struct{}

// TRLC-LINKS: REQ-DSO-1001
func (census) Run(ctx context.Context, _ bool) ([]hal.Verdict, error) {
	return []hal.Verdict{{Part: "fake", Verdict: "present"}}, ctx.Err()
}

// KV is an in-memory hal.KV with a byte budget over all values.
type KV struct {
	mu     sync.Mutex
	m      map[string][]byte
	budget int
}

// NewKV returns an empty store; budget 0 is unbounded.
// TRLC-LINKS: REQ-DSO-1001
func NewKV(budget int) *KV { return &KV{m: map[string][]byte{}, budget: budget} }

// Get implements hal.KV.
// TRLC-LINKS: REQ-DSO-1001
func (k *KV) Get(key string) ([]byte, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	return slices.Clone(v), ok
}

// Put implements hal.KV.
// TRLC-LINKS: REQ-DSO-1001
func (k *KV) Put(key string, val []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.budget > 0 {
		used := len(val)
		for kk, v := range k.m {
			if kk != key {
				used += len(v)
			}
		}
		if used > k.budget {
			return hal.Refuse("store "+key, fmt.Sprintf("%d bytes past the %d-byte budget", used-k.budget, k.budget))
		}
	}
	k.m[key] = slices.Clone(val)
	return nil
}

// Budget implements hal.KV.
// TRLC-LINKS: REQ-DSO-1001
func (k *KV) Budget() int { return k.budget }
