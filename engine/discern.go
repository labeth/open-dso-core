// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"github.com/labeth/open-dso-core/dsp"
)

// Ptp returns min, max and peak-to-peak of sig.
// TRLC-LINKS: REQ-SDS-009, REQ-SDS-011
func Ptp(sig []uint8) (lo, hi, p int) {
	if len(sig) == 0 {
		return 0, 0, 0
	}
	l, h := dsp.MinMax8(sig)
	lo, hi = int(l), int(h)
	return lo, hi, hi - lo
}

// NoiseFloor estimates the per-sample ADC noise as the MEDIAN absolute second
// difference |s[i+1] − 2·s[i] + s[i−1]|. The second difference cancels any
// linear ramp (independent of its slope AND of the signal period), leaving only
// high-frequency noise — so this is a PERIOD-INDEPENDENT noise estimate. That is
// what a moving-average gate could not be: a fixed window washes out a signal
// with many periods on screen (aliased/slow-timebase), whereas this holds at
// every timebase. Exact median via a histogram over the small integer |Δ²|
// values: O(n) time, O(1) space. Floored at 0.5 (a perfectly clean slow ramp has
// median |Δ²| = 0 because adjacent codes repeat).
// TRLC-LINKS: REQ-SDS-011
func NoiseFloor(sig []uint8) float64 {
	n := len(sig)
	if n < 3 {
		return 0.5
	}
	var hist [513]int
	total := 0
	for i := 1; i < n-1; i++ {
		d := int(sig[i+1]) - 2*int(sig[i]) + int(sig[i-1])
		if d < 0 {
			d = -d
		}
		if d > 512 {
			d = 512
		}
		hist[d]++
		total++
	}
	half := total / 2
	cum, med := 0, 0
	for b := range 513 {
		if cum += hist[b]; cum > half {
			med = b
			break
		}
	}
	if med < 1 {
		return 0.5
	}
	return float64(med)
}

// SignalPresent reports whether the record holds a real signal rather than a
// flat / quiet rail: peak-to-peak must exceed k times the noiseFloor. Raw ptp
// alone CANNOT decide this — a noisy rail (ptp up to ~18 at σ≈2.5) can exceed a
// real small on-screen signal (a 2.4 Vpp cal signal is ptp 8 at 10 V/div) — but
// ptp-relative-to-noise separates them cleanly at ANY timebase (a real signal
// has ptp ≫ noise; a rail's ptp IS its noise, ~5σ). Used as the DECIMATED lock
// gate; native-fast keeps the raw-ptp test (its record spans < 1 period).
// TRLC-LINKS: REQ-SDS-011
func SignalPresent(sig []uint8, k float64) bool {
	_, _, p := Ptp(sig)
	return float64(p) >= k*NoiseFloor(sig)
}

// MidLevel is the crossing threshold: (min+max)/2 over the drained samples
// (128 for an empty slice). It floats with amplitude so it works at any V/div.
// TRLC-LINKS: REQ-SDS-011
func MidLevel(sig []uint8) int {
	if len(sig) == 0 {
		return 128
	}
	lo, hi, _ := Ptp(sig)
	return (lo + hi) / 2
}
