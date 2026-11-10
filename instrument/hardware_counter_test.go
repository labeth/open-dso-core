// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT
package instrument

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// TRLC-LINKS: REQ-DSO-009
func TestHardwareCounterSnapshot(t *testing.T) {
	m := New(State{}, nil)
	if n := testing.AllocsPerRun(100, func() { _ = m.Snapshot() }); n != 0 {
		t.Fatalf("absent counter added snapshot allocations: %g", n)
	}
	if m.Snapshot().Counter != nil {
		t.Fatal("counter advertised on a board without one")
	}
	m.SetHardwareCounter(1, sample.Counters{FreqHz: 25000.123, Valid: true})
	got := m.Snapshot().Counter
	if got == nil || got.Source != 1 || !got.Valid || got.Text() != "Counter C2 25.00012 kHz" {
		t.Fatalf("reading: %+v", got)
	}
	got.FreqHz = 1
	if m.Snapshot().Counter.FreqHz != 25000.123 {
		t.Fatal("snapshot mutated the stored reading")
	}
	for _, tc := range []struct {
		name  string
		hz    float64
		valid bool
	}{
		{"waiting", 1, false}, {"negative", -1, true}, {"NaN", math.NaN(), true}, {"infinite", math.Inf(1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.SetHardwareCounter(0, sample.Counters{FreqHz: tc.hz, Valid: tc.valid})
			r := m.Snapshot().Counter
			if r.Valid || r.FreqHz != 0 || r.Text() != "Counter C1 --" {
				t.Fatalf("invalid reading: %+v", r)
			}
		})
	}
	m.SetHardwareCounter(0, sample.Counters{Valid: true})
	if r := m.Snapshot().Counter; !r.Valid || r.Text() != "Counter C1 0 Hz" {
		t.Fatalf("zero frequency: %+v", r)
	}
}
