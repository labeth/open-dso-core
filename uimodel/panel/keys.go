// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"strings"
	"time"
)

// The keys the UI model acts on, by their core names: a board's front panel
// reports its keys under these names (hal.InputEvent.Key), and /api/panel
// and SCPI inject them by the same names or an alias (KeyName).
const (
	KeyRunStop      = "run"
	KeySingle       = "single"
	KeyAuto         = "auto"
	KeyDefaultSetup = "defaultsetup"
	KeySet50        = "set50"
	KeyForce        = "force"
	KeyPrint        = "print"
	KeyHelp         = "help"
	KeyMenu         = "menu"
	KeyCh1          = "ch1"
	KeyCh2          = "ch2"
	KeyMath         = "math"
	KeyRef          = "ref"
	KeyTrigMenu     = "trigmenu"
	KeyAcquire      = "acquire"
	KeyDisplay      = "display"
	KeyHorizMenu    = "horizmenu"
	KeyMeasure      = "measure"
	KeyCursors      = "cursors"
	KeyUtility      = "utility" // the BUS sheet tab
	KeySaveRecall   = "saverecall"
	KeyF1           = "f1"
	KeyF2           = "f2"
	KeyF3           = "f3"
	KeyF4           = "f4"
	KeyF5           = "f5"

	// Knob pushes, with their printed jobs.
	KeyCh1VdivPush  = "ch1vdivpush"  // CH1 V/DIV push: Variable
	KeyCh2VdivPush  = "ch2vdivpush"  // CH2 V/DIV push: Variable
	KeyCh1PosPush   = "ch1pospush"   // CH1 POSITION push: Zero
	KeyCh2PosPush   = "ch2pospush"   // CH2 POSITION push: Zero
	KeyTrigLvlPush  = "triglvlpush"  // TRIG LEVEL push: level 0 V
	KeyTdivPush     = "tdivpush"     // S/DIV push: Zoom
	KeyHorizPosPush = "horizpospush" // HORIZONTAL POSITION push: Zero
	KeyAdjustPush   = "adjustpush"   // ADJUST push: the workspace's tool
)

// The knobs, by their core names; a knob event's steps are signed, positive
// clockwise.
const (
	KnobTdiv      = "tdiv"
	KnobCh1Vdiv   = "ch1vdiv"
	KnobCh2Vdiv   = "ch2vdiv"
	KnobCh1Pos    = "ch1pos"
	KnobCh2Pos    = "ch2pos"
	KnobTrigLevel = "triglevel"
	KnobAdjust    = "adjust"
	KnobHorizPos  = "horizpos"
)

// softkeys are F1..F5, top to bottom.
var softkeys = []string{KeyF1, KeyF2, KeyF3, KeyF4, KeyF5}

// keyMath is the Math key's name (it opens the MATH/REF tab on its row).
const keyMath = KeyMath

// tabKeys are the dedicated keys that open a sheet tab; keyTab knows them
// by these names.
var tabKeys = map[string]bool{
	KeyTrigMenu: true, KeyAcquire: true, KeyDisplay: true, KeyHorizMenu: true, KeyMeasure: true,
	KeyCursors: true, KeyMath: true, KeyRef: true, KeyUtility: true, KeySaveRecall: true,
}

// keyAliases are the other names /api/panel and SCPI have always taken.
var keyAliases = map[string]string{
	"runstop": KeyRunStop, "util": KeyUtility, "intensity": KeyAdjustPush, "trigger": KeyTrigMenu,
	"acq": KeyAcquire, "disp": KeyDisplay, "horiz": KeyHorizMenu, "meas": KeyMeasure,
	"cursor": KeyCursors, "hardcopy": KeyPrint,
}

// coreKeys are the keys by their core names.
var coreKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range []string{
		KeyRunStop, KeySingle, KeyAuto, KeyDefaultSetup, KeySet50, KeyForce, KeyPrint, KeyHelp, KeyMenu,
		KeyCh1, KeyCh2, KeyCh1VdivPush, KeyCh2VdivPush, KeyCh1PosPush, KeyCh2PosPush, KeyTrigLvlPush,
		KeyTdivPush, KeyHorizPosPush, KeyAdjustPush,
	} {
		m[k] = true
	}
	for _, k := range softkeys {
		m[k] = true
	}
	for k := range tabKeys {
		m[k] = true
	}
	return m
}()

// knobNames are the knobs by their core names.
var knobNames = map[string]bool{
	KnobTdiv: true, KnobCh1Vdiv: true, KnobCh2Vdiv: true, KnobCh1Pos: true,
	KnobCh2Pos: true, KnobTrigLevel: true, KnobAdjust: true, KnobHorizPos: true,
}

// KeyName is the core name of a key named name or one of its aliases (any
// case); ok is false for a name the UI model has no key for.
// TRLC-LINKS: REQ-DSO-135
func KeyName(name string) (string, bool) {
	n := strings.ToLower(name)
	if a, ok := keyAliases[n]; ok {
		n = a
	}
	return n, coreKeys[n]
}

// Keys are the keys the UI model acts on, by their core names.
// TRLC-LINKS: REQ-DSO-135
func Keys() []string {
	out := make([]string, 0, len(coreKeys))
	for k := range coreKeys {
		out = append(out, k)
	}
	return out
}

// Knobs are the knobs the UI model acts on, by their core names.
// TRLC-LINKS: REQ-DSO-135
func Knobs() []string {
	out := make([]string, 0, len(knobNames))
	for k := range knobNames {
		out = append(out, k)
	}
	return out
}

// InjectButton runs a named button press on the panel goroutine (non-blocking).
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) InjectButton(name string) bool {
	key, ok := KeyName(name)
	if !ok {
		return false
	}
	select {
	case c.inject <- func() { c.lcdButton(key) }:
		return true
	default:
		return false
	}
}

// InjectButtonWait runs a named button on the panel goroutine and waits
// (bounded) until it has run. *RST needs this: its DEFAULT SETUP must land
// before the commands that follow it, or "*RST;TDIV 2US" ends at 500 us.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) InjectButtonWait(name string, timeout time.Duration) bool {
	key, ok := KeyName(name)
	if !ok {
		return false
	}
	done := make(chan struct{})
	select {
	case c.inject <- func() { c.lcdButton(key); close(done) }:
	case <-time.After(timeout):
		return false
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// InjectKnob runs a named knob step (dir ±1, steps≥1) on the panel goroutine.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) InjectKnob(name string, dir, steps int) bool {
	if !knobNames[name] {
		return false
	}
	if steps < 1 {
		steps = 1
	}
	if dir >= 0 {
		dir = 1
	} else {
		dir = -1
	}
	select {
	case c.inject <- func() { c.resync(); c.lcdKnob(name, dir, steps) }:
		return true
	default:
		return false
	}
}
