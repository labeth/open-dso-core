// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERVISOR-AGENT

package agent

import (
	"strings"
	"testing"
	"time"
)

// TRLC-LINKS: REQ-DSO-027
func TestTakeoverResultSteps(t *testing.T) {
	r := &TakeoverResult{}
	r.Step("gate: inherited gpmc fd=%d", 7)
	r.Step("plain")
	if len(r.Steps) != 2 || r.Steps[0] != "gate: inherited gpmc fd=7" {
		t.Errorf("Steps = %v", r.Steps)
	}
	if got := r.Summary(); got != "gate: inherited gpmc fd=7; plain" {
		t.Errorf("Summary = %q", got)
	}
}

// TRLC-LINKS: REQ-DSO-027
func TestTakeoverIdempotentWhenAlreadyTakenOver(t *testing.T) {
	a := testAgent(t)
	if err := a.st.update(func(s *State) { s.TakenOver = true }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.wd.Disarm)
	res := a.Takeover(TakeoverOpts{})
	if !res.OK {
		t.Fatalf("second takeover must be idempotent: %+v", res)
	}
	if !strings.Contains(res.Summary(), "already taken over") {
		t.Errorf("summary = %q", res.Summary())
	}
	// It re-arms the watchdog in the background (against the fake board's
	// temp-file device).
	deadline := time.Now().Add(5 * time.Second)
	for !a.wd.Status().Armed {
		if time.Now().After(deadline) {
			t.Fatal("the idempotent takeover did not re-arm the watchdog")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TRLC-LINKS: REQ-DSO-104
func TestUntakeoverReleasesControl(t *testing.T) {
	a := testAgent(t)
	if err := a.st.update(func(s *State) { s.TakenOver = true; s.AutoTakeover = true }); err != nil {
		t.Fatal(err)
	}
	_ = a.store.Init()
	// Run the real supervisor so untakeover's internal ctl "stop" is served.
	// No slot has a binary and the fake board has no vendor holder, so the
	// loop just idles.
	go a.superviseLoop()
	defer a.Stop()

	resp := a.Dispatch([]byte(`{"cmd":"untakeover"}`))
	if !resp.OK {
		t.Fatalf("untakeover failed: %s", resp.Err)
	}
	st := a.st.get()
	if st.TakenOver || st.AutoTakeover {
		t.Errorf("state after untakeover = %+v, want both flags cleared", st)
	}
	if !pausedOf(a) {
		t.Error("untakeover must pause the supervisor")
	}
	// The cleared state must be persisted for the next agent generation.
	a2 := New(a.cfg, a.board, "test")
	if got := a2.st.get(); got.TakenOver || got.AutoTakeover {
		t.Errorf("persisted state = %+v, want cleared", got)
	}
}
