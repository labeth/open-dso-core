// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import "time"

// Liveness is AUTO's rule for the captures a gate (a protocol or zone
// qualifier) holds: held, but a frame goes through every fallback holds,
// or once AutoLiveMax has passed since the last publish, so an AUTO
// display never freezes on a gate that does not match. Owner only.
type Liveness struct{ held int }

// Due counts one held capture and reports a liveness frame due. last is
// read only when the count alone does not decide; zero: nothing published
// yet, so a frame is due at once.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-014
func (l *Liveness) Due(fallback int, last, now func() time.Time) bool {
	l.held++
	if l.held < fallback {
		if t := last(); !t.IsZero() && now().Sub(t) < AutoLiveMax {
			return false
		}
	}
	l.held = 0
	return true
}

// Reset starts the count again: the gate qualified a capture.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-014
func (l *Liveness) Reset() { l.held = 0 }
