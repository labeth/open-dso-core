// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package shape

import (
	"sync"
	"time"

	"github.com/labeth/open-dso-core/measure"
)

// MeasKey is what a frame's auto-measurements depend on, by value: the
// frame, each channel's coupling, the volts per code, the offsets and the
// sample interval. A change from any path (the web, SCPI, the panel)
// changes the key and refreshes the measurements.
type MeasKey struct {
	Seq        uint64
	Cpl1, Cpl2 int
	Vpc, Off   [2]float64
	SampleS    float64
}

// MeasVal is a frame's measurements: each channel's result and whether it
// railed, the frame measured and its edge windows' read.
type MeasVal struct {
	M1, M2       *measure.Result
	Clip1, Clip2 bool
	Seq          uint64 // the frame measured
	EdgeRead     uint64 // its edge windows' read
}

// MeasCache is the single-entry measurement cache. measure.Compute over a
// deep record is the heaviest in-lock CPU besides serialization, and every
// frame request needs the same numbers for a given published frame — so it
// runs once per key and is shared.
type MeasCache struct {
	mu  sync.Mutex
	key MeasKey
	val MeasVal
	at  time.Time // when val was computed (drives the fast-flow throttle)
}

// Get returns the measurements for key, computing them with compute when
// the key changed. Fast-flow throttle: while frames stream at full rate
// (throttle), it recomputes at most every 100 ms and reuses the previous
// values in between — the numbers jitter per frame anyway, and at 20 fps ×
// 2×20480 samples the computation was a large share of the device's
// per-frame CPU. Only a seq advance may reuse: any configuration change
// (coupling, scale, offset) recomputes at once, and a caller passes
// throttle false for single-shot or stopped captures, where one frame's
// values must be exact (a stale reuse there would never be corrected by a
// next frame).
// TRLC-LINKS: REQ-DSO-164
func (c *MeasCache) Get(key MeasKey, throttle bool, compute func() MeasVal) MeasVal {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != key {
		sameCfg := key.Cpl1 == c.key.Cpl1 && key.Cpl2 == c.key.Cpl2 &&
			key.Vpc == c.key.Vpc && key.Off == c.key.Off && key.SampleS == c.key.SampleS
		if !throttle || !sameCfg || c.val.M1 == nil || time.Since(c.at) >= 100*time.Millisecond {
			c.key = key
			c.at = time.Now()
			c.val = compute()
		}
	}
	return c.val
}

// Expire makes the next Get recompute even within the throttle window.
// TRLC-LINKS: REQ-DSO-164
func (c *MeasCache) Expire() {
	c.mu.Lock()
	c.at = time.Time{}
	c.mu.Unlock()
}
