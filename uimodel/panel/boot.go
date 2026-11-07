// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// The Next boot row's values.
const (
	bootOpen   = 0
	bootVendor = 1
)

// SetBootTarget gives the SYS sheet the board's next-boot choice: its Next
// boot row shows on the screen, the web's sheets and the remote, and sets
// the same value SCPI and the web's /api/boot set. Call it before Run; a
// board without one (nil) has no such row.
// TRLC-LINKS: REQ-DSO-297
func (c *Controller) SetBootTarget(t hal.BootTarget) {
	c.mu.Lock()
	c.bootTarget = t
	c.ui.boot = t != nil
	c.mu.Unlock()
}

// bootNextLocked is the next power-up's row value; c.mu is held.
// TRLC-LINKS: REQ-DSO-297
func (c *Controller) bootNextLocked() int {
	if c.bootTarget != nil && c.bootTarget.Next() == hal.BootVendor {
		return bootVendor
	}
	return bootOpen
}

// shape is which rows this scope's sheets have.
// TRLC-LINKS: REQ-DSO-212
func (c *Controller) shape() rowShape { return rowShape{nch: c.nch, boot: c.ui.boot} }

// shape is which rows the snapshot's scope has.
// TRLC-LINKS: REQ-DSO-212
func (s *sv) shape() rowShape { return rowShape{nch: s.nch, boot: s.boot} }

// shape is which rows the UI state's scope has.
// TRLC-LINKS: REQ-DSO-212
func (u *uiState) shape() rowShape { return rowShape{nch: u.nch, boot: u.boot} }

// bootRow is the SYS sheet's Next boot row, on a scope whose board offers
// a boot target. It has no factory value: Default setup leaves it alone,
// and it is not part of a saved setup (the choice is the board's file).
// TRLC-LINKS: REQ-DSO-297
func bootRow() *row {
	return &row{
		id: "sys.boot", label: "Next boot", kind: wsview.Enum, boot: true,
		help: "What the next power-up runs: this firmware, or the vendor firmware (nothing is uninstalled; choose Open to come back).",
		opts: staticOpts([]opt{{bootOpen, "Open firmware"}, {bootVendor, "Vendor firmware"}}),
		get:  func(s *sv) int { return s.bootNext },
		set: func(c *Controller, v int) error {
			c.mu.Lock()
			t := c.bootTarget
			c.mu.Unlock()
			if t == nil {
				return hal.Refuse("next boot", "this scope starts no vendor firmware")
			}
			target := hal.BootOpen
			if v == bootVendor {
				target = hal.BootVendor
			}
			return t.Set(target, "asked through the SYS sheet")
		},
	}
}
