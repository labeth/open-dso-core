// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// A trigger source change is heard once it is staged.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-098
func TestOnTrigSourceHearsTheSource(t *testing.T) {
	m := New(State{Channels: make([]hal.Channel, 2)}, nil)
	var heard []int
	m.OnTrigSource(func() { heard = append(heard, m.State().Trigger.Source().Index) })
	m.SetTrigSource(1)
	m.SetTrigSource(0)
	if len(heard) != 2 || heard[0] != 1 || heard[1] != 0 {
		t.Fatalf("heard %v, want [1 0]", heard)
	}
}

// A type change is projected inside the change by the hooks a board
// registered (OnTrigType), with the type before; a slope the running type
// has no setting for reaches its hooks (OnTrigSlope); a model without
// hooks keeps every type's own settings.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func TestTypeAndSlopeHooks(t *testing.T) {
	plain := New(State{Channels: make([]hal.Channel, 2)}, nil)
	plain.SetTrigSource(1)
	plain.SetTrigType(int(TrigPulse))
	if plain.State().Trigger.Source().Index != 0 {
		t.Fatal("a model without hooks carried the source over")
	}
	m := New(State{Channels: make([]hal.Channel, 2)}, nil)
	var froms []TrigType
	m.OnTrigType(func(s *State, from TrigType) {
		froms = append(froms, from)
		before := s.Trigger
		before.Type = from
		_ = s.Trigger.SetSource(before.Source(), len(s.Channels))
	})
	var slopes []hal.Slope
	m.OnTrigSlope(func(_ *State, sl hal.Slope) { slopes = append(slopes, sl) })
	m.SetTrigSource(1)
	m.SetTrigType(int(TrigPulse))
	m.SetTrigType(int(TrigPulse)) // no change: no projection
	if len(froms) != 1 || froms[0] != TrigEdge || m.State().Trigger.Source().Index != 1 {
		t.Fatalf("projected from %v, source %d", froms, m.State().Trigger.Source().Index)
	}
	m.SetTrigSlope(false) // pulse has a slope setting
	m.SetTrigType(int(TrigVideo))
	m.SetTrigSlope(false)
	if len(slopes) != 1 || slopes[0] != hal.Falling {
		t.Fatalf("slopes heard %v", slopes)
	}
}

// A long read's progress shows on the loading indicator until the next
// thing the owner is busy with.
// TRLC-LINKS: REQ-DSO-168
func TestBusyProgress(t *testing.T) {
	m := New(State{Channels: make([]hal.Channel, 2)}, nil)
	m.SetBusy("reading")
	if st := m.Snapshot(); st.BusyPct != -1 {
		t.Fatalf("an unknown progress reads %v", st.BusyPct)
	}
	m.SetBusyProgress(1, 4)
	if st := m.Snapshot(); st.BusyPct != 25 || st.Busy != "reading" {
		t.Fatalf("progress %v %q", st.BusyPct, st.Busy)
	}
	m.SetBusy("processing")
	if st := m.Snapshot(); st.BusyPct != -1 {
		t.Fatalf("the next stretch kept the read's progress %v", st.BusyPct)
	}
}
