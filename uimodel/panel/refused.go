// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"time"
)

// refusedFor is how long a refusal the board answered stays on the screen.
const refusedFor = 2500 * time.Millisecond

// Refused shows a refusal the board answered a commit with (the instrument
// state's OnRefused listener). The board refuses after a row's setter has
// returned, so without it a setting the board did not take would go back
// with nothing said; the reason is shown as a row's own refusal is.
// TRLC-LINKS: REQ-DSO-212, REQ-DSO-1001
func (c *Controller) Refused(err error) {
	if err == nil {
		return
	}
	c.toast("Not taken: "+err.Error(), refusedFor)
}
