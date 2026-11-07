// ENGMODEL-OWNER-UNIT: FU-DSO-DSP

package dsp

// minMax8Block is the extremes of the n codes at p, n a multiple of 32 and
// at least 32.
//
// TRLC-LINKS: REQ-DSO-017
//
//go:noescape
func minMax8Block(p *uint8, n int) (lo, hi uint8)
