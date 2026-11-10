// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE
package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

type readingCounter struct {
	reading sample.Counters
	err     error
	calls   int
}

func (r *readingCounter) Read() (sample.Counters, error) { r.calls++; return r.reading, r.err }

func (r *readingCounter) Source() hal.Source { return hal.Source{Kind: hal.Analog, Index: 0} }

type counterBoard struct {
	*halfake.Board
	counter *readingCounter
}

func (b counterBoard) Counter() (hal.Counter, bool) { return b.counter, true }

// TRLC-LINKS: REQ-DSO-008, REQ-DSO-009
func TestHALHardwareCounter(t *testing.T) {
	r := &readingCounter{reading: sample.Counters{FreqHz: 12345, Valid: true}}
	b := counterBoard{halfake.New(halfake.Config{}), r}
	m := instrument.New(loopState(hal.NormalSweep), nil)
	_, c := NewHALLoop(HALConfig{Board: b, Instrument: m, IdleWait: time.Millisecond})
	c.Open()
	defer c.Close()
	c.Plan(context.Background())
	c.Serve()
	if got := m.Snapshot().Counter; got == nil || !got.Valid || got.FreqHz != 12345 {
		t.Fatalf("NORMAL waiting: %+v", got)
	}
	calls := r.calls
	c.Serve()
	if r.calls != calls {
		t.Fatal("counter polled on every frame/service")
	}
	m.SetRunning(false)
	c.Plan(context.Background())
	c.readCounter(time.Now().Add(time.Second))
	if got := m.Snapshot(); got.Running || got.Counter == nil || !got.Counter.Valid || r.calls != calls+1 {
		t.Fatalf("stopped counter: %+v", got.Counter)
	}
	m.SetTrigSource(1)
	c.Plan(context.Background())
	if got := m.Snapshot().Counter; got.Valid || got.Source != -1 {
		t.Fatalf("stale source after change: %+v", got)
	}
	r.err = errors.New("read failed")
	c.Serve()
	if got := m.Snapshot().Counter; got.Valid || got.FreqHz != 0 {
		t.Fatalf("stale rate after failure: %+v", got)
	}
	_, absent := NewHALLoop(HALConfig{Board: halfake.New(halfake.Config{}), Instrument: instrument.New(loopState(hal.Auto), nil)})
	absent.Serve()
	if absent.inst.Snapshot().Counter != nil {
		t.Fatal("counter advertised without hardware")
	}
}
