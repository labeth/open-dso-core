// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"math"
	"time"
)

// Take bounds: STOP keeps the newest soundStopTakeS; a triggered take is
// pre plus post, at most soundTakeMaxS (the ring holds 17 s).
const (
	SoundStopTakeS = 15.0
	soundTakeMaxS  = 16.0
	soundPreMaxS   = 10.0
	soundPostMinS  = 0.1
)

// SoundTake is a frozen stretch of the audio stream: the samples, scales,
// gaps and marks of First..First+N-1, and the trigger in it.
type SoundTake struct {
	SoundBlock
	ID         uint64
	Why        string // "stop", "single" or "normal"
	HasTrig    bool
	Trig       uint64 // the trigger's sample (stream ordinal)
	TrigCh     int
	TrigRising bool
	TrigLevel  uint8 // the FPGA's level, an 8-bit code
	At         time.Time
}

// SoundTakeTimes bounds pre- and post-trigger seconds.
// TRLC-LINKS: REQ-SDS-275
func SoundTakeTimes(pre, post float64) (float64, float64) {
	if !(pre >= 0) || math.IsInf(pre, 0) {
		pre = 1
	}
	if !(post >= soundPostMinS) || math.IsInf(post, 0) {
		post = 4
	}
	pre = math.Min(pre, soundPreMaxS)
	post = math.Min(post, soundTakeMaxS-soundPostMinS)
	if pre+post > soundTakeMaxS {
		pre = soundTakeMaxS - post
	}
	return pre, post
}

// FirstCrossingFrom is the first crossing at index start or later; the
// comparator's arming runs from the start of q (a crossing before start
// disarms it as in the fabric).
// TRLC-LINKS: REQ-SDS-275
func FirstCrossingFrom(q []uint16, level, hyst int, rising bool, start int) (int, bool) {
	armed := false
	if !rising { // the rising test on negated codes: arm at or above level+hyst, fire at or below level
		armAt, fireAt := level+hyst, level
		for i, w := range q {
			switch c := int(w >> 8); {
			case c >= armAt:
				armed = true
			case armed && c <= fireAt:
				if i >= start {
					return i, true
				}
				armed = false
			}
		}
		return 0, false
	}
	armAt, fireAt := level-hyst, level
	for i, w := range q {
		switch c := int(w >> 8); {
		case c <= armAt:
			armed = true
		case armed && c >= fireAt:
			if i >= start {
				return i, true
			}
			armed = false
		}
	}
	return 0, false
}
