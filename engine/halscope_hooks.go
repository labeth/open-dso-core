// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import "time"

// SincePublish is how long ago the cycle published its newest frame; zero
// before the first. A running NORMAL acquisition that has published nothing
// for a while is waiting for a trigger, whatever the held frame's own state.
// ANY.
// TRLC-LINKS: REQ-DSO-011
func (c *HALCycle) SincePublish() time.Duration {
	c.mu.Lock()
	at := c.latestAt
	c.mu.Unlock()
	if at.IsZero() {
		return 0
	}
	return time.Since(at)
}
