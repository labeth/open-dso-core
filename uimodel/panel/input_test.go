// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// key is a key press of the front panel.
func key(name string) hal.InputEvent {
	return hal.InputEvent{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: name, Down: true}
}

// knob is a knob turned steps detents (negative: counter-clockwise).
func knob(name string, steps int) hal.InputEvent {
	return hal.InputEvent{Source: hal.FromPanel, Kind: hal.KnobEvent, Knob: name, Steps: steps}
}

// RUN/STOP toggles the run from the engine's state; a key's release does
// nothing.
// TRLC-LINKS: REQ-DSO-135
func TestKeyRunStop(t *testing.T) {
	c, eng, _ := newC(t)
	c.Input(key(KeyRunStop))
	if len(eng.calls) != 1 || eng.calls[0] != (call{"run", 0, 0}) {
		t.Fatalf("run/stop press: %v", eng.calls)
	}
	c.Input(hal.InputEvent{Kind: hal.KeyEvent, Key: KeyRunStop})
	if len(eng.calls) != 1 {
		t.Fatalf("a release ran the key: %v", eng.calls)
	}
	c.Input(key(KeyRunStop))
	if eng.calls[len(eng.calls)-1] != (call{"run", 1, 0}) {
		t.Fatalf("second press: %v", eng.calls)
	}
}

// SINGLE arms a shot and lights its lamp; AUTO is the AUTO mode.
// TRLC-LINKS: REQ-DSO-135
func TestKeySingleAndAuto(t *testing.T) {
	c, eng, _ := newC(t)
	c.Input(key(KeySingle))
	if len(eng.calls) != 1 || eng.calls[0] != (call{"single", 0, 0}) {
		t.Fatalf("single: %v", eng.calls)
	}
	if last := eng.leds[len(eng.leds)-1]; last&ledSingle == 0 {
		t.Fatalf("single LED not set: %#04x", last)
	}
	c.Input(key(KeyAuto))
	if eng.calls[len(eng.calls)-2] != (call{"norm", 0, 0}) {
		t.Fatalf("auto: %v", eng.calls)
	}
}

// TIME/DIV clockwise is one detent toward the fast end of the ladder.
// TRLC-LINKS: REQ-DSO-135
func TestKnobTdiv(t *testing.T) {
	c, eng, _ := newC(t)
	c.Input(knob(KnobTdiv, 1))
	if len(eng.calls) != 1 || eng.calls[0].what != "tdiv" || eng.calls[0].a != int(200e-6*1e9) {
		t.Fatalf("tdiv CW: %v", eng.calls)
	}
	c.Input(knob(KnobTdiv, 1))
	if len(eng.calls) != 2 {
		t.Fatalf("the next detent: %v", eng.calls)
	}
}

// CH1 V/DIV counter-clockwise is away from mV: 1 V to 2 V.
// TRLC-LINKS: REQ-DSO-135
func TestKnobVdiv(t *testing.T) {
	c, _, fe := newC(t)
	c.Input(knob(KnobCh1Vdiv, -1))
	if len(fe.calls) != 1 || fe.calls[0] != (call{"vdiv", 0, 9}) {
		t.Fatalf("ch1 vdiv CCW: %v", fe.calls)
	}
}

// TRIG LEVEL clockwise raises the level: the comparator code falls by
// 40 a detent at 1 V/div.
// TRLC-LINKS: REQ-DSO-135
func TestKnobTrigLevelSign(t *testing.T) {
	c, eng, _ := newC(t)
	c.Input(knob(KnobTrigLevel, 1))
	if len(eng.calls) != 1 || eng.calls[0] != (call{"triglevel", 31394, 0}) {
		t.Fatalf("trig level CW: %v", eng.calls)
	}
}

// A position knob's steps are what the board counted (its acceleration):
// 100 steps of 0.2 div at 1 V/div.
// TRLC-LINKS: REQ-DSO-135
func TestKnobPositionSteps(t *testing.T) {
	c, _, fe := newC(t)
	c.Input(knob(KnobCh1Pos, 100))
	want := 100 * 20.0 / 100.0
	if len(fe.calls) != 1 || fe.calls[0].what != "offset" || fe.offReqV[0] < want-1e-9 || fe.offReqV[0] > want+1e-9 {
		t.Fatalf("ch1 pos: fe=%+v want volts %v", fe.calls, want)
	}
}

// A knob step starts from the state the web or SCPI left, not from a stale
// shadow.
// TRLC-LINKS: REQ-DSO-135
func TestKnobResyncFromEngine(t *testing.T) {
	c, eng, fe := newC(t)
	eng.stats.TrigCode = 30000
	fe.idx = [2]int{5, 5}
	c.Input(knob(KnobTrigLevel, 1)) // at 100 mV/div: 30000 − 4 = 29996
	if eng.calls[len(eng.calls)-1] != (call{"triglevel", 29996, 0}) {
		t.Fatalf("resync trig: %v", eng.calls)
	}
	c.Input(knob(KnobCh1Vdiv, 1)) // toward mV from the resynced step 5: 4
	if fe.calls[len(fe.calls)-1] != (call{"vdiv", 0, 4}) {
		t.Fatalf("resync vdiv: %v", fe.calls)
	}
}

// The knob pushes do their printed jobs: V/DIV push is Variable (the front
// end has no fine V/div, so it says so and changes nothing), the Level and
// Position pushes are Zero, the S/DIV push is Zoom.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-213
func TestKnobPushPrintedJobs(t *testing.T) {
	c, eng, fe := newC(t)
	withClock(c)
	c.Input(key(KeyCh1VdivPush))
	if len(eng.calls) != 0 || len(fe.calls) != 0 || !strings.Contains(c.View().Notice, "no fine V/div") {
		t.Fatalf("V/DIV push: engine %v fe %v notice %q", eng.calls, fe.calls, c.View().Notice)
	}
	eng.stats.TrigCode = 30000
	c.Input(key(KeyTrigLvlPush))
	if last := eng.calls[len(eng.calls)-1]; last != (call{"triglevel", 31437, 0}) {
		t.Fatalf("Level push: %v", eng.calls)
	}
	fe.offReqV[1] = 0.4
	c.Input(key(KeyCh2PosPush))
	if fe.offReqV[1] != 0 {
		t.Fatalf("CH2 Position push: offset %v", fe.offReqV[1])
	}
	c.Input(key(KeyTdivPush))
	if c.zoom != 10 {
		t.Fatalf("S/DIV push: zoom %d", c.zoom)
	}
	c.Input(key(KeyTdivPush))
	if c.zoom != 1 {
		t.Fatalf("S/DIV push again: zoom %d", c.zoom)
	}
}

// The position knob steps 0.2 displayed div on every detent.
// TRLC-LINKS: REQ-DSO-135
func TestPositionStepFollowsVdiv(t *testing.T) {
	for _, tc := range []struct {
		idx  int
		want float64
	}{{0, 0.0004}, {8, 0.2}, {11, 2}} {
		c, _, fe := newC(t)
		fe.idx = [2]int{tc.idx, tc.idx}
		c.Input(knob(KnobCh1Pos, 1))
		if d := fe.offReqV[0] - tc.want; d > 1e-12 || d < -1e-12 {
			t.Fatalf("detent %d: offset %v, want %v", tc.idx, fe.offReqV[0], tc.want)
		}
	}
}

// Autoset's fallback scan spans the trigger source's screen, offset included.
// TRLC-LINKS: REQ-DSO-137
func TestTrigScanSpansScreen(t *testing.T) {
	c, _, fe := newC(t)
	fe.idx = [2]int{2, 2} // 10 mV/div
	fe.offReqV[0] = 0.02  // trace raised 2 div: 0 V sits at +2 div
	levels := c.trigScanLevels()
	if len(levels) != 10 {
		t.Fatalf("%d levels", len(levels))
	}
	if d := levels[0] + 0.065; d < -1e-9 || d > 1e-9 {
		t.Fatalf("first %v, want -0.065", levels[0])
	}
	if d := levels[9] - 0.025; d < -1e-9 || d > 1e-9 {
		t.Fatalf("last %v, want 0.025", levels[9])
	}
}

// RUN/STOP right after a single self-stopped (before any lamp sync refreshed the
// shadow) must restart acquisition, not toggle the stale "running" shadow to STOP.
// TRLC-LINKS: REQ-DSO-135
func TestRunAfterSingleSelfStop(t *testing.T) {
	c, eng, _ := newC(t)
	c.button(KeySingle)
	eng.stats.Running, eng.stats.Single = false, false // the engine captured and stopped
	c.button(KeyRunStop)
	if last := eng.calls[len(eng.calls)-1]; last != (call{"run", 1, 0}) {
		t.Fatalf("RUN after a self-stopped single: %v", eng.calls)
	}
}

// A touch presses the key the screen's touch layout puts there.
// TRLC-LINKS: REQ-DSO-135
func TestTouchPressesTheLayoutsKey(t *testing.T) {
	c, eng, _ := newC(t)
	c.Input(hal.InputEvent{Kind: hal.TouchEvent, TouchX: 10, TouchY: 10, Down: true}) // no layout: nothing
	c.SetTouchKeys(func(x, _ int) string {
		if x > 900 {
			return KeyRunStop
		}
		return ""
	})
	c.Input(hal.InputEvent{Kind: hal.TouchEvent, TouchX: 10, TouchY: 10, Down: true})
	if len(eng.calls) != 0 {
		t.Fatalf("a touch on nothing: %v", eng.calls)
	}
	c.Input(hal.InputEvent{Kind: hal.TouchEvent, TouchX: 950, TouchY: 10, Down: true})
	if len(eng.calls) != 1 || eng.calls[0].what != "run" {
		t.Fatalf("a touch on RUN: %v", eng.calls)
	}
}

// A panel's own trigger-mode keys pick AUTO and NORMAL.
// TRLC-LINKS: REQ-DSO-135
func TestTriggerModeKeys(t *testing.T) {
	c, eng, _ := newC(t)
	c.Input(key(KeyTrigNormal))
	if !eng.stats.Norm {
		t.Fatalf("NORMAL: %v", eng.calls)
	}
	c.Input(key(KeyTrigAuto))
	if eng.stats.Norm {
		t.Fatalf("AUTO: %v", eng.calls)
	}
}
