package engine

import (
	"testing"
	"time"
)

// A held capture goes through every fallback holds, or once AutoLiveMax
// passed since the last publish; nothing published yet lets one through.
// TRLC-LINKS: REQ-DSO-013
func TestLiveness(t *testing.T) {
	now := time.Unix(1000, 0)
	clock := func() time.Time { return now }
	last := now
	published := func() time.Time { return last }
	var l Liveness
	for i := 1; i < 3; i++ {
		if l.Due(3, published, clock) {
			t.Fatalf("hold %d went through", i)
		}
	}
	if !l.Due(3, published, clock) {
		t.Fatal("the third hold did not go through")
	}
	if l.Due(3, published, clock) {
		t.Fatal("the count did not start again")
	}
	now = now.Add(AutoLiveMax)
	if !l.Due(3, published, clock) {
		t.Fatal("no frame after AutoLiveMax")
	}
	l.Reset()
	if !l.Due(3, func() time.Time { return time.Time{} }, clock) {
		t.Fatal("nothing published yet: a frame is due")
	}
}
