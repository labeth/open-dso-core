// ENGMODEL-OWNER-UNIT: FU-DSO-MEASURE

package measure

import (
	"math/rand"
	"slices"
	"testing"
)

// TRLC-LINKS: REQ-DSO-017
func TestNthFloatMatchesSort(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for range 20000 {
		n := 1 + r.Intn(300)
		x := make([]float64, n)
		levels := 1 + r.Intn(20)
		for i := range x {
			x[i] = float64(r.Intn(levels)) + r.Float64()*float64(r.Intn(2))
		}
		sorted := slices.Sorted(slices.Values(x))
		k := r.Intn(n)
		if got := nthFloat(x, k); got != sorted[k] {
			t.Fatalf("n %d k %d: %v, want %v", n, k, got, sorted[k])
		}
	}
}
