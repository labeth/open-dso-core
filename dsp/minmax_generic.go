//go:build !arm

// ENGMODEL-OWNER-UNIT: FU-DSO-DSP

package dsp

import "unsafe"

// TRLC-LINKS: REQ-DSO-017
func minMax8Block(p *uint8, n int) (lo, hi uint8) {
	s := unsafe.Slice(p, n) //nolint:gosec // the block ABI the NEON twin shares: p points at n codes
	lo, hi = s[0], s[0]
	for _, v := range s {
		lo, hi = min(lo, v), max(hi, v)
	}
	return lo, hi
}
