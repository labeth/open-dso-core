// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"fmt"
	"math"
	"time"

	"github.com/labeth/open-dso-core/hal"

	"github.com/labeth/open-dso-core/instrument"
)

// Input is where the panel's keys, knobs and touches come from and where
// its lamps go: the board's front panel (hal.Input). Its events are taken
// on the panel goroutine; SetLamps is called there when the lit lamps
// change.
type Input interface {
	Events() <-chan hal.InputEvent
	SetLamps(on []string) error
}

// Run is the panel event loop: the board's key, knob and touch events, the
// remote's and the API's injected actions, and the 40 ms tick on which the
// lamps follow changes made elsewhere. in may be nil: no front panel's
// events, the lamps going where SetInput sent them.
// Blocks; run as a goroutine.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) Run(stop <-chan struct{}, in Input) {
	// Adopt the engine's restored setup; starting the panel must not overwrite it.
	st := c.eng.Snapshot()
	c.mu.Lock()
	c.adoptQual(&st)
	if in != nil {
		c.input, c.ledSet = in, false
	}
	c.mu.Unlock()
	c.looping.Store(true) // the remote's changes now run on this goroutine
	go c.snd.run(stop)    // SOUND reads the audio stream while something needs it
	defer c.looping.Store(false)

	var events <-chan hal.InputEvent
	if in != nil {
		events = in.Events()
	}
	c.pushLEDs()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case fn := <-c.inject:
			c.noteInput(false) // a remote input wakes the screen and still does its job
			fn()               // API-injected button/knob, run on the panel goroutine
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			c.Input(ev)
		case <-tick.C:
			// Lamps follow engine-side changes (a SINGLE self-stopping, web/SCPI
			// run toggles) within a tick, whatever the LCD render costs.
			c.SyncLEDs()
			c.checkPageStream()
		}
	}
}

// Input runs one event of the scope's own front panel: a key pressed, a
// knob turned or the screen touched. An input that wakes the screen does
// nothing else; knobs are inert while Autoset sweeps. Call it on the panel
// goroutine (Run does).
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) Input(ev hal.InputEvent) {
	switch ev.Kind {
	case hal.KeyEvent:
		if !ev.Down {
			return
		}
		key, _ := KeyName(ev.Key)
		if !c.hasKey(key) {
			return
		}
		if !c.noteInput(true) { // a key that wakes the screen does nothing else
			c.lcdButton(key)
		}
	case hal.KnobEvent:
		if ev.Steps == 0 || !knobNames[ev.Knob] || !c.hasKey(ev.Knob) {
			return
		}
		// Autoset owns the shadows while it sweeps; resync() below writes
		// them unguarded, so the busy check comes first.
		c.mu.Lock()
		busy := c.autosetBusy
		c.mu.Unlock()
		if busy {
			return
		}
		c.resync()
		if c.noteInput(true) {
			return // it woke the screen: nothing else
		}
		dir, steps := 1, ev.Steps
		if steps < 0 {
			dir, steps = -1, -steps
		}
		c.lcdKnob(ev.Knob, dir, steps)
	case hal.TouchEvent:
		if !ev.Down || c.noteInput(true) {
			return
		}
		c.touch(ev.TouchX, ev.TouchY)
	case hal.Overload: // an input's protection tripped: the board's own alarm, no notice of the UI model's yet
	}
}

// button handles a front-panel key press.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) button(key string) {
	if !c.hasKey(key) {
		return
	}
	// While an autoset sweep runs, ignore everything except AUTO (which cancels)
	// — this both gives a clean "busy" UX and avoids racing its scale changes.
	c.mu.Lock()
	busy := c.autosetBusy
	c.mu.Unlock()
	if busy && key != KeyAuto {
		return
	}
	defer c.afterInput()
	switch key {
	case KeyRunStop:
		if c.streaming() { // Stream: STOP ends the stream (manual trigger), RUN restarts it
			c.mu.Lock()
			s := c.stream
			c.mu.Unlock()
			if s.Active() {
				c.stopStream()
			} else {
				c.startStream()
			}
			return
		}
		// Toggle RUN/STOP from the engine's state, not the shadow: a single that
		// self-stopped moments ago may not be synced into the shadow yet.
		_ = c.setRun(b2ic(c.eng.Snapshot().Running)) // cannot fail
	case KeySingle:
		c.single1() // Normal + one triggered capture, then stop
	case KeyAuto:
		c.runAuto()
	case KeyDefaultSetup:
		c.defaultSetupUI()
	case KeySet50:
		c.setTrigMid()
	case KeyTrigAuto, KeyTrigNormal:
		_ = c.setNorm(key == KeyTrigNormal) // cannot fail
	case KeyForce:
		_ = c.force() // cannot fail
	case KeyPrint:
		c.printScreen()
	case KeyCh1VdivPush, KeyCh2VdivPush, KeyCh3VdivPush, KeyCh4VdivPush:
		ch, _, _ := keyChan(key)
		c.variablePush(ch)
	case KeyCh1PosPush, KeyCh2PosPush, KeyCh3PosPush, KeyCh4PosPush:
		ch, _, _ := keyChan(key)
		if c.fe != nil {
			_ = c.setOffsetTip(ch, 0) // Zero: the trace back to 0 V
			c.toast(fmt.Sprintf("CH%d offset 0 V", ch+1), 900*time.Millisecond)
		}
	case KeyTrigLvlPush:
		_ = c.zeroTrigLevel() // Zero: the trigger level to 0 V
		c.toast("Trigger level 0 V", 900*time.Millisecond)
	case KeyTdivPush:
		c.tdivPush()
	case KeyHorizPosPush:
		c.positionPush()
	case KeyAdjustPush:
		c.adjustPush()
	case KeyMenu:
		c.menuKey()
	case KeyHelp:
		c.helpKey()
	case KeyCh1, KeyCh2, KeyCh3, KeyCh4:
		ch, _, _ := keyChan(key)
		c.chanKey(ch)
	default:
		for i, sk := range softkeys {
			if key == sk {
				c.fKey(i)
				return
			}
		}
		if tabKeys[key] {
			c.mu.Lock()
			c.closePickLocked()
			c.mu.Unlock()
			c.keyTab(key)
		}
		// anything else is claimed and ignored, so it cannot cross-drive a control
	}
}

// resync refreshes the knob shadows from authoritative state before a step,
// so a step lands relative to whatever the web UI / SCPI last set — not a
// stale panel-local value (which would snap the setting on the first click).
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) resync() {
	var st instrument.Stats
	c.resyncInto(&st)
}

// resyncInto is resync, leaving the engine stats it read in st and
// returning the channels' V/div at the BNC; ok false when it read nothing
// (Autoset sweeping).
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) resyncInto(st *instrument.Stats) (vdiv [maxChans]float64, ok bool) {
	// Autoset owns the shadows while it sweeps (it writes them under mu on its own
	// goroutine). Skip here so the injected-knob path can't race it either.
	c.mu.Lock()
	busy := c.autosetBusy
	c.mu.Unlock()
	if busy {
		return vdiv, false
	}
	*st = c.eng.Snapshot()
	c.mu.Lock()
	c.running, c.norm, c.single = st.Running, st.Norm, st.Single // guarded — pushLEDs reads these
	c.mu.Unlock()
	for i, t := range c.tdivs { // the timebase asked for: one waiting for the next capture counts
		if ts := st.TdivSet(); ts > 0 && math.Abs(t-ts) <= t*1e-6 {
			c.tdivIdx = i
			break
		}
	}
	if c.fe != nil {
		for ch := range c.nch {
			vdiv[ch] = c.fe.VdivV(ch)
		}
	}
	return vdiv, true
}

// dispatch turns knob name dir (+1 clockwise) by steps detents.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) dispatch(name string, dir, steps int) {
	if !c.hasKey(name) {
		return
	}
	c.mu.Lock()
	busy := c.autosetBusy
	c.mu.Unlock()
	if busy { // knobs are inert while autoset sweeps (avoids racing its writes)
		return
	}
	defer c.afterInput()
	switch name {
	case "tdiv":
		// CW (+1) → faster timebase, toward "nS" on the knob's printed arc
		// ("S ... nS"); a sheet turns its tab instead, a zoom its factor.
		c.tdivTurn(dir)
	case KnobCh1Vdiv, KnobCh2Vdiv, KnobCh3Vdiv, KnobCh4Vdiv:
		ch, _, _ := keyChan(name)
		if c.fe == nil {
			return // claim-and-ignore without an analog front end
		}
		// CW (+1) → smaller V/div, toward "mV" on the knob's printed arc
		// ("V ... mV"); the detents are ascending. It turned the other way,
		// against the marking.
		if err := c.setVpos(ch, clampInt(c.vpos(ch)-dir, 0, len(c.ladder(ch))-1)); err != nil {
			c.logf("panel: SetVdiv: %v", err)
		}
	case KnobCh1Pos, KnobCh2Pos, KnobCh3Pos, KnobCh4Pos:
		ch, _, _ := keyChan(name)
		if c.fe == nil {
			return // no analog front end: offset knob claim-and-ignore
		}
		// Offset step is 0.2 displayed div per accel-step on every detent;
		// positive steps raise the trace. A fixed 20 DAC codes was 0.2 div at
		// 1 V/div but 2 div at 2 mV/div and 0.02 div at 10 V/div.
		v := c.fe.OffsetReqV(ch) + float64(dir*steps)*panelOffsetStepDiv*c.fe.VdivV(ch)
		c.lv.SetOffset(ch, v)
	case "triglevel":
		// CW raises the level. The step is 40 level steps (DAC codes) per
		// accel step at 1 V/div, scaled by the source's displayed V/div (at
		// least one): a fixed 40 was 22 div at 2 mV/div.
		c.lv.StepTrigLevel(dir*c.trigSteps()*steps, c.chOf(c.eng.Snapshot().TrigSource))
	case "adjust":
		// The workspace's tool, the sheet's row, or the picker's byte.
		c.adjustTurn(dir, steps)
	case "horizpos":
		// The workspace's tool, the sheet's value, or a stopped stream's list.
		c.positionTurn(dir, steps)
	}
}

// panelOffsetStepDiv is the position knob's step in displayed divisions.
const panelOffsetStepDiv = 0.2

// panelTrigStepsPerVdiv is the trigger-level knob's step in level steps
// (TrigLevelGrid, one DAC code) per volt of the source channel's V/div at
// the BNC (40 at 1 V/div, 0.044 div).
const panelTrigStepsPerVdiv = 40.0

// trigSteps is the trigger-level knob step for the current source.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) trigSteps() int {
	if c.fe == nil {
		return int(panelTrigStepsPerVdiv)
	}
	return trigStepsAt(c.fe.VdivV(c.chOf(c.eng.Snapshot().TrigSource)))
}

// trigStepsAt is trigSteps for the trigger source's V/div at the BNC vdiv
// already read (snap holds it).
// TRLC-LINKS: REQ-DSO-135
func trigStepsAt(vdiv float64) int {
	return max(1, int(math.Round(panelTrigStepsPerVdiv*vdiv)))
}

// setTrigMid is SET TO 50%: the trigger level at the midpoint of the source
// channel's latest frame. Without a signal there it says so and leaves the
// level as it is.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) setTrigMid() {
	m, ok := c.measureChans()
	src := c.chOf(c.eng.Snapshot().TrigSource)
	if !ok || !has(m[src]) {
		c.toast(fmt.Sprintf("Set to 50 %%: no signal on C%d", src+1), 1500*time.Millisecond)
		return
	}
	c.lv.SetTrigLevel((m[src].Vmax+m[src].Vmin)/2, src)
}

// SetTouchKeys gives the screen's touch layout: the key a touch at (x, y)
// presses, "" for none (the display's composition knows where it drew the
// F-keys). Without one a touch does nothing.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) SetTouchKeys(fn func(x, y int) string) {
	c.mu.Lock()
	c.touchKeys = fn
	c.mu.Unlock()
}

// touch presses the key the screen shows at (x, y).
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) touch(x, y int) {
	c.mu.Lock()
	fn := c.touchKeys
	c.mu.Unlock()
	if fn == nil {
		return
	}
	if key, ok := KeyName(fn(x, y)); ok {
		c.lcdButton(key)
	}
}
