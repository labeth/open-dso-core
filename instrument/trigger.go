// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/prototrig"
)

// TrigType is a trigger type as a user picks it. Each maps onto the HAL's
// stages as hal.TrigStage's table says: the type is the user's word, the
// stages are what a board programs. Edge, pulse, slope and video keep the
// numbers Controls and Stats have always given them.
type TrigType int

// The trigger types.
const (
	TrigEdge TrigType = iota
	TrigPulse
	TrigSlope
	TrigVideo
	TrigWindow
	TrigInterval
	TrigDropout
	TrigRunt
	TrigPattern
	TrigQualified
	TrigNthEdge
	TrigDelay
	TrigSetupHold
	TrigI2C
	TrigSPI
	TrigUART
	TrigCAN
	TrigLIN
	NumTrigTypes
)

var trigTypeNames = [NumTrigTypes]string{
	"edge", "pulse", "slope", "video", "window", "interval", "dropout", "runt", "pattern",
	"qualified", "nth_edge", "delay", "setup_hold", "i2c", "spi", "uart", "can", "lin",
}

// String is the type's name, as the web and the settings spell it.
// TRLC-LINKS: REQ-DSO-011
func (t TrigType) String() string {
	if t < 0 || t >= NumTrigTypes {
		return fmt.Sprintf("trigger(%d)", int(t))
	}
	return trigTypeNames[t]
}

// TrigTypeNamed is the type a name spells; false for no type.
// TRLC-LINKS: REQ-DSO-011
func TrigTypeNamed(name string) (TrigType, bool) {
	i := slices.Index(trigTypeNames[:], name)
	return TrigType(i), i >= 0
}

// protocol is the protocol a protocol type matches ("" for the others).
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013
func (t TrigType) protocol() string {
	switch t { //nolint:exhaustive // default: not a protocol type
	case TrigI2C, TrigSPI, TrigUART, TrigCAN, TrigLIN:
		return t.String()
	}
	return ""
}

// Avail reports whether a trigger image with caps c serves type t, and
// why not when it does not. A type is never substituted: a type an image
// lacks is refused with this reason. Caps without nodes are a scope that
// says nothing of its trigger: edge only.
// TRLC-LINKS: REQ-DSO-011
func (t TrigType) Avail(c hal.TrigCaps) (bool, string) {
	if len(c.Nodes) == 0 {
		if t == TrigEdge {
			return true, ""
		}
		return false, "this scope's trigger takes the edge trigger only"
	}
	node := func(n hal.TrigNode) (hal.NodeCaps, string) {
		nc, ok := c.Nodes[n]
		if !ok {
			return nc, fmt.Sprintf("the trigger has no %s node", n)
		}
		return nc, ""
	}
	why := ""
	switch t { //nolint:exhaustive // default: NumTrigTypes and out of range are no type
	case TrigEdge:
		_, why = node(hal.NodeEdge)
	case TrigPulse:
		_, why = node(hal.NodePulse)
	case TrigSlope:
		_, why = node(hal.NodeSlope)
	case TrigVideo:
		_, why = node(hal.NodeVideo)
	case TrigWindow:
		_, why = node(hal.NodeWindow)
	case TrigDropout:
		_, why = node(hal.NodeTimeout)
	case TrigRunt:
		_, why = node(hal.NodeRunt)
	case TrigPattern:
		_, why = node(hal.NodePattern)
	case TrigSetupHold:
		_, why = node(hal.NodeSetupHold)
	case TrigInterval:
		var nc hal.NodeCaps
		if nc, why = node(hal.NodeEdge); why == "" && len(nc.QualOps) == 0 {
			why = "the edge node takes no time condition"
		}
	case TrigQualified:
		var nc hal.NodeCaps
		if nc, why = node(hal.NodeEdge); why == "" && !nc.Gate {
			why = "the edge node takes no gate input"
		}
	case TrigNthEdge:
		var nc hal.NodeCaps
		if nc, why = node(hal.NodeEdge); why == "" && nc.MaxCount < 2 {
			why = "the edge node counts no edges"
		}
	case TrigDelay:
		if _, why = node(hal.NodePattern); why == "" {
			_, why = node(hal.NodeEdge)
		}
		if why == "" && c.MaxStages < 2 {
			why = "the trigger runs one stage: no pattern then an edge"
		}
	case TrigI2C, TrigSPI, TrigUART, TrigCAN, TrigLIN:
		if _, why = node(hal.NodeProtocol); why == "" && !slices.Contains(c.Protocols, t.protocol()) {
			why = fmt.Sprintf("the trigger has no %s matcher (decode only)", t.protocol())
		}
	default:
		why = "not a trigger type"
	}
	return why == "", why
}

// TrigConfig is one trigger type's configuration: its stages and the
// trigger-wide settings it runs with. Each type keeps its own, as scopes'
// host interfaces keep them per type, so a type switched away from and
// back finds its settings as they were left.
type TrigConfig struct {
	// Stages are the type's stages in the HAL's terms; the last one is the
	// stage that fires (the delay trigger's edge after its pattern).
	Stages      []hal.TrigStage
	HoldoffS    float64 // 0: off
	Coupling    hal.TrigCoupling
	NoiseReject bool
	// NoTime runs the type without its firing stage's time condition, whose
	// settings are kept: a qualified edge's undelayed forms.
	NoTime bool
}

// Trigger is the trigger setting: every type's configuration and the type
// that runs.
type Trigger struct {
	Configs [NumTrigTypes]TrigConfig
	Type    TrigType
}

// running is the type that runs: Type, or edge for a type out of range.
// TRLC-LINKS: REQ-DSO-011
func (t Trigger) running() TrigType {
	if t.Type < 0 || t.Type >= NumTrigTypes {
		return TrigEdge
	}
	return t.Type
}

// Active is the running type's configuration.
// TRLC-LINKS: REQ-DSO-011
func (t Trigger) Active() TrigConfig { return t.Configs[t.running()] }

// Running is the running type's configuration, to edit.
// TRLC-LINKS: REQ-DSO-011
func (t *Trigger) Running() *TrigConfig { return &t.Configs[t.running()] }

// Source is the running type's source: the input its firing stage watches
// (a pattern's first term, a protocol's data line).
// TRLC-LINKS: REQ-DSO-011
func (t Trigger) Source() hal.Source {
	tt := t.running()
	if tt == TrigPattern {
		if c := t.Configs[tt]; len(c.Stages) > 0 && c.Stages[0].Pattern != nil {
			for _, term := range c.Stages[0].Pattern.Terms {
				if term.State != hal.DontCare {
					return term.Source
				}
			}
		}
		return hal.Source{Kind: hal.Analog}
	}
	v, err := t.param(tt, sourceParam(tt), 0)
	if err != nil {
		return hal.Source{Kind: hal.Analog}
	}
	src, _ := ParseSource(v.Text, -1)
	return src
}

// LevelV is the running type's level at its source, volts at the tip.
// TRLC-LINKS: REQ-DSO-011
func (t Trigger) LevelV() float64 {
	tt := t.running()
	id, ch := levelParam(tt), 0
	if tt == TrigPattern {
		ch = t.Source().Index
	}
	v, err := t.param(tt, id, ch)
	if err != nil {
		return 0
	}
	return v.Num
}

// Slope is the running type's slope (rising when it has none).
// TRLC-LINKS: REQ-DSO-011
func (t Trigger) Slope() hal.Slope {
	c := t.Active()
	if len(c.Stages) == 0 {
		return hal.Rising
	}
	if s := c.Stages[len(c.Stages)-1].Levels.Slope; s != 0 {
		return s
	}
	return hal.Rising
}

// SetSource sets the running type's source (its data line for a protocol);
// a type without a single source refuses.
// TRLC-LINKS: REQ-DSO-011
func (t *Trigger) SetSource(src hal.Source, channels int) error {
	tt := t.running()
	return t.setParam(tt, sourceParam(tt), ParamValue{Text: SourceName(src)}, channels)
}

// SetLevel sets the running type's level at its source.
// TRLC-LINKS: REQ-DSO-011
func (t *Trigger) SetLevel(v float64, channels int) error {
	tt := t.running()
	ch := 0
	if tt == TrigPattern {
		ch = t.Source().Index
	}
	return t.setParam(tt, levelParam(tt), ParamValue{Num: v, Ch: ch}, channels)
}

// SetSlope sets the running type's slope.
// TRLC-LINKS: REQ-DSO-011
func (t *Trigger) SetSlope(s hal.Slope, channels int) error {
	tt := t.running()
	return t.setParam(tt, "slope", ParamValue{Text: slopeText(s)}, channels)
}

// slopeText names a slope as the slope settings take it.
// TRLC-LINKS: REQ-DSO-011
func slopeText(s hal.Slope) string {
	switch s { //nolint:exhaustive // default: rising
	case hal.Falling:
		return "falling"
	case hal.Either:
		return "either"
	}
	return "rising"
}

// sourceParam is the setting that names type t's source.
// TRLC-LINKS: REQ-DSO-011
func sourceParam(t TrigType) string {
	switch t { //nolint:exhaustive // default: the type's own source
	case TrigI2C:
		return "sda_source"
	case TrigSPI:
		return "data_source"
	}
	return "source"
}

// levelParam is the setting that holds type t's level at its source.
// TRLC-LINKS: REQ-DSO-011
func levelParam(t TrigType) string {
	switch t { //nolint:exhaustive // default: the type's own level
	case TrigI2C:
		return "sda_level"
	case TrigSPI:
		return "data_level"
	case TrigPattern:
		return "pattern_level"
	}
	return "level"
}

// Spec is the trigger in the HAL's terms, swept as sweep. The stages are
// the trigger's own: the caller reads them and does not edit them.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (t Trigger) Spec(sweep hal.Sweep) hal.TriggerSpec {
	c := t.Active()
	if c.NoTime && len(c.Stages) > 0 {
		st := slices.Clone(c.Stages)
		st[len(st)-1].Qual = nil
		c.Stages = st
	}
	return hal.TriggerSpec{
		Stages: c.Stages, Sweep: sweep, Coupling: c.Coupling, NoiseReject: c.NoiseReject, HoldoffS: c.HoldoffS,
	}
}

// Clone is t with its own stages: an edit of the clone, down to a pattern
// term or a protocol's data bytes, never reaches t.
// TRLC-LINKS: REQ-DSO-008
func (t Trigger) Clone() Trigger {
	for i := range t.Configs {
		c := &t.Configs[i]
		if c.Stages == nil {
			continue
		}
		st := make([]hal.TrigStage, len(c.Stages))
		for k, s := range c.Stages {
			st[k] = cloneStage(s)
		}
		c.Stages = st
	}
	return t
}

// Equal reports whether t and o are the same setting.
// TRLC-LINKS: REQ-DSO-008
func (t Trigger) Equal(o Trigger) bool {
	if t.Type != o.Type {
		return false
	}
	for i := range t.Configs {
		a, b := &t.Configs[i], &o.Configs[i]
		if a.HoldoffS != b.HoldoffS || a.Coupling != b.Coupling || a.NoiseReject != b.NoiseReject || a.NoTime != b.NoTime ||
			len(a.Stages) != len(b.Stages) || !reflect.DeepEqual(a.Stages, b.Stages) {
			return false
		}
	}
	return true
}

// cloneStage is s with its own node configurations.
// TRLC-LINKS: REQ-DSO-008
func cloneStage(s hal.TrigStage) hal.TrigStage {
	if s.Qual != nil {
		q := *s.Qual
		s.Qual = &q
	}
	if s.Gate != nil {
		g := *s.Gate
		s.Gate = &g
	}
	if s.Pattern != nil {
		p := *s.Pattern
		p.Terms = slices.Clone(p.Terms)
		s.Pattern = &p
	}
	if s.Video != nil {
		v := *s.Video
		s.Video = &v
	}
	s.Zones = slices.Clone(s.Zones)
	s.Proto = cloneProto(s.Proto)
	return s
}

// cloneProto is p with its own data. A configuration the switch does not
// name (prototrig.Bus) is a value whose slices are replaced, never edited,
// by contract.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-013
func cloneProto(p hal.ProtoTrigger) hal.ProtoTrigger {
	switch c := p.(type) {
	case prototrig.I2C:
		c.Data = slices.Clone(c.Data)
		return c
	case prototrig.SPI:
		c.Bits = slices.Clone(c.Bits)
		return c
	case prototrig.CAN:
		c.Data = slices.Clone(c.Data)
		return c
	case prototrig.LIN:
		c.Data = slices.Clone(c.Data)
		return c
	}
	return p
}

// Fill gives every type without stages its default configuration on a
// scope with channels analog channels (DefaultTrigConfig), so every type
// can be switched to and edited.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (t *Trigger) Fill(channels int) {
	for i := range t.Configs {
		if len(t.Configs[i].Stages) == 0 {
			t.Configs[i] = DefaultTrigConfig(TrigType(i), channels)
		}
	}
}

// DefaultTrigConfig is type t's starting configuration on a scope with
// channels analog channels: on C1 (a second input on C2), levels at 0 V,
// rising, one microsecond limits. These are neutral starting values, not a
// board's: a scope whose host interface documents other defaults sets them
// in its default state, and a board's own per-node defaults come from
// hal.Trigger.Default.
// TRLC-LINKS: REQ-DSO-011
func DefaultTrigConfig(t TrigType, channels int) TrigConfig {
	c1 := hal.Source{Kind: hal.Analog}
	c2 := c1
	if channels > 1 {
		c2.Index = 1
	}
	rise := hal.Levels{Slope: hal.Rising}
	qual := func(op hal.QualOp) *hal.Qualifier { return &hal.Qualifier{Op: op, MinS: 1e-6, MaxS: 2e-6} }
	two := hal.Levels{High: 0.1, Low: -0.1, Slope: hal.Rising}
	terms := func() *hal.Pattern {
		p := &hal.Pattern{}
		for ch := range max(channels, 1) {
			p.Terms = append(p.Terms, hal.PatternTerm{Source: hal.Source{Kind: hal.Analog, Index: ch}})
		}
		p.Terms[0].State = hal.High
		return p
	}
	one := func(s hal.TrigStage) TrigConfig {
		if s.Source == (hal.Source{}) {
			s.Source = c1
		}
		return TrigConfig{Stages: []hal.TrigStage{s}}
	}
	line := func(src hal.Source) prototrig.Line { return prototrig.Line{Source: src} }
	switch t { //nolint:exhaustive // default: the edge trigger
	case TrigPulse:
		return one(hal.TrigStage{Node: hal.NodePulse, Levels: rise, Qual: qual(hal.Shorter)})
	case TrigSlope:
		return one(hal.TrigStage{Node: hal.NodeSlope, Levels: two, Qual: qual(hal.Shorter)})
	case TrigVideo:
		return one(hal.TrigStage{Node: hal.NodeVideo, Levels: rise, Video: &hal.Video{Standard: "ntsc", Line: 1, Field: 1, AnyLine: true}})
	case TrigWindow:
		return one(hal.TrigStage{Node: hal.NodeWindow, Levels: hal.Levels{High: 0.1, Low: -0.1}})
	case TrigInterval:
		return one(hal.TrigStage{Node: hal.NodeEdge, Levels: rise, Qual: qual(hal.Shorter)})
	case TrigDropout:
		return one(hal.TrigStage{Node: hal.NodeTimeout, Levels: rise, Qual: qual(hal.Longer)})
	case TrigRunt:
		return one(hal.TrigStage{Node: hal.NodeRunt, Levels: two, Qual: qual(hal.Shorter)})
	case TrigPattern:
		return one(hal.TrigStage{Node: hal.NodePattern, Levels: rise, Pattern: terms(), Qual: qual(hal.Longer)})
	case TrigQualified:
		c := one(hal.TrigStage{Node: hal.NodeEdge, Levels: rise, Gate: &hal.Gate{Source: c2}, Qual: qual(hal.Shorter)})
		c.NoTime = true
		return c
	case TrigNthEdge:
		return one(hal.TrigStage{Node: hal.NodeEdge, Levels: rise, Count: 2, Qual: qual(0)})
	case TrigDelay:
		return TrigConfig{Stages: []hal.TrigStage{
			{Node: hal.NodePattern, Source: c1, Levels: rise, Pattern: terms()},
			{Node: hal.NodeEdge, Source: c2, Levels: rise, Qual: qual(hal.Shorter)},
		}}
	case TrigSetupHold:
		return one(hal.TrigStage{Node: hal.NodeSetupHold, Levels: rise, Gate: &hal.Gate{Source: c2}, Qual: qual(hal.Shorter)})
	case TrigI2C:
		return one(hal.TrigStage{Node: hal.NodeProtocol, Source: c2, Proto: prototrig.I2C{
			SCL: line(c1), SDA: line(c2), Address: -1, RW: prototrig.AnyRW, DataLength: 1,
		}})
	case TrigSPI:
		bits := slices.Repeat([]hal.PatternState{hal.DontCare}, 8)
		return one(hal.TrigStage{Node: hal.NodeProtocol, Source: c2, Proto: prototrig.SPI{
			Clock: line(c1), Data: line(c2), Select: line(c1), SelectMode: prototrig.SelectTimeout, IdleS: 1e-6,
			MSBFirst: true, Bits: bits,
		}})
	case TrigUART:
		return one(hal.TrigStage{Node: hal.NodeProtocol, Proto: prototrig.UART{
			Line: line(c1), Baud: 9600, Bits: 8, StopBits: 1, IdleHigh: true, Value: -1,
		}})
	case TrigCAN:
		return one(hal.TrigStage{Node: hal.NodeProtocol, Proto: prototrig.CAN{Line: line(c1), Baud: 500e3, ID: -1}})
	case TrigLIN:
		return one(hal.TrigStage{Node: hal.NodeProtocol, Proto: prototrig.LIN{Line: line(c1), Baud: 19200, ID: -1}})
	}
	return one(hal.TrigStage{Node: hal.NodeEdge, Levels: rise})
}
