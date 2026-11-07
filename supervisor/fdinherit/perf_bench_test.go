// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERVISOR-FDINHERIT

package fdinherit

import "testing"

// TRLC-LINKS: REQ-DSO-085
func BenchmarkHoldersOf(b *testing.B) {
	for b.Loop() {
		HoldersOf("/dev/null")
	}
}
