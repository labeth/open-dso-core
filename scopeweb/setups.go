// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// setupSlots is the slot range the web offers (the SCPI *SAV range).
const setupSlots = 20

// Setups is the setup store: the scope's own, or the board's setup slots
// holding the instrument state.
// TRLC-LINKS: REQ-DSO-219
func (s *Scope) Setups() (webserver.Setups, bool) {
	if s.cfg.Setups != nil {
		return s.cfg.Setups, true
	}
	if s.cfg.Board == nil || s.cfg.Instrument == nil {
		return nil, false
	}
	kv, ok := s.cfg.Board.Storage().Setups()
	if !ok {
		return nil, false
	}
	return stateSetups{m: s.cfg.Instrument, kv: kv}, true
}

// stateSetups keeps whole instrument states in the board's setup slots.
type stateSetups struct {
	m  *instrument.Model
	kv hal.KV
}

// setupKey is slot n's key in the store.
// TRLC-LINKS: REQ-DSO-219
func setupKey(n int) string { return fmt.Sprintf("setup-%02d", n) }

// SaveSetup stores the wanted state in slot n.
// TRLC-LINKS: REQ-DSO-219
func (st stateSetups) SaveSetup(n int) error {
	b, err := json.Marshal(st.m.State())
	if err != nil {
		return err
	}
	return st.kv.Put(setupKey(n), b)
}

// RecallSetup stages the state slot n holds; one of another channel count
// is refused.
// TRLC-LINKS: REQ-DSO-219
func (st stateSetups) RecallSetup(n int) error {
	b, ok := st.kv.Get(setupKey(n))
	if !ok {
		return fmt.Errorf("setup %d is empty", n)
	}
	var saved instrument.State
	if err := json.Unmarshal(b, &saved); err != nil {
		return fmt.Errorf("setup %d is unreadable: %w", n, err)
	}
	return st.m.Change(func(cur *instrument.State) error {
		if len(saved.Channels) != len(cur.Channels) {
			return errors.New("the setup is of a scope with another number of channels")
		}
		*cur = saved
		return nil
	})
}

// SetupSlots says what each slot holds: the timebase and each channel's
// V/div.
// TRLC-LINKS: REQ-DSO-219
func (st stateSetups) SetupSlots() []wire.SetupSlot {
	out := make([]wire.SetupSlot, 0, setupSlots)
	for n := 1; n <= setupSlots; n++ {
		slot := wire.SetupSlot{Slot: n}
		if b, ok := st.kv.Get(setupKey(n)); ok {
			var saved instrument.State
			if json.Unmarshal(b, &saved) == nil {
				slot.Used = true
				parts := []string{fmt.Sprintf("%gs/div", saved.TimeDivS)}
				for i, ch := range saved.Channels {
					if ch.On {
						parts = append(parts, fmt.Sprintf("C%d %gV", i+1, ch.VoltsDiv))
					}
				}
				slot.Summary = strings.Join(parts, " ")
			}
		}
		out = append(out, slot)
	}
	return out
}
