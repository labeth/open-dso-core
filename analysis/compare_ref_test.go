// ENGMODEL-OWNER-UNIT: FU-DSO-ANALYSIS

package analysis

// Repeats is RepeatsBand for captures read as one sample a column (each
// sample its own low and high): every sample of each lies within +-tolDiv of
// the other's min/max envelope over +-slack samples.
// TRLC-LINKS: REQ-DSO-218
func Repeats(prev, cur []float64, tolDiv float64, slack int) bool {
	if len(prev) != len(cur) {
		return false
	}
	return RepeatsBand(prev, prev, cur, cur, tolDiv, slack)
}
