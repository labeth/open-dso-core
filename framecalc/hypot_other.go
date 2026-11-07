//go:build !arm

// ENGMODEL-OWNER-UNIT: FU-DSO-FRAMECALC

package framecalc

import "math"

// hypotFinite is math.Hypot (in assembly on some architectures).
// TRLC-LINKS: REQ-DSO-070
func hypotFinite(p, q float64) float64 { return math.Hypot(p, q) }
