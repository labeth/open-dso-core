package simboard

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// q88 is an 8-bit scope's codes with eight fraction bits below them, as
// wire v2 carries its precision words beside its codes.
var q88 = sample.CodeFormat{Bits: 8, Storage: sample.U16, Frac: 8, Max: 255 << 8, Mid: 128 << 8}

// pairsRecord is a peak-pairs record of n Q8.8 words a channel on two
// channels, at the given time/div, its words w(i).
func pairsRecord(n int, tdiv float64, w func(i int) uint16) *sample.Frame {
	f := &sample.Frame{
		Content: sample.PeakPairs, SampleS: 248e-9, CaptureS: 2e-9, AcqDecim: 1, TriggerAt: 435.1,
		Depth: 1 << 20, WinPoints: 4032, Snapshot: sample.Settings{TdivS: tdiv, DisplayedS: tdiv},
	}
	for ch := range 2 {
		words := make([]uint16, n)
		for i := range words {
			words[i] = w(i)
		}
		f.Traces = append(f.Traces, sample.Trace{
			Ch: ch, Format: q88, Codes16: words, Valid: n,
			Scale: sample.Scale{Zero: 128 << 8, PerCode: 0.04 / 256, OffsetV: -1.62}, CodesPerDiv: 25 << 8,
		})
	}
	return f
}

// A scope's record of Q8.8 precision words (its 8-bit codes with their
// calibrated fraction) replays on the 8-bit board as the scope held it:
// its codes rounded as the scope rounds them, the words kept beside them
// as precision words, its scale in codes. At the record's own time/div the
// board's record is the scope's peak-pairs readout word for word, though
// its own plan there is a decimated record of another interval.
// TRLC-LINKS: REQ-DSO-1001
func TestReplayPrecisionPairs(t *testing.T) {
	b := newBoard(t, 2, format8, 25)
	words := []uint16{0x7070, 0x8180, 0x8080, 0x55A2, 0xFF00, 0x0000, 0x1234, 0xA27F}
	rec := pairsRecord(4096, 100e-6, func(i int) uint16 { return words[i%len(words)] })
	if err := b.Replay(rec); err != nil {
		t.Fatal(err)
	}
	f, _, err := acquire(t, b, hal.Channel{On: true, VoltsDiv: 1, Probe: 1}, hal.Request{TimePerDivS: 100e-6},
		edge(0, 0, hal.NormalSweep), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if f.Content != sample.PeakPairs || f.SampleS != rec.SampleS || f.TriggerAt != rec.TriggerAt || f.Len() != 4096 || !f.Flags.Has(sample.Precision) {
		t.Fatalf("content %v, %g s a sample, trigger at %g, %d samples, flags %v", f.Content, f.SampleS, f.TriggerAt, f.Len(), f.Flags)
	}
	tr := f.Traces[0]
	wantCodes := []uint8{0x70, 0x82, 0x81, 0x56, 0xFF, 0x00, 0x12, 0xA2}
	if tr.Format != format8 || tr.PrecisionFrac != 8 || !slices.Equal(tr.Codes8[:8], wantCodes) || !slices.Equal(tr.Precision[:8], words) {
		t.Fatalf("format %+v frac %d codes %x precision %x", tr.Format, tr.PrecisionFrac, tr.Codes8[:8], tr.Precision[:8])
	}
	if tr.Scale != (sample.Scale{Zero: 128, PerCode: 0.04, OffsetV: -1.62}) || tr.CodesPerDiv != 25 {
		t.Fatalf("scale %+v, %g codes a division", tr.Scale, tr.CodesPerDiv)
	}
}

// A record whose words are not this scope's codes, refined or not, is
// refused.
// TRLC-LINKS: REQ-DSO-1001
func TestReplayRefusesOtherConverters(t *testing.T) {
	b := newBoard(t, 2, format8, 25)
	rec := pairsRecord(16, 100e-6, func(int) uint16 { return 0x8000 })
	for i := range rec.Traces {
		rec.Traces[i].Format = format12
	}
	var r *hal.Refusal
	if err := b.Replay(rec); !errors.As(err, &r) {
		t.Fatalf("a 12-bit record on an 8-bit scope: %v", err)
	}
}
