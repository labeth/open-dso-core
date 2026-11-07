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
	KeyCh3          = "ch3" // on a scope with a third channel
	KeyCh4          = "ch4"
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
	// The trigger mode's own keys, on a panel that has them.
	KeyTrigAuto   = "trigauto"
	KeyTrigNormal = "trignormal"

	// Knob pushes, with their printed jobs.
	KeyCh1VdivPush  = "ch1vdivpush" // CH1 V/DIV push: Variable
	KeyCh2VdivPush  = "ch2vdivpush" // CH2 V/DIV push: Variable
	KeyCh1PosPush   = "ch1pospush"  // CH1 POSITION push: Zero
	KeyCh2PosPush   = "ch2pospush"  // CH2 POSITION push: Zero
	KeyCh3VdivPush  = "ch3vdivpush"
	KeyCh4VdivPush  = "ch4vdivpush"
	KeyCh3PosPush   = "ch3pospush"
	KeyCh4PosPush   = "ch4pospush"
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
	KnobCh3Vdiv   = "ch3vdiv"
	KnobCh4Vdiv   = "ch4vdiv"
	KnobCh3Pos    = "ch3pos"
	KnobCh4Pos    = "ch4pos"
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
		KeyCh3, KeyCh4, KeyCh3VdivPush, KeyCh4VdivPush, KeyCh3PosPush, KeyCh4PosPush,
		KeyTdivPush, KeyHorizPosPush, KeyAdjustPush, KeyTrigAuto, KeyTrigNormal,
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
	KnobCh3Vdiv: true, KnobCh4Vdiv: true, KnobCh3Pos: true, KnobCh4Pos: true,
}

// keyChan is the channel a channel's key, push or knob names ("ch3",
// "ch3vdivpush", "ch3pos") and the rest of its name; ok false for any other
// name.
// TRLC-LINKS: REQ-DSO-135
func keyChan(name string) (ch int, rest string, ok bool) {
	if len(name) < 3 || !strings.HasPrefix(name, "ch") || name[2] < '1' || name[2] > '0'+maxChans {
		return 0, "", false
	}
	return int(name[2] - '1'), name[3:], true
}

// hasKey reports whether this scope has the key or knob named name: a
// channel's keys and knobs only for the channels it has.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) hasKey(name string) bool {
	ch, _, ok := keyChan(name)
	return !ok || ch < c.nch
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
	if !ok || !c.hasKey(key) {
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
	if !ok || !c.hasKey(key) {
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
	if !knobNames[name] || !c.hasKey(name) {
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
