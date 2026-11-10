// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/framecalc"
)

// Lamps (spec 08 §8.2): only the confirmed lamps, each with one meaning,
// sheet open or not: the key's function is in use. Measure: readouts on.
// Acquire: acquisition not Normal, or super-res on. Display: a view other
// than Y-T, or persistence. Utility: decoding. Save/Recall: the settings are
// unchanged since the last save or recall. 1 and 2: the channel is shown.
// Single: armed. Run/Stop: green running, amber waiting for a trigger
// (Normal or Single), red stopped. Math: a math trace is on. Ref: REF A or
// REF B is shown. Cursors: the cursors are on. INTENSITY stays dark (no
// firmware has lit it, so its latch bit is not confirmed).

// lampWord is the latch word for state s.
// TRLC-LINKS: REQ-DSO-135, REQ-DSO-211
func (c *Controller) lampWord(s *sv) uint16 {
	var w uint16
	if s.chDisp[0] {
		w |= ledCH1
	}
	if s.chDisp[1] {
		w |= ledCH2
	}
	if s.mathMode != 0 {
		w |= ledMath
	}
	if s.refHas[0] && s.refShow[0] || s.refHas[1] && s.refShow[1] {
		w |= ledRef
	}
	if s.curOn {
		w |= ledCursors
	}
	switch {
	case !s.st.Running:
		w |= ledStop
	case s.st.Single || c.waitingForTrigger(s):
		w |= ledRun | ledStop // both elements: amber
	default:
		w |= ledRun
	}
	if s.st.Single && s.st.Running {
		w |= ledSingle
	}
	if s.showMeas && len(s.meas) > 0 {
		w |= ledMeasure
	}
	if s.st.AcqMode != 0 || s.srActive {
		w |= ledAcquire
	}
	if s.viewMode != viewYT || s.persist {
		w |= ledDisplay
	}
	if s.decProto != framecalc.ProtoOff {
		w |= ledUtility
	}
	if c.sameAsSavedSetup(s) {
		w |= ledSaveRec
	}
	return w
}

// waitingForTrigger reports Normal mode, running, with nothing published
// lately: the held frame's trigger state no longer describes now.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) waitingForTrigger(s *sv) bool {
	sp, ok := c.eng.(interface{ SincePublish() time.Duration })
	if !ok || !s.st.Norm || !s.st.Running {
		return false
	}
	return sp.SincePublish() > time.Duration(math.Max(1, 25*s.st.TdivS)*float64(time.Second))
}

// setupPrint is a fingerprint of the settings a setup holds (for the
// Save/Recall lamp).
// TRLC-LINKS: REQ-DSO-136
func setupPrint(s *sv) string {
	// Appended, not formatted: each value's shortest text, a space after it,
	// so equal prints are equal settings.
	b := make([]byte, 0, 256)
	f := func(v float64) { b = append(strconv.AppendFloat(b, v, 'g', -1, 64), ' ') }
	n := func(v int) { b = append(strconv.AppendInt(b, int64(v), 10), ' ') }
	t := func(v bool) { b = append(strconv.AppendBool(b, v), ' ') }
	f(s.st.TdivSet())
	n(s.st.TrigSource)
	t(s.st.TrigRising)
	t(s.trigSaved.Set())
	f(s.trigV)
	n(s.st.TrigType)
	t(s.st.Norm)
	f(s.st.HoldoffS)
	f(s.st.TrigPosFrac)
	n(s.st.AcqMode)
	n(s.st.AvgCount)
	n(s.st.EresLen)
	for ch := range s.nch {
		f(s.vert[ch].vdiv)
		f(s.offV[ch])
		n(s.cpl[ch])
		f(s.probe[ch])
		t(s.bwl[ch])
		t(s.chDisp[ch])
	}
	t(s.showMeas)
	t(s.persist)
	for _, v := range []int{s.viewMode, s.mathMode, s.decProto, s.decBaud, s.decChA, s.decChB} {
		n(v)
	}
	t(s.decCPOL)
	t(s.decCPHA)
	n(s.decFormat)
	b = append(b, '[')
	for _, m := range s.meas {
		n(m.ch)
		n(m.param)
	}
	b = append(b, "] "...)
	n(s.fftSrc)
	n(int(s.ui.ws))
	return string(b)
}

// SyncLEDs refreshes the lamps from the engine and the panel state and
// re-latches only when the word changed, so the lamps follow a single-shot
// self-stopping or the web/SCPI toggling run without a key press; while a
// remote waits in its long-poll it also publishes the panel snapshot, at
// most every syncPublishEvery (an input publishes at once; a remote's own
// read publishes before it reads).
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) SyncLEDs() {
	st := c.eng.Snapshot()
	c.mu.Lock()
	c.running, c.norm, c.single = st.Running, st.Norm, st.Single
	webTook := st.DecodedWeb && !c.webCapture
	c.webCapture = st.DecodedWeb
	c.mu.Unlock()
	if webTook { // the acquisition stops while a web page streams decoded captures: say why, and how to end it
		c.toast("A web page streams decoded captures - RUN here ends it", 4*time.Second)
	}
	c.pushLEDs()
	c.rmu.Lock()
	at, waiting := c.remote.pubAt, c.remote.waiting > 0
	c.rmu.Unlock()
	if now := c.now(); waiting && (at.IsZero() || now.Sub(at) >= syncPublishEvery || now.Before(at)) {
		c.publish()
	}
}

// syncPublishEvery bounds the periodic publish: hashing every sheet row and
// encoding the snapshot 45 times a second takes a tenth of the CPU.
const syncPublishEvery = 150 * time.Millisecond

// pushLEDs lights the board's lamps when the lit set changed.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) pushLEDs() {
	s := c.snap()
	w := c.lampWord(s)
	on := lampSet(w, s.st.Norm)
	for ch := 2; ch < s.nch; ch++ { // channels past C2 have no bit in the word: their lamps go by name
		if s.chDisp[ch] {
			on = append(on, fmt.Sprintf("ch%d", ch+1))
		}
	}
	key := strings.Join(on, " ")
	c.mu.Lock()
	changed := !c.ledSet || key != c.ledLit
	c.ledWord, c.ledLit, c.ledSet = w, key, true
	in := c.input
	c.mu.Unlock()
	if changed && in != nil {
		if err := in.SetLamps(on); err != nil {
			c.logf("panel: lamps: %v", err)
		}
	}
}

// lampBits are the lamps of the lamp word by their core names; a board
// lights the ones it has.
var lampBits = []struct {
	bit  uint16
	name string
}{
	{ledCursors, "cursors"},
	{ledCH1, "ch1"},
	{ledMath, "math"},
	{ledCH2, "ch2"},
	{ledRef, "ref"},
	{ledMeasure, "measure"},
	{ledAcquire, "acquire"},
	{ledDisplay, "display"},
	{ledSaveRec, "save"},
	{ledUtility, "decode"},
	{ledRun, "run"},
	{ledStop, "stop"},
	{ledSingle, "single"},
}

// lampSet is the lit lamps by core name: the word's, and the trigger
// mode's own lamp (AUTO or NORMAL) for a panel that has one.
// TRLC-LINKS: REQ-DSO-135
func lampSet(w uint16, norm bool) []string {
	var on []string
	for _, l := range lampBits {
		if w&l.bit != 0 {
			on = append(on, l.name)
		}
	}
	if norm {
		return append(on, "normal")
	}
	return append(on, "auto")
}

// LampWord is the lamp word lighting the named lamps (lampSet's names);
// names the word has no bit for light nothing.
// TRLC-LINKS: REQ-DSO-135
func LampWord(on []string) uint16 {
	var w uint16
	for _, n := range on {
		for _, l := range lampBits {
			if l.name == n {
				w |= l.bit
			}
		}
	}
	return w
}

// SetInput connects the board's front panel without running the loop (Run
// connects it too): its lamps follow from now on.
// TRLC-LINKS: REQ-DSO-135
func (c *Controller) SetInput(in Input) {
	c.mu.Lock()
	c.input, c.ledSet = in, false
	c.mu.Unlock()
}

// clampInt is v limited to lo..hi.
// TRLC-LINKS: REQ-DSO-135
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
