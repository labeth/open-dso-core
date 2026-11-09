package engine

import (
	"context"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// rollBoard is the fake with a stream of codes that rolls from 100 ms/div.
type rollBoard struct{ streamBoard }

func (b rollBoard) Profile() hal.Profile {
	p := b.Board.Profile()
	p.Timebase.RollFromS = 100e-3
	return p
}

// AUTO at a roll timebase scrolls the board's stream: frames flagged Roll,
// untriggered, a screen of the stream's newest codes; NORMAL captures again
// and the stream stops.
// TRLC-LINKS: REQ-DSO-010
func TestHALRollOverTheStream(t *testing.T) {
	st := &fakeStream{}
	b := rollBoard{streamBoard{halfake.New(halfake.Config{}), st}}
	s := loopState(hal.Auto)
	s.TimeDivS = 200e-3
	inst := instrument.New(s, nil)
	l, c := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	var f *sample.Frame
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if g, _ := c.Consume(); g != nil && g.Flags.Has(sample.Roll) && !g.Traces[0].Held[g.Len()-1] {
			f = g
			break
		}
		time.Sleep(time.Millisecond)
	}
	if f == nil {
		t.Fatal("no roll frame")
	}
	if f.TrigKind != sample.Untriggered || f.TriggerAt >= 0 || len(f.Traces) != 2 || st.starts.Load() == 0 {
		t.Fatalf("roll frame %+v, %d stream starts", f, st.starts.Load())
	}
	if inst.Acquisition() != instrument.Rolling {
		t.Errorf("run state %v, want Rolling", inst.Acquisition())
	}
	n := f.Len()
	if a, b := f.Traces[0].Codes8[n-2], f.Traces[0].Codes8[n-1]; b != a+1 {
		t.Errorf("the newest codes %d, %d: want the stream's ramp", a, b)
	}

	inst.SetNorm(true)
	deadline = time.Now().Add(5 * time.Second)
	for st.stops.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if st.stops.Load() == 0 {
		t.Fatal("NORMAL did not stop the roll stream")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if g, _ := c.Consume(); g != nil && !g.Flags.Has(sample.Roll) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no captured frame after NORMAL")
}
