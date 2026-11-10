// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// readCounter runs only on the acquisition owner, including while stopped
// or waiting for NORMAL. Throttle MMIO polling independently of frame rate.
// TRLC-LINKS: REQ-DSO-009, REQ-DSO-008
func (c *HALCycle) readCounter(now time.Time) {
	if c.counter == nil || c.gen == 0 || now.Before(c.counterAt) {
		return
	}
	c.counterAt = now.Add(250 * time.Millisecond)
	reading, err := c.counter.Read()
	if err != nil {
		reading = sample.Counters{}
	}
	source := -1
	if ctr, ok := c.counter.(hal.SourcedCounter); ok {
		src := ctr.Source()
		if src.Kind == hal.Analog && src.Index >= 0 && src.Index < c.b.Profile().Channels {
			source = src.Index
		}
	}
	c.inst.SetHardwareCounter(source, reading)
}
