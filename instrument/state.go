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
//
// Run control is Running, Sweep and Single (ADR-RUN-CONTROL): Sweep is the
// trigger mode, AUTO or NORMAL; Single is a single shot armed, which waits
// in that mode and stops the run after its triggered frame. A controller
// that writes Sweep = hal.Single arms a shot (Change keeps that meaning).
//
// Trigger holds every trigger type's configuration and the type that runs
// (ADR-TRIGGER-STATE): the owner programs the running type's stages, the
// others wait as they were left.
type State struct {
	Channels []hal.Channel // at the probe tip
	TimeDivS float64
	DelayS   float64 // the screen centre's time after the trigger
	Depth    int     // samples per channel; 0: chosen by the scope
	Mode     hal.AcqMode
	Averages int // the average's frame count (Mode Average)
	EresLen  int // the high-resolution filter's length (Mode HighRes)
	ETS      bool
	Trigger  Trigger
	Sweep    hal.Sweep
	Single   bool
	Running  bool
}

// Clone is s with its own Channels and trigger stages.
// TRLC-LINKS: REQ-DSO-008
func (s State) Clone() State {
	s.Channels = append([]hal.Channel(nil), s.Channels...)
	s.Trigger = s.Trigger.Clone()
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
		s.Averages == o.Averages && s.EresLen == o.EresLen && s.ETS == o.ETS &&
		s.Trigger.Equal(o.Trigger) && s.Sweep == o.Sweep && s.Single == o.Single && s.Running == o.Running
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

// TriggerSpec is the running trigger type of state s in the HAL's terms,
// at the sweep the board runs.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (s State) TriggerSpec() hal.TriggerSpec { return s.Trigger.Spec(s.BoardSweep()) }

// BoardSweep is the sweep the board runs: a single shot armed in NORMAL is
// the board's single sweep; one armed in AUTO free-runs until a triggered
// frame ends it. A state written whole with Sweep Single (not through a
// Model) runs the single sweep.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (s State) BoardSweep() hal.Sweep {
	if s.Single && s.Sweep == hal.NormalSweep {
		return hal.Single
	}
	return s.Sweep
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
