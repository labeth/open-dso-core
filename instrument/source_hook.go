// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import "github.com/labeth/open-dso-core/hal"

// OnTrigSource has fn hear every SetTrigSource once the source is staged:
// a board that keeps the trigger level as its comparator's code (the code
// stays where it is when the source changes, its volts move) projects the
// level onto the new source there. fn runs on the setter's goroutine,
// outside the model's lock.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-098
func (m *Model) OnTrigSource(fn func()) {
	m.mu.Lock()
	m.sourced = append(m.sourced, fn)
	m.mu.Unlock()
}

// OnTrigType has fn project every change of the trigger type made by
// SetTrigType or SetTrigTypeOf into the state as it is staged: s has the
// new type, from was the type before. A board whose trigger keeps one
// source, slope and level for every type (the SDS1000CML+'s one
// comparator) carries them over there. fn runs inside the change, under
// the model's lock: it edits s and calls nothing on the model.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) OnTrigType(fn func(s *State, from TrigType)) {
	m.mu.Lock()
	m.typed = append(m.typed, fn)
	m.mu.Unlock()
}

// OnTrigSlope has fn keep a slope set by SetTrigSlope on a running type
// that has no slope setting of its own (video), inside the change, under
// the model's lock: a board whose one comparator has a slope whatever the
// type keeps it there, for the read-backs and the type after it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (m *Model) OnTrigSlope(fn func(s *State, sl hal.Slope)) {
	m.mu.Lock()
	m.sloped = append(m.sloped, fn)
	m.mu.Unlock()
}
