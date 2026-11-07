package scpi

import (
	"github.com/labeth/open-dso-core/instrument"
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

// State is the instrument's settings (package instrument).
type State = instrument.State

// Trigger is the trigger setting: every type's configuration and the type
// that runs (package instrument).
type Trigger = instrument.Trigger

// TrigConfig is one trigger type's configuration (package instrument).
type TrigConfig = instrument.TrigConfig

// TrigType is a trigger type (package instrument).
type TrigType = instrument.TrigType

// TrigEdge is the edge trigger type.
const TrigEdge = instrument.TrigEdge

// RunState is the acquisition's run state.
type RunState = instrument.RunState

// The run states.
const (
	Armed     = instrument.Armed
	Ready     = instrument.Ready
	AutoRun   = instrument.AutoRun
	Triggered = instrument.Triggered
	Stopped   = instrument.Stopped
	Rolling   = instrument.Rolling
)
