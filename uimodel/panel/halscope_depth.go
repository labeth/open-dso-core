// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

// MemDepth is the record depth the instrument state asks for (0: the
// board's own), for the setup's mem_depth.
// TRLC-LINKS: REQ-DSO-089
func (e halEngine) MemDepth() int { return e.Model.State().Depth }
