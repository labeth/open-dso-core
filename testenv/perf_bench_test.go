// ENGMODEL-OWNER-UNIT: FU-DSO-TESTENV

package testenv

import "testing"

// TRLC-LINKS: REQ-DSO-179
func BenchmarkNeedNode(b *testing.B) {
	for b.Loop() {
		NeedNode(b)
	}
}
