// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package shape

import (
	"testing"

	"github.com/labeth/open-dso-core/measure"
)

// The cache measures a frame once; running, the next frame within 100 ms
// reuses it, a configuration change or a stopped capture never does, and an
// expired cache measures again.
// TRLC-LINKS: REQ-DSO-164
func TestMeasCache(t *testing.T) {
	var c MeasCache
	runs := 0
	get := func(k MeasKey, throttle bool) MeasVal {
		return c.Get(k, throttle, func() MeasVal {
			runs++
			return MeasVal{M1: &measure.Result{}, Seq: k.Seq}
		})
	}
	k := MeasKey{Seq: 5, Vpc: [2]float64{0.04, 0.04}}
	if v := get(k, true); v.Seq != 5 || runs != 1 {
		t.Fatalf("first: seq %d runs %d", v.Seq, runs)
	}
	if v := get(k, true); runs != 1 || v.Seq != 5 {
		t.Fatal("the same frame measured twice")
	}
	k.Seq = 6
	if v := get(k, true); runs != 1 || v.Seq != 5 {
		t.Fatalf("running: the next frame was measured (runs %d, seq %d)", runs, v.Seq)
	}
	k.Cpl2 = 1
	if v := get(k, true); runs != 2 || v.Seq != 6 {
		t.Fatal("a coupling change reused the old values")
	}
	k.Seq = 7
	if v := get(k, false); runs != 3 || v.Seq != 7 {
		t.Fatal("a stopped capture reused the old values")
	}
	k.Seq = 8
	c.Expire()
	if v := get(k, true); runs != 4 || v.Seq != 8 {
		t.Fatal("an expired cache reused the old values")
	}
}
