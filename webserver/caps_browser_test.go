// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webserver_test

import (
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/testenv"
	"github.com/labeth/open-dso-core/webclient"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/webtest"
)

// pageStatus is a two-channel status of the fields the page reads.
// TRLC-LINKS: REQ-SDS-164
func pageStatus() map[string]any {
	return map[string]any{"running": true, "seq": 1, "tdiv_s": 1e-6, "fps": 30.0, "band": "native", "version": "test",
		"tdivs": []float64{1e-7, 2e-7, 5e-7, 1e-6, 2e-6}, "vdivs": []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1},
		"vdiv1": 0.1, "vdiv2": 0.2, "probe1": 1, "probe2": 1, "cpl1": 0, "cpl2": 0, "off1_v": 0.0, "off2_v": 0.0,
		"trig_source": 0, "trig_code": 31437, "trig_volts": 0.0, "trig_rising": true, "zoom1": 1, "zoom2": 1,
		"trig_pos_frac": 0.5, "mem_depth": 20480, "cal_source": "test", "mmap_drain": true}
}

// plainScope is the Fake with the page's status.
type plainScope struct{ *webtest.Fake }

// TRLC-LINKS: REQ-SDS-164
func (plainScope) Status() any { return pageStatus() }

// twinScope is the Twin with the page's status.
type twinScope struct{ *webtest.Twin }

// TRLC-LINKS: REQ-SDS-164
func (twinScope) Status() any { return pageStatus() }

// pageRun is what caps_browser.mjs reports of the page.
type pageRun struct {
	CapsServed    bool                       `json:"capsServed"`
	Chans         []int                      `json:"chans"`
	Colors        []string                   `json:"colors"`
	Wire          int                        `json:"wire"`
	Frame         map[string]json.RawMessage `json:"frame"`
	Vpc           []float64                  `json:"vpc"`
	Shown         []bool                     `json:"shown"`
	Toggles       []string                   `json:"toggles"`
	Source        []string                   `json:"source"`
	DecScl        []string                   `json:"decScl"`
	MeasCh        []string                   `json:"measCh"`
	ZmCh          []string                   `json:"zmCh"`
	EjCh          []string                   `json:"ejCh"`
	TtypeDisabled []bool                     `json:"ttypeDisabled"`
	Quick         string                     `json:"quick"`
	Meas          string                     `json:"meas"`
	CursorRows    []string                   `json:"cursorRows"`
	VgCells       int                        `json:"vgCells"`
	Brand         string                     `json:"brand"`
	Errors        []string                   `json:"errors"`
	Requests      []string                   `json:"requests"`
}

// drivePage runs caps_browser.mjs against a server over sc, publishing a
// frame every 50 ms.
// TRLC-LINKS: REQ-SDS-163, REQ-SDS-164
func drivePage(t *testing.T, sc webserver.Scope, publish func()) pageRun {
	t.Helper()
	srv := httptest.NewServer(webserver.New(sc).Handler())
	defer srv.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				publish()
			}
		}
	}()
	cmd := exec.CommandContext(t.Context(), "node", "caps_browser.mjs", srv.URL)
	cmd.Dir = webclient.Dir()
	out, err := cmd.CombinedOutput()
	if strings.HasPrefix(string(out), "SKIP") {
		testenv.SkipBrowser(t, "%s", out)
	}
	if err != nil {
		t.Fatalf("caps_browser.mjs: %v\n%s", err, out)
	}
	var r pageRun
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(r.Errors) > 0 {
		t.Fatalf("page errors: %v", r.Errors)
	}
	return r
}

// The page over a scope without caps, over the same two-channel 8-bit
// scope serving caps and v2 frames, and over a four-channel 12-bit scope:
// the first two build the same page and draw the same frame (the second
// only reads caps and asks for v2 besides), and the third lists, draws,
// measures and sets four channels.
// TRLC-LINKS: REQ-SDS-163, REQ-SDS-164
func TestCapsBrowser(t *testing.T) {
	testenv.NeedNode(t)
	fake := webtest.NewFake(false)
	plain := drivePage(t, plainScope{fake}, fake.Publish)
	twin := webtest.NewTwin()
	served := drivePage(t, twinScope{twin}, twin.Publish)

	if plain.CapsServed || plain.Wire != 1 || !served.CapsServed || served.Wire != 2 {
		t.Fatalf("caps served %v/%v, wire %d/%d", plain.CapsServed, served.CapsServed, plain.Wire, served.Wire)
	}
	for _, r := range plain.Requests {
		if strings.Contains(r, "/api/caps") || strings.Contains(r, "v=2") {
			t.Fatalf("a scope without caps was asked %q", r)
		}
	}
	// the same page: everything but the wire and caps reads
	a, b := plain, served
	a.CapsServed, a.Wire, a.Requests = b.CapsServed, b.Wire, nil
	b.Requests = nil
	b.Brand = a.Brand
	if !reflect.DeepEqual(a, b) {
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		t.Fatalf("the page differs with two-channel 8-bit caps:\n without %s\n    with %s", ja, jb)
	}
	var extra []string
	for _, r := range served.Requests {
		base := strings.ReplaceAll(strings.ReplaceAll(r, "&v=2", ""), "?v=2", "")
		if !slices.Contains(plain.Requests, base) {
			extra = append(extra, r)
		}
	}
	if !reflect.DeepEqual(extra, []string{"GET /api/caps"}) {
		t.Fatalf("with caps the page asked besides: %v", extra)
	}

	wide := webtest.NewWide()
	w := drivePage(t, wide, wide.Publish)
	if !reflect.DeepEqual(w.Chans, []int{1, 2, 3, 4}) || w.Wire != 2 {
		t.Fatalf("channels %v wire %d", w.Chans, w.Wire)
	}
	if w.Colors[2] != "#4fa8ff" || w.Colors[3] != "#5fd35f" || w.Colors[0] != plain.Colors[0] {
		t.Fatalf("colours %v", w.Colors)
	}
	for k := 1; k <= 4; k++ {
		var a struct {
			Type string    `json:"type"`
			N    int       `json:"n"`
			Head []float64 `json:"head"`
		}
		if err := json.Unmarshal(w.Frame["c"+string(rune('0'+k))], &a); err != nil || a.Type != "Float32Array" || a.N != 2000 {
			t.Fatalf("C%d: %+v %v", k, a, err)
		}
	}
	want := []string{"C1=0", "C2=1", "C3=2", "C4=3"}
	if !reflect.DeepEqual(w.Source, want) || !reflect.DeepEqual(w.ZmCh, want) {
		t.Fatalf("source %v, mask channel %v", w.Source, w.ZmCh)
	}
	if want := []string{"C1=1", "C2=2", "C3=3", "C4=4"}; !reflect.DeepEqual(w.DecScl, want) || !reflect.DeepEqual(w.EjCh, want) {
		t.Fatalf("decode %v, eye %v", w.DecScl, w.EjCh)
	}
	if w.MeasCh[0] != "Both=0" || len(w.MeasCh) != 5 {
		t.Fatalf("measure channels %v", w.MeasCh)
	}
	if !reflect.DeepEqual(w.TtypeDisabled, []bool{false, true, true, true}) {
		t.Fatalf("trigger types disabled %v", w.TtypeDisabled)
	}
	if !reflect.DeepEqual(w.Toggles, []string{"tC1", "tC2", "tC3", "tC4"}) || !reflect.DeepEqual(w.Shown, []bool{true, true, true, false}) {
		t.Fatalf("toggles %v shown %v", w.Toggles, w.Shown)
	}
	if w.VgCells != 9*5 || !strings.Contains(w.Meas, `data-meas="4:Vpp"`) || !strings.Contains(w.Quick, `class="hd cc4"`) {
		t.Fatalf("vertical cells %d, tables %s %s", w.VgCells, w.Quick, w.Meas)
	}
	if !slices.Contains(w.CursorRows, "C4 A / B") || !slices.Contains(w.CursorRows, "ΔV C3") {
		t.Fatalf("cursor rows %v", w.CursorRows)
	}
	if !slices.ContainsFunc(w.Requests, func(r string) bool { return strings.HasPrefix(r, `POST /api/set {"control":"vdiv","ch":3,`) }) {
		t.Fatalf("C4's V/div was not set by channel: %v", w.Requests)
	}
	if w.Brand != "fake four-channel 12-bit" {
		t.Fatalf("brand %q", w.Brand)
	}
}
