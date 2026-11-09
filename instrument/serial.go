// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"github.com/labeth/open-dso-core/hal/prototrig"
)

// SetSerialMode arms the serial trigger (1) on the bus and match
// SetSerialParams set, or turns it off (0): the board's protocol trigger
// then decides every capture in place of the trigger type.
// TRLC-LINKS: REQ-DSO-013
func (m *Model) SetSerialMode(mode int) {
	if mode != 0 {
		mode = 1
	}
	m.set(func(s *State) { s.SerialMode = mode })
}

// SetSerialParams sets the serial trigger's bus and match.
// TRLC-LINKS: REQ-DSO-013
func (m *Model) SetSerialParams(p prototrig.SerialParams) {
	p.Bytes, p.Pattern = append([]int(nil), p.Bytes...), append([]prototrig.PatternElem(nil), p.Pattern...)
	m.set(func(s *State) { s.Serial = p })
}

// SerialConfig is the serial trigger's bus and match.
// TRLC-LINKS: REQ-DSO-013
func (m *Model) SerialConfig() prototrig.SerialParams {
	return m.State().Serial
}

// Backends says what decides the trigger the state asks for (OWNER): for a
// pulse, slope or video trigger and for the serial trigger, "hardware" when
// the board's trigger takes it, or "none: <why>" while it cannot and the
// captures run untriggered; "" when the state asks for neither.
type Backends struct {
	Trigger, Serial string
}

// SetBackends records what decides the trigger (OWNER).
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-013
func (m *Model) SetBackends(b Backends) {
	m.mu.Lock()
	m.backends = b
	m.mu.Unlock()
}

// trigHealth counts the hardware edge triggers whose record had no
// crossing at the index, and their recent share.
type trigHealth struct {
	falses uint64
	share  float64
}

// NoteTrigger counts a hardware edge trigger checked on its record (OWNER):
// noise, it had no crossing at the index and was dropped. The share is an
// average over about the last twenty triggers (Stats.NoiseTriggers).
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-016
func (m *Model) NoteTrigger(noise bool) {
	x := 0.0
	m.mu.Lock()
	if noise {
		x = 1
		m.trig.falses++
	}
	m.trig.share += (x - m.trig.share) / 20
	m.mu.Unlock()
}

// ClearNoiseTriggers forgets the share (OWNER): the trigger, the timebase or
// the mode changed, and the next triggers say anew.
// TRLC-LINKS: REQ-DSO-011
func (m *Model) ClearNoiseTriggers() {
	m.mu.Lock()
	m.trig.share = 0
	m.mu.Unlock()
}
