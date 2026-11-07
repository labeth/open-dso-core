package simdemo_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/simdemo"
	"github.com/labeth/open-dso-core/testenv"
)

// The demo's test matrix (testdata/demo_matrix.mjs) on the WebAssembly
// build in headless Chromium: the owner's UART flow, AUTO in BUS on every
// bus the generator makes with its payload decoded, BUS's views, the
// timebase across the workspaces, SCOPE and MEASURE, SPECTRUM and SOUND,
// COMPARE, SYS, the channel keys and a bounded fuzz of the front panel.
// DEMO_PROFILE and DEMO_FILE name a scope's profile and demo file (make
// demo-test runs each scope's; without them the test skips); DEMO_CASES picks
// cases by a regular expression; DEMO_SHOTS keeps screenshots of failures.
// TRLC-LINKS: REQ-DSO-180, REQ-DSO-295
func TestDemoMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("a WebAssembly build and a browser run")
	}
	p, d := os.Getenv("DEMO_PROFILE"), os.Getenv("DEMO_FILE")
	if p == "" || d == "" {
		t.Skip("the matrix runs on a scope's own profile and demo file (make demo-test)")
	}
	testenv.NeedNode(t)
	profileYAML, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	demoJSON, err := os.ReadFile(d)
	if err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(t.TempDir(), "site")
	if err := simdemo.BuildSite(t.Context(), profileYAML, demoJSON, site); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(site)))
	t.Cleanup(srv.Close)
	cmd := exec.CommandContext(t.Context(), "node", filepath.Join("testdata", "demo_matrix.mjs"), srv.URL+"/")
	cmd.Env = append(os.Environ(), "CASES="+os.Getenv("DEMO_CASES"), "SHOTS="+os.Getenv("DEMO_SHOTS"))
	out, err := cmd.Output()
	type result struct {
		Skip    string          `json:"skip"`
		Case    string          `json:"case"`
		OK      bool            `json:"ok"`
		Err     string          `json:"err"`
		Ms      float64         `json:"ms"`
		Detail  json.RawMessage `json:"detail"`
		Summary json.RawMessage `json:"summary"`
	}
	failed := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r result
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		switch {
		case r.Skip != "":
			testenv.SkipBrowser(t, "%s", r.Skip)
		case r.Summary != nil:
			t.Logf("summary %s", r.Summary)
		case r.OK:
			t.Logf("ok   %-24s %6.0f ms %s", r.Case, r.Ms, r.Detail)
		default:
			failed++
			t.Errorf("FAIL %-24s %s", r.Case, r.Err)
		}
	}
	if err != nil && failed == 0 {
		t.Fatalf("demo_matrix.mjs: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}
