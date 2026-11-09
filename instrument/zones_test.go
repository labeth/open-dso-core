package instrument

import (
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// The zones are kept in display codes and given to the board in volts at
// the channel's V/div and offset; the mode is off or trigger.
// TRLC-LINKS: REQ-DSO-014
func TestZonesInTheState(t *testing.T) {
	m := New(State{Channels: []hal.Channel{{VoltsDiv: 0.5, OffsetV: 0.25}, {VoltsDiv: 1}}}, nil)
	m.SetZones([]Zone{{DtLoS: -1e-6, DtHiS: 2e-6, CodeLo: 103, CodeHi: 153, Ch: 0}, {CodeLo: 128, CodeHi: 128, Ch: 5}})
	m.SetZoneMode(7)
	if s := m.State(); s.ZoneMode != ZoneOff || s.ZonesArmed() {
		t.Fatalf("mode %d", s.ZoneMode)
	}
	m.SetZoneMode(ZoneTrigger)
	s := m.State()
	if !s.ZonesArmed() || len(m.Zones()) != 2 {
		t.Fatal("zones not armed")
	}
	z := s.HALZones()
	if len(z) != 1 || z[0].Low != -0.75 || z[0].High != 0.25 || z[0].FromS != -1e-6 || z[0].ToS != 2e-6 || z[0].Source.Index != 0 {
		t.Fatalf("%+v", z)
	}
	if st := m.Snapshot(); st.ZoneMode != ZoneTrigger || st.ZoneCount != 2 {
		t.Fatalf("%+v", st)
	}
}
