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
type TrigStage struct {
	Node   TrigNode
	Source Source
	Levels Levels
	Qual   *Qualifier
	Proto  ProtoTrigger // nil unless Node is a protocol, symbol, sequence or frame-start node
	DelayS float64
	Count  int // the Nth event
}

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
// the core's decoders do for their trigger configurations, so a board's
// Trigger.Program switches over a known set.
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
