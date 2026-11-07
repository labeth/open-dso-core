// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"time"
)

// Clock abstracts time for tests. Sleep must also advance Now in fakes.
// TRLC-LINKS: REQ-DSO-008
type Clock struct {
	Now   func() time.Time
	Sleep func(time.Duration)
}

// RealClock is the wall clock and a real sleep.
// TRLC-LINKS: REQ-DSO-008
func RealClock() Clock { return Clock{Now: time.Now, Sleep: time.Sleep} }
