// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"github.com/labeth/open-dso-core/sample"
)

// MeasureRange is the stretch of the record measurements read: a roll view's
// longest run of captured samples - its held stretches are no signal (a 1 kHz
// square measured across them read 769 Hz) - else the whole record.
// TRLC-LINKS: REQ-SDS-010, REQ-SDS-021
func MeasureRange(f *sample.Frame) (int, int) {
	held, n := f.Held(0), f.Len()
	if len(held) != n {
		return 0, n
	}
	best, bestAt, at := 0, 0, -1
	for i := 0; i <= n; i++ {
		if i < n && !held[i] {
			if at < 0 {
				at = i
			}
			continue
		}
		if at >= 0 && i-at > best {
			best, bestAt = i-at, at
		}
		at = -1
	}
	return bestAt, bestAt + best
}
