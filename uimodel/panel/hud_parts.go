// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
)

// The HUD's optional parts: an engine, front end or levels that has the
// method gives the display what it shows; any other leaves the HUD's field
// as it is.
type (
	// hudPublishAge is how long ago a frame was published (the run state's
	// wait for a trigger the hardware decides).
	hudPublishAge interface{ SincePublish() time.Duration }
	// hudSerialConfig is the serial trigger's bus and match.
	hudSerialConfig interface{ SerialConfig() engine.SerialParams }
	// hudZones are the zones installed for the zone trigger.
	hudZones interface{ Zones() []engine.Zone }
	// hudMaskEnvelope is the mask installed for the mask test.
	hudMaskEnvelope interface{ MaskEnvelope() *engine.Mask }
	// hudBode is the frequency-response sweep the BODE view arms.
	hudBode interface {
		SetBodeMode(on bool, refCh, dutCh int)
		BodePoints() []engine.BodePoint
	}
	// hudZoom is a front end's display zoom of its analog range.
	hudZoom interface{ Zoom(ch int) int }
	// hudLevelMarker is a level kept as a comparator's code: its volts at
	// the BNC as the comparator's calibration gives them, ok false while
	// the comparator was never set (no marker is drawn).
	hudLevelMarker interface {
		TrigLevelBNCIn(st instrument.Stats, src int) (v float64, ok bool)
	}
)

// hudWaitAfter is how long a running trigger the hardware decides may go
// without a published frame before the screen says it waits: a second, or
// two and a half screen spans.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-168
func hudWaitAfter(tdivS float64) time.Duration {
	return time.Duration(math.Max(1, 25*tdivS) * float64(time.Second))
}

// fillHUDWait says what a running capture waits for: in NORMAL, and in AUTO
// a serial match, zones or a qualifier, the hardware fires on the signal
// only, so past hudWaitAfter without a published frame the held frame's
// TRIG'd state no longer describes now.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-168
func (c *Controller) fillHUDWait(hud *render.HUD, st instrument.Stats) {
	pa, ok := c.eng.(hudPublishAge)
	if !ok || !st.Running {
		return
	}
	since := pa.SincePublish()
	hud.WaitS = since.Seconds()
	decided := st.Norm || hud.SerialArmed || hud.ZoneTrig || hud.TrigType != 0
	hud.Waiting = decided && since > hudWaitAfter(st.TdivS)
}

// fillHUDLevel is the trigger level's marker (divisions on the source's
// scale and offset) and its readout (volts at the probe tip). A level kept
// as a comparator's code is drawn from its volts at the BNC, and not at all
// while the comparator was never set.
// TRLC-LINKS: REQ-DSO-098, REQ-DSO-168
func (c *Controller) fillHUDLevel(hud *render.HUD, st instrument.Stats) {
	src := st.TrigSource
	if src < 0 || src >= hud.NumChans || hud.Ch[src].VdivV <= 0 {
		return
	}
	k := hud.Ch[src]
	probe := max(k.Probe, 1)
	if lm, ok := c.lv.(hudLevelMarker); ok {
		v, set := lm.TrigLevelBNCIn(st, src)
		if set {
			hud.TrigLvlDiv = (v + k.OffV) / k.VdivV
			hud.TrigLvlV = v * probe
		}
		return
	}
	v := c.lv.TrigLevelOrIn(st, 0, src)
	hud.TrigLvlV = v
	hud.TrigLvlDiv = (v/probe + k.OffV) / k.VdivV
}

// fillHUDOverlays are the zones and the mask the test draws, the serial
// trigger's match and the display invert of each channel.
// TRLC-LINKS: REQ-DSO-014, REQ-DSO-168, REQ-DSO-212
func (c *Controller) fillHUDOverlays(hud *render.HUD, st instrument.Stats) {
	if sc, ok := c.eng.(hudSerialConfig); ok {
		hud.SerialMatch = SerialMatchText(sc.SerialConfig())
	}
	c.mu.Lock()
	inv := c.inverter
	c.mu.Unlock()
	if inv != nil {
		for ch := range min(hud.NumChans, len(hud.Ch)) {
			hud.Ch[ch].Inv = inv.IsInverted(ch)
		}
	}
	hud.ZoneMode, hud.ZoneSkip = st.ZoneMode, st.ZoneSkip
	if zs, ok := c.eng.(hudZones); ok && st.ZoneCount > 0 {
		z := zs.Zones()
		hud.Zones = make([]render.Zone, len(z))
		for i := range z {
			hud.Zones[i] = render.Zone(z[i])
		}
	}
	if me, ok := c.eng.(hudMaskEnvelope); ok && st.MaskMode > 0 && st.MaskSet {
		if m := me.MaskEnvelope(); m != nil {
			hud.MaskLo, hud.MaskHi, hud.MaskWin = m.Lo, m.Hi, m.WinCols
			hud.MaskTdivS, hud.MaskSampleS = m.TdivS, m.SampleS
		}
	}
}

// fillHUDBode arms the frequency response while the BODE view shows (on
// the reference and DUT channels the VIEW tab's Bode rows and the web
// choose) and gives the view its curve; leaving the view disarms only the
// arming the view made, so a sweep the web armed goes on.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-224
func (c *Controller) fillHUDBode(hud *render.HUD, st instrument.Stats) {
	b, ok := c.eng.(hudBode)
	if !ok {
		return
	}
	if hud.ViewMode == bodeView {
		b.SetBodeMode(true, st.BodeRef, st.BodeDut)
		hud.BodeRef, hud.BodeDut = st.BodeRef, st.BodeDut
		pts := b.BodePoints()
		hud.BodeFreq = make([]float64, len(pts))
		hud.BodeGain = make([]float64, len(pts))
		hud.BodePhase = make([]float64, len(pts))
		for i, p := range pts {
			hud.BodeFreq[i], hud.BodeGain[i], hud.BodePhase[i] = p.FreqHz, p.GainDB, p.PhaseDeg
		}
		c.hudBodeArmed.Store(true)
		return
	}
	if st.BodeMode > 0 && c.hudBodeArmed.Swap(false) {
		b.SetBodeMode(false, st.BodeRef, st.BodeDut)
	}
}

// bodeView is the display's BODE view (DisplayState.ViewMode).
const bodeView = 3

// SerialMatchText is a serial trigger's match as the BUS table writes its
// units: hex, "?" for a masked nibble, ERR for an error, SOF for a frame's
// start and CMD for a MIL-1553 command word.
// TRLC-LINKS: REQ-DSO-168
func SerialMatchText(p engine.SerialParams) string {
	var toks []string
	if len(p.Pattern) == 0 {
		for _, b := range p.Bytes {
			toks = append(toks, fmt.Sprintf("%02X", b))
		}
		return strings.Join(toks, " ")
	}
	for _, u := range p.Pattern {
		switch {
		case u.Kind == engine.PatternError:
			toks = append(toks, "ERR")
			continue
		case u.Kind == engine.PatternStart:
			toks = append(toks, "SOF")
			continue
		case u.Mask&engine.MILCommand != 0 && u.Value&engine.MILCommand != 0:
			toks = append(toks, "CMD")
			continue
		}
		h := []byte(fmt.Sprintf("%02X", u.Value))
		for i := range h {
			if shift := 4 * (len(h) - 1 - i); (u.Mask>>shift)&15 != 15 {
				h[i] = '?'
			}
		}
		toks = append(toks, string(h))
	}
	return strings.Join(toks, " ")
}
