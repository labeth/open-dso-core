// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import "time"

// On a board whose acquisition shares one core with the display
// (Budget.QuietAcquire), as the SDS1000CML+'s does, the owner gives the
// display its share: the display's governor asks each capture to take at
// least its pace while the pictures fall behind (Pacing's display share),
// and a consumer drawing a picture or serialising a frame holds the quiet
// lock, which the owner takes while it reads the record off the board, so
// their work runs in the loop's dead time. Elsewhere the board paces itself
// and nothing waits.

// SetDisplayPace asks each live capture to take at least d from the last
// (0: as fast as the board gives them).
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-008
func (c *HALCycle) SetDisplayPace(d time.Duration) { c.pace.Store(int64(max(d, 0))) }

// QuietRLock holds the owner's record reads off while a consumer's work
// runs; QuietRUnlock ends it.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-001
func (c *HALCycle) QuietRLock() { c.quiet.RLock() }

// QuietRUnlock ends a QuietRLock.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-001
func (c *HALCycle) QuietRUnlock() { c.quiet.RUnlock() }

// quietRead takes the quiet lock for a record read on a board that shares
// its core; the function returned lets it go.
// TRLC-LINKS: REQ-DSO-001
func (c *HALCycle) quietRead() func() {
	if !c.quietAcq {
		return func() {}
	}
	c.quiet.Lock()
	return c.quiet.Unlock
}
