// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"fmt"
	"time"
)

// counters are the owner's frame counts and the last pass's stage times,
// which the status and the health token report: every frame read, the
// ones held, the serial trigger's matches, the published rate.
type counters struct {
	frames, held uint64
	serial       int64
	fps          float64
	fpsAt        time.Time // the start of the published rate's window
	fpsN         int       // frames published in it
	stages       FrameStages
	stagesText   string // stages as the status states them; "" until a snapshot formats it
	haveStages   bool
	symbols      int64 // the symbol trigger's matches during the last record; -1 unknown
	symbolArmedS float64
	haveSymbols  bool
}

// fpsWindow is how long the published rate counts frames before it is
// stated again.
const fpsWindow = time.Second

// FrameStages is one pass of the owner's cycle in its stages: arming,
// waiting for the record, reading it, its detail windows, its processing,
// publishing it, and the pacing hold after the pass before.
type FrameStages struct {
	Arm, Wait, Read, Detail, Proc, Post, PrevPace time.Duration
}

// String is the stages in milliseconds, as the status states them.
// TRLC-LINKS: REQ-DSO-127
func (s FrameStages) String() string {
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	return fmt.Sprintf("arm %.0f wait %.0f read %.0f detail %.0f proc %.0f post %.0f prev-pace %.0f (ms)",
		ms(s.Arm), ms(s.Wait), ms(s.Read), ms(s.Detail), ms(s.Proc), ms(s.Post), ms(s.PrevPace))
}

// CountFrame counts a frame the owner read: published or held, and
// whether it is a match of the serial trigger. OWNER.
// TRLC-LINKS: REQ-DSO-127, REQ-DSO-294
func (m *Model) CountFrame(published, serialMatch bool, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := &m.count
	c.frames++
	if !published {
		c.held++
		return
	}
	if serialMatch {
		c.serial++
	}
	if c.fpsAt.IsZero() {
		c.fpsAt = now
	}
	c.fpsN++
	if dt := now.Sub(c.fpsAt); dt >= fpsWindow {
		c.fps = float64(c.fpsN) / dt.Seconds()
		c.fpsAt, c.fpsN = now, 0
	}
}

// ResetSerialMatches starts the serial trigger's match count again (the
// trigger armed anew). OWNER.
// TRLC-LINKS: REQ-DSO-013
func (m *Model) ResetSerialMatches() {
	m.mu.Lock()
	m.count.serial = 0
	m.mu.Unlock()
}

// SetFrameStages states the last pass's stage times. OWNER.
// TRLC-LINKS: REQ-DSO-127
func (m *Model) SetFrameStages(s FrameStages) {
	m.mu.Lock()
	m.count.stages, m.count.stagesText, m.count.haveStages = s, "", true
	m.mu.Unlock()
}

// SetSymbolMatches states the symbol trigger's matches during the last
// record (-1: not known) and how long it was armed. OWNER.
// TRLC-LINKS: REQ-DSO-013
func (m *Model) SetSymbolMatches(n int64, armed time.Duration) {
	m.mu.Lock()
	m.count.symbols, m.count.symbolArmedS, m.count.haveSymbols = n, armed.Seconds(), true
	m.mu.Unlock()
}

// fill puts the counts into st: every frame read is a coherent one (the
// board hands the owner whole records), the published rate drops to
// nothing over a window with none.
// TRLC-LINKS: REQ-DSO-127, REQ-DSO-294
func (c *counters) fill(st *Stats, now time.Time) {
	st.Frames, st.Coherent, st.Held = c.frames, c.frames, c.held
	st.FPS = c.fps
	if !c.fpsAt.IsZero() {
		if dt := now.Sub(c.fpsAt); dt >= 2*fpsWindow {
			st.FPS = float64(c.fpsN) / dt.Seconds()
		}
	}
	st.SerialMatches = c.serial // kept after the trigger is off, until it is armed anew
	if c.haveStages {
		st.FrameStages = c.stagesText
	}
	if c.haveSymbols {
		st.SymbolMatches, st.SymbolArmedS = c.symbols, c.symbolArmedS
	}
}
