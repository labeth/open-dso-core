package hal

// TriggerSpec is the typed trigger model.
type TriggerSpec struct {
	Stages    []TrigStage // A, then B; Reset aborts the sequence
	Reset     *TrigStage
	HoldoffS  float64
	HoldoffEv int // or holdoff by events
	Sweep     Sweep
	// Coupling and NoiseReject apply to the whole trigger: on some boards
	// they also move the trigger position the readout reports.
	Coupling    TrigCoupling
	NoiseReject bool
}

// TrigCoupling is the trigger path's coupling.
type TrigCoupling int

// The trigger couplings.
const (
	TrigDC TrigCoupling = iota
	TrigAC
	TrigHFReject
	TrigLFReject
)

// Sweep is the trigger sweep mode.
type Sweep int

// The sweeps.
const (
	Auto Sweep = iota
	NormalSweep
	Single
)

// TrigStage is one stage of a sequence: a condition, then a delay or count.
//
// A node's own configuration is a typed field, set only for the nodes it
// names; a board refuses a stage whose node needs one and lacks it. How the
// common trigger types map onto stages:
//
//	edge        NodeEdge
//	interval    NodeEdge with Qual: the time between two edges of the slope
//	Nth edge    NodeEdge with Count > 1; Qual Longer is the idle time before counting
//	qualified   NodeEdge with Gate; Qual makes it the delayed form
//	dropout     NodeTimeout with Qual Longer; Timeout says edge or state form
//	setup/hold  NodeSetupHold: Source is the clock, Gate the data line
//	pattern     NodePattern with Pattern
//	video       NodeVideo with Video
//	delay       two stages: NodePattern (event A), then NodeEdge (event B) with Qual
//	serial      NodeProtocol with Proto
//	window, runt, slope use Levels.High and Levels.Low
type TrigStage struct {
	Node   TrigNode
	Source Source
	Levels Levels
	Qual   *Qualifier
	// Gate is a second input that qualifies the node: on NodeEdge the edge
	// counts only while (or after) the gate is in its state (or saw its
	// edge); on NodeSetupHold it is the data line.
	Gate    *Gate
	Pattern *Pattern     // NodePattern
	Video   *Video       // NodeVideo
	Timeout TimeoutForm  // NodeTimeout
	Proto   ProtoTrigger // nil unless Node is a protocol, symbol, sequence or frame-start node
	DelayS  float64
	Count   int // the Nth event
}

// Gate is a qualifying second input.
type Gate struct {
	Source Source
	Level  float64 // threshold, volts at the tip
	Edge   bool    // after an edge of the gate input, not while it is in a state
	High   bool    // the state high, or a rising edge
	Hold   bool    // NodeSetupHold: check the hold time, not the setup time
}

// PatternState is one input's state in a pattern or a bit of a bit pattern.
type PatternState int8

// The states; the zero value is don't care.
const (
	DontCare PatternState = iota
	Low
	High
)

// PatternLogic combines a pattern's states.
type PatternLogic int

// The logic functions; the zero value is AND.
const (
	PatternAnd PatternLogic = iota
	PatternOr
	PatternNand
	PatternNor
)

// Pattern is a logic pattern over inputs. Inputs it does not name are
// don't care. Levels.Slope of the stage says whether the node fires as the
// pattern becomes true (Rising) or false (Falling).
type Pattern struct {
	Terms []PatternTerm
	Logic PatternLogic
}

// PatternTerm is one input's state in a pattern.
type PatternTerm struct {
	Source Source
	State  PatternState
	Level  float64 // threshold, volts at the tip; unused on a digital line
}

// Video is a video trigger's signal standard and line selection.
type Video struct {
	// Standard: "ntsc", "pal", "720p50", "720p60", "1080p50", "1080p60",
	// "1080i50", "1080i60" or "custom"; a board refuses one it lacks.
	Standard string
	Line     int  // 1-based line in the field
	Field    int  // 1-based field; progressive standards have one
	AnyLine  bool // every line's sync
	// The custom standard: lines per frame, frame rate, interlace factor
	// and fields counted.
	Lines     int
	FrameHz   float64
	Interlace int
	Fields    int
}

// TimeoutForm is what NodeTimeout waits for.
type TimeoutForm int

// The forms; the zero value is the edge form.
const (
	// TimeoutEdge fires when no further edge of the slope comes within
	// the time.
	TimeoutEdge TimeoutForm = iota
	// TimeoutState fires when the input stays in the state the slope leads
	// into (high after Rising) for the time.
	TimeoutState
)

// Source is a trigger source: an analog channel, a digital line or an input.
type Source struct {
	Kind  SourceKind
	Index int
}

// SourceKind is the kind of trigger source.
type SourceKind int

// The source kinds.
const (
	Analog   SourceKind = iota + 1
	Digital             // a logic-probe line
	External            // EXT, EXT/5
	ACLine
)

// TrigNode names a hardware trigger node; TrigCaps says which ones run.
type TrigNode uint16

// The nodes.
const (
	NodeEdge TrigNode = iota + 1
	NodePulse
	NodeSlope
	NodeRunt
	NodeWindow
	NodePattern
	NodeSetupHold
	NodeTimeout
	NodeVideo
	NodeZone
	NodeProtocol
)

// Generic receivers: one sliced-symbol trigger carries many buses.
const (
	NodeSymbol     TrigNode = iota + 100 // sliced symbol classes on one or two lines
	NodeSequence                         // a sequence of symbol classes
	NodeFrameStart                       // a bus's frame start (start-of-frame tokens, an idle gap)
)

// Levels is a level pair with hysteresis, in input-referred volts.
type Levels struct {
	High, Low  float64 // Low unused by single-level nodes
	Hysteresis float64
	Slope      Slope
}

// Slope is the edge direction a node fires on.
type Slope int

// The slopes.
const (
	Rising Slope = iota + 1
	Falling
	Either
)

// Qualifier is a time condition on a node.
type Qualifier struct {
	Op         QualOp
	MinS, MaxS float64
}

// QualOp compares a measured time with the qualifier's limits.
type QualOp int

// The operators.
const (
	Shorter QualOp = iota + 1
	Longer
	Inside
	Outside
)

// ProtoTrigger is a protocol trigger's typed configuration. The set is
// closed: a type implements it only by embedding ProtoTriggerType, which
// the configurations in package hal/prototrig do, so a board's
// Trigger.Program switches over a known set. Node configurations that are
// not protocols (pattern, video, gate, timeout form) are TrigStage fields,
// never a ProtoTrigger.
type ProtoTrigger interface {
	Protocol() string // the decoder name: "uart", "spi", "i2c", ...
	protoTrigger()
}

// ProtoTriggerType is embedded by every ProtoTrigger implementation.
type ProtoTriggerType struct{}

func (ProtoTriggerType) protoTrigger() {}

// TrigCaps is what a bitstream implements.
type TrigCaps struct {
	Nodes     map[TrigNode]NodeCaps `yaml:"nodes"`
	MaxStages int                   `yaml:"max_stages"`
	Reset     bool                  `yaml:"reset"`
	Protocols []string              `yaml:"protocols"`
	Sources   []SourceKind          `yaml:"sources"`
}

// Has reports whether node n is in c.
func (c TrigCaps) Has(n TrigNode) bool {
	_, ok := c.Nodes[n]
	return ok
}

// NodeCaps is what one node supports, including known-broken modes to refuse.
type NodeCaps struct {
	Slopes  []Slope  `yaml:"slopes"`
	QualOps []QualOp `yaml:"qual_ops"`
	MinS    float64  `yaml:"min_s"`
	MaxS    float64  `yaml:"max_s"`
	Broken  []string `yaml:"broken"` // refused with this text: "runt either"
}

// Trigger programs the hardware trigger. It is the only trigger entry point.
type Trigger interface {
	// Program stages s, or returns a *Refusal; it never substitutes another
	// node. Levels are volts: the board converts them with the FrontEnd
	// state staged before it. COMMIT.
	Program(s TriggerSpec) error
	// Caps is what the current image implements, intersected with the
	// profile. ANY.
	Caps() TrigCaps
	// LevelGrid is the level's step and range for a source at the current
	// vertical setting, so a knob steps one hardware step. ANY.
	LevelGrid(src Source) Grid
	// Default is node's default stage on src at the source's staged
	// vertical setting: the levels and node configuration a user starts
	// from when choosing the node. Which choice is the first is the
	// controller's to know; the values are the board's. A *Refusal for a
	// node without a single-source default or a source the node cannot
	// take. ANY.
	Default(node TrigNode, src Source) (TrigStage, error)
	// Events reads hardware-decoded events, if the image has a queue. OWNER.
	Events(dst []Event) (int, error)
	// Matches is the protocol trigger's match counters; false without one. ANY.
	Matches() (MatchStats, bool)
}

// Grid is a setting's hardware step and range, in its unit.
type Grid struct{ Step, Min, Max float64 }

// Snap returns v on the grid, clamped to its range.
func (g Grid) Snap(v float64) float64 {
	if g.Step > 0 {
		n := (v - g.Min) / g.Step
		v = g.Min + float64(int64(n+0.5))*g.Step
		if n < 0 {
			v = g.Min
		}
	}
	return min(max(v, g.Min), g.Max)
}

// Event is one hardware-decoded event.
type Event struct {
	At    uint64 // stream or record position
	Kind  uint8
	Value uint32
}

// MatchStats counts a protocol trigger's matches.
type MatchStats struct {
	Matches uint64
	Frames  uint64 // frames the matcher saw
}
