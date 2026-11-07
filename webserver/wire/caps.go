// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

// Caps is the capabilities document (GET /api/caps): everything the page
// would otherwise assume about a scope. Every list the page offers comes
// from it (channels, ladders, couplings, trigger types and sources,
// protocols, acquisition modes, slots, workspaces and keys); a feature the
// scope lacks is listed with ok false and why, and the page shows it
// disabled with that reason. The page never tests a model name.
type Caps struct {
	API         int                `json:"api"`
	Gen         uint64             `json:"caps_gen"` // bumped on any change; /api/status repeats it
	Wire        Versions           `json:"wire"`
	Identity    Identity           `json:"identity"`
	Channels    []CapsChannel      `json:"channels"`
	Codes       CapsCodes          `json:"codes"`
	Timebase    CapsTimebase       `json:"timebase"`
	Vertical    CapsVertical       `json:"vertical"`
	Trigger     CapsTrigger        `json:"trigger"`
	Acquisition CapsAcquisition    `json:"acquisition"`
	Features    map[string]Feature `json:"features,omitempty"`
	Controls    []CapsControl      `json:"controls,omitempty"`
	Screen      CapsScreen         `json:"screen"`
	UI          CapsUI             `json:"ui"`
	Limits      CapsLimits         `json:"limits"`
}

// Versions are the binary shapes this server speaks, per resource.
type Versions struct {
	Frame  []int `json:"frame"`
	Record []int `json:"record,omitempty"`
	Sound  []int `json:"sound,omitempty"`
}

// Identity names the instrument.
type Identity struct {
	Vendor   string `json:"vendor,omitempty"`
	Model    string `json:"model,omitempty"`
	Firmware string `json:"firmware,omitempty"`
	Stage    int    `json:"stage,omitempty"`
}

// CapsChannel is one analog channel. Color names its design token
// (c1..cN); Codes overrides the scope's code format for this channel.
type CapsChannel struct {
	Ch    int        `json:"ch"`
	Name  string     `json:"name"`
	Color string     `json:"color,omitempty"`
	Codes *CapsCodes `json:"codes,omitempty"`
}

// CapsCodes is how the scope stores and draws its codes: the storage
// format, the codes per screen division (in stored units at frac 0) and the
// fraction bits of its finer precision words, if any.
type CapsCodes struct {
	Bits              uint8   `json:"bits"`
	Storage           string  `json:"storage"`
	Shift             uint8   `json:"shift"`
	Frac              uint8   `json:"frac"`
	Min               uint32  `json:"min"`
	Max               uint32  `json:"max"`
	Mid               uint32  `json:"mid"`
	CodesPerDiv       float64 `json:"codes_per_div"`
	FracBitsPrecision uint8   `json:"frac_bits_precision,omitempty"`
}

// CapsTimebase is the timebase ladder and record depths.
type CapsTimebase struct {
	Tdivs         []float64 `json:"tdivs"`
	FullRateTdivS float64   `json:"full_rate_tdiv_s,omitempty"` // slowest tdiv served at the full rate
	FullRateS     float64   `json:"full_rate_s,omitempty"`      // the converters' fastest interval
	TdivMaxS      float64   `json:"tdiv_max_s,omitempty"`
	Depths        []int     `json:"depths,omitempty"`
	Positions     []float64 `json:"positions,omitempty"` // the trigger position's range, screen fractions
}

// CapsVertical is the vertical ladder and the front end's choices.
// OffsetRangeV rows are {vdiv, min V, max V}: the offset range from that
// V/div up.
type CapsVertical struct {
	Vdivs        []float64    `json:"vdivs"`
	ZoomedVdivs  []float64    `json:"zoomed_vdivs,omitempty"`
	Probes       []float64    `json:"probes"`
	Couplings    []string     `json:"couplings"`
	BWLimits     []string     `json:"bw_limits,omitempty"`
	Impedances   []string     `json:"impedances,omitempty"`
	OffsetRangeV [][3]float64 `json:"offset_range_v,omitempty"`
}

// CapsTrigger is what the trigger can be set to.
type CapsTrigger struct {
	Types      []string       `json:"types"`
	Sources    []string       `json:"sources"`
	LevelSteps int            `json:"level_steps,omitempty"`
	Modes      []string       `json:"modes,omitempty"`
	HoldoffS   [2]float64     `json:"holdoff_s"`
	Protocols  []CapsProtocol `json:"protocols,omitempty"`
	MaxZones   int            `json:"max_zones,omitempty"`
	// Avail is every trigger type a scope's instrument state holds, each
	// with whether its trigger serves it and why not; Params are the
	// settings of each type that runs, by type name (/api/set takes them as
	// "trig.<id>"). Both are absent on a scope whose own engine keeps the
	// trigger.
	Avail  []CapsTrigType             `json:"type_avail,omitempty"`
	Params map[string][]CapsTrigParam `json:"params,omitempty"`
}

// CapsTrigType is a trigger type with whether it can run here.
type CapsTrigType struct {
	ID  string `json:"id"`
	OK  bool   `json:"ok"`
	Why string `json:"why,omitempty"`
}

// CapsTrigParam is one setting of a trigger type: kind is number, int,
// choice, bool, source or states.
type CapsTrigParam struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Unit       string   `json:"unit,omitempty"`
	Choices    []string `json:"choices,omitempty"`
	PerChannel bool     `json:"per_channel,omitempty"`
}

// CapsProtocol is one protocol a serial trigger takes.
type CapsProtocol struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// CapsAcquisition is the acquisition modes and the kinds of content the
// frames can carry.
type CapsAcquisition struct {
	Modes   []string `json:"modes"`
	Content []string `json:"content,omitempty"`
}

// Feature is one optional capability: ok, or why not.
type Feature struct {
	OK         bool     `json:"ok"`
	Why        string   `json:"why,omitempty"`
	Needs      []string `json:"needs,omitempty"`  // content kinds it requires
	Routes     []string `json:"routes,omitempty"` // a scope extension's routes
	Payload    string   `json:"payload,omitempty"`
	Slots      int      `json:"slots,omitempty"`
	MinSampleS float64  `json:"min_sample_s,omitempty"`
}

// CapsControl is one /api/set control: its unit, whether it takes a
// channel, and the caps list that is its ladder.
type CapsControl struct {
	Name       string `json:"name"`
	Unit       string `json:"unit,omitempty"`
	PerChannel bool   `json:"per_channel,omitempty"`
	Ladder     string `json:"ladder,omitempty"`
}

// CapsScreen is the device screen as /api/screen.png serves it.
type CapsScreen struct {
	W      int    `json:"w"`
	H      int    `json:"h"`
	Format string `json:"format,omitempty"`
	Touch  bool   `json:"touch,omitempty"`
}

// CapsUI is the shared UI model as this scope runs it.
type CapsUI struct {
	Remote     bool     `json:"remote"`
	FKeys      int      `json:"fkeys,omitempty"`
	Workspaces []string `json:"workspaces,omitempty"`
	Keys       []string `json:"keys,omitempty"`
	Knobs      []string `json:"knobs,omitempty"`
}

// CapsLimits bound what a page may ask for.
type CapsLimits struct {
	FrameCols   [2]int `json:"frame_cols"`
	LiveClients int    `json:"live_clients,omitempty"`
	RecordMax   int    `json:"record_max,omitempty"`
}
