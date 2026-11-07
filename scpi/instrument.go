package scpi

import (
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// Instrument is the instrument state a dialect's commands bind to. It is in
// physical units: volts at the probe tip, seconds. Commands read it and stage
// changes through it; whoever implements it (the core instrument state, a
// scope's adapter) commits them to the board, which snaps and converts.
type Instrument interface {
	// State is the wanted state: what was last set, snapped values once the
	// board applied them.
	State() State
	// Change edits a copy of the state (its Channels are the copy's own) and
	// stages it; an error from fn or a refusal leaves the state as it was.
	Change(fn func(*State) error) error
	// WithFrame calls fn with the last published frame, nil when there is
	// none. The frame is read-only and valid only inside fn.
	WithFrame(fn func(*sample.Frame))
	// Acquisition is the acquisition's run state as an instrument shows it.
	Acquisition() RunState
	// Force makes the acquisition trigger once now.
	Force() error
}

// State is the instrument's settings.
type State struct {
	Channels []hal.Channel // at the probe tip
	TimeDivS float64
	DelayS   float64 // the screen centre's time after the trigger
	Depth    int     // samples per channel; 0: chosen by the scope
	Mode     hal.AcqMode
	Trigger  Edge
	Sweep    hal.Sweep
	Running  bool
}

// Edge is the edge trigger's setting.
type Edge struct {
	Source      hal.Source
	Slope       hal.Slope
	LevelV      float64
	Coupling    hal.TrigCoupling
	NoiseReject bool
	HoldoffS    float64 // 0: off
}

// Clone is s with its own Channels.
// TRLC-LINKS: REQ-DSO-024
func (s State) Clone() State {
	s.Channels = append([]hal.Channel(nil), s.Channels...)
	return s
}

// RunState is the acquisition's run state.
type RunState int

// The run states.
const (
	Armed     RunState = iota // waiting for the first trigger
	Ready                     // armed, a trigger is due
	AutoRun                   // running without a trigger
	Triggered                 // triggered
	Stopped
	Rolling
)

// String is the state as SCPI spells it.
// TRLC-LINKS: REQ-DSO-024
func (r RunState) String() string {
	switch r {
	case Armed:
		return "Arm"
	case Ready:
		return "Ready"
	case AutoRun:
		return "Auto"
	case Triggered:
		return "Trig'd"
	case Stopped:
		return "Stop"
	case Rolling:
		return "Roll"
	}
	return "Stop"
}
