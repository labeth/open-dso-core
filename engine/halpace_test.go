package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// sharedCore is the fake on a core it shares with the display.
type sharedCore struct{ *halfake.Board }

func (b sharedCore) Profile() hal.Profile {
	p := b.Board.Profile()
	p.Budget.QuietAcquire = true
	return p
}

// On a board that shares its core with the display the owner paces its
// captures by the display's share and reads no record while a consumer
// holds the quiet lock.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-001
func TestHALSharedCorePaceAndQuiet(t *testing.T) {
	var mu sync.Mutex
	n := 0
	inst := instrument.New(loopState(hal.Auto), nil)
	l, c := NewHALLoop(HALConfig{
		Board: sharedCore{halfake.New(halfake.Config{})}, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond,
		Publish: func(*sample.Frame) { mu.Lock(); n++; mu.Unlock() },
	})
	c.SetDisplayPace(5 * time.Millisecond)
	if _, d := c.Pacing(); d != 5*time.Millisecond {
		t.Fatalf("display share %v", d)
	}
	c.SetDisplayPace(0)
	c.QuietRLock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	held := n
	mu.Unlock()
	c.QuietRUnlock()
	if held != 0 {
		t.Fatalf("%d frames read under the quiet lock", held)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		mu.Lock()
		got := n
		mu.Unlock()
		if got > 0 {
			return
		}
	}
	t.Fatal("no frame after the quiet lock was let go")
}
