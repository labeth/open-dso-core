package engine

import (
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// ERES smooths each trace's Q8.8 samples, made from the codes when the
// frame has none, and notes its conditioning.
// TRLC-LINKS: REQ-DSO-012
func TestEres(t *testing.T) {
	c := []uint8{100, 100, 100, 104, 104, 104, 100, 100}
	f := &sample.Frame{Traces: []sample.Trace{{Codes8: append([]uint8(nil), c...)}, {Codes8: append([]uint8(nil), c...)}}, SampleS: 1e-6}
	f.SetLen(len(c))
	var scratch []uint16
	Eres(f, 4, &scratch)
	for i := range f.Traces {
		if len(f.Traces[i].Precision) != len(c) {
			t.Fatalf("trace %d: no precision", i)
		}
	}
	if f.Cond.Guard != 2 || f.Cond.NoiseGain == 0 || f.Cond.BandwidthHz == 0 || f.Cond.Filter != "; ERES 4-point boxcar" {
		t.Fatalf("conditioning %+v", f.Cond)
	}
	if f.Traces[0].Precision[3] == 104<<8 && f.Traces[0].Precision[2] == 100<<8 {
		t.Fatal("the step was not smoothed")
	}
}
