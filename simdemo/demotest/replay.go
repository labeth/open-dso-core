package demotest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/session"
	"github.com/labeth/open-dso-core/simdemo"
)

// Recipe is a capture made on the demo itself, from its generator: the
// signals on its inputs, the rows that set the record up and the decoder
// its packets are decoded with.
type Recipe struct {
	Name    string
	About   string
	Signals map[string]session.Signal
	Rows    map[string]string
	Decode  *session.CaptureDecode
}

// Captures replays every capture in dir through the demo of the scope's
// profile and demo file and checks that the demo reports of each what the
// scope reported, exactly: the same record through the same code. With
// UPDATE_DEMO_FIXTURES=1 it first writes the recipes' captures, made on
// the demo (a bench capture is made with dsosessions capture).
// TRLC-LINKS: REQ-DSO-298
func Captures(t *testing.T, profileYAML, demoJSON []byte, dir string, recipes []Recipe) {
	t.Helper()
	c := startDemo(t, profileYAML, demoJSON)
	ctx := t.Context()
	if Update() {
		for _, r := range recipes {
			make1(ctx, t, c, dir, r)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no captures in %s (%v)", dir, err)
	}
	for _, path := range files {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".json"), func(t *testing.T) {
			raw, err := os.ReadFile(path) //nolint:gosec // G304: the scope's own capture
			if err != nil {
				t.Fatal(err)
			}
			f, err := session.ParseCapture(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, bad, err := f.Replay(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range bad {
				t.Error(m)
			}
			t.Logf("%s: %d reported fields replayed exactly (%d packets)", f.About, len(got), len(asList(got["packets"].V)))
		})
	}
}

// asList is a packets field's list.
// TRLC-LINKS: REQ-DSO-298
func asList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	}
	return nil
}

// make1 writes recipe r's capture into dir, made on the demo at c.
// TRLC-LINKS: REQ-DSO-298
func make1(ctx context.Context, t *testing.T, c *session.Client, dir string, r Recipe) {
	t.Helper()
	if _, _, err := c.Do(ctx, http.MethodDelete, "/api/sim/replay", nil); err != nil {
		t.Fatal(err)
	}
	if err := (session.SimBench{C: c}).Apply(ctx, r.Signals); err != nil {
		t.Fatalf("%s: %v", r.Name, err)
	}
	for _, id := range session.ReplayRows {
		if text, ok := r.Rows[id]; ok {
			if err := session.SetRow(ctx, c, id, text); err != nil {
				t.Fatalf("%s: %v", r.Name, err)
			}
		}
	}
	if err := c.Post(ctx, "/api/panel", map[string]any{"button": "single"}); err != nil {
		t.Fatal(err)
	}
	end := time.Now().Add(20 * time.Second)
	for {
		var st struct {
			Running bool `json:"running"`
		}
		if err := c.JSON(ctx, http.MethodGet, "/api/status", nil, &st); err == nil && !st.Running {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("%s: the demo took no single record", r.Name)
		}
		time.Sleep(200 * time.Millisecond)
	}
	f, err := session.RecordCapture(ctx, c, r.About)
	if err != nil {
		t.Fatalf("%s: %v", r.Name, err)
	}
	if err := f.WithDecode(r.Decode); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: a repository directory
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, r.Name+".json"), append(b, '\n'), 0o644); err != nil { //nolint:gosec // G306: a repository file
		t.Fatal(err)
	}
}

// startDemo runs the native demo of a scope's files until the test ends
// and returns a client of its web API.
// TRLC-LINKS: REQ-DSO-298
func startDemo(t *testing.T, profileYAML, demoJSON []byte) *session.Client {
	t.Helper()
	d, err := simdemo.ParseDemo(demoJSON)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var h http.Handler
	if _, _, err := simdemo.Start(ctx, profileYAML, d, simdemo.Options{Logf: func(string, ...any) {}, Version: "test", Serve: func(x http.Handler) { h = x }}); err != nil {
		cancel()
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); cancel() })
	return session.NewClient(srv.URL)
}
