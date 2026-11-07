// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"sync"
	"time"
)

// Zone is one qualification rectangle. Times are SECONDS RELATIVE TO THE
// TRIGGER EDGE (portable across bands); codes are display codes (0..255).
// TRLC-LINKS: REQ-DSO-014
type Zone struct {
	DtLoS, DtHiS   float64
	CodeLo, CodeHi int
	Avoid          bool // true: the frame must NOT enter the zone
	Ch             int  // 0 = C1, 1 = C2
}

// Mask is a per-display-column envelope: a frame FAILS if any sample in
// column j falls outside [Lo[j], Hi[j]]. Columns are the same edge-anchored
// display window the renderer uses (WinCols samples, edge at PosFrac).
// TRLC-LINKS: REQ-DSO-014
type Mask struct {
	Lo, Hi  []uint8
	WinCols int
	Ch      int
	// identity captured at install time: the mask is only comparable while the
	// band (time geometry) and the channel's vertical mapping are unchanged.
	// WinCols alone is NOT an identity — every ≥200 µs/div band clamps to the
	// same 2048 columns while seconds/column differ 10× (review finding).
	TdivS   float64
	SampleS float64
	// FrameIdent marks an identity taken from the published frames (the SRAM
	// engine has no band geometry): the mask compares only with frames of the
	// same window, sample interval, timebase and kind (PeakDetect).
	FrameIdent bool
	PeakDetect bool
	VdivKey    uint64 // channel V/div bits at install (0 = unknown/don't guard)
	OffKey     uint16 // channel offset DAC shadow at install
}

// zone/mask modes
const (
	ZoneOff     = 0
	ZoneTrigger = 1

	MaskOff      = 0
	MaskTest     = 1
	MaskStopFail = 2

	ZoneFallback = 60 // AUTO liveness: publish an unqualified frame every N holds
	MaskRingCap  = 8
)

// MaskFail is one captured failing frame (ring entry).
// TRLC-LINKS: REQ-DSO-014
type MaskFail struct {
	C1, C2 []uint8
	Valid  int
	// Seq is the mask CAPTURE ordinal (the Nth tested frame), not the publish
	// sequence: held frames are tested too, and stamping them with the next
	// publish seq gave every hold-window failure the same — and wrong —
	// identity (found by the publish-policy breaker).
	Seq        uint64
	EdgeX      float64
	SampleS    float64
	WinCols    int
	AtNs       int64 // monotonic capture time (engine clock)
	FailCol    int   // first violating display column
	FailCode   int   // the violating sample value
	FailSample int   // raw sample index of the violation
}

// ZoneMaskState is the zone and mask tests' configuration and failure ring
// under their own lock.
// TRLC-LINKS: REQ-DSO-014
type ZoneMaskState struct {
	// LOCK ORDER: e.mu is acquired BEFORE zm.mu (Snapshot nests that way).
	// Never take e.mu while holding zm.mu — that is an ABBA deadlock against
	// any status poll (found by the concurrency chaos test).
	Mu      sync.Mutex
	Zones   []Zone
	Mask    *Mask
	MaskGen uint64 // each install or clear of the mask counts one (Stats.MaskGen)
	Ring    []MaskFail
	T0      time.Time // engine-clock epoch for AtNs
	EpochOK bool
}

// MaskMapKey is one reading of a mask through time: the mask and the frame
// window it was read for.
type MaskMapKey struct {
	Mask    *Mask
	W       int
	SampleS float64
	Peak    bool
	PosFrac float64
}

// MaskMapState caches the last reading, for the engine goroutine.
type MaskMapState struct {
	Key    MaskMapKey
	Lo, Hi []uint8
}

// BuildMaskFromEnvelope dilates a per-column [lo,hi] envelope by ±tolCols
// horizontally and ±tolCodes vertically — the standard mask morphology. The
// input envelopes must be winCols long.
// TRLC-LINKS: REQ-DSO-014
func BuildMaskFromEnvelope(lo, hi []uint8, winCols, tolCols, tolCodes, ch int) *Mask {
	if len(lo) != winCols || len(hi) != winCols || winCols <= 0 {
		return nil
	}
	// Columns NEVER OBSERVED during the build (per-frame edge position moves the
	// window; its fringes may go uncovered) still carry the accumulator's
	// initial lo>hi. Left alone, dilation turns them into inverted-garbage
	// bounds that fail every sample (found by the 50-wave breaker: env [247,8]).
	// An unobserved column is UNTESTABLE: normalize to the always-pass [0,255]
	// before dilating.
	nLo := make([]uint8, winCols)
	nHi := make([]uint8, winCols)
	for j := range winCols {
		if lo[j] > hi[j] {
			nLo[j], nHi[j] = 0, 255
		} else {
			nLo[j], nHi[j] = lo[j], hi[j]
		}
	}
	// Each column's extremes over the ±tolCols columns about it: a running
	// minimum and maximum, the same at any tolerance (van Herk/Gil-Werman).
	wLo, wHi := slideExtreme(nLo, max(tolCols, 0), false), slideExtreme(nHi, max(tolCols, 0), true)
	outLo := make([]uint8, winCols)
	outHi := make([]uint8, winCols)
	for j := range winCols {
		mn, mx := int(wLo[j]), int(wHi[j])
		mn -= tolCodes
		mx += tolCodes
		if mn < 0 {
			mn = 0
		}
		if mx > 255 {
			mx = 255
		}
		outLo[j] = uint8(mn)
		outHi[j] = uint8(mx)
	}
	return &Mask{Lo: outLo, Hi: outHi, WinCols: winCols, Ch: ch}
}

// slideExtreme is each x[j]'s window x[j-r..j+r] (clipped to x) reduced to
// its minimum, or its maximum (high, as 255 less the minimum of 255-x):
// blocks of the window's width, a running minimum from each block's left and
// from its right, and the two read at a window's ends - three steps a column
// whatever r is (van Herk/Gil-Werman).
// TRLC-LINKS: REQ-DSO-014
func slideExtreme(x []uint8, r int, high bool) []uint8 {
	n, w := len(x), 2*r+1
	m := n + 2*r
	p := make([]uint8, 3*m) // x padded r either side with 255 (adds nothing), then g and h
	xp, g, h := p[:m], p[m:2*m], p[2*m:]
	for i := range xp {
		xp[i] = 255
	}
	flip := uint8(0)
	if high {
		flip = 255
	}
	for i, v := range x {
		xp[r+i] = v ^ flip // 255-v for the maximum
	}
	for i, k := 0, 0; i < m; i++ { // k: i's place in its block (counted, not i%w)
		g[i] = xp[i]
		if k != 0 {
			g[i] = min(g[i-1], g[i])
		}
		if k++; k == w {
			k = 0
		}
	}
	for i, k := m-1, (m-1)%w; i >= 0; i-- {
		h[i] = xp[i]
		if k != w-1 && i+1 < m {
			h[i] = min(h[i+1], h[i])
		}
		if k--; k < 0 {
			k = w - 1
		}
	}
	out := make([]uint8, n)
	for j := range out { // the window x[j-r..j+r] is xp[j .. j+2r]
		out[j] = min(h[j], g[j+2*r]) ^ flip
	}
	return out
}

// FrameIdent is the geometry of the latest published frame (guarded by e.mu),
// the identity a mask installed on the SRAM engine compares against.
type FrameIdent struct {
	TdivS, SampleS float64
	PeakDetect     bool
}
