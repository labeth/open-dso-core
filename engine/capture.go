// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"time"
)

// TRLC-LINKS: REQ-DSO-040
func RoundQ8(x uint16) uint8 {
	v := min((uint32(x)+128)>>8, 255)
	return uint8(v)
}

// QBuffer is q sized to n, reused when it holds n; a buffer four times
// larger than needed (a deep stopped record's, under live frames again) is
// let go: the frame slots kept a 1 M-sample record's Q8.8 codes for good,
// and with the other held copies the heap sat at the memory limit.
// TRLC-LINKS: REQ-DSO-040
func QBuffer(q []uint16, n int) []uint16 {
	if cap(q) < n || cap(q) >= 1<<18 && cap(q) > 4*n {
		return make([]uint16, n)
	}
	return q[:n]
}

// AutoForceDue reports AUTO's wait for a trigger over: running AUTO (or a
// stopped scope's SINGLE served at once), waited past autoWait, and the
// hardware not triggered - a triggered record only fills its post-trigger
// part, which at slow timebases outlasts the wait (10ms/div: 135 ms).
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-011
func AutoForceDue(norm, running, triggered bool, waited, autoWait time.Duration) bool {
	return (!norm || !running) && !triggered && waited >= autoWait
}

// StatusPollWait is the wait between the status polls of a capture waiting
// for its event, waited since its arm: 1 ms while a fast record and its
// trigger are still due, longer for one that goes on waiting (a slow or
// NORMAL trigger, a serial match). Each poll is a bus read on the worker;
// a capture past 10 ms is seen at most 2 ms later.
// TRLC-LINKS: REQ-DSO-010, REQ-DSO-011
func StatusPollWait(waited time.Duration) time.Duration {
	if waited < 10*time.Millisecond {
		return time.Millisecond
	}
	return 2 * time.Millisecond
}

// SettingsPollEvery is how often a capture waiting for its event looks for
// changed settings (a fresh capture with them): building and comparing the
// whole configuration on every status poll was a third of the wait's work.
const SettingsPollEvery = 4 * time.Millisecond

// ChScale is a channel's front-end scale: analog V/div (the codes' range) and
// the applied input-referred offset. A code reads (code-128)·vdiv/25 - off.
type ChScale struct{ Vdiv, Off float64 }

// RescaleCodes re-expresses codes captured at one scale at another, so every
// consumer that reads them at the live V/div and offset sees the captured
// volts. Codes that leave the 8-bit range clip, as on the screen.
// TRLC-LINKS: REQ-DSO-015, REQ-DSO-040
func RescaleCodes(c []uint8, q []uint16, from, to ChScale) {
	if !(from.Vdiv > 0) || !(to.Vdiv > 0) {
		return
	}
	k := from.Vdiv / to.Vdiv
	b := (to.Off - from.Off) * 25 / to.Vdiv
	// Tables, not per-sample float math: 2M samples of float clamping take
	// 3.7 s on the ARM.
	var lut8 [256]uint8
	for v := range lut8 {
		lut8[v] = uint8(math.Round(math.Max(0, math.Min(255, 128+(float64(v)-128)*k+b))))
	}
	for i, v := range c {
		c[i] = lut8[v]
	}
	if len(q) == 0 {
		return
	}
	// Fixed point, not a 64K table: random lookups into 128 KB miss the ARM's
	// caches on every sample (~0.6 s for a deep record per V/div step).
	kf := int64(math.Round(k * (1 << 16)))
	bf := int64(math.Round((32768 + b*256) * (1 << 16)))
	for i, v := range q {
		x := ((int64(v)-32768)*kf + bf + 1<<15) >> 16
		q[i] = uint16(min(max(x, 0), 65535))
	}
}
