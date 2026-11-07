// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

// snapProto reads the decode protocol.
// TRLC-LINKS: REQ-DSO-214
func (c *Controller) snapProto() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.decProto
}
