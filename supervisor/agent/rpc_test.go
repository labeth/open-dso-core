// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERVISOR-AGENT

package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/supervisor/config"
	"github.com/labeth/open-dso-core/supervisor/fdinherit"
)

// TRLC-LINKS: REQ-DSO-027, REQ-DSO-113
func testAgent(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OTA_DIR", dir+"/ota")
	t.Setenv("OTA_SLOT_ROOT", dir+"/slots")
	t.Setenv("OTA_HEALTH_DIR", dir)
	t.Setenv("OTA_LISTEN", "") // don't bind in tests
	t.Setenv("OTA_NATS", "")
	if err := os.MkdirAll(dir+"/ota", 0o755); err != nil {
		t.Fatal(err) // state.json / agent.intent need the dir to exist
	}
	// Never let a test near the host's real watchdog: the fake board's is a
	// plain temp file (arming the host watchdog would be a real reset risk).
	cfg := config.Load()
	return New(cfg, newFakeBoard(cfg, dir), "test")
}

// TRLC-LINKS: REQ-DSO-113
func TestDispatchUnknownCmd(t *testing.T) {
	a := testAgent(t)
	resp := a.Dispatch([]byte(`{"cmd":"nope"}`))
	if resp.OK {
		t.Error("unknown cmd should not be OK")
	}
	if !strings.Contains(resp.Err, "unknown cmd") {
		t.Errorf("err = %q", resp.Err)
	}
}

// TRLC-LINKS: REQ-DSO-113
func TestDispatchBadJSON(t *testing.T) {
	a := testAgent(t)
	resp := a.Dispatch([]byte(`{not json`))
	if resp.OK || !strings.Contains(resp.Err, "bad request json") {
		t.Errorf("expected bad-json error, got %+v", resp)
	}
}

// TRLC-LINKS: REQ-DSO-113
func TestDispatchPing(t *testing.T) {
	a := testAgent(t)
	resp := a.Dispatch([]byte(`{"cmd":"ping"}`))
	if !resp.OK {
		t.Fatalf("ping failed: %s", resp.Err)
	}
	b, _ := json.Marshal(resp.Data)
	if !strings.Contains(string(b), "device") {
		t.Errorf("ping data missing device: %s", b)
	}
}

// TRLC-LINKS: REQ-DSO-113
func TestDispatchPanicContained(t *testing.T) {
	// Register a handler that panics; Dispatch must contain it (the agent must
	// never die to a bad request — it holds the inherited fds).
	handlers["_boom"] = func(_ *Agent, _ json.RawMessage) (any, error) { panic("kaboom") }
	defer delete(handlers, "_boom")
	a := testAgent(t)
	resp := a.Dispatch([]byte(`{"cmd":"_boom"}`))
	if resp.OK || !strings.Contains(resp.Err, "panic") {
		t.Errorf("panic not contained: %+v", resp)
	}
}

// TRLC-LINKS: REQ-DSO-113, REQ-DSO-117
func TestAppStartRequiresTakeover(t *testing.T) {
	a := testAgent(t)
	resp := a.Dispatch([]byte(`{"cmd":"app.start"}`))
	if resp.OK || !strings.Contains(resp.Err, "not taken over") {
		t.Errorf("app.start before takeover should be refused: %+v", resp)
	}
}

// TRLC-LINKS: REQ-DSO-027
func TestTakeoverRefusedByTheBoard(t *testing.T) {
	a := testAgent(t)
	fakeOf(a).land = func(res *TakeoverResult, _ TakeoverOpts) bool {
		res.Err = "no inherited bus fd"
		return false
	}
	resp := a.Dispatch([]byte(`{"cmd":"takeover"}`))
	if resp.OK || resp.Err != "no inherited bus fd" {
		t.Errorf("a refused landing must refuse the takeover: %+v", resp)
	}
	if a.TakenOver() {
		t.Error("a refused takeover persisted taken_over")
	}
}

// TRLC-LINKS: REQ-DSO-027
func TestTakeoverDryRunReportsCandidates(t *testing.T) {
	a := testAgent(t)
	fakeOf(a).holders = []fdinherit.Holder{{PID: 4242, Comm: "vendor"}}
	resp := a.Dispatch([]byte(`{"cmd":"takeover","args":{"dry_run":true}}`))
	if !resp.OK {
		t.Fatalf("dry-run should succeed: %s", resp.Err)
	}
	b, _ := json.Marshal(resp.Data)
	if !strings.Contains(string(b), "dry-run") || !strings.Contains(string(b), `"pid":4242`) {
		t.Errorf("dry-run result missing marker or candidate: %s", b)
	}
	if a.TakenOver() || fakeOf(a).killed != 0 {
		t.Error("a dry run persisted or killed")
	}
}

// TRLC-LINKS: REQ-DSO-027
func TestTakeoverSequence(t *testing.T) {
	a := testAgent(t)
	fb := fakeOf(a)
	fb.holders = []fdinherit.Holder{{PID: 4242, Comm: "vendor"}}
	t.Cleanup(a.wd.Disarm)
	res := a.Takeover(TakeoverOpts{})
	if !res.OK {
		t.Fatalf("takeover failed: %+v", res)
	}
	want := []string{"state persisted: taken_over=true", "killed 1 factory processes", "watchdog acquired", "takeover complete"}
	prev := -1
	for _, w := range want {
		i := -1
		for j, s := range res.Steps {
			if strings.Contains(s, w) {
				i = j
				break
			}
		}
		if i <= prev {
			t.Fatalf("step %q missing or out of order: %s", w, res.Summary())
		}
		prev = i
	}
	if !a.TakenOver() || !a.Watchdog().Armed {
		t.Errorf("after takeover: taken_over=%v watchdog=%+v", a.TakenOver(), a.Watchdog())
	}
}

// A board without a watchdog takes over without one.
// TRLC-LINKS: REQ-DSO-027, REQ-DSO-086
func TestTakeoverWithoutWatchdog(t *testing.T) {
	a := testAgent(t)
	fakeOf(a).wd = nil
	a.wd = newKeeper(a.board)
	res := a.Takeover(TakeoverOpts{})
	if !res.OK || strings.Contains(res.Summary(), "watchdog") {
		t.Errorf("takeover without a watchdog: %+v", res)
	}
}

// A takeover whose taken_over cannot be persisted stops before the kill.
// TRLC-LINKS: REQ-DSO-027, REQ-DSO-104
func TestTakeoverPersistFailureStopsBeforeTheKill(t *testing.T) {
	a := testAgent(t)
	fb := fakeOf(a)
	fb.holders = []fdinherit.Holder{{PID: 4242}}
	a.st.path = t.TempDir() + "/missing/state.json"
	res := a.Takeover(TakeoverOpts{})
	if res.OK || !strings.Contains(res.Err, "persist state") {
		t.Fatalf("takeover with an unwritable state: %+v", res)
	}
	if a.TakenOver() || fb.killed != 0 || a.Watchdog().Armed {
		t.Error("a failed persist went on past the point of no return")
	}
}

// fakeOf is the test agent's fake board.
// TRLC-LINKS: REQ-DSO-027
func fakeOf(a *Agent) *fakeBoard {
	fb, _ := a.board.(*fakeBoard)
	return fb
}
