// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package framecalc

import (
	"testing"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/decode/synth"
	"github.com/labeth/open-dso-core/sample"
)

// Detect gives a find's measures in the frame's samples, a strided
// detection's too: a stepper's STEP pulse (Feature), as its bit time, which
// read in the stride's samples sent the search to a timebase five hundred
// times too fast.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-214
func TestDetectFeatureInFrameSamples(t *testing.T) {
	const sr = 1e8
	b, err := synth.Make(synth.Spec{Proto: "stepper"}, sr)
	if err != nil {
		t.Fatal(err)
	}
	a, d := b.Lines[0].Codes(), b.Lines[1].Codes()
	a, d = append(append([]uint8{}, a...), a...), append(append([]uint8{}, d...), d...)
	n := len(a)
	f := &sample.Frame{Content: sample.RawFullRate, SampleS: 1 / sr, WinPoints: n, Traces: []sample.Trace{{Ch: 0, Codes8: a, Valid: n}, {Ch: 1, Codes8: d, Valid: n}}}
	r := Detect(f, "hex")
	if got, want := decode.FeatureSamples(r)/sr, 50e-6; r.Proto != "stepper" || got < 0.9*want || got > 1.1*want {
		t.Fatalf("%s: feature %g s, want the STEP pulse's %g s", r.Proto, got, want)
	}
}
