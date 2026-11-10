// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"fmt"
	"math"

	"github.com/labeth/open-dso-core/sample"
)

// CounterReading is the trigger source's dedicated hardware frequency
// counter, independent of frequency estimated from a displayed waveform.
type CounterReading struct {
	Source int     `json:"source"`
	FreqHz float64 `json:"freq_hz"`
	Valid  bool    `json:"valid"`
}

// SetHardwareCounter publishes the owner's latest counter reading. Invalid
// readings and read failures clear the value instead of showing a stale rate.
// TRLC-LINKS: REQ-DSO-009
func (m *Model) SetHardwareCounter(source int, reading sample.Counters) {
	if !reading.Valid || reading.FreqHz < 0 || math.IsNaN(reading.FreqHz) || math.IsInf(reading.FreqHz, 0) {
		reading.Valid, reading.FreqHz = false, 0
	}
	m.mu.Lock()
	m.hardwareCounter = CounterReading{Source: source, FreqHz: reading.FreqHz, Valid: reading.Valid}
	m.haveCounter = true
	m.mu.Unlock()
}

// Text labels the hardware counter separately from waveform measurements.
// Displayed digits do not establish the reference clock's calibration.
// TRLC-LINKS: REQ-DSO-009
func (r CounterReading) Text() string {
	label := "Counter "
	if r.Source >= 0 {
		label = fmt.Sprintf("Counter C%d ", r.Source+1)
	}
	if !r.Valid {
		return label + "--"
	}
	scale, unit := 1.0, "Hz"
	for _, u := range []struct {
		scale float64
		unit  string
	}{{1e9, "GHz"}, {1e6, "MHz"}, {1e3, "kHz"}} {
		if r.FreqHz >= u.scale {
			scale, unit = u.scale, u.unit
			break
		}
	}
	return label + fmt.Sprintf("%.7g %s", r.FreqHz/scale, unit)
}
