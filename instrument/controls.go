// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

// Controls is the instrument's settings surface as every controller drives
// it: the front panel's UI model, SCPI, the settings file and the web server
// all stage through it (ADR-HAL-INSTRUMENT-STATE). Setters stage and never
// touch hardware; Snapshot is the state they resync from. *Model implements
// it over the wanted state for a scope whose owner loop is the core's
// (HALCycle); a scope whose engine still keeps its own state implements it
// over that engine.
//
// Run control is three facts (ADR-RUN-CONTROL): running, the trigger mode
// (AUTO or NORM) and a single shot armed. SetSingle arms a shot in NORM and
// keeps the mode it replaced, which the next RUN brings back; SetNorm while
// a shot is armed changes the mode the shot waits in and leaves it armed
// (that choice is the user's: nothing is brought back); SetRunning, either
// way, cancels an armed shot.
type Controls interface {
	Snapshot() Stats

	// SetTdiv stages the timebase nearest tdivS on the scope's ladder; ok
	// is false when the ladder has no such step.
	SetTdiv(tdivS float64) (tb Timebase, ok bool)
	SetTrigPosFrac(frac float64) // the trigger's place across the screen, 0..1
	SetMemDepth(samples int) int

	SetRunning(on bool)
	SetNorm(on bool)
	SetSingle()

	SetTrigSource(ch int)
	SetTrigSlope(rising bool)
	SetTrigType(t int) // 0 edge, 1 pulse, 2 slope, 3 video
	SetHoldoff(sec float64) float64
	SetPulseParams(lvlFrac, wMinNs, wMaxNs float64, cond int)
	SetSlopeParams(loFrac, hiFrac, tMinNs, tMaxNs float64, cond int)
	SetVideoParams(std, line int, neg bool)

	SetAcqMode(m int) // 0 normal, 1 average, 2 ERES, 3 peak, 4 precision
	SetAvgCount(n int)
	SetEresLen(l int)
	SetETS(on bool)
}

// Timebase is a timebase step as the scope resolved it: its seconds a
// division and the interval it captures samples at.
type Timebase struct {
	TdivS   float64
	SampleS float64
}
