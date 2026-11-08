// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// A controller's trigger and acquisition settings are clamped as the
// SDS1000CML+ clamps them, on every scope.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-012
func TestControllerClamps(t *testing.T) {
	m := New(State{Channels: make([]hal.Channel, 2), Running: true, Sweep: hal.Auto}, nil)
	for in, want := range map[float64]float64{-1: 0, math.NaN(): 0, 3: 3, 30: 10, math.Inf(1): 10} {
		if got := m.SetHoldoff(in); got != want || m.State().Trigger.Active().HoldoffS != want {
			t.Errorf("holdoff %g: %g", in, got)
		}
	}
	m.SetAvgCount(0)
	if a := m.State().Averages; a != 1 {
		t.Errorf("averages %d", a)
	}
	m.SetAvgCount(999)
	m.SetEresLen(10)
	if s := m.State(); s.Averages != 256 || s.EresLen != 9 {
		t.Errorf("averages %d eres %d", s.Averages, s.EresLen)
	}
	m.SetPulseParams(-1, 50, 10, 7)
	m.SetSlopeParams(2, .2, 0, 30, 4)
	m.SetVideoParams(5, -1, false)
	q := m.State().Qual
	if q.PulseLvl != 0 || q.PulseMinNs != 10 || q.PulseMaxNs != 50 || q.PulseCond != 3 ||
		q.SlopeLo != .2 || q.SlopeHi != 1 || q.SlopeMaxNs != 30 || q.SlopeCond != 0 || q.VideoStd != 0 || q.VideoLine != 0 {
		t.Errorf("qualifiers %+v", q)
	}
	m.SetTrigType(99)
	if ty := m.State().Trigger.Type; ty != TrigEdge {
		t.Errorf("type %v", ty)
	}
}
