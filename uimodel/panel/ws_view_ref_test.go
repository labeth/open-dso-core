// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import "github.com/labeth/open-dso-core/uimodel/wsview"

// View is the UI snapshot alone.
// TRLC-LINKS: REQ-DSO-211
func (c *Controller) View() wsview.View { return c.view(c.snap()) }
