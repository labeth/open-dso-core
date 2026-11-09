// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

// Package panel is the scope's UI model as the front panel, the display, the
// web page and the phone remote drive it: the task workspaces and their
// views, the settings sheets and their rows, what a key, a knob or a touch
// does, Autoset and the bus Auto, references, the mask flow, SOUND, the
// screen saver and the lamps. It drives the instrument through
// instrument.Controls, the front end and the board's levels; it touches no
// hardware itself, so every setting is staged for the engine's owner. A
// board feeds its keys, knobs and touches in through Input under the core
// key names (keys.go) and takes the lit lamps back by name.
package panel

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/settings"
	"github.com/labeth/open-dso-core/superres"
)

// Engine is the command surface the controller drives: the instrument's
// controls and the mask test.
// TRLC-LINKS: REQ-DSO-135
type Engine interface {
	instrument.Controls
	SetMask(m *engine.Mask)
	SetMaskMode(m int)
	ClearMaskFails()
}

// Analog is the V/div front end, in volts at the BNC (the HAL's Ladder is
// at the probe tip); nil → V/div knobs claim-and-ignore. The offsets are
// set through the board's levels (Levels).
// TRLC-LINKS: REQ-DSO-135
type Analog interface {
	Ladder(ch int) []hal.Step
	SetVdivV(ch int, v float64) error
	VdivV(ch int) float64
	RangeV(ch int) float64 // the analog range's V/div the codes are on
	OffsetReqV(ch int) float64
	OffsetRangeV(ch int) float64
	SetCoupling(ch, mode int) error
	Coupling(ch int) int
	SetProbe(ch int, x float64)
	ProbeFactor(ch int) float64
}

// Levels is the board's trigger level and offsets in volts over what it
// stages (the SDS1000CML+'s DACs, a HAL scope's instrument state).
// TRLC-LINKS: REQ-DSO-135
type Levels interface {
	SetTrigLevel(v float64, src int) float64
	TrigLevelOrIn(st instrument.Stats, v float64, src int) float64
	StepTrigLevel(n, src int) float64
	TrigLevelGrid(src int) hal.Grid
	// TrigStepsV is the volts n of the level's knob steps move it.
	TrigStepsV(n int) float64
	SaveTrigLevel() instrument.TrigLevelSaved
	SaveTrigLevelIn(st instrument.Stats) instrument.TrigLevelSaved
	RestoreTrigLevel(s instrument.TrigLevelSaved)
	ResetTrigLevel()
	// LevelWordV is the volts of a saved setup's level word
	// (settings.Trigger.LevelCode).
	LevelWordV(word int) float64
	SetOffset(ch int, v float64) float64
	OffsetIn(ch int, st instrument.Stats) float64
	OffsetStagedIn(ch int, st instrument.Stats) float64
}

// The engine's and the front end's optional capabilities: one that has the
// method does it, any other leaves the request (a test fake, an engine
// without the view).
type (
	spectrumViewLeaser interface{ LeaseSpectrumView(time.Duration) }
	searchViewLeaser   interface{ LeaseSearchView(time.Duration) }
	sampleViewLeaser   interface{ LeaseSampleView(time.Duration) }
	busSampleLeaser    interface{ LeaseBusSamples(float64, time.Duration) }
	rollStreamer       interface{ SetRollStream(bool) }
	bwLimiter          interface{ SetBWL(ch int, on bool) error }
	// acqLogger exposes the per-capture instrumentation ring; autoset uses
	// it to verify its chosen trigger level fires the comparator.
	acqLogger interface {
		AcqLog(n int) ([]engine.AcqSample, float64)
	}
)

// The lamp word: what the lamps show, as the status document's Lamps word
// carries it. Its bits are the SDS1000CML+'s latch layout, which the pages
// have always read; a board lights its own lamps from the names (lampSet).
const (
	ledCursors = 0x0002 // CURSORS — the cursors are on
	ledCH1     = 0x0010
	ledMath    = 0x0020 // MATH — a math trace is on
	ledCH2     = 0x0040
	ledRef     = 0x0080 // REF — REF A or REF B is shown
	ledMeasure = 0x0100
	ledAcquire = 0x0200
	ledDisplay = 0x0400
	ledSaveRec = 0x0800 // SAVE/RECALL — the settings are those last saved or recalled
	ledUtility = 0x1000 // UTILITY — a bus is decoded
	ledRun     = 0x2000 // RUN (green element of the bicolor RUN/STOP lamp)
	ledStop    = 0x4000 // STOP (red element)
	ledSingle  = 0x8000
)

// Controller is the front-panel worker: the keys, knobs and LEDs, the
// workspace UI and its settings, driving the engine and the analog front
// end through their interfaces.
// TRLC-LINKS: REQ-DSO-135
type Controller struct {
	eng   Engine
	fe    Analog
	lv    Levels
	hw    Hardware
	input Input // the board's front panel (Run's), guarded by mu
	// screenHeight is the display's rows (SetScreen; 0: 480)
	screenHeight atomic.Int64
	// touchKeys is the screen's touch layout (SetTouchKeys), guarded by mu
	touchKeys func(x, y int) string
	// touchM is the screen of the touch-first layout (SetTouchLayout; nil:
	// the F-key layout), guarded by mu; tin is its gestures under way, the
	// panel goroutine's alone.
	touchM *render.Metrics
	tin    touchIn
	logf   func(string, ...any)

	tdivs   []float64
	tdivIdx int
	running bool
	norm    bool
	single  bool // a single-shot is armed/waiting → SINGLE lamp

	zoom    int     // horizontal magnification (1,2,5,10,20,50); 1 = no zoom
	zoomOff float64 // pan offset of the zoom window, fraction of the record
	persist bool    // display persistence (afterglow)
	// Protocol decode: a framecalc protocol number, never ProtoAuto (Auto
	// detects the bus and fills these in). chA/chB = channel roles
	// (0=C1,1=C2): UART uses chA (source); I2C chA=SCL,chB=SDA; SPI chA=CLK,chB=DATA.
	decProto         int
	decBaud          int
	decChA, decChB   int
	decCPOL, decCPHA bool
	decLSB           bool // SPI least significant bit first
	// decParity is Modbus RTU's framing ("even", "odd", "none"; "" the
	// decoder finds it); decInv says the IR or DALI line's marks are at its
	// high level (an emitter's drive), as detected, and for the protocols
	// whose decoders report it the polarity or line they found (Result.Inverted:
	// an RC, Morse or DCF77 line's marks low, a DSI line's high, an RFID
	// reader's carrier).
	decParity string
	decInv    bool
	// decBits is SIRC's, Wiegand's or a T55xx command's frame length (0:
	// not known), the widths of their units for the hardware matcher, or the
	// variant a detection found (Result.FrameBits).
	decBits int
	// decLayer is the bus's application layer (analysis.Layers' ID, ""
	// none); decLayerAddr and decLayerDir are the I2C filter layer's address
	// (-1 any) and direction (0 both, 1 read, 2 write).
	decLayer     string
	decLayerAddr int
	decLayerDir  int
	// SPI's data line is read decDelayNs after the clock's sampling edge, or
	// with decDelayAuto at the middle of the data's eye measured on the
	// full-rate records (spiEye); decFine asks for the FPGA's receivers on
	// the samples (the fast image) at any rate.
	decDelayNs   float64
	decDelayAuto bool
	decFine      bool
	spiEye       decode.SPIEye // the last full-rate SPI record's eye (PeriodS 0: none)
	// the bursts' ends and starts weighed on the records since the SPI bus
	// or its clock rate changed (spiEyeKey): the middle after the edge or
	// before it
	spiEyeKey   spiEyeK
	spiEyeVotes decode.SPIEyeVotes
	decFormat   int // 0=hex, 1=ascii, 2=both
	// what Auto filled in, and whether the bus it found still decodes
	auto autoWatch
	// how the detection under way frames the bus: 0 not, busFrameEntry for
	// BUS's own detection, busFrameAuto for AUTO in BUS
	busFrameNext int
	// lookTook is how long a bus search's sampled records took to come, the
	// decimated ones' [1] and the full-rate ones' [0], guarded by mu
	// (captureWait)
	lookTook [2]time.Duration
	// Decoded streaming (ADR-PANEL-DECODED-STREAM), guarded by mu.
	decMode  int         // decModeView / decModeStream
	decTrig  int         // index into decTrigNames
	decValue int         // StreamOnValue unit
	kb       matchEditor // SERIAL trigger match editor (full-screen keyboard)
	stream   Stream
	// a stopped stream's screen: transcript list or the record's waveform
	streamList bool
	streamTop  int // list's first line; -1 follows the newest
	streamGen  int // counts stream starts: a start superseded meanwhile gives way

	// UI state (ADR-TASK-WORKSPACES): written by the panel goroutine, read by
	// the LCD renderer and the remote, guarded by mu. inject runs API-driven
	// panel events on the panel goroutine so dispatch stays single-threaded.
	mu     sync.Mutex
	ui     uiState
	snd    *soundSvc        // SOUND's settings and service (sound.go)
	clock  func() time.Time // nil: time.Now (tests drive notices with their own)
	chDisp [maxChans]bool
	nch    int // the scope's channels (the front end's, else two)
	// cpd is each channel's codes a division on the 8-bit view, as the last
	// frame analysed stated it (float64 bits; 0 before one).
	cpd      [maxChans]atomic.Uint64
	showMeas bool
	meas     []measItem // the chosen readouts (at most maxMeasItems)
	viewMode int        // 0 Y-T, 1 X-Y, 2 FFT, 3 Bode, 4 spectrogram (the VIEW tab's View)
	mathMode int        // 0 = off, 1 = C1+C2, 2 = C1-C2, 3 = C2-C1, 4 = C1×C2
	fftSrc   int        // the channel SPECTRUM and the spectrogram analyse
	mathCh   [2]int     // the channels math combines (A, B)
	xyCh     [2]int     // the channels X-Y plots (X, Y)
	inverter Inverter   // the display invert (SCPI Cn:INVS state); nil without one

	// Per-frame analysis for the panes (OnFrame), guarded by amu. Lock order:
	// amu may be taken without mu held only; mu may be taken under amu.
	amu sync.Mutex
	ana anaState

	// The remote's snapshot and long-poll (remote.go), guarded by rmu.
	rmu    sync.Mutex
	remote remoteState

	apiDepth       int       // a remote action is running: it leaves the LCD's menus alone
	page           pageClaim // a stream the web page asked for (page_stream.go), guarded by mu
	ledWord        uint16    // the lamp word lit
	ledLit         string    // the lamps lit, by name
	ledSet         bool
	lastSetup      int               // the slot last saved or recalled (Save/Recall lamp)
	lastSetupPrint string            // the settings then (a store without setupCurrent)
	lastSetupSt    settings.Settings // the setup then, as a save writes it
	setupSame      bool              // the settings are still lastSetupSt (cached)
	setupSameAt    time.Time         // when setupSame was taken; zero: take it again
	slotUsed       [setupSlots + 1]bool
	slotSum        [setupSlots + 1]string // the FILE rows' summaries
	slotWS         [setupSlots + 1]int    // the workspace each slot holds (-1 unknown)
	remoteNotices  bool                   // show REMOTE notes on the LCD
	bootTarget     hal.BootTarget         // the board's next-boot choice; nil: the board has none
	saverMin       int                    // Screen saver after this many minutes without input (0 = off)
	offMin         int                    // Screen off after this many minutes without input (0 = off)
	lastInput      time.Time              // the last input run on the panel goroutine (zero: none yet)
	wakeUntil      time.Time              // until then the scope's keys and knobs only wake the screen
	stackN         int                    // the master's depth
	webCapture     bool                   // a web page's decoded capture holds the acquisition (seen by SyncLEDs)
	stack          *stackState            // a master being stacked
	maskAutoTest   bool                   // the COMPARE MASK row or a master: test once built
	maskAutoMode   int                    // and in which mode: count, or stop on fail (0: count)
	detectSync     bool                   // tests: run the bus detection inline
	searchMu       sync.Mutex             // one bus search at a time (findBus steps the timebase)
	autoSearch     atomic.Bool            // the next bus search is AUTO's own (runBusAuto)
	searchOwn      bool                   // the running bus search is AUTO's own (guarded by searchMu)
	decGen         uint64                 // counts the protocol settings (set, recalled, defaulted, locked): a search's result stands only if none came meanwhile
	// On-screen cursors (spec 08 §6): two X (time) and two Y (volts) cursors,
	// anchored to the signal: the time cursors' times from the trigger (s),
	// the voltage cursors' volts at the tip of channel curVCh; ADJUST moves
	// the selected one. curPend holds screen fractions (across, down) to
	// anchor at the next state read (the power-on and factory places, an older
	// settings file's).
	curOn   bool
	curType int // 0 = X (time), 1 = Y (volts)
	curSel  int // 0 = A, 1 = B
	curT    [2]float64
	curV    [2]float64
	curVCh  int
	curPend *[2][2]float64
	// the record's length in screens of the last frame analysed (a zoomed
	// screen's pan is a fraction of the record): float64 bits
	curRecords  atomic.Uint64
	eyeLeased   atomic.Bool // the eye holds a raw lease (leaseEyeRaw, endEyeRaw)
	busRawHeld  atomic.Bool // BUS holds a raw lease to decode (busRawFrames, endBusRaw)
	busSampHeld atomic.Bool // BUS holds a lease of finely sampled records to decode (holdBusLease)
	inject      chan func()
	looping     atomic.Bool // Run is serving inject (else a remote change runs inline)
	inlineMu    sync.Mutex  // orders inline remote changes without the loop

	// frameFn reads the latest published frame (fo.WithFrame) for autoset; nil
	// until SetFrameSource is called (AUTO then falls back to plain AUTO/run).
	frameFn func(func(*sample.Frame))
	// view8 is the frame on screen on the 8-bit view the analysis reads
	// (every code format: 12-bit words as Q8.8 precision beside the view's
	// codes); an 8-bit frame is read as it is.
	view8 sample.View8Cache
	// autoset runs as a background sweep; autosetStop cancels it (second AUTO).
	autosetBusy bool
	autosetStop chan struct{}
	autosetMsg  string      // banner text while busy ("AUTOSET…" / a result note)
	autoFor     int         // while a page's AUTO press runs: the workspace it frames for + 1 (0: the LCD's)
	qualHeld    atomic.Bool // AUTO in BUS holds a loaded qualifier image (edgeOnGeneralImage)
	// each sweep is numbered; when one ends, the rows it changed are kept for
	// the remote that started it (Status.AutosetDone, AutosetChanged)
	autosetRun     uint64
	autosetFrom    changeMark
	autosetDone    uint64
	autosetChanged []string
	// PRINT key: hardcopy saves a screenshot and names the file (shown as a
	// notice).
	hardcopy func() (string, error)
	refs     [2]refWave // saved reference waveforms (REF A/B)
	refGen   uint64     // counts reference stores (refWave.gen)

	maskSetup    cmpSetup // the setup the mask built here was installed at
	maskSetupGen uint64   // the engine's mask generation it names

	// Super-res (device stack-and-crunch, reference-locked — SINGLE a frame, then
	// UTILITY). srActive gates the mode; a stacker goroutine feeds srStack OFF the
	// render lock. srFocus is the intensity-button cycle: 0=watch (live+gate),
	// 1=edit gate START, 2=edit gate END (ADJUST knob moves the edge), 3=review
	// the stacked trace. srManLo/srManHi are the manual gate (-1 = auto).
	srActive     bool
	srFocus      int
	srManLo      int
	srManHi      int
	srStack      *superres.Stack
	srStop       chan struct{}
	srStatus     string
	srK          int     // fine-grid factor
	srStopMode   int     // 0=bits, 1=stacks, 2=time (menu order bit→stacks→time)
	srStopVal    float64 // target for the active stop mode
	srFPGACancel func()
	srFPGAGen    uint64
	// The seeder: the period search and the seed take seconds on the ARM, so
	// they run off the panel goroutine; a request's generation lets the
	// latest one win and Off discard one in flight.
	srSeedGen  uint64
	srSeeding  bool          // a seed requested or running: the mode reads On
	srSeederOn bool          // the seeder goroutine runs
	srRunAt    uint64        // runReq when the latest seed was asked for
	runReq     atomic.Uint64 // Run, Stop and Single asked for: a seed resumes RUN only when none came after it was asked for
	srFPGABusy bool          // an FPGA stacking session is running (superres_fpga.go)
	srCh       int           // stacked/aligned channel (0=C1,1=C2)
	srT0       time.Time
	srMean     []float32 // latest crunched trace for the review render (guarded by mu)
	srMean2    []float32 // the OTHER channel's crunched trace (stacked X-Y / dual FFT)
	srBits     float64   // latest measured bits gained (guarded by mu)

	// Mask testing (device flow, docs/zonemask-plan.md §3): build a golden
	// envelope from N live frames on the trigger-source channel, dilate by the
	// selected tolerance preset, install in the engine. All guarded by mu.
	maskN        int         // frames per build
	setups       SetupMemory // SAVE/RECALL slots (nil = unavailable)
	maskTol      int         // index into maskTols presets
	maskBuilding bool        // a build goroutine is running
	maskGen      uint64      // counts mask clears: a build running when the mask is cleared installs nothing
	maskMsg      string      // build/status line for the LCD HUD
	srFrames     int         // stacked frame count (guarded by mu — srStack.Frames races)
	srRejected   int         // rejected (non-matching) frame count (guarded by mu)
	srResetReq   bool        // Reset softkey → srLoop clears the accumulation next tick
	srWinLo      int         // the selected span (frozen on-screen window / manual gate):
	srWinHi      int         // the review renders exactly this span, so the view is unchanged
	srPeriod     int         // detected period within the span (samples); >0 → the stack is
	// ONE period and the review TILES it across the span (fast multi-wave cheat)

	// Trigger-qualifier shadows (an engine without getters for these), edited
	// on the TRIG tab. ns for widths/times; fractions 0..1.
	pulseLvl, pulseMin, pulseMax         float64
	pulseCond                            int
	slopeLo, slopeHi, slopeMin, slopeMax float64
	slopeCond                            int
	videoStd, videoLine                  int
	videoNeg                             bool
}

// New builds the controller over the engine, the front end, the board's
// levels lv and its hardware's facts hw. The timebase ladder is injected (the controller
// does not own it); startTdiv seeds the knob index. All shadows are seeded
// WITHOUT driving anything — the inherited analog state stays untouched
// until the user turns a knob (spec 08 §7).
// TRLC-LINKS: REQ-DSO-135
func New(eng Engine, fe Analog, lv Levels, hw Hardware, tdivs []float64, startTdiv float64, logf func(string, ...any)) *Controller {
	c := &Controller{
		eng: eng, fe: fe, lv: lv, hw: hw, logf: logf,
		tdivs:        tdivs,
		chDisp:       [maxChans]bool{true, true, true, true},
		mathCh:       [2]int{0, 1},
		xyCh:         [2]int{0, 1},
		nch:          numChans(fe),
		curPend:      &cursorHome,
		inject:       make(chan func(), 32),
		running:      true,
		zoom:         1,
		decBaud:      115200,
		decLayerAddr: -1,
		decChA:       0, decChB: 1, // UART on C1; I2C/SPI clk on C1, data on C2
		// Super-res defaults: fine-grid ×32, stop on +4 bits (the user's example),
		// stacking C1. Menu stop-mode order is bit→stacks→time (0/1/2).
		srK: 32, srStopMode: 0, srStopVal: 4, srCh: 0,
		// Mask defaults: 32 build frames, tolerance preset ±5 samp / ±8 codes
		// (covers trigger-point jitter + 3σ noise — docs/zonemask-plan.md §1.4).
		maskN: 32, maskTol: 1,
		// Seed the qualifier shadows to the engine's defaultTrigParams so the
		// TRIG tab agrees with the engine from boot (slope 0.2/0.8, neg sync).
		pulseLvl: 0.5, pulseMin: powerOnLimitNs[0], pulseMax: powerOnLimitNs[1],
		slopeLo: 0.2, slopeHi: 0.8, slopeMin: powerOnLimitNs[0], slopeMax: powerOnLimitNs[1],
		videoLine: 0, videoNeg: true,
		ui:            freshUI(),
		stackN:        64,
		remoteNotices: true,
		saverMin:      defSaverMin,
		offMin:        defOffMin,
	}
	c.ui.nch = c.nch
	c.snd = newSoundSvc(c)
	for i := range c.slotWS {
		c.slotWS[i] = -1
	}
	c.ui.statT = c.now()
	for i, t := range tdivs {
		if t >= startTdiv*(1-1e-6) {
			c.tdivIdx = i
			break
		}
	}
	// a serial trigger never set up takes any I2C address and direction: its
	// zero value reads address 0x00, write, and the address is kept across
	// buses (serialBus); a restored setup sets its own after this
	if se := c.serialTrig(); se != nil {
		if p := se.SerialConfig(); p.Proto == 0 && len(p.Bytes) == 0 && len(p.Pattern) == 0 && p.Addr == 0 && p.RW == 0 {
			p.Addr, p.RW = -1, 2
			se.SetSerialParams(p)
		}
	}
	return c
}

// maxChans is the most channels the UI model serves: the HUD's.
const maxChans = render.MaxChans

// defaultCodesPerDiv is the codes a division on the 8-bit view of a trace
// that states none (the SDS1000CML+'s 25).
const defaultCodesPerDiv = 25

// channelCounter is a front end that knows its scope's channel count.
type channelCounter interface{ Channels() int }

// numChans is the channels of the scope behind front end fe: its own count,
// else two (the SDS1000CML+'s, and a dev host's without a front end).
// TRLC-LINKS: REQ-DSO-212
func numChans(fe Analog) int {
	if n, ok := fe.(channelCounter); ok {
		return min(max(n.Channels(), 1), maxChans)
	}
	return 2
}

// Channels is the scope's channel count the UI model serves.
// TRLC-LINKS: REQ-DSO-212
func (c *Controller) Channels() int { return c.nch }

// chOf is channel number x on this scope (sv.chOf).
// TRLC-LINKS: REQ-DSO-212
func (c *Controller) chOf(x int) int { return chMod(x, c.nch) }

// channelShower is a front end whose channels are switched on and off in
// the instrument state (a HAL scope's Channel.On), so the trace shown is the
// same on every surface; without one the UI model keeps it.
type channelShower interface {
	ChannelOn(ch int) bool
	SetChannelOn(ch int, on bool)
}

// pushShown puts the channels shown into a front end that keeps them.
// TRLC-LINKS: REQ-DSO-212
func (c *Controller) pushShown() {
	cs, ok := c.fe.(channelShower)
	if !ok {
		return
	}
	c.mu.Lock()
	d := c.chDisp
	c.mu.Unlock()
	for ch := range c.nch {
		if cs.ChannelOn(ch) != d[ch] {
			cs.SetChannelOn(ch, d[ch])
		}
	}
}

// Inverter is the display-level trace invert (the SCPI Cn:INVS state) of
// each channel. A scope without one has the Invert rows refused with the
// reason (noInvert).
type Inverter interface {
	IsInverted(ch int) bool
	SetInverted(ch int, on bool)
}

// PairInverter is a two-channel display invert: C1 and C2's states as a
// pair.
type PairInverter interface {
	Inverted() [2]bool
	SetInverted(ch int, on bool)
}

// Inverter2 is a two-channel display invert as an Inverter.
// TRLC-LINKS: REQ-DSO-212
func Inverter2(i PairInverter) Inverter { return inverter2{i} }

// inverter2 is Inverter2's Inverter.
type inverter2 struct{ i PairInverter }

// TRLC-LINKS: REQ-DSO-212
func (v inverter2) IsInverted(ch int) bool { return ch >= 0 && ch < 2 && v.i.Inverted()[ch] }

// TRLC-LINKS: REQ-DSO-212
func (v inverter2) SetInverted(ch int, on bool) {
	if ch >= 0 && ch < 2 {
		v.i.SetInverted(ch, on)
	}
}

// noInvert is why the Invert rows are refused on a scope with no display
// invert.
const noInvert = "invert unavailable: this scope has no display invert"

// SetInverter wires the CH tabs' Invert rows to the display invert.
// TRLC-LINKS: REQ-DSO-212
func (c *Controller) SetInverter(i Inverter) {
	c.mu.Lock()
	c.inverter = i
	c.mu.Unlock()
}

// SetScreen gives the display's size the views are laid out for (the eye
// diagram's rows); without it, 800×480.
// TRLC-LINKS: REQ-DSO-211
func (c *Controller) SetScreen(_, h int) {
	c.screenHeight.Store(int64(h))
}

// screenH is the display's rows.
// TRLC-LINKS: REQ-DSO-211
func (c *Controller) screenH() int {
	if h := c.screenHeight.Load(); h > 0 {
		return int(h)
	}
	return 480
}
