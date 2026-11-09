// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

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
