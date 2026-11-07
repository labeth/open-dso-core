// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labeth/open-dso-core/sample"
)

// FrameMask is the envelope mask test run in software on the frames a
// HAL cycle reads, for a board whose hardware has no mask comparison: the
// mask is a per-column envelope on the 8-bit view (Mask, as the page and
// the UI model build it), anchored at the frame's trigger like the
// display window. Every frame read is tested and counted whether or not
// it is shown; a frame the mask cannot be compared with (another
// timebase, window or vertical scale, an average) counts as skipped, never
// as passed. A failure is copied into a ring, and stop-on-fail stops the
// run on it. Every method is safe for concurrent use; Test runs on the
// owner.
type FrameMask struct {
	mu      sync.Mutex
	mask    *Mask
	gen     uint64
	ring    []MaskFail
	t0      time.Time
	scale   sample.Scale // the vertical scale the mask was first compared at
	scaleOK bool
	mapped  MaskMapState // the mask read through time, for the owner

	mode                      atomic.Int32
	pass, fail, skip, ordinal atomic.Uint64
	stopped                   atomic.Bool
}

// NewFrameMask is a mask test with no mask, off.
// TRLC-LINKS: REQ-DSO-014
func NewFrameMask() *FrameMask { return &FrameMask{t0: time.Now()} }

// SetMask installs a copy of m (nil clears it); its identity is the
// timebase and sample interval it carries (Mask.FrameIdent), and the
// vertical scale of the first frame it is compared with.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) SetMask(m *Mask) {
	var cp *Mask
	if m != nil {
		c := *m
		c.Lo, c.Hi = append([]uint8(nil), m.Lo...), append([]uint8(nil), m.Hi...)
		c.FrameIdent = true
		cp = &c
	}
	x.mu.Lock()
	x.mask, x.scaleOK, x.mapped = cp, false, MaskMapState{}
	x.gen++
	x.mu.Unlock()
}

// ClearMask removes the mask with its counts and failures.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) ClearMask() {
	x.SetMask(nil)
	x.ClearMaskFails()
}

// MaskGen counts every install and clear.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) MaskGen() uint64 {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.gen
}

// SetMaskMode switches the test (MaskOff, MaskTest, MaskStopFail); turning
// it on starts the counts afresh, and leaving stop-on-fail releases its
// latch.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) SetMaskMode(m int) {
	if m < MaskOff || m > MaskStopFail {
		m = MaskOff
	}
	if m != MaskOff && x.mode.Load() == MaskOff {
		x.pass.Store(0)
		x.fail.Store(0)
		x.skip.Store(0)
	}
	if m != MaskStopFail {
		x.stopped.Store(false)
	}
	x.mode.Store(int32(m))
}

// MaskMode is the test's mode.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) MaskMode() int { return int(x.mode.Load()) }

// ClearMaskFails empties the failure ring and the counts.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) ClearMaskFails() {
	x.mu.Lock()
	x.ring = nil
	x.mu.Unlock()
	x.pass.Store(0)
	x.fail.Store(0)
	x.skip.Store(0)
	x.stopped.Store(false)
}

// MaskFails is a copy of the failure ring, the newest last.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) MaskFails() []MaskFail {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]MaskFail(nil), x.ring...)
}

// MaskEnvelope is a copy of the installed mask, nil without one.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) MaskEnvelope() *Mask {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.mask == nil {
		return nil
	}
	c := *x.mask
	c.Lo, c.Hi = append([]uint8(nil), x.mask.Lo...), append([]uint8(nil), x.mask.Hi...)
	return &c
}

// MaskCounts are the test's counts: frames passed, failed and skipped as
// not comparable, whether stop-on-fail stopped the run, the failures kept
// and whether a mask is installed.
type MaskCounts struct {
	Mode             int
	Pass, Fail, Skip uint64
	Stopped, Set     bool
	Ring             int
	Gen              uint64
}

// Counts are the test's counts now.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) Counts() MaskCounts {
	x.mu.Lock()
	set, ring, gen := x.mask != nil, len(x.ring), x.gen
	x.mu.Unlock()
	return MaskCounts{
		Mode: x.MaskMode(), Pass: x.pass.Load(), Fail: x.fail.Load(), Skip: x.skip.Load(),
		Stopped: x.stopped.Load(), Set: set, Ring: ring, Gen: gen,
	}
}

// Test compares frame f with the mask, its trigger posFrac across the
// screen, counts the verdict, keeps a failure and reports whether the run
// stops on it (stop-on-fail). OWNER.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) Test(f *sample.Frame, posFrac float64, averaged bool) (fail, stop bool) {
	mode := int(x.mode.Load())
	if mode == MaskOff || f == nil {
		return false, false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	m := x.mask
	if m == nil || m.WinCols <= 0 {
		return false, false
	}
	var tr *sample.Trace
	for i := range f.Traces {
		if f.Traces[i].Ch == m.Ch {
			tr = &f.Traces[i]
		}
	}
	valid := f.Len()
	if tr == nil || averaged || f.TriggerAt < 0 || valid <= 0 || f.Flags.Has(sample.Roll) {
		x.skip.Add(1)
		return false, false
	}
	if !x.scaleOK {
		x.scale, x.scaleOK = tr.Scale, true
	} else if tr.Scale != x.scale {
		x.skip.Add(1) // another vertical scale or offset: the codes moved under the mask
		return false, false
	}
	w, lo, hi, ok := x.window(m, f, valid, posFrac)
	if !ok {
		x.skip.Add(1)
		return false, false
	}
	seq := x.ordinal.Add(1)
	left := int(math.Round(f.TriggerAt - float64(w)*posFrac))
	failCol, failCode, failSample, tested := -1, 0, 0, 0
	for j := range w {
		s := left + j
		if s < 0 || s >= valid || lo[j] > hi[j] {
			continue // off the record, or no mask column covers its time
		}
		tested++
		if v := tr.Format.Code8(tr.Word(s)); v < lo[j] || v > hi[j] {
			failCol, failCode, failSample = j, int(v), s
			break
		}
	}
	switch {
	case failCol < 0 && tested == 0:
		x.skip.Add(1) // a window wholly off the record would pass on nothing
		return false, false
	case failCol < 0:
		x.pass.Add(1)
		return false, false
	}
	x.fail.Add(1)
	mf := MaskFail{
		Valid: valid, Seq: seq, EdgeX: f.TriggerAt, SampleS: f.SampleS, WinCols: w,
		AtNs: int64(time.Since(x.t0)), FailCol: failCol, FailCode: failCode, FailSample: failSample,
	}
	for i := range f.Traces {
		switch f.Traces[i].Ch {
		case 0:
			mf.C1 = f.Traces[i].View8()
			if f.Traces[i].Format.Storage != sample.U16 {
				mf.C1 = append([]uint8(nil), mf.C1...)
			}
		case 1:
			mf.C2 = f.Traces[i].View8()
			if f.Traces[i].Format.Storage != sample.U16 {
				mf.C2 = append([]uint8(nil), mf.C2...)
			}
		}
	}
	x.ring = append(x.ring, mf)
	if len(x.ring) > MaskRingCap {
		x.ring = x.ring[len(x.ring)-MaskRingCap:]
	}
	if mode == MaskStopFail {
		x.stopped.Store(true)
		return true, true
	}
	return true, false
}

// window is the frame's screen window in its own samples and the mask's
// bounds for each of them: the mask's columns when the frame samples the
// screen as the mask was built, else the mask read through
// trigger-relative time. ok is false for a frame of another timebase or
// span. x.mu is held.
// TRLC-LINKS: REQ-DSO-014
func (x *FrameMask) window(m *Mask, f *sample.Frame, valid int, posFrac float64) (w int, lo, hi []uint8, ok bool) {
	w = f.WinPoints
	if w <= 0 || w > valid {
		w = valid
	}
	if m.TdivS > 0 && m.TdivS != f.Snapshot.TdivS && f.Snapshot.TdivS > 0 {
		return w, nil, nil, false
	}
	peak := f.Content == sample.PeakPairs
	if w == m.WinCols && (m.SampleS == 0 || m.SampleS == f.SampleS) && m.PeakDetect == peak {
		return w, m.Lo, m.Hi, true
	}
	cols := min(m.WinCols, len(m.Lo), len(m.Hi))
	if cols <= 0 || !(f.SampleS > 0) || !(m.SampleS > 0) {
		return w, nil, nil, false
	}
	span := float64(m.WinCols) * m.SampleS
	if math.Abs(float64(w)*f.SampleS-span) > 0.01*span+0.5*math.Max(f.SampleS, m.SampleS) {
		return w, nil, nil, false
	}
	key := MaskMapKey{Mask: m, W: w, SampleS: f.SampleS, Peak: peak, PosFrac: posFrac}
	if x.mapped.Key == key {
		return w, x.mapped.Lo, x.mapped.Hi, true
	}
	ratio, reach := f.SampleS/m.SampleS, 1.0
	if peak {
		reach = 2
	}
	lo, hi = make([]uint8, w), make([]uint8, w)
	for i := range w {
		jc := (float64(i)-float64(w)*posFrac)*ratio + float64(m.WinCols)*posFrac
		a := max(int(math.Floor(jc-reach*ratio-0.5)), 0)
		b := min(int(math.Ceil(jc+reach*ratio+0.5)), cols-1)
		l, h := uint8(255), uint8(0)
		for j := a; j <= b; j++ {
			l, h = min(l, m.Lo[j]), max(h, m.Hi[j])
		}
		lo[i], hi[i] = l, h
	}
	x.mapped = MaskMapState{Key: key, Lo: lo, Hi: hi}
	return w, lo, hi, true
}

// MaskFromFrame builds a mask around channel ch of frame f, its trigger
// posFrac across a screen of divsH divisions: each screen column's sample
// on the 8-bit view, widened by xDivs divisions either way in time and
// yDivs divisions either way in level (a pass/fail mask made from the
// current waveform). Nil when f has no such trace or no trigger.
// TRLC-LINKS: REQ-DSO-014
func MaskFromFrame(f *sample.Frame, ch int, posFrac, xDivs, yDivs float64, divsH int) *Mask {
	if f == nil || f.TriggerAt < 0 || divsH <= 0 {
		return nil
	}
	var tr *sample.Trace
	for i := range f.Traces {
		if f.Traces[i].Ch == ch {
			tr = &f.Traces[i]
		}
	}
	valid := f.Len()
	w := f.WinPoints
	if w <= 0 || w > valid {
		w = valid
	}
	if tr == nil || w <= 0 {
		return nil
	}
	lo, hi := make([]uint8, w), make([]uint8, w)
	left := int(math.Round(f.TriggerAt - float64(w)*posFrac))
	for j := range w {
		lo[j], hi[j] = 255, 0 // a column off the record stays untested
		if s := left + j; s >= 0 && s < valid {
			v := tr.Format.Code8(tr.Word(s))
			lo[j], hi[j] = v, v
		}
	}
	cols := int(math.Round(xDivs * float64(w) / float64(divsH)))
	codes := int(math.Round(yDivs * tr.CodesPerDiv8()))
	m := BuildMaskFromEnvelope(lo, hi, w, cols, codes, ch)
	if m != nil {
		m.TdivS, m.SampleS, m.FrameIdent = f.Snapshot.TdivS, f.SampleS, true
		m.PeakDetect = f.Content == sample.PeakPairs
	}
	return m
}
