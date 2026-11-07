// ENGMODEL-OWNER-UNIT: FU-DSO-SETTINGS

package settings

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/labeth/open-dso-core/hal"
)

// Slots is numbered setup memory (SCPI *SAV/*RCL, the panel's SAVE/RECALL)
// in a scope's setups store, so saved setups survive a restart. Collect and
// Apply are the same paths the boot restore and the saver use. Key names
// slot n in the store (nil: its number, "1"); a named setup (SCPI STPN) is
// stored under its file name where the store keeps names.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
type Slots struct {
	KV      hal.KV
	Key     func(n int) string
	Collect func() Settings
	Apply   func(Settings)
}

// SlotFile is the SDS1000CML+'s slot n: setup03.json beside the settings
// file.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
func SlotFile(n int) string {
	d := strconv.Itoa(n)
	if n >= 0 && n < 10 {
		d = "0" + d
	}
	return "setup" + d + ".json"
}

// key is slot n's key.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
func (s Slots) key(n int) string {
	if s.Key == nil {
		return strconv.Itoa(n)
	}
	return s.Key(n)
}

// SaveSetup stores the current setup in slot n.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
func (s Slots) SaveSetup(n int) error {
	if s.Collect == nil {
		return errors.New("setup memory unavailable")
	}
	return Save(s.KV, s.key(n), s.setup())
}

// RecallSetup applies the setup stored in slot n; an empty slot is an error.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
func (s Slots) RecallSetup(n int) error {
	st, ok := Load(s.KV, s.key(n), nil)
	if !ok {
		return fmt.Errorf("setup slot %d is empty", n)
	}
	if s.Apply == nil {
		return errors.New("setup memory unavailable")
	}
	st.Screen = Screen{} // the screen saver is the instrument's
	s.Apply(st)
	return nil
}

// CurrentSetup is the setup a save would write now (the panel's Save/Recall
// lamp compares it with the one last saved or recalled).
// TRLC-LINKS: REQ-DSO-136
func (s Slots) CurrentSetup() Settings {
	if s.Collect == nil {
		return Settings{}
	}
	return s.setup()
}

// setup is the current setup as a setup memory keeps it: without the
// instrument's screen saver.
// TRLC-LINKS: REQ-DSO-221
func (s Slots) setup() Settings {
	st := s.Collect()
	st.Screen = Screen{}
	return st
}

// PeekSetup reads slot n without applying it (the panel's FILE rows show
// what each slot holds).
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
func (s Slots) PeekSetup(n int) (Settings, bool) {
	return Load(s.KV, s.key(n), nil)
}

// SaveSetupFile stores the current setup as a named file (SCPI STPN). The
// name is a bare file name the caller has validated.
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
func (s Slots) SaveSetupFile(name string) error {
	if s.Collect == nil || name != filepath.Base(name) {
		return errors.New("setup file unavailable")
	}
	return Save(s.KV, name, s.setup())
}

// RecallSetupFile applies a setup file stored by SaveSetupFile (SCPI RCPN).
// TRLC-LINKS: REQ-DSO-072, REQ-DSO-089
func (s Slots) RecallSetupFile(name string) error {
	if name != filepath.Base(name) {
		return errors.New("setup file unavailable")
	}
	st, ok := Load(s.KV, name, nil)
	if !ok {
		return fmt.Errorf("no setup file %s", name)
	}
	if s.Apply == nil {
		return errors.New("setup memory unavailable")
	}
	st.Screen = Screen{} // the screen saver is the instrument's
	s.Apply(st)
	return nil
}
