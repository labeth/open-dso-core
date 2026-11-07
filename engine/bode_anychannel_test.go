package engine

import (
	"math"
	"testing"

	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// sineFrame is a four-channel 8-bit frame: C3 a sine of amp3 codes, C4 the
// same tone at amp4 codes delayed by lag samples, C1 absent (switched off).
func sineFrame(n int, period, amp3, amp4, lag float64) *sample.Frame {
	f := &sample.Frame{SampleS: 1e-6, TrigKind: sample.HardwareNode, Traces: make([]sample.Trace, 4)}
	for ch := range f.Traces {
		f.Traces[ch] = sample.Trace{Ch: ch, Valid: n, Format: sample.Format8, CodesPerDiv: 25}
	}
	for ch := 1; ch < 4; ch++ {
		f.Traces[ch].Codes8 = make([]uint8, n)
		for i := range n {
			v := 128.0
			switch ch {
			case 2:
				v += amp3 * math.Sin(2*math.Pi*float64(i)/period)
			case 3:
				v += amp4 * math.Sin(2*math.Pi*(float64(i)-lag)/period)
			}
			f.Traces[ch].Codes8[i] = uint8(math.Round(v))
		}
	}
	return f
}

// The frequency response takes its reference and DUT on any channels, C1
// switched off included, and says why a channel without a trace gives no
// point.
// TRLC-LINKS: REQ-DSO-066
func TestBodeOnAnyChannels(t *testing.T) {
	b := NewBode()
	b.SetBodeMode(true, 2, 3)
	b.Frame(sineFrame(4000, 100, 80, 40, 25), func(int) float64 { return 1 })
	var st instrument.Stats
	b.FillStats(&st)
	if !st.BodeValid || st.BodeRef != 2 || st.BodeDut != 3 || st.BodeMode != BodeOn {
		t.Fatalf("no point on C3/C4: %+v", st)
	}
	if math.Abs(st.BodeGainDB+6.02) > 0.3 || math.Abs(math.Abs(st.BodePhaseDeg)-90) > 3 {
		t.Fatalf("gain %.2f dB phase %.1f°, want -6 dB, 90°", st.BodeGainDB, st.BodePhaseDeg)
	}
	if math.Abs(st.BodeFreqHz-10e3) > 100 {
		t.Fatalf("tone %g Hz", st.BodeFreqHz)
	}
	b.SetBodeMode(true, 0, 3)
	b.Frame(sineFrame(4000, 100, 80, 40, 25), nil)
	b.FillStats(&st)
	if st.BodeValid || st.BodeWhy == "" {
		t.Fatalf("C1 off gave a point: %+v", st)
	}
}
