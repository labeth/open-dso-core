package session

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/simdemo"
)

// Every session of the set parses strictly and validates: one action a
// step, named checkpoints, expectations on earlier ones, and the relays
// flag the steps imply.
// TRLC-LINKS: REQ-DSO-295
func TestSessionSetValid(t *testing.T) {
	ss, base, err := Load(filepath.Join(Dir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) < 15 || len(base.Rows) == 0 {
		t.Fatalf("%d sessions, %d baseline rows", len(ss), len(base.Rows))
	}
	for _, s := range ss {
		for _, st := range s.Steps {
			if st.Check != nil && len(st.Check.Fields) == 0 && len(st.Check.Expect) == 0 {
				t.Errorf("%s/%s: compares the default fields only; name what the checkpoint is about", s.Name, st.Check.Name)
			}
		}
	}
}

// A session that clicks relays must say so, and a board key must be
// declared as a need.
// TRLC-LINKS: REQ-DSO-295
func TestValidateRelaysAndNeeds(t *testing.T) {
	for name, tc := range map[string]struct {
		s   string
		bad string
	}{
		"undeclared relay": {`{"name":"x","relays":false,"steps":[{"key":"auto"},{"check":{"name":"a"}}]}`, "relays is false"},
		"board key":        {`{"name":"x","relays":false,"steps":[{"key":"ch1","via":"board"},{"check":{"name":"a"}}]}`, "needs"},
		"two actions":      {`{"name":"x","relays":false,"steps":[{"key":"run","wait_s":1},{"check":{"name":"a"}}]}`, "exactly one"},
		"later reference":  {`{"name":"x","relays":false,"steps":[{"check":{"name":"a","expect":{"workspace":{"same_as":"b"}}}},{"check":{"name":"b"}}]}`, "not an earlier"},
		"unknown field":    {`{"name":"x","relays":false,"steps":[{"keys":"run"}]}`, "unknown field"},
		"relay row":        {`{"name":"x","relays":false,"steps":[{"row":{"id":"ch1.vdiv","text":"2V"}},{"check":{"name":"a"}}]}`, "relays is false"},
	} {
		if _, err := Parse([]byte(tc.s)); err == nil || !strings.Contains(err.Error(), tc.bad) {
			t.Errorf("%s: %v; want an error about %q", name, err, tc.bad)
		}
	}
}

// Tolerances compare as they say.
// TRLC-LINKS: REQ-DSO-295
func TestTolerances(t *testing.T) {
	n := num
	s := func(x string) Value { return Value{V: x} }
	l := func(x ...string) Value { return Value{V: x} }
	for _, tc := range []struct {
		tol  string
		a, b Value
		want bool
	}{
		{"exact", s("1ms"), s("1ms"), true},
		{"exact", s("1ms"), s("2ms"), false},
		{"abs:0.1", n(1.6), n(1.65), true},
		{"abs:0.1", n(1.4), n(1.65), false},
		{"rel:0.02", s("10.00MHz"), s("10.1MHz"), true},
		{"rel:0.02", s("10.00MHz"), s("11MHz"), false},
		{"ratio:2.5", s("1ms"), s("2ms"), true},
		{"ratio:2.5", s("1ms"), s("5ms"), false},
		{"set", l("b", "a", "a"), l("a", "b"), true},
		{"set", l("a"), l("a", "b"), false},
		{"overlap", l("x", "y"), l("y", "z"), true},
		{"overlap", l("x"), l("z"), false},
		{"present", s("x"), s("y"), true},
		{"present", s(""), s("y"), false},
		{"info", s("x"), s("y"), true},
	} {
		tol, err := ParseTolerance(tc.tol)
		if err != nil {
			t.Fatal(err)
		}
		if got := tol.Match(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: %v vs %v = %v, want %v", tc.tol, tc.a.V, tc.b.V, got, tc.want)
		}
	}
	for _, bad := range []string{"near:1", "abs:x", "ratio:0.5", "set:1"} {
		if _, err := ParseTolerance(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// Two runs compare field by field: a field one target cannot produce, or
// a checkpoint after a step it could not perform, is n/a, never a match.
// TRLC-LINKS: REQ-DSO-295
func TestCompare(t *testing.T) {
	ses := &Session{Name: "s", Steps: []*Step{{Check: &Checkpoint{
		Name: "c", Fields: []string{"workspace", "row.time.tdiv", "packets"}, Tol: map[string]string{"packets": "overlap"},
	}}}}
	a := &Run{Target: "a", Sessions: []*Played{{Session: ses, Checks: []*CheckRun{{Name: "c", Snapshot: Snapshot{
		"workspace": {V: "bus"}, "row.time.tdiv": {V: "1ms"}, "packets": {V: []string{"x", "y"}},
	}}}}}}
	b := &Run{Target: "b", Sessions: []*Played{{Session: ses, Checks: []*CheckRun{{Name: "c", Snapshot: Snapshot{
		"workspace": {V: "scope"}, "row.": na("no sheet model"), "packets": {V: []string{"y"}},
	}}}}}}
	got := map[string]string{}
	for _, f := range Compare(a, b).Fields {
		got[f.Field] = f.Verdict
	}
	want := map[string]string{"workspace": VerdictDiff, "row.time.tdiv": VerdictNA, "packets": VerdictMatch}
	for f, v := range want {
		if got[f] != v {
			t.Errorf("%s: %s, want %s (all %v)", f, got[f], v, got)
		}
	}
	b.Sessions[0].Checks[0].Tainted = "step key ch1 cannot run"
	for _, f := range Compare(a, b).Fields {
		if f.Verdict != VerdictNA {
			t.Errorf("%s after a step b could not run: %s", f.Field, f.Verdict)
		}
	}
}

// Expectations hold or fail on one run, against earlier checkpoints too.
// TRLC-LINKS: REQ-DSO-295
func TestExpectations(t *testing.T) {
	before := Snapshot{"row.time.tdiv": {V: "1ms"}, "shown.ch1": {V: true}}
	now := Snapshot{"row.time.tdiv": {V: "2ms"}, "shown.ch1": {V: true}, "packets.text": {V: "xxhello"}, "workspace": {V: "bus"}}
	c := &Checkpoint{Name: "now", Expect: map[string]Expect{
		"row.time.tdiv": {SameAs: "before"},
		"shown.ch1":     {SameAs: "before"},
		"packets.text":  {Contains: "hello"},
		"workspace":     {Eq: "bus"},
		"view":          {Eq: "Packets"},
	}}
	got := map[string]Outcome{}
	for _, o := range evaluate(c, now, map[string]Snapshot{"before": before}, "") {
		got[o.Field] = o
	}
	if got["row.time.tdiv"].OK || !got["shown.ch1"].OK || !got["packets.text"].OK || !got["workspace"].OK {
		t.Errorf("outcomes %+v", got)
	}
	if v := got["view"]; v.OK || !v.NA {
		t.Errorf("an uncaptured field: %+v; want n/a", v)
	}
}

// Signals become the generator's texts and bus requests; a bench source
// is found by the signals it provides.
// TRLC-LINKS: REQ-DSO-295
func TestSignals(t *testing.T) {
	sigs := map[string]Signal{
		"C1": {Kind: "uart", Rate: 9600, Vpp: 3.3, Offset: 1.65, Text: "hello"},
		"C2": {Kind: "dc", Offset: 0},
		"C3": {Kind: "spi", Line: "clk", Rate: 1e6, Vpp: 3.3, Offset: 1.65, Hex: "a5"},
		"C4": {Kind: "spi", Line: "mosi", Rate: 1e6, Vpp: 3.3, Offset: 1.65, Hex: "a5"},
	}
	signals, buses, err := SimRequests(sigs)
	if err != nil {
		t.Fatal(err)
	}
	if signals["C1"] != "uart,9600,3.3,1.65,data=68656c6c6f" || signals["C2"] != "dc,0,0,0" || len(buses) != 1 {
		t.Fatalf("signals %v, buses %v", signals, buses)
	}
	if l, _ := buses[0]["lines"].(map[string]string); l["clk"] != "C3" || l["mosi"] != "C4" {
		t.Fatalf("bus lines %v", buses[0])
	}
	b := &Bench{Sources: []BenchSource{{ID: "gen", Provides: map[string]Signal{"C1": sigs["C1"], "C2": sigs["C2"]}}}}
	if src, ok := b.Source(map[string]Signal{"C1": sigs["C1"]}); !ok || src.ID != "gen" {
		t.Error("a source providing the signal is not found")
	}
	other := sigs["C1"]
	other.Rate = 115200
	if _, ok := b.Source(map[string]Signal{"C1": other}); ok {
		t.Error("a source found for a signal it does not provide")
	}
}

// A dry run prints every request it would make and sends none.
// TRLC-LINKS: REQ-DSO-295
func TestDryRun(t *testing.T) {
	var out bytes.Buffer
	c := NewClient("http://192.0.2.1:8080")
	c.Plan = &out
	b := &Bench{Name: "b", Sources: []BenchSource{{ID: "gen", Provides: map[string]Signal{"C1": {Kind: "square", Hz: 1e3, Vpp: 3.3, Offset: 1.65}}, Select: []string{"echo select"}}}}
	tg := &Target{Name: "bench", Client: c, Signals: &BenchDriver{Bench: b, Mode: SourcesRun, Plan: &out}, Restore: true, MinimalRelay: true, Log: &out}
	s, err := Parse([]byte(`{"name":"s","relays":false,"signals":{"C1":{"kind":"square","hz":1000,"vpp":3.3,"offset":1.65}},
		"steps":[{"key":"run"},{"check":{"name":"c","fields":["run.running"]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := tg.RunSession(t.Context(), s, Baseline{Rows: map[string]string{"ch1.vdiv": "1V", "time.tdiv": "1ms"}})
	if r.Skipped != "" {
		t.Fatalf("skipped: %s", r.Skipped)
	}
	for _, want := range []string{"$ echo select", `POST http://192.0.2.1:8080/api/panel {"button":"run"}`, "ch1.vdiv already reads", "GET http://192.0.2.1:8080/api/status", "/api/sheets/set"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the plan lacks %q:\n%s", want, out.String())
		}
	}
}

// The native demo of the core's test scope runs a session end to end:
// signals on its generator, a key, a checkpoint with its expectations.
// TRLC-LINKS: REQ-DSO-295
func TestNativeDemoSession(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the demo")
	}
	td := filepath.Join(Dir(), "..", "simdemo", "testdata")
	profileYAML, err := os.ReadFile(filepath.Join(td, "profile.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	demoJSON, err := os.ReadFile(filepath.Join(td, "demo.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := simdemo.ParseDemo(demoJSON)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var web http.Handler
	if _, _, err := simdemo.Start(ctx, profileYAML, d, simdemo.Options{Version: "test", Serve: func(h http.Handler) { web = h }}); err != nil {
		t.Fatal(err)
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: web, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()
	c := NewClient("http://" + ln.Addr().String())
	sb := SimBench{C: c}
	tg := &Target{Name: "native", Client: c, Signals: sb, Input: sb, Restore: true}
	s, err := Parse([]byte(`{"name":"run-stop","relays":false,
		"signals":{"C1":{"kind":"square","hz":1000,"vpp":2}},
		"steps":[
		 {"check":{"name":"running","fields":["run.running","workspace"],"expect":{"run.running":{"eq":true}}}},
		 {"key":"run"},
		 {"check":{"name":"stopped","fields":["run.running"],"expect":{"run.running":{"eq":false}}}},
		 {"key":"run","via":"board"},
		 {"check":{"name":"again","fields":["run.running","row.time.tdiv"],"expect":{"run.running":{"eq":true},"row.time.tdiv":{"same_as":"running"}}}}],
		"needs":["board-input"]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := tg.RunSession(ctx, s, Baseline{Rows: map[string]string{"time.tdiv": "1ms"}})
	if r.Skipped != "" {
		t.Fatalf("skipped: %s", r.Skipped)
	}
	for _, st := range r.Steps {
		if !st.OK {
			t.Errorf("step %s: %s", st.Step, st.Err)
		}
	}
	for _, o := range r.Outcomes {
		if !o.OK {
			t.Errorf("%s %s: expected %s, got %s", o.Checkpoint, o.Field, o.Expect, o.Got)
		}
	}
	if len(r.Checks) != 3 || r.Checks[0].Snapshot["workspace"].V != "scope" {
		t.Errorf("checks %+v", r.Checks)
	}
	if !strings.HasPrefix(r.Restored, "every row") {
		t.Errorf("restore: %s", r.Restored)
	}
}
