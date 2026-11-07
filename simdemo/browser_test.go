package simdemo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/simdemo"
	"github.com/labeth/open-dso-core/testenv"
)

// The operator fuzzer (webclient/e2e/fuzz.mjs) drives the web UI of the
// native demo: every invariant holds after every action. DEMO_FUZZ_ITERS
// and DEMO_FUZZ_SEED run it longer or on another seed.
// TRLC-LINKS: REQ-DSO-180, REQ-DSO-295
func TestDemoBrowserFuzz(t *testing.T) {
	if testing.Short() {
		t.Skip("a browser run")
	}
	testenv.NeedNode(t)
	s := startDemo(t)
	iters, seed := os.Getenv("DEMO_FUZZ_ITERS"), os.Getenv("DEMO_FUZZ_SEED")
	if iters == "" {
		iters = "30"
	}
	if seed == "" {
		seed = "1"
	}
	out := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "node", "e2e/fuzz.mjs")
	cmd.Dir = testenv.WebDir(t)
	cmd.Env = append(os.Environ(), "SCOPE_URL="+s.URL+"/", "ITERS="+iters, "SEED="+seed, "OUT="+out)
	b, err := cmd.CombinedOutput()
	text := string(b)
	if strings.Contains(text, "SKIP:") {
		testenv.SkipBrowser(t, "%s", strings.TrimSpace(text))
	}
	if err != nil {
		t.Fatalf("fuzz.mjs: %v\n%s", err, text)
	}
	m := regexp.MustCompile(`done: (\d+) iterations, (\d+) findings`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("fuzz.mjs ended without its summary:\n%s", text)
	}
	if n, _ := strconv.Atoi(m[2]); n > 0 {
		findings, _ := os.ReadFile(filepath.Join(out, "findings.jsonl"))
		t.Fatalf("%s iterations, %s findings:\n%s", m[1], m[2], findings)
	}
	t.Logf("%s iterations, no findings", m[1])
}

// wasmRun is what testdata/wasm_smoke.mjs reports of the browser demo.
type wasmRun struct {
	Skip           string   `json:"skip"`
	Errors         []string `json:"errors"`
	FirstPictureMs float64  `json:"firstPictureMs"`
	ModuleMs       float64  `json:"moduleMs"`
	WebUpMs        float64  `json:"webUpMs"`
	ScreenFps      float64  `json:"screenFps"`
	WebFps         float64  `json:"webFps"`
	Stopped        bool     `json:"stopped"`
	RunAgain       bool     `json:"runAgain"`
}

// The browser demo as WebAssembly in headless Chromium: the site builds,
// the firmware's first picture shows, the web UI receives frames through
// the service worker, and RUN/STOP on the bench's panel stops and runs
// the acquisition the web UI shows. It reports the module's size, the
// load times and the frame rates.
// TRLC-LINKS: REQ-DSO-180, REQ-DSO-295
func TestDemoBrowserWasm(t *testing.T) {
	if testing.Short() {
		t.Skip("a WebAssembly build and a browser run")
	}
	testenv.NeedNode(t)
	profileYAML, demoJSON, _ := fixture(t)
	site := filepath.Join(t.TempDir(), "site")
	if err := simdemo.BuildSite(t.Context(), profileYAML, demoJSON, site); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(site, simdemo.WasmFile)); err == nil {
		t.Logf("module %.1f MB", float64(st.Size())/1e6)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(site)))
	t.Cleanup(srv.Close)
	cmd := exec.CommandContext(t.Context(), "node", filepath.Join("testdata", "wasm_smoke.mjs"), srv.URL+"/")
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("wasm_smoke.mjs: %v\n%s", err, b)
	}
	var r wasmRun
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	if r.Skip != "" {
		testenv.SkipBrowser(t, "%s", r.Skip)
	}
	t.Logf("module %.0f ms, first picture %.0f ms, web UI up %.0f ms, screen %.1f fps, web %.1f fps", r.ModuleMs, r.FirstPictureMs, r.WebUpMs, r.ScreenFps, r.WebFps)
	if len(r.Errors) > 0 || r.FirstPictureMs <= 0 || r.WebFps <= 0 || r.ScreenFps <= 0 || !r.Stopped || !r.RunAgain {
		t.Fatalf("the browser demo: %+v", r)
	}
}
