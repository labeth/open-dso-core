// ENGMODEL-OWNER-UNIT: FU-APP-FRAMES

package fanout

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/labeth/open-dso-core/sample"
)

// tickSource publishes a fresh frame with an advancing Seq each time `fresh`
// is armed; otherwise it re-presents the last frame as stale.
// TRLC-LINKS: REQ-SDS-020
type tickSource struct {
	seq   atomic.Uint64
	fresh atomic.Bool
	f     sample.Frame
}

// TRLC-LINKS: REQ-SDS-020
func (s *tickSource) Consume() (*sample.Frame, bool) {
	if !s.fresh.Swap(false) {
		return &s.f, false
	}
	s.f.Seq = s.seq.Add(1)
	s.f.Traces = []sample.Trace{
		{Valid: 4, Codes8: []uint8{1, 2, 3, 4}},
		{Valid: 4, Codes8: []uint8{4, 3, 2, 1}},
	}
	return &s.f, true
}

// A parked waiter wakes on the next publish. In a synctest bubble the
// ticker's time is the bubble's and Wait returns once the waiter is parked.
// TRLC-LINKS: REQ-SDS-020
func TestWaitNextWakesOnPublish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &tickSource{}
		fo := NewFrames()
		stop := make(chan struct{})
		defer close(stop)
		go fo.Run(src, stop)

		src.fresh.Store(true)
		if seq := fo.WaitNext(0, time.Second); seq != 1 {
			t.Fatalf("WaitNext(0) = %d, want 1", seq)
		}
		done := make(chan uint64, 1)
		go func() { done <- fo.WaitNext(1, time.Second) }()
		synctest.Wait() // the waiter is parked
		select {
		case <-done:
			t.Fatal("WaitNext returned with nothing new")
		default:
		}
		src.fresh.Store(true)
		if seq := <-done; seq != 2 {
			t.Fatalf("parked WaitNext = %d, want 2", seq)
		}
	})
}

// With nothing new, WaitNext returns the current Seq after its timeout.
// TRLC-LINKS: REQ-SDS-020
func TestWaitNextTimesOutIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &tickSource{}
		fo := NewFrames()
		stop := make(chan struct{})
		defer close(stop)
		go fo.Run(src, stop)

		src.fresh.Store(true)
		if seq := fo.WaitNext(0, time.Second); seq != 1 {
			t.Fatalf("setup publish: seq = %d", seq)
		}
		t0 := time.Now()
		seq := fo.WaitNext(1, 120*time.Millisecond) // nothing new coming
		if el := time.Since(t0); el != 120*time.Millisecond {
			t.Fatalf("timeout took %v, want 120ms", el)
		}
		if seq != 1 {
			t.Fatalf("idle WaitNext = %d, want unchanged 1", seq)
		}
	})
}

// TRLC-LINKS: REQ-SDS-020
func TestWaitNextConcurrentWithReaders(t *testing.T) {
	src := &tickSource{}
	fo := NewFrames()
	stop := make(chan struct{})
	defer close(stop)
	go fo.Run(src, stop)

	// Hammer WithFrame while frames publish and waiters park (race detector food).
	done := make(chan struct{})
	go func() {
		for range 50 {
			fo.WithFrame(func(_ *sample.Frame) {})
			time.Sleep(time.Millisecond)
		}
		close(done)
	}()
	last := uint64(0)
	for i := range 3 {
		src.fresh.Store(true)
		got := fo.WaitNext(last, time.Second)
		if got == last {
			t.Fatalf("iteration %d: no advance from %d", i, last)
		}
		last = got
	}
	<-done
}

// The snapshot is a copy: the producer reusing its frame does not reach it,
// and a reader sees nil before the first publish.
// TRLC-LINKS: REQ-SDS-020
func TestSnapshotIsACopy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &tickSource{}
		fo := NewFrames()
		stop := make(chan struct{})
		defer close(stop)
		fo.WithFrame(func(f *sample.Frame) {
			if f != nil {
				t.Error("a frame before any publish")
			}
		})
		go fo.Run(src, stop)
		src.fresh.Store(true)
		fo.WaitNext(0, time.Second)
		src.f.Traces[0].Codes8[0] = 99
		fo.WithFrame(func(f *sample.Frame) {
			if f.Seq != 1 || f.Traces[0].Codes8[0] != 1 {
				t.Errorf("snapshot seq %d code %d", f.Seq, f.Traces[0].Codes8[0])
			}
		})
	})
}

// The fan-out runs over another frame type with the scope's copy function.
// TRLC-LINKS: REQ-SDS-020
func TestOtherFrameType(t *testing.T) {
	type frame struct {
		Seq   uint64
		Codes []byte
	}
	copies := 0
	fo := New(func(dst, src *frame) {
		copies++
		dst.Seq = src.Seq
		dst.Codes = append(dst.Codes[:0], src.Codes...)
	}, func(f *frame) uint64 { return f.Seq })
	f := &frame{Seq: 3, Codes: []byte{7}}
	fo.copyFn(&fo.latest, f) // as one Run tick does
	fo.has = true
	if seq := fo.WaitNext(2, time.Second); seq != 3 || copies != 1 {
		t.Fatalf("seq %d copies %d", seq, copies)
	}
}
