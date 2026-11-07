// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// A decimated record's clock replaces a learned SPI clock only where it
// would resolve it: a 10 MHz clock filtered to its alias on 32 ns samples
// keeps the 10 MHz; the full rate's samples measure a bus that slowed.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-229
func TestDecimatedAliasKeepsTheLearnedClock(t *testing.T) {
	if ResolvesClock(10e6, &sample.Frame{SampleS: 32e-9, AcqDecim: int(16), Traces: []sample.Trace{{}, {Ch: 1}}}) {
		t.Fatal("3.1 samples a 10 MHz period replaced it")
	}
	if !ResolvesClock(10e6, &sample.Frame{SampleS: 2e-9, AcqDecim: int(1), Traces: []sample.Trace{{}, {Ch: 1}}}) || !ResolvesClock(1e6, &sample.Frame{SampleS: 32e-9, AcqDecim: int(16), Traces: []sample.Trace{{}, {Ch: 1}}}) {
		t.Fatal("a frame that resolves the clock kept the old")
	}
	if !ResolvesClock(0, &sample.Frame{SampleS: 1e-6, AcqDecim: int(512), Traces: []sample.Trace{{}, {Ch: 1}}}) {
		t.Fatal("no clock learned: the first measurement refused")
	}
}
