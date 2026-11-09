// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
)

// CycleSource is what the UI model asks the owner loop's cycle
// (engine.HALCycle) beside the instrument state.
type CycleSource interface {
	// SincePublish is how long ago the newest frame was published; zero
	// before the first.
	SincePublish() time.Duration
}

// WithCycle gives the UI model the owner loop's cycle: how long ago it
// published (the Run/Stop lamp's wait for a trigger).
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-011
func (s *HALScope) WithCycle(c CycleSource) *HALScope {
	s.cycle = c
	return s
}

// ForceTrigger completes the capture armed now without a trigger, once:
// the FORCE key, through the instrument state's force.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-212
func (e halEngine) ForceTrigger() { _ = e.Force() }

// SincePublish is how long ago the owner published its newest frame (zero
// before the first, or without a cycle): a running NORMAL acquisition that
// published nothing for a while is waiting for a trigger.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-135
func (e halEngine) SincePublish() time.Duration {
	if e.cycle == nil {
		return 0
	}
	return e.cycle.SincePublish()
}

// ChannelState is channel ch's offset at the BNC, coupling, probe factor
// and bandwidth limit, read together from one instrument state.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-089
func (a halAnalog) ChannelState(ch int) (offV float64, cpl int, probe float64, bwl bool) {
	c, _ := a.s.channel(ch)
	return c.OffsetV / probeOf(c), int(c.Coupling), probeOf(c), c.BWLimit > 0
}

// fillConverterHealth puts what board b's converter checks found into st:
// the converter fault the screen notices, the recoveries and the last
// check's time. A board without the checks leaves st as it is.
// TRLC-LINKS: REQ-DSO-231, REQ-DSO-135
func fillConverterHealth(b hal.Board, st *instrument.Stats) {
	if b == nil {
		return
	}
	cm, ok := b.Platform().(hal.ConverterMonitor)
	if !ok {
		return
	}
	h := cm.ConverterHealth()
	st.ADCFault, st.ADCRecoveries, st.ADCRecovery, st.ADCCheckMs = h.Fault, h.Recoveries, h.Recovery, h.CheckMs
}
