// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// fakeBoot is a board's boot target in memory.
type fakeBoot struct {
	next, why string
}

func (f *fakeBoot) Next() string { return f.next }

func (f *fakeBoot) Set(target, why string) error {
	f.next, f.why = target, why
	return nil
}

// sysRowIDs are the SYS tab's rows as the web's sheets list them.
func sysRowIDs(c *Controller) []string {
	for _, t := range c.Sheets().Tabs {
		if t.ID == "sys" {
			var ids []string
			for _, r := range t.Rows {
				ids = append(ids, r.ID)
			}
			return ids
		}
	}
	return nil
}

// The SYS sheet's Next boot row is there only on a scope whose board
// offers a boot target; it shows and sets the board's value, which SCPI
// and the web set too, and Default setup leaves it alone.
// TRLC-LINKS: REQ-DSO-297
func TestBootTargetRow(t *testing.T) {
	plain, _, _ := newC(t)
	before := sysRowIDs(plain)
	if strings.Contains(strings.Join(before, " "), "sys.boot") {
		t.Fatalf("a scope without a boot target has the row: %v", before)
	}
	if res := plain.SheetSet("sys.boot", nil, "Vendor firmware"); res.OK {
		t.Fatalf("the row set on a scope without a boot target: %+v", res)
	}

	c, _, _ := newC(t)
	b := &fakeBoot{next: hal.BootOpen}
	c.SetBootTarget(b)
	after := sysRowIDs(c)
	if len(after) != len(before)+1 || after[len(after)-1] != "sys.boot" {
		t.Fatalf("SYS rows with a boot target %v, without %v", after, before)
	}
	if got := rowText(c, "sys.boot"); got != "Open firmware" {
		t.Fatalf("the row shows %q", got)
	}
	if res := c.SheetSet("sys.boot", nil, "Vendor firmware"); !res.OK || b.next != hal.BootVendor || b.why == "" {
		t.Fatalf("set vendor: %+v, the board's %q (%q)", res, b.next, b.why)
	}
	b.next = hal.BootOpen // as SCPI or the web would set it
	if got := rowText(c, "sys.boot"); got != "Open firmware" {
		t.Fatalf("the row after another surface set it: %q", got)
	}
	b.next = hal.BootVendor
	if res := c.SheetAction("sys.default", ""); !res.OK || b.next != hal.BootVendor {
		t.Fatalf("Default setup: %+v, the board's %q", res, b.next)
	}
	if got := sysRowIDs(c); len(got) != len(after) {
		t.Fatalf("SYS rows after Default setup %v", got)
	}
}
