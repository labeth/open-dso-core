// Package instrument is the instrument state every controller of a scope
// writes and the owner loop commits: the settings in volts and seconds, a
// generation that moves with every change, the run state the acquisition
// reports back, and the requests (a forced trigger, LONG work) that wait
// for the owner. The front panel's UI model, SCPI, the web server and the
// settings file all stage through one Model; only the owner loop reads it
// to program the board (ADR-HAL-INSTRUMENT-STATE).
// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT
package instrument

import (
	"github.com/labeth/open-dso-core/hal"
)

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
// TRLC-LINKS: REQ-DSO-008
func (s State) Clone() State {
	s.Channels = append([]hal.Channel(nil), s.Channels...)
	return s
}

// Equal reports whether s and o are the same settings.
// TRLC-LINKS: REQ-DSO-008
func (s State) Equal(o State) bool {
	if len(s.Channels) != len(o.Channels) {
		return false
	}
	for i := range s.Channels {
		if s.Channels[i] != o.Channels[i] {
			return false
		}
	}
	return s.TimeDivS == o.TimeDivS && s.DelayS == o.DelayS && s.Depth == o.Depth && s.Mode == o.Mode &&
		s.Trigger == o.Trigger && s.Sweep == o.Sweep && s.Running == o.Running
}

// Request is what the planner is asked for in state s: the channels that
// are on, the timebase, the depth and the mode.
// TRLC-LINKS: REQ-DSO-008
func (s State) Request() hal.Request {
	var on []int
	for c, ch := range s.Channels {
		if ch.On {
			on = append(on, c)
		}
	}
	return hal.Request{Channels: on, TimePerDivS: s.TimeDivS, DelayS: s.DelayS, Depth: s.Depth, Mode: s.Mode}
}

// TriggerSpec is the edge trigger of state s in the HAL's terms.
// TRLC-LINKS: REQ-DSO-008
func (s State) TriggerSpec() hal.TriggerSpec {
	t := s.Trigger
	return hal.TriggerSpec{
		Stages:      []hal.TrigStage{{Node: hal.NodeEdge, Source: t.Source, Levels: hal.Levels{High: t.LevelV, Slope: t.Slope}}},
		Sweep:       s.Sweep,
		Coupling:    t.Coupling,
		NoiseReject: t.NoiseReject,
		HoldoffS:    t.HoldoffS,
	}
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
