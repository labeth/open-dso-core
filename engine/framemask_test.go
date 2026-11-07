package engine

import (
	"testing"

	"github.com/labeth/open-dso-core/sample"
)

// maskFrame is a 12-bit frame of n samples on channel ch whose 8-bit view
// reads v everywhere but sample at, which reads spike; the trigger sits mid
// record.
func maskFrame(n, ch int, v, spike uint8, at int) *sample.Frame {
	f := sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Max: 0xFFF0, Mid: 0x8000}
	words := make([]uint16, n)
	for i := range words {
		c := v
		if i == at {
			c = spike
		}
		words[i] = uint16(c) << 8
	}
	return &sample.Frame{
		Traces:    []sample.Trace{{Ch: ch, Format: f, Codes16: words, Valid: n, Scale: sample.Scale{PerCode: 1}}},
		SampleS:   1e-9,
		WinPoints: n,
		TriggerAt: float64(n) / 2,
		Snapshot:  sample.Settings{TdivS: 1e-7},
	}
}

// The software mask passes a frame inside it, fails one that leaves it
// (keeping the failure, and stopping the run with stop-on-fail), and skips
// frames it cannot compare: another timebase, another vertical scale, an
// average, no trace of its channel.
// TRLC-LINKS: REQ-DSO-014
func TestFrameMask(t *testing.T) {
	const n = 1000
	x := NewFrameMask()
	lo, hi := make([]uint8, n), make([]uint8, n)
	for i := range lo {
		lo[i], hi[i] = 100, 140
	}
	x.SetMask(&Mask{Lo: lo, Hi: hi, WinCols: n, Ch: 2, TdivS: 1e-7, SampleS: 1e-9})
	if fail, _ := x.Test(maskFrame(n, 2, 120, 120, 0), .5, false); fail {
		t.Fatal("mask off: tested")
	}
	x.SetMaskMode(MaskTest)
	if fail, stop := x.Test(maskFrame(n, 2, 120, 120, 0), .5, false); fail || stop {
		t.Fatal("a frame inside fails")
	}
	if fail, stop := x.Test(maskFrame(n, 2, 120, 200, 600), .5, false); !fail || stop {
		t.Fatalf("a frame outside: fail %v stop %v", fail, stop)
	}
	if fs := x.MaskFails(); len(fs) != 1 || fs[0].FailSample != 600 || fs[0].FailCode != 200 {
		t.Fatalf("failures %+v", fs)
	}
	other := maskFrame(n, 2, 120, 200, 600)
	other.Snapshot.TdivS = 2e-7
	x.Test(other, .5, false)
	scaled := maskFrame(n, 2, 120, 200, 600)
	scaled.Traces[0].Scale.PerCode = 2
	x.Test(scaled, .5, false)
	x.Test(maskFrame(n, 2, 120, 200, 600), .5, true)
	x.Test(maskFrame(n, 0, 120, 200, 600), .5, false)
	if k := x.Counts(); k.Pass != 1 || k.Fail != 1 || k.Skip != 4 || !k.Set || k.Ring != 1 {
		t.Fatalf("counts %+v", k)
	}
	x.SetMaskMode(MaskStopFail)
	if _, stop := x.Test(maskFrame(n, 2, 120, 200, 600), .5, false); !stop || !x.Counts().Stopped {
		t.Fatal("stop on fail")
	}
	x.ClearMask()
	if k := x.Counts(); k.Set || k.Ring != 0 || k.Fail != 0 || k.Stopped {
		t.Fatalf("cleared: %+v", k)
	}
}
