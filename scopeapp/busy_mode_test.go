// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"bytes"
	"testing"

	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
)

// While the engine loads (a full record, an image) the display draws the
// loading indicator alone, in the status slot's rows; AUTO's search draws
// the whole screen a few times a second; a quick read (inside the
// indicator's grace) and the stream view enter no busy mode.
// TRLC-LINKS: REQ-DSO-168
func TestLCDBusyMode(t *testing.T) {
	for _, c := range []struct {
		st                 instrument.Stats
		autoset, streaming bool
		want               int
	}{
		{instrument.Stats{}, false, false, busyNone},
		{instrument.Stats{Busy: "reading", BusyMs: 100}, false, false, busyNone},
		{instrument.Stats{Busy: "reading", BusyMs: 400, BusyPct: 30}, false, false, busyLoad},
		{instrument.Stats{Busy: "loading qualify image", BusyMs: 900}, true, false, busyLoad},
		{instrument.Stats{}, true, false, busyAutoset},
		{instrument.Stats{Busy: "reading", BusyMs: 400}, true, true, busyNone},
	} {
		if got := busyMode(c.st, c.autoset, c.streaming); got != c.want {
			t.Errorf("%q autoset %v streaming %v: mode %d, want %d", c.st.Busy, c.autoset, c.streaming, got, c.want)
		}
	}
	back := render.NewImage565(&render.Metrics800x480)
	for i := range back.Pix {
		back.Pix[i] = 0x5A
	}
	before := append([]byte(nil), back.Pix...)
	if busyDraw(busyAutoset, instrument.Stats{}, back, "") || !bytes.Equal(back.Pix, before) {
		t.Fatal("AUTO's busy mode drew the indicator")
	}
	if !busyDraw(busyLoad, instrument.Stats{Busy: "reading", BusyMs: 400, BusyPct: 30}, back, "") {
		t.Fatal("the loading mode drew nothing")
	}
	slot := render.StatusSlotRows * render.Metrics800x480.W * 2
	if bytes.Equal(back.Pix[:slot], before[:slot]) {
		t.Fatal("the indicator is not in the status slot's rows")
	}
	if !bytes.Equal(back.Pix[slot:], before[slot:]) {
		t.Fatal("the loading mode drew outside the status slot")
	}
}
