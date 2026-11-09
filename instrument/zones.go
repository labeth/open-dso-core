// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"slices"

	"github.com/labeth/open-dso-core/hal"
)

// Zone is one qualification rectangle of the zone trigger. Times are
// seconds relative to the trigger (portable across timebases); codes are
// the 8-bit display view's (0..255, ZoneCodeMid at the centre,
// ZoneCodesPerDiv a division), so a zone stays where it was drawn on the
// screen when the channel's V/div or offset changes.
// TRLC-LINKS: REQ-DSO-014
type Zone struct {
	DtLoS, DtHiS   float64
	CodeLo, CodeHi int
	Avoid          bool // true: the frame must NOT enter the zone
	Ch             int  // 0 = C1, 1 = C2
}

// The zone trigger's modes (State.ZoneMode).
const (
	ZoneOff     = 0
	ZoneTrigger = 1
)

// The display view the zones' codes are on: the screen's centre and 25
// codes a division.
const (
	ZoneCodeMid     = 128
	ZoneCodesPerDiv = 25
)

// SetZones installs the zone rectangles (none: nothing to test; the mode
// stays).
// TRLC-LINKS: REQ-DSO-014
func (m *Model) SetZones(z []Zone) {
	z = slices.Clone(z)
	m.set(func(s *State) { s.Zones = z })
}

// SetZoneMode arms the zone trigger (ZoneTrigger) or turns it off; any
// other mode is off.
// TRLC-LINKS: REQ-DSO-014
func (m *Model) SetZoneMode(mode int) {
	if mode != ZoneTrigger {
		mode = ZoneOff
	}
	m.set(func(s *State) { s.ZoneMode = mode })
}

// Zones is a copy of the installed zones.
// TRLC-LINKS: REQ-DSO-014
func (m *Model) Zones() []Zone { return m.State().Zones }

// ZonesArmed reports a zone trigger with zones to test in state s.
// TRLC-LINKS: REQ-DSO-014
func (s State) ZonesArmed() bool { return s.ZoneMode == ZoneTrigger && len(s.Zones) > 0 }

// HALZones is state s's zones in the HAL's terms: each zone's codes in
// volts at its channel's tip at the channel's V/div and offset, its times
// as they are. A zone on a channel the state has not is left out.
// TRLC-LINKS: REQ-DSO-014
func (s State) HALZones() []hal.Zone {
	out := make([]hal.Zone, 0, len(s.Zones))
	for _, z := range s.Zones {
		if z.Ch < 0 || z.Ch >= len(s.Channels) {
			continue
		}
		c := s.Channels[z.Ch]
		volts := func(code int) float64 {
			return float64(code-ZoneCodeMid)*(c.VoltsDiv/ZoneCodesPerDiv) - c.OffsetV
		}
		out = append(out, hal.Zone{
			Source: hal.Source{Kind: hal.Analog, Index: z.Ch}, FromS: z.DtLoS, ToS: z.DtHiS,
			Low: volts(z.CodeLo), High: volts(z.CodeHi), Avoid: z.Avoid,
		})
	}
	return out
}
