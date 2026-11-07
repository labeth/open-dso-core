package simdemo_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal/simboard"
	"github.com/labeth/open-dso-core/scopeapp"
	"github.com/labeth/open-dso-core/simdemo"
)

// fixture is the test scope's profile and demo file.
func fixture(t *testing.T) (profileYAML, demoJSON []byte, d simdemo.Demo) {
	t.Helper()
	var err error
	if profileYAML, err = os.ReadFile("testdata/profile.yaml"); err != nil {
		t.Fatal(err)
	}
	if demoJSON, err = os.ReadFile("testdata/demo.json"); err != nil {
		t.Fatal(err)
	}
	if d, err = simdemo.ParseDemo(demoJSON); err != nil {
		t.Fatal(err)
	}
	return profileYAML, demoJSON, d
}

// demoServer runs the demo as the native host does: the web UI at /, the
// bench page at /demo/. It ends with the test.
type demoServer struct {
	*httptest.Server
	b   *simboard.Board
	env *scopeapp.Env

	mu      sync.Mutex
	stored  map[string]string
	present int
}

// startDemo starts the test scope's demo.
func startDemo(t *testing.T) *demoServer {
	t.Helper()
	profileYAML, demoJSON, d := fixture(t)
	s := &demoServer{stored: map[string]string{}}
	var web http.Handler
	o := simdemo.Options{
		Logf: t.Logf, Version: "test", Serve: func(h http.Handler) { web = h },
		Persist: func(store, key string, val []byte) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.stored[store+"/"+key] = string(val)
		},
		OnPresent: func([]byte, int, int) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.present++
		},
	}
	b, env, err := simdemo.Start(t.Context(), profileYAML, d, o)
	if err != nil {
		t.Fatal(err)
	}
	s.b, s.env = b, env
	mux := http.NewServeMux()
	mux.Handle("/demo/", simdemo.SiteHandler("/demo/", simdemo.Mode{Mode: "native", UI: "/"}, profileYAML, demoJSON))
	mux.Handle("/", web)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// get is path's body; it fails the test on a status other than 200.
func (s *demoServer) get(t *testing.T, path string) []byte {
	t.Helper()
	r, err := http.Get(s.URL + path) //nolint:noctx // a test's short request
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, r.StatusCode, b)
	}
	return b
}

// post sends body to path and decodes the JSON reply.
func (s *demoServer) post(t *testing.T, path, body string) map[string]any {
	t.Helper()
	r, err := http.Post(s.URL+path, "application/json", strings.NewReader(body)) //nolint:noctx // a test's short request
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	var out map[string]any
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return out
}

// status is the web's status.
func (s *demoServer) status(t *testing.T) map[string]any {
	t.Helper()
	var st map[string]any
	if err := json.Unmarshal(s.get(t, "/api/status"), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// num is a number of the web's status.
func (s *demoServer) num(t *testing.T, key string) float64 {
	t.Helper()
	v, ok := s.status(t)[key].(float64)
	if !ok {
		t.Fatalf("status %s is no number", key)
	}
	return v
}

// waitFor polls cond for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("no %s", what)
}

// The demo runs the product on the simulated board: the web serves the
// scope's page, its capabilities name the bench, frames flow and the
// display presents; the bench page and the scope's data are served beside
// it.
// TRLC-LINKS: REQ-DSO-295, REQ-DSO-161
func TestDemoServes(t *testing.T) {
	s := startDemo(t)
	if !bytes.Contains(s.get(t, "/"), []byte("<html")) {
		t.Fatal("no web page at /")
	}
	if !bytes.Contains(s.get(t, "/api/caps"), []byte(simboard.BenchFeature)) {
		t.Fatal("the capabilities do not name the bench")
	}
	waitFor(t, "frames", func() bool { return s.num(t, "seq") > 3 })
	waitFor(t, "a picture", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.present > 0
	})
	for _, p := range []string{"/demo/", "/demo/demo.js", "/demo/sw.js", "/demo/engine.js", "/demo/design/tokens.css", "/demo/scope/demo.json", "/demo/scope/profile.yaml"} {
		s.get(t, p)
	}
	var m simdemo.Mode
	if err := json.Unmarshal(s.get(t, "/demo/mode.json"), &m); err != nil || m.Mode != "native" || m.UI != "/" {
		t.Fatalf("mode %+v %v", m, err)
	}
	if px := s.get(t, "/api/sim/screen.rgba?after=0"); len(px) != 800*480*4 {
		t.Fatalf("screen of %d bytes", len(px))
	}
}

// The bench's panel drives the scope as its keys and knobs do: RUN/STOP
// stops and starts the acquisition and lights its lamp, TIME/DIV steps the
// timebase; a signal set on the generator reaches the measurements' input.
// TRLC-LINKS: REQ-DSO-295, REQ-DSO-1001
func TestBenchDrivesTheScope(t *testing.T) {
	s := startDemo(t)
	waitFor(t, "frames", func() bool { return s.num(t, "seq") > 0 })
	if r := s.post(t, "/api/sim/input", `{"key":"run"}`); r["ok"] != true {
		t.Fatalf("run: %v", r)
	}
	waitFor(t, "a stop", func() bool { return s.status(t)["running"] == false })
	waitFor(t, "the stop lamp", func() bool { return slices.Contains(s.b.Lamps(), "stop") })
	s.post(t, "/api/sim/input", `{"key":"run"}`)
	waitFor(t, "a run", func() bool { return s.status(t)["running"] == true })
	tdiv := s.num(t, "tdiv_set_s")
	s.post(t, "/api/sim/input", `{"knob":"tdiv","steps":1}`)
	waitFor(t, "a new timebase", func() bool { return s.num(t, "tdiv_set_s") != tdiv })
	if r := s.post(t, "/api/sim/signal", `{"ch":"C2","spec":"square,5e3,1,0"}`); r["ok"] != true {
		t.Fatalf("signal: %v", r)
	}
	if got := s.b.Signals().Specs()[1]; got != "square,5e3,1,0" {
		t.Fatalf("C2 carries %q", got)
	}
	if r := s.post(t, "/api/sim/signal", `{"ch":"C3","spec":"dc,0,0,1"}`); r["ok"] != false {
		t.Fatalf("an input the scope has not: %v", r)
	}
	if r := s.post(t, "/api/sim/bus", `{"kind":"uart","lines":{"tx":"C1"},"rate":9600,"vpp":3.3,"offset":1.65,"opts":{"data":"55"}}`); r["ok"] != true {
		t.Fatalf("bus: %v", r)
	}
	if r := s.post(t, "/api/sim/input", `{"touch":{"x":1,"y":2}}`); r["ok"] != false {
		t.Fatalf("a touch on a scope without a touch screen: %v", r)
	}
}

// A change of the setup reaches the host's store, so a browser keeps it
// for the next visit.
// TRLC-LINKS: REQ-DSO-089, REQ-DSO-295
func TestSetupPersists(t *testing.T) {
	s := startDemo(t)
	waitFor(t, "frames", func() bool { return s.num(t, "seq") > 0 })
	s.post(t, "/api/sim/input", `{"knob":"ch1vdiv","steps":1}`)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.stored)
		s.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no setup reached the host's store")
}

// A demo file whose identity matches no profile variant, or whose signals
// name an input the scope has not, makes no board.
// TRLC-LINKS: REQ-DSO-295
func TestBadDemoFile(t *testing.T) {
	profileYAML, _, d := fixture(t)
	bad := d
	bad.Identity.Model = "OTHER"
	if _, err := simdemo.Board(profileYAML, bad, simdemo.Options{}); err == nil {
		t.Fatal("an identity no variant matches")
	}
	bad = d
	bad.Signals = map[string]string{"C3": "dc,0,0,1"}
	if _, err := simdemo.Board(profileYAML, bad, simdemo.Options{}); err == nil {
		t.Fatal("a signal on an input the scope has not")
	}
	if _, err := simdemo.ParseDemo([]byte("{")); err == nil {
		t.Fatal("a demo file that is no JSON")
	}
}
