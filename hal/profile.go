package hal

// Profile is what a scope has: pure data loaded from the scope's
// profile.yaml (package profile), the base overlaid with every variant that
// matches the unit's Identity. Fields are only ever added, and every zero
// value is the safe one. The core reads it; it never asks for a model name.
type Profile struct {
	Identity   Identity       `yaml:"-"`          // the unit's, from Platform.BringUp
	Channels   int            `yaml:"channels"`   // analog channels
	Format     CodeFormat     `yaml:"format"`     // native code format of RawFullRate frames
	Rates      []Rate         `yaml:"rates"`      // sample rates and the channels each holds for
	MaxDepth   int            `yaml:"max_depth"`  // samples per channel at the deepest acquisition
	Bandwidth  float64        `yaml:"bandwidth"`  // analog bandwidth, Hz
	Content    ContentSet     `yaml:"content"`    // what Readout can deliver
	RawAccess  RawAccess      `yaml:"raw_access"` // how much raw data reaches the CPU
	Triggers   TrigCaps       `yaml:"triggers"`   // the nodes any image can have; Images narrow it
	Display    DisplaySpec    `yaml:"display"`
	Vertical   VerticalSpec   `yaml:"vertical"` // the front end's offered lists
	Inputs     InputSpec      `yaml:"inputs"`
	Supervisor SupervisorSpec `yaml:"supervisor"`
	Options    OptSet         `yaml:"options"` // optional hardware fitted
	// Absent gives the reason for each optional part this scope lacks,
	// keyed by part name (OptionalParts, TriggerParts), so a feature that needs it is
	// refused with the scope's own words.
	Absent   map[string]string `yaml:"absent"`
	Budget   Budget            `yaml:"budget"`
	Stage    int               `yaml:"stage"` // 1: vendor bitstream, 2: open bitstream
	Timebase TimebaseSpec      `yaml:"timebase"`
	Limits   Limits            `yaml:"limits"`
	// TriggerIndex is the samples between the places a hardware edge
	// trigger's index can name (2: a record word of two samples); above 1
	// the core places an edge trigger on its level crossing in the record
	// and drops one whose record has no crossing near the index (the raw
	// codes firing on converter noise). 0 or 1: the index is the crossing.
	TriggerIndex int `yaml:"trigger_index"`
	// StatusBand is the band word every capture's status reports
	// (Stats.BandKind) on a scope whose records are all one kind: "sram",
	// the record memory read in screen windows, which the UI model reads
	// as frame-confirmed triggers, no equivalent-time or decimated-depth
	// rows, and the precision mode. Empty: none.
	StatusBand string `yaml:"status_band"`
}

// Identity is what bring-up reads from the unit. Profile variants match on
// it; an identity that matches no variant is refused before any image load.
type Identity struct {
	Vendor         string            `yaml:"vendor"`
	Model          string            `yaml:"model"`
	Serial         string            `yaml:"serial"`
	HWRevision     string            `yaml:"hw_revision"`
	ADCPart        string            `yaml:"adc_part"`
	VendorFirmware string            `yaml:"vendor_firmware"` // the vendor version a stage-1 scope runs on
	Licences       []string          `yaml:"licences"`
	Fitted         map[string]string `yaml:"fitted"` // read-only census verdicts: "sram": "absent"
}

// Rate is one sample rate and the channels it holds for.
type Rate struct {
	SamplesPerS float64 `yaml:"samples_per_s"`
	MaxChannels int     `yaml:"max_channels"`
}

// RawAccess is how much raw data the board can hand the CPU.
type RawAccess struct {
	MaxDepth   int  `yaml:"max_depth"`   // per readout window
	PerWindow  bool `yaml:"per_window"`  // one channel and one window at a time
	ExportOnly bool `yaml:"export_only"` // raw only through the slow Readout.Export path
}

// DisplaySpec is the panel and its rendering paths.
type DisplaySpec struct {
	W          int         `yaml:"w"`
	H          int         `yaml:"h"`
	Format     PixelFormat `yaml:"format"`
	Touch      bool        `yaml:"touch"`
	HWRenderer bool        `yaml:"hw_renderer"` // a hardware waveform renderer draws part of the panel
	DivsH      int         `yaml:"divs_h"`      // graticule divisions across
	DivsV      int         `yaml:"divs_v"`      // graticule divisions down
	Area       Rect        `yaml:"area"`        // where a hardware renderer's plane lands
}

// Rect is a pixel rectangle.
type Rect struct {
	X int `yaml:"x"`
	Y int `yaml:"y"`
	W int `yaml:"w"`
	H int `yaml:"h"`
}

// PixelFormat is a framebuffer pixel layout.
type PixelFormat int

// The pixel formats.
const (
	RGB565   PixelFormat = iota + 1
	XRGB8888             // 32-bit, alpha ignored
	ARGB8888             // an overlay plane over a hardware renderer: alpha 0 shows the waveform
)

// VerticalSpec is what the front end offers a channel beyond its ladder:
// the probe factors and the bandwidth limits by name ("full", "20M"). The
// front end takes any value and refuses at commit, so the lists the
// product offers are the scope's data; empty: 1, 10, 100 and "full".
type VerticalSpec struct {
	Probes   []float64 `yaml:"probes"`
	BWLimits []string  `yaml:"bw_limits"`
}

// InputSpec is the front panel's controls, by core name.
type InputSpec struct {
	Keys   []string `yaml:"keys"`
	Knobs  []string `yaml:"knobs"`
	Lamps  []string `yaml:"lamps"`
	Touch  bool     `yaml:"touch"`
	Beeper bool     `yaml:"beeper"`
}

// SupervisorSpec says who restarts a hung app on this scope.
type SupervisorSpec struct {
	Watchdog WatchdogKind `yaml:"watchdog"`
	Restart  string       `yaml:"restart"` // "watchdog", "software-health", "android-service"
}

// WatchdogKind is the hardware the supervisor can rely on.
type WatchdogKind int

// The watchdog kinds.
const (
	NoWatchdog  WatchdogKind = iota
	DevWatchdog              // /dev/watchdog
	Heartbeat                // an external service that expects a heartbeat
)

// OptSet is the optional hardware fitted to a unit.
type OptSet uint32

// The options.
const (
	OptLogicProbe OptSet = 1 << iota
	OptAWG
	OptCounter
	OptSegments
	OptStacker
)

// Has reports whether every option in o is in s.
// TRLC-LINKS: REQ-DSO-1001
func (s OptSet) Has(o OptSet) bool { return s&o == o }

// Budget is the resource envelope the core stays inside on this scope.
type Budget struct {
	RAMBytes  int64   `yaml:"ram_bytes"`
	Cores     int     `yaml:"cores"`
	TargetFPS int     `yaml:"target_fps"`
	DisplayHz float64 `yaml:"display_hz"` // fixed display cadence; 0: as fast as frames come
	// QuietAcquire: the engine holds heavy consumers off during arm-settle
	// and drain (a single core that shares its time with the acquisition).
	QuietAcquire bool           `yaml:"quiet_acquire"`
	DepthCaps    map[string]int `yaml:"depth_caps"` // per feature: the deepest record it may process
}

// TimebaseSpec is the horizontal ladder.
type TimebaseSpec struct {
	TdivS         []float64 `yaml:"tdiv_s"`           // every time/div the scope offers, ascending
	FullRateTdivS float64   `yaml:"full_rate_tdiv_s"` // the slowest time/div still at the full rate
	RollFromS     float64   `yaml:"roll_from_s"`      // time/div from which acquisition rolls
}

// Limits are the product's counted resources on this scope.
type Limits struct {
	WebClients int `yaml:"web_clients"` // live web clients served at once
	SetupSlots int `yaml:"setup_slots"`
	RefSlots   int `yaml:"ref_slots"`
	RecordMax  int `yaml:"record_max"` // samples per channel one record download may hold
}

// OptionalParts names the optional parts, as Board's optional accessors and
// Profile.Absent name them.
var OptionalParts = []string{"streamer", "images", "stacker", "segments", "counter", "logic-probe", "generator"}

// TriggerParts names the trigger's optional parts a profile may also give
// an absent reason for: "zones", the zone qualifier (NodeZone).
var TriggerParts = []string{"zones"}

// AbsentReason is why part is absent on this scope: the profile's reason,
// or a generic one when the profile gives none.
// TRLC-LINKS: REQ-DSO-1001
func (p Profile) AbsentReason(part string) string {
	if r := p.Absent[part]; r != "" {
		return r
	}
	return "this scope has no " + part
}
