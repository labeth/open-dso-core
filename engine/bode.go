// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"sync"
)

// The Bode mode states (SetBodeMode), and the accumulation limits.
const (
	BodeOff = 0 // frames are not measured for the frequency response
	BodeOn  = 1 // every frame adds a point to the frequency response

	BodeBinsPerDecade = 30   // log-frequency accumulation resolution
	BodeMaxPoints     = 4096 // hard cap on the accumulated set
	BodeMinCycles     = 4.0  // need at least this many cycles in the record
)

// BodePoint is one accumulated transfer-function sample.
// TRLC-LINKS: REQ-DSO-066
type BodePoint struct {
	FreqHz   float64
	GainDB   float64
	PhaseDeg float64
	Seq      uint64 // capture ordinal of the frame that set this bin (freshness)
}

// TRLC-LINKS: REQ-DSO-066
type BodeState struct {
	Mu    sync.Mutex
	RefCh int // 0 = C1, 1 = C2 (DUT input reference)
	DutCh int // 0 = C1, 1 = C2 (DUT output)
	Bins  map[int]BodePoint
	Cap   uint64 // capture ordinal

	// last live point (for the status readout), guarded by mu
	Live      BodePoint
	LiveValid bool
	Why       string            // why the last capture gave no point ("" when it did)
	Scale     func(int) float64 // input-referred volts/div, including probe factor
}

// FundamentalHz estimates the fundamental frequency of a periodic record by
// mean-level crossing spacing (works for square or sine stimuli; no FFT). The
// single-bin DFT that follows only needs this as an f0 estimate — the gain/
// phase come from the X2/X1 ratio, which is robust to a small f0 error.
// TRLC-LINKS: REQ-DSO-066
func FundamentalHz(sig []uint8, sampleS float64) float64 {
	n := len(sig)
	if n < 8 {
		return 0
	}
	sum := 0 // an integer sum: the float one was exact, every step
	for _, v := range sig {
		sum += int(v)
	}
	mean := float64(sum) / float64(n)
	// rising crossings of the mean with a small hysteresis to reject noise
	const hyst = 2.0
	// codes are integers: v < m is v < ⌈m⌉, v >= m is v >= ⌈m⌉
	armBelow, at := int(math.Ceil(mean-hyst)), int(math.Ceil(mean))
	var firstX, lastX float64
	crossings := 0
	armed := false
	firstX, lastX = -1, -1
	for i := 1; i < n; i++ {
		ai, bi := int(sig[i-1]), int(sig[i])
		if !armed && bi < armBelow {
			armed = true
		}
		if armed && ai < at && bi >= at {
			a, b := float64(ai), float64(bi)
			// linear-interpolated crossing position
			x := float64(i-1) + (mean-a)/(b-a)
			if firstX < 0 {
				firstX = x
			}
			lastX = x
			crossings++
			armed = false
		}
	}
	if crossings < 2 || lastX <= firstX {
		return 0
	}
	periodSamples := (lastX - firstX) / float64(crossings-1)
	if periodSamples <= 1 {
		return 0
	}
	return 1.0 / (periodSamples * sampleS)
}

// SingleBinDFT returns the real/imag parts of the DFT coefficient at frequency
// f0: Σ (v[n]−mean)·exp(−j·2π·f0·n·dt). Mean-subtracted so the DC term does not
// leak into the fundamental.
// TRLC-LINKS: REQ-DSO-066
func SingleBinDFT(sig []uint8, f0, sampleS float64) (re, im float64) {
	n := len(sig)
	var sum float64
	for _, v := range sig {
		sum += float64(v)
	}
	mean := sum / float64(n)
	w := 2 * math.Pi * f0 * sampleS // radians per sample
	// incremental cos/sin via rotation to avoid a trig call per sample
	c, s := math.Cos(w), math.Sin(w)
	cosk, sink := 1.0, 0.0
	for i := range n {
		v := float64(sig[i]) - mean
		re += v * cosk
		im -= v * sink
		cosk, sink = cosk*c-sink*s, sink*c+cosk*s
	}
	return re, im
}
