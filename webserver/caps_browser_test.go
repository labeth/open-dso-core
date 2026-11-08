// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

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
// TRLC-LINKS: REQ-DSO-164
func pageStatus() map[string]any {
	return map[string]any{
		"running": true, "seq": 1, "tdiv_s": 1e-6, "fps": 30.0, "band": "native", "version": "test",
		"tdivs": []float64{1e-7, 2e-7, 5e-7, 1e-6, 2e-6}, "vdivs": []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1},
		"vdiv1": 0.1, "vdiv2": 0.2, "probe1": 1, "probe2": 1, "cpl1": 0, "cpl2": 0, "off1_v": 0.0, "off2_v": 0.0,
		"trig_source": 0, "trig_code": 31437, "trig_volts": 0.0, "trig_rising": true, "zoom1": 1, "zoom2": 1,
		"trig_pos_frac": 0.5, "mem_depth": 20480, "cal_source": "test", "mmap_drain": true, "win_cols": 1000,
	}
}

// plainScope is the Fake with the page's status.
type plainScope struct{ *webtest.Fake }

// TRLC-LINKS: REQ-DSO-164
func (plainScope) Status() any { return pageStatus() }

// twinScope is the Twin with the page's status.
type twinScope struct{ *webtest.Twin }

// TRLC-LINKS: REQ-DSO-164
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
	BrandName     string                     `json:"brandName"`
	Title         string                     `json:"title"`
	VertOver      int                        `json:"vertOver"`
	Areas         pageAreas                  `json:"areas"`
	AllGroups     []string                   `json:"allGroups"`
	AllRows       bool                       `json:"allRows"`
	Sound         pageSound                  `json:"sound"`
	Fewer         *pageFewer                 `json:"fewer"`
	Errors        []string                   `json:"errors"`
	Requests      []string                   `json:"requests"`
}

// pageAreas is what the page made of its last channel: its peaks, math,
// X-Y, references and exports.
type pageAreas struct {
	Peaks    bool     `json:"peaks"`
	PkSub    string   `json:"pkSub"`
	SpgCh    []string `json:"spgCh"`
	MathOpts []string `json:"mathOpts"`
	MathFn   string   `json:"mathFn"`
	MathN    int      `json:"mathN"`
	XYOpts   []string `json:"xyOpts"`
	XYPair   []int    `json:"xyPair"`
	RefChans []int    `json:"refChans"`
	RefInfo  string   `json:"refInfo"`
	CSVHead  string   `json:"csvHead"`
	CSVVolts int      `json:"csvVolts"`
	WAVCh    int      `json:"wavCh"`
	VCDVars  int      `json:"vcdVars"`
}

// pageSound is SOUND's lane labels and gate choices.
type pageSound struct {
	Src    []string `json:"src"`
	Meters []string `json:"meters"`
	Gate   []string `json:"gate"`
}

// pageFewer is the page after a caps document of three channels.
type pageFewer struct {
	Toggles   []string `json:"toggles"`
	VgCells   int      `json:"vgCells"`
	AllGroups []string `json:"allGroups"`
	MathOpts  int      `json:"mathOpts"`
	Gate      int      `json:"gate"`
	Source    []string `json:"source"`
	Keys      []string `json:"keys"`
	Stale     int      `json:"stale"`
}

// drivePage runs caps_browser.mjs against a server over sc, publishing a
// frame every 50 ms.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
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
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
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
	b.Brand, b.BrandName, b.Title = a.Brand, a.BrandName, a.Title
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
	if w.Brand != "fake four-channel 12-bit" || w.BrandName != "fake firmware" || w.Title != "fake firmware / fake four-channel 12-bit" {
		t.Fatalf("brand %q %q, title %q", w.BrandName, w.Brand, w.Title)
	}
	if plain.BrandName != "open-dso" || plain.Title != "open-dso" {
		t.Fatalf("without caps: brand %q, title %q", plain.BrandName, plain.Title)
	}
	checkTwoChannelAreas(t, plain)
	checkFourChannelAreas(t, w)
}

// hasRequest reports a request with prefix p.
// TRLC-LINKS: REQ-DSO-164
func hasRequest(rs []string, p string) bool {
	return slices.ContainsFunc(rs, func(r string) bool { return strings.HasPrefix(r, p) })
}

// checkTwoChannelAreas: on the two-channel scope without caps every part
// keeps its two-channel shape and v1 requests.
// TRLC-LINKS: REQ-DSO-164
func checkTwoChannelAreas(t *testing.T, p pageRun) {
	t.Helper()
	a := p.Areas
	if !hasRequest(p.Requests, `POST /api/set {"control":"triglevelcode","value":`) || hasRequest(p.Requests, `POST /api/set {"control":"triglevel",`) {
		t.Errorf("the trigger level was not set as a DAC code: %v", p.Requests)
	}
	if hasRequest(p.Requests, `POST /api/set {"control":"coupling","ch"`) || !hasRequest(p.Requests, `POST /api/set {"control":"coupling2","value":1}`) {
		t.Errorf("C2's coupling was not set by its v1 name: %v", p.Requests)
	}
	if !hasRequest(p.Requests, "GET /api/frame.bin?lease=raw") && !slices.ContainsFunc(p.Requests, func(r string) bool { return strings.Contains(r, "raw=1") }) {
		t.Errorf("the mask did not read the raw feed: %v", p.Requests)
	}
	want := []string{"off", "c1-c2", "c2-c1", "c1+c2", "c1*c2", "res1", "res2"}
	if !reflect.DeepEqual(a.MathOpts, want) || a.MathFn != "c1-c2" || a.MathN == 0 {
		t.Errorf("math %v %q %d", a.MathOpts, a.MathFn, a.MathN)
	}
	if a.XYOpts != nil || !reflect.DeepEqual(a.XYPair, []int{1, 2}) {
		t.Errorf("X-Y %v %v", a.XYOpts, a.XYPair)
	}
	if !a.Peaks || a.PkSub != "CH2" || !reflect.DeepEqual(a.SpgCh, []string{"C1", "C2"}) {
		t.Errorf("peaks %v %q, spectrogram %v", a.Peaks, a.PkSub, a.SpgCh)
	}
	if a.CSVHead != "t_s,c1_v,c2_v" || a.CSVVolts != 3 || a.WAVCh != 2 || a.VCDVars != 2 {
		t.Errorf("exports %q %d %d %d", a.CSVHead, a.CSVVolts, a.WAVCh, a.VCDVars)
	}
	if !reflect.DeepEqual(a.RefChans, []int{1}) || !strings.HasPrefix(a.RefInfo, "C1") {
		t.Errorf("references %v %q", a.RefChans, a.RefInfo)
	}
	if len(p.AllGroups) != 2 || !p.AllRows {
		t.Errorf("All settings %v %v", p.AllGroups, p.AllRows)
	}
	if !reflect.DeepEqual(p.Sound.Src, []string{"C1", "C2"}) || !reflect.DeepEqual(p.Sound.Meters, []string{"C1", "C2"}) ||
		!reflect.DeepEqual(p.Sound.Gate, []string{"Signal level", "Gate C1", "Gate C2"}) {
		t.Errorf("SOUND %+v", p.Sound)
	}
	if p.VertOver != 0 || p.Fewer != nil {
		t.Errorf("vertical overflow %d, fewer %v", p.VertOver, p.Fewer)
	}
}

// checkFourChannelAreas: on the four-channel 12-bit scope every part offers
// and reads C3 and C4, sets them by channel and the level in volts.
// TRLC-LINKS: REQ-DSO-164
func checkFourChannelAreas(t *testing.T, w pageRun) {
	t.Helper()
	a := w.Areas
	for _, r := range []string{
		`POST /api/set {"control":"triglevel","ch":0,"value_v":0.2`,
		`POST /api/set {"control":"coupling","ch":3,"value":1}`,
		`POST /api/set {"control":"bwlimit","ch":3,"value":1}`,
	} {
		if !hasRequest(w.Requests, r) {
			t.Errorf("not sent: %s\n%v", r, w.Requests)
		}
	}
	if hasRequest(w.Requests, `POST /api/set {"control":"triglevelcode"`) {
		t.Errorf("a DAC code was sent to a scope that takes volts")
	}
	if !slices.ContainsFunc(w.Requests, func(r string) bool {
		return strings.HasPrefix(r, "GET /api/frame.bin?") && strings.Contains(r, "raw=1") && strings.Contains(r, "v=2")
	}) {
		t.Errorf("the raw feed was not read in wire v2: %v", w.Requests)
	}
	if !slices.ContainsFunc(w.Requests, func(r string) bool { return strings.HasPrefix(r, "POST /api/mask ") && strings.Contains(r, `"ch":3`) }) {
		t.Errorf("no mask of C4 was installed")
	}
	if !a.Peaks || a.PkSub != "CH4" || len(a.SpgCh) != 4 {
		t.Errorf("peaks %v %q, spectrogram %v", a.Peaks, a.PkSub, a.SpgCh)
	}
	if len(a.MathOpts) != 1+6*4+4 || a.MathFn != "c3-c4" || a.MathN == 0 || !slices.Contains(a.MathOpts, "res4") {
		t.Errorf("math %v %q %d", a.MathOpts, a.MathFn, a.MathN)
	}
	if !reflect.DeepEqual(a.XYOpts, []string{"C1", "C2", "C3", "C4"}) || !reflect.DeepEqual(a.XYPair, []int{3, 4}) {
		t.Errorf("X-Y %v %v", a.XYOpts, a.XYPair)
	}
	if !reflect.DeepEqual(a.RefChans, []int{1, 2, 3, 4}) || !strings.HasPrefix(a.RefInfo, "C1+C2+C3+C4") {
		t.Errorf("references %v %q", a.RefChans, a.RefInfo)
	}
	if a.CSVHead != "t_s,c1_v,c2_v,c3_v,c4_v" || a.CSVVolts != 5 || a.WAVCh != 4 || a.VCDVars != 4 {
		t.Errorf("exports %q %d %d %d", a.CSVHead, a.CSVVolts, a.WAVCh, a.VCDVars)
	}
	if !reflect.DeepEqual(w.AllGroups, []string{"Channel 1", "Channel 2", "Channel 3", "Channel 4"}) || !w.AllRows {
		t.Errorf("All settings %v %v", w.AllGroups, w.AllRows)
	}
	if !reflect.DeepEqual(w.Sound.Src, []string{"C4", "C1"}) || !reflect.DeepEqual(w.Sound.Meters, []string{"C4", "C1"}) || len(w.Sound.Gate) != 5 {
		t.Errorf("SOUND %+v", w.Sound)
	}
	if w.VertOver != 0 {
		t.Errorf("%d cells of the Vertical pane overflow", w.VertOver)
	}
	f := w.Fewer
	if f == nil {
		t.Fatal("no caps change")
	}
	if !reflect.DeepEqual(f.Toggles, []string{"tC1", "tC2", "tC3"}) || f.VgCells != 9*4 || !reflect.DeepEqual(f.AllGroups, []string{"Channel 1", "Channel 2", "Channel 3"}) ||
		f.MathOpts != 1+3*4+3 || f.Gate != 4 || !reflect.DeepEqual(f.Source, []string{"C1", "C2", "C3"}) ||
		!reflect.DeepEqual(f.Keys, []string{"1", "2", "3"}) || f.Stale != 0 {
		t.Errorf("after a caps document of three channels: %+v", *f)
	}
}
