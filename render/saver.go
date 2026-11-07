// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import (
	"strconv"
	"time"

	"github.com/labeth/open-dso-core/sample"
)

// The screen saver picture (ADR-SCREEN-SAVER): a small live scope with its run
// state, drifting over black so that no pixel holds one image.

// SaverStep is how often the saver moves one pixel each way (5 px/s).
const SaverStep = 200 * time.Millisecond

// saverLinesH is the saver box's two lines under its mini scope.
const saverLinesH = 36

// SaverPos is the box's top-left corner after step steps: it bounces off the
// screen's edges one pixel each way per step, on a path that repeats only
// after hours.
// TRLC-LINKS: REQ-SDS-221
func SaverPos(m *Metrics, step int) (x, y int) {
	return bounce(step, m.W-m.SaverW), bounce(step, m.H-m.SaverH-saverLinesH)
}

// bounce runs 0..span and back.
// TRLC-LINKS: REQ-SDS-221
func bounce(t, span int) int {
	if span <= 0 || t < 0 {
		return 0
	}
	t %= 2 * span
	if t > span {
		t = 2*span - t
	}
	return t
}

// DrawSaver draws the saver picture for a step: black, the live mini scope,
// its run state and time/div, and a running mask test's counts or the hint.
// TRLC-LINKS: REQ-SDS-221
func DrawSaver(sf Canvas, f *sample.Frame, hud HUD, step int) {
	sf.Fill(RGB(0, 0, 0)) // black: a memclr on a memory canvas
	sm := metricsOf(sf)
	x, y := SaverPos(sm, step)
	saverH := sm.SaverH
	drawMiniScope(sf, f, hud, x, y, sm.SaverW, saverH)
	state, col := "RUN", colOK
	switch {
	case !hud.Running:
		state, col = "STOP", colStale
	case hud.Single:
		state, col = "SINGLE", colTrig
	}
	DrawText(sf, x, y+saverH+8, state, col, 2)
	DrawText(sf, x+TextWidth(state, 2)+12, y+saverH+8, fmtTdiv(hud.TdivS)+"/div", colInfo, 2)
	line, lcol := "Any key wakes the screen", colDim
	if hud.MaskMode > 0 {
		line, lcol = "MASK pass "+strconv.FormatInt(hud.MaskPass, 10)+"  FAIL "+strconv.FormatInt(hud.MaskFail, 10), colInfo
		if hud.MaskFail > 0 || hud.MaskStopped {
			lcol = colWarn
		}
	}
	DrawText(sf, x, y+saverH+28, line, lcol, 1)
}
