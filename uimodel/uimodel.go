// ENGMODEL-OWNER-UNIT: FU-DSO-UIMODEL

package uimodel

// WorkspaceCount is the number of workspaces (SCOPE, BUS, SPECTRUM,
// MEASURE, COMPARE).
const WorkspaceCount = 5

// Status is the panel snapshot in /api/status: what the LCD and lamps show.
type Status struct {
	Seq         uint64
	SheetsSeq   uint64 // moves only when a sheet value or a row's availability changes
	AutosetBusy bool
	AutosetMsg  string
	Workspace   string
	WorkspaceID string
	View        string // the active workspace's view
	Views       [WorkspaceCount]string
	Mode        string // workspace, sheet, picker, editor
	Sheet       SheetStatus
	FKeys       [5]KeyStatus
	Tool        []ToolSeg
	AdjustRow   string `json:",omitempty"` // the row SCOPE's Adjust tunes (its label alone is ambiguous: CH1 and CH2 both have an Offset)
	Again       string // the pane's again line (COMPARE: the key's job, not the trigger's stability)
	AgainNext   string // what the workspace's key again shows next, whatever the LCD shows ("X-Y")
	Notice      string
	Remote      string
	Picker      PickerStatus
	Lamps       uint16
	LampsLit    []string
	Display     Display
	Screen      string // on, saver or off (ADR-SCREEN-SAVER)
	// the last Autoset sweep that ended and the rows it changed (a remote
	// that started it books them as its own)
	AutosetDone    uint64
	AutosetChanged []string `json:",omitempty"`
	// what REF A and B hold: a page reads them again (GET /api/refs) when
	// one changes
	Refs [2]RefTag
}

// SheetStatus is the LCD's open sheet.
type SheetStatus struct {
	Open     bool
	Tab      string // tab id
	TabName  string
	TabTitle string
	Tabs     []string // in this workspace's order
	Edit     bool
	Row      *RowModel // the highlighted row (an action row without its live status text)
}

// PickerStatus is the byte picker or the match editor.
type PickerStatus struct {
	Open   bool
	Mode   string // "pick" or "editor"
	Tokens []string
	Cursor int
	KeySet string `json:",omitempty"`
	Key    string `json:",omitempty"`
}

// KeyStatus is one F-key label as drawn.
type KeyStatus struct {
	Label, Value string
	Sel, Dim     bool
}

// ToolSeg is one knob or push and its job now.
type ToolSeg struct{ Knob, Job string }

// Display is the instrument display state the LCD renders the frame with
// (the trace overlays the panel owns).
type Display struct {
	ShowC1, ShowC2   bool
	ShowMeas         bool
	MeasItems        []string // "1:pkpk": the chosen readouts, channel and parameter
	ViewMode         int      // 0 Y-T, 1 X-Y, 2 FFT, 3 Bode, 4 spectrogram
	MathMode         int
	Zoom             int
	ZoomOff          float64
	Persist          bool
	FFTSrc           int
	DecProto         int
	DecBaud          int
	DecChA, DecChB   int
	DecCPOL, DecCPHA bool
	DecLSB           bool
	DecParity        string // Modbus RTU's framing ("": found by the decoder)
	DecInverted      bool   // the IR and DALI lines' marks at the high level
	DecBits          int    // SIRC's or Wiegand's frame length (0: not known)
	// DecLayer is the bus's layer (analysis.Layers' ID, "" none) and
	// DecLayerAddr/Dir the I2C filter's address (-1 any) and direction.
	DecLayer     string
	DecLayerAddr int
	DecLayerDir  int
	DecDelayNs   float64 // SPI's data delay in use after the sampling edge (ns)
	DecFormat    int
	// Auto: a detection runs (DecDetecting); the settings are what it filled
	// in (DecAuto); what the last one found, or why it found none (DecNote)
	DecDetecting bool
	DecAuto      bool
	DecNote      string
	CurOn        bool
	CurType      int
	CurSel       int
	CurX, CurY   [2]float64 // where the screen shows the cursors now: fractions across and down (beyond 0..1 off it)
	CurT, CurV   [2]float64 // their signal: times from the trigger (s), volts at CurVCh's tip
	CurVCh       int
	AutosetBusy  bool
	AutosetMsg   string
	Remote       string // the last remote change, while it shows
}

// SheetsModel is the whole sheet model for the remote.
type SheetsModel struct {
	Seq       uint64
	SheetsSeq uint64 // the sheet values' count (Status.SheetsSeq) when this model was read
	Tabs      []TabModel
}

// TabModel is one sheet tab.
type TabModel struct {
	ID, Name, Title, Key string
	Rows                 []RowModel
	Notes                []TabNote `json:",omitempty"` // the TRIG tab's status lines, as under its rows on the LCD
}

// TabNote is one status line of a tab; Warn when it says what does not work.
type TabNote struct {
	Text string
	Warn bool `json:",omitempty"`
}

// RowModel is one setting: its id, what it is, its value and how to set it.
type RowModel struct {
	ID        string
	Label     string
	Kind      string // enum, number, action, bytes
	Available bool   // in use now (else it is shown dim)
	Help      string
	Section   string     `json:",omitempty"` // the section header the LCD draws above this row
	Why       string     `json:",omitempty"` // what puts the row in use, while it is not
	Value     any        `json:",omitempty"` // raw: option value, number in Unit, or the match text
	Text      string     // as written on the sheet
	Options   []OptModel `json:",omitempty"`
	Unit      string     `json:",omitempty"`
	Min       *float64   `json:",omitempty"`
	Max       *float64   `json:",omitempty"`
	Step      *float64   `json:",omitempty"` // one knob step (absent: a ladder)
	Ladder    []float64  `json:",omitempty"`
	Presets   []string   `json:",omitempty"` // a number row's labelled values, run by /api/sheets/action
	Actions   []ActModel `json:",omitempty"`
}

// OptModel is one option of an enum row.
type OptModel struct {
	Value int
	Text  string
}

// ActModel is one action of an action row.
type ActModel struct {
	ID, Text  string
	Available bool
}

// SetResult is the outcome of a remote set, action or typed entry. A
// remote call (remoteCall) also says what it did, measured on the panel
// goroutine: the rows whose value it changed ("workspace" too when it
// switched it), the notice it raised, the sheet values' count after it and
// the Autoset sweep it started.
type SetResult struct {
	OK        bool     `json:"ok"`
	Row       string   `json:"row,omitempty"`
	Value     any      `json:"value,omitempty"`
	Text      string   `json:"text,omitempty"`
	Error     string   `json:"error,omitempty"`
	Changed   []string `json:"changed,omitempty"`
	Notice    string   `json:"notice,omitempty"`
	SheetsSeq uint64   `json:"sheetsSeq,omitempty"`
	Autoset   uint64   `json:"autoset,omitempty"` // Status.AutosetDone names it when the sweep ends
}

// RefExport is a saved reference as the web app takes it (GET /api/refs):
// its screen columns, the volts they were saved at, the timebase and where
// the trigger sat, the probes then and the setup it was stored at, so a page
// shows the scope's REF A and B as the LCD does.
type RefExport struct {
	Has     bool       `json:"has"`
	Show    bool       `json:"show"`
	Gen     uint64     `json:"gen"` // names the store: it changes with each one
	At      int64      `json:"at"`  // when it was stored, Unix milliseconds
	Cols    int        `json:"cols"`
	C1      []int      `json:"c1,omitempty"`
	C2      []int      `json:"c2,omitempty"`
	Vdiv    [2]float64 `json:"vdiv"` // at the probe tip, as the web's volts per code
	Off     [2]float64 `json:"off"`  // at the probe tip
	Probe   [2]float64 `json:"probe"`
	TdivS   float64    `json:"tdiv_s"`
	PosFrac float64    `json:"pos_frac"`
	Setup   *RefSetup  `json:"setup,omitempty"`
}

// RefSetup is the setup a reference was stored at (AUTO in COMPARE puts the
// scope back on it): the timebase asked for, the trigger's place, source and
// slope, and each channel's V/div at the probe tip.
type RefSetup struct {
	TdivS   float64    `json:"tdiv_s"`
	PosFrac float64    `json:"pos_frac"`
	Vdiv    [2]float64 `json:"vdiv"`
	TrigSrc int        `json:"trig_src"`
	Rising  bool       `json:"rising"`
}

// RefTag says what a reference slot holds, for the status: a page reads the
// slot again (GET /api/refs) when its Gen or Show changes.
type RefTag struct {
	Has  bool
	Show bool
	Gen  uint64
}

// SoundSettings are SOUND's choices.
type SoundSettings struct {
	A4       float64 `json:"a4"`        // the tuner's reference, Hz
	Ch       int     `json:"ch"`        // the tuned channel (the VCO in V/oct; its CV on the other)
	Ref      string  `json:"ref"`       // the meters' 0 dB (sound.Presets)
	Range    string  `json:"range"`     // the tuner's range: "", "bass", "high"
	Slow     bool    `json:"slow"`      // the tuner's smoothing
	Page     int     `json:"page"`      // the LCD's readout page (soundPages)
	Cycles   int     `json:"cycles"`    // periods the cycle trace shows: 1, 2 or 4
	ABIn     int     `json:"ab_in"`     // A/B: the input channel (the output on the other)
	EnvCV    bool    `json:"env_cv"`    // the envelope is a CV read directly, not a tone's amplitude
	Gate     int     `json:"gate"`      // the envelope's trigger: -1 the signal's own level, 0 C1, 1 C2 (a gate)
	GateV    float64 `json:"gate_v"`    // its level, volts (the signal's: above its quiet mean)
	VOctAuto bool    `json:"voct_auto"` // take V/oct points when the CV and pitch hold still
	Drift    bool    `json:"drift"`     // log the pitch a second
	PreS     float64 `json:"pre_s"`     // a triggered take's seconds before the trigger
	PostS    float64 `json:"post_s"`    // and after it
	TrigGate bool    `json:"trig_gate"` // a take's trigger is the gate (Gate, GateV), not the TRIG settings
}
