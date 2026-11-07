// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERVISOR-SYSINFO

package sysinfo

import "testing"

// TRLC-LINKS: REQ-DSO-102
func BenchmarkUname(b *testing.B) {
	for b.Loop() {
		Uname()
	}
}

// TRLC-LINKS: REQ-DSO-102
func BenchmarkMem(b *testing.B) {
	for b.Loop() {
		mem()
	}
}
