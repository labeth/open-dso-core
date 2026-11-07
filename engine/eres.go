// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"fmt"
	"math"

	"github.com/labeth/open-dso-core/dsp"
	"github.com/labeth/open-dso-core/sample"
)

// EnsurePrecision gives each trace of frame f that has none Q8.8 samples
// made from its 8-bit codes, in bufs[i]'s storage when it is given (the
// frame slot's buffers kept between frames).
// TRLC-LINKS: REQ-DSO-012
func EnsurePrecision(f *sample.Frame, bufs ...[]uint16) {
	n := f.Len()
	for i := range f.Traces {
		t := &f.Traces[i]
		if len(t.Precision) == n {
			continue
		}
		var q []uint16
		if i < len(bufs) {
			q = bufs[i]
		}
		t.Precision = QBuffer(q, n)
		c, p := t.Codes8[:n], t.Precision
		for k := range n {
			p[k] = uint16(c[k]) << 8
		}
	}
}

// Eres filters frame f in place with the enhanced-resolution mode's
// length-point boxcar on its Q8.8 samples (made from its codes when it has
// none, in bufs' storage), rounds its codes from them and notes the filter,
// its guard, noise gain and bandwidth in the frame's conditioning. scratch
// is the caller's working buffer, kept between frames (a deep record's is
// megabytes, so the caller shares it with its other conditioning).
// TRLC-LINKS: REQ-DSO-012
func Eres(f *sample.Frame, length int, scratch *[]uint16, bufs ...[]uint16) {
	EnsurePrecision(f, bufs...)
	n := f.Len()
	*scratch = QBuffer(*scratch, n)
	for i := range f.Traces {
		EresQ8(f.Traces[i].Precision, *scratch, length)
	}
	for i := range f.Traces {
		dsp.RoundQ8(f.Traces[i].Codes8[:n], f.Traces[i].Precision[:n])
	}
	f.Cond.Filter += fmt.Sprintf("; ERES %d-point boxcar", length)
	f.Cond.Guard += length / 2
	bw := .443 / (float64(length) * f.SampleS)
	f.Cond.NoiseGain = math.Sqrt(float64(length))
	if f.Cond.BandwidthHz == 0 || bw < f.Cond.BandwidthHz {
		f.Cond.BandwidthHz = bw
	}
}
