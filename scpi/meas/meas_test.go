package meas

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/measure"
)

func square(n, period, phase int) []uint8 {
	c := make([]uint8, n)
	for i := range c {
		c[i] = 28
		if ((i + period - phase) % period) < period/2 {
			c[i] = 228
		}
	}
	return c
}

// Delays between two squares 10 samples apart.
func TestDelay(t *testing.T) {
	a, b := square(1000, 100, 0), square(1000, 100, 10)
	for name, want := range map[string]float64{"LRR": 10e-9, "LFF": 10e-9, "SKEW": 10e-9, "PHA": 36} {
		v, ok := Delay(name, a, b, 1000, 1e-9, 500)
		if !ok || math.Abs(v-want) > want*1e-6 {
			t.Errorf("%s: %v %v", name, v, ok)
		}
	}
	if _, ok := Delay("FRR", a, make([]uint8, 1000), 1000, 1e-9, -1); ok {
		t.Error("flat channel has a delay")
	}
	if !IsDelay("PHA") || IsDelay("PKPK") {
		t.Error("IsDelay")
	}
}

// A reading needs what its parameter needs.
func TestReadings(t *testing.T) {
	r := &measure.Result{Vpp: 2, Freq: 1e6, RiseS: 1e-9}
	pk, _ := Lookup("PKPK")
	fr, _ := Lookup("FREQ")
	ri, _ := Lookup("RISE")
	if pk.Legacy(r) != "2.000000E+00V" || fr.Legacy(r) != NoReading || ri.Legacy(r) != "1.000000E-09s" {
		t.Fatal(pk.Legacy(r), fr.Legacy(r), ri.Legacy(r))
	}
	r.HasTiming = true
	if fr.Legacy(r) != "1.000000E+06Hz" || pk.Legacy(nil) != NoReading {
		t.Fatal(fr.Legacy(r))
	}
	if len(Params) != 18 {
		t.Fatal(len(Params))
	}
}
