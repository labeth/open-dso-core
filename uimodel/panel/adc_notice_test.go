// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"testing"
	"time"

	"github.com/labeth/open-dso-core/instrument"
)

// Converters that stay stuck after the engine's recoveries show a notice
// over the plot until they convert again; a tool's notice shows first.
// TRLC-LINKS: REQ-DSO-231
func TestADCFaultNotice(t *testing.T) {
	c, eng, _ := newC(t)
	if n := c.View().Notice; n != "" {
		t.Fatalf("notice %q with healthy converters", n)
	}
	fault := "ADC fault: converter E2 not converting - restart the scope"
	eng.set(func(st *instrument.Stats) { st.ADCFault = fault })
	if n := c.View().Notice; n != fault {
		t.Errorf("notice %q", n)
	}
	c.toast("AUTO: done", time.Second)
	if n := c.View().Notice; n != "AUTO: done" {
		t.Errorf("a tool's notice: %q", n)
	}
	c.mu.Lock()
	c.ui.noticeUnt = time.Time{}
	c.mu.Unlock()
	eng.set(func(st *instrument.Stats) { st.ADCFault = "" })
	if n := c.View().Notice; n != "" {
		t.Errorf("notice %q after the converters came back", n)
	}
}
