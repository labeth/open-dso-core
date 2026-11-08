package simdemo_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/decode/synth"
	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/simdemo"
	"github.com/labeth/open-dso-core/uimodel"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// TestAutoInBusSurvey runs AUTO in BUS over the simulated board on every
// bus the core's generators write (siggen bus.PROTO.ROLE), as a scope's
// decode sweep does on its own simulator, and logs a line a bus: what AUTO
// named, the trigger and timebase it framed the bus with, and the decode of
// the record against the generator's truth. A survey, run by hand
// (SDS_BUS_SURVEY=1): SDS_BUS_SURVEY_DEMO=dir a scope's profile.yaml and
// demo.json (default testdata), SDS_BUS_SURVEY_REFUSE=1 drops the protocol
// node (a board whose trigger takes no bus request),
// SDS_BUS_SURVEY_CASES=a,b the buses, SDS_BUS_SURVEY_ALL=1 every variant.
// TRLC-LINKS: REQ-DSO-223, REQ-DSO-295
func TestAutoInBusSurvey(t *testing.T) {
	if os.Getenv("SDS_BUS_SURVEY") == "" {
		t.Skip("a survey run by hand: SDS_BUS_SURVEY=1")
	}
	dir := os.Getenv("SDS_BUS_SURVEY_DEMO")
	if dir == "" {
		dir = "testdata"
	}
	profileYAML, err := os.ReadFile(filepath.Join(dir, "profile.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	demoJSON, err := os.ReadFile(filepath.Join(dir, "demo.json"))
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SDS_BUS_SURVEY_REFUSE") != "" {
		profileYAML = bytes.Replace(profileYAML, []byte("    protocol: {}\n"), nil, 1)
	}
	d, err := simdemo.ParseDemo(demoJSON)
	if err != nil {
		t.Fatal(err)
	}
	var web http.Handler
	o := simdemo.Options{Logf: func(f string, a ...any) {
		if strings.Contains(f, "bus auto") || strings.Contains(f, "bus search") || os.Getenv("SDS_BUS_SURVEY_LOG") != "" {
			t.Logf(f, a...)
		}
	}, Version: "test", Serve: func(h http.Handler) { web = h }}
	b, _, err := simdemo.Start(t.Context(), profileYAML, d, o)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(web)
	defer srv.Close()
	nch := b.Signals().Inputs()
	u := &rig{t: t, url: srv.URL}
	until(5*time.Second, func() bool { return u.status()["seq"] != nil })
	var ws map[string]any
	_ = u.post("/api/panel", `{"workspace":"bus"}`, &ws)
	sweep := os.Getenv("SDS_BUS_SURVEY_ALL") != ""
	pairs := [][2]int{{0, 1}, {2, 3}, {1, 3}, {0, 2}}
	if nch == 2 {
		pairs = [][2]int{{0, 1}, {1, 0}}
	}
	k := 0
	pass, n := 0, 0
	var lines []string
	for _, p := range synth.Protocols() {
		vs := synth.Variants(p.Proto)
		if !sweep {
			vs = vs[:1]
		}
		for _, s := range vs {
			id := strings.ReplaceAll(s.String(), " ", "/")
			place := map[string]int{}
			if len(p.Roles) == 1 {
				place[p.Roles[0]] = k % nch
			} else {
				pr := pairs[k%len(pairs)]
				place[p.Roles[0]], place[p.Roles[1]] = pr[0], pr[1]
			}
			k++
			if cs := os.Getenv("SDS_BUS_SURVEY_CASES"); cs != "" && !slices.ContainsFunc(strings.Split(cs, ","), func(w string) bool { return strings.Contains(id, w) }) {
				continue
			}
			ok, line := u.runCase(b.Signals().Set, nch, id, s, place)
			n++
			if ok {
				pass++
			}
			lines = append(lines, line)
			t.Log(line)
		}
	}
	t.Logf("SUMMARY %d/%d pass\n%s", pass, n, strings.Join(lines, "\n"))
}

type rig struct {
	t   *testing.T
	url string
}

func (u *rig) post(path, body string, v any) error {
	r, err := http.Post(u.url+path, "application/json", strings.NewReader(body)) //nolint:noctx // test
	if err != nil {
		return err
	}
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}

func (u *rig) get(path string, v any) error {
	r, err := http.Get(u.url + path) //nolint:noctx // test
	if err != nil {
		return err
	}
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}

func (u *rig) status() map[string]any {
	var st map[string]any
	_ = u.get("/api/status", &st)
	return st
}

func (u *rig) panel() uimodel.Status {
	var st uimodel.Status
	_ = u.get("/api/panel/wait?seq=0&timeout_ms=1", &st)
	return st
}

func (u *rig) setRow(row string, value any) uimodel.SetResult {
	b, _ := json.Marshal(map[string]any{"row": row, "value": value})
	var res uimodel.SetResult
	if err := u.post("/api/sheets/set", string(b), &res); err != nil {
		res.Error = err.Error()
	}
	return res
}

func until(d time.Duration, f func() bool) bool {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if f() {
			return true
		}
	}
	return false
}

func (u *rig) runCase(set func(int, string) error, nch int, id string, s synth.Spec, place map[string]int) (bool, string) {
	bus, err := synth.Make(s, 0)
	if err != nil {
		return false, id + " " + err.Error()
	}
	vpp, off := 3.3, 1.65
	switch s.Proto {
	case "arinc429":
		vpp, off = 20, 0
	case "mil1553":
		vpp, off = 14, 0
	}
	for ch := range nch {
		_ = set(ch, "dc,0,0,0")
	}
	var opts []string
	for k, v := range s.Set {
		opts = append(opts, k+"="+v)
	}
	slices.Sort(opts)
	var on []string
	for role, ch := range place {
		spec := strings.Join(append([]string{"bus." + s.Proto + "." + role, "0", fmt.Sprint(vpp), fmt.Sprint(off)}, opts...), ",")
		if err := set(ch, spec); err != nil {
			return false, id + " signal: " + err.Error()
		}
		if r := u.setRowText(fmt.Sprintf("ch%d.on", ch+1), "On"); !r.OK {
			u.t.Logf("ch%d.on: %s", ch+1, r.Error)
		}
		on = append(on, fmt.Sprintf("%s=C%d", role, ch+1))
	}
	slices.Sort(on)
	time.Sleep(500 * time.Millisecond)
	until(60*time.Second, func() bool { return !u.panel().AutosetBusy })
	if r := u.setRow("bus.protocol", framecalc.ProtoOff); !r.OK {
		return false, id + " off: " + r.Error
	}
	before := u.panel().AutosetDone
	t0 := time.Now()
	var rep map[string]any
	_ = u.post("/api/panel", `{"button":"auto","for":"bus"}`, &rep)
	done := until(150*time.Second, func() bool {
		st := u.panel()
		return !st.AutosetBusy && st.AutosetDone != before
	})
	took := time.Since(t0).Round(100 * time.Millisecond)
	p, named, settings := u.busSettings()
	if d := u.panel().Display; d.DecProto == p.Proto { // the decode the app runs
		p = framecalc.Params(d.DecProto, d.DecBaud, d.DecChA, d.DecChB, d.DecCPOL, d.DecCPHA, d.DecLSB, 0, d.DecDelayNs, d.DecParity)
		if p.Proto == framecalc.ProtoUART {
			p.Parity, p.Inverted = d.DecParity, d.DecInverted
		}
	}
	st := u.status()
	trig := fmt.Sprintf("trig=%v ser=%v tb=%v sb=%v tdiv=%v", st["trig"], st["serial_mode"], st["trigger_backend"], st["serial_backend"], st["tdiv_set_s"])
	head := fmt.Sprintf("%-40s %-22s auto=%v(%v) named=%-9s [%s] %s NOTE{%s}", id, strings.Join(on, ","), took, done, named, settings, trig, u.panel().Display.DecNote)
	if named != bus.Proto {
		return false, "FAIL " + head + " :: wrong name"
	}
	// settled frame of the tdiv set
	var f *sample.Frame
	var tdiv float64
	if v, ok := st["tdiv_set_s"].(float64); ok {
		tdiv = v
	}
	until(30*time.Second, func() bool {
		f, err = u.rawFrame()
		return err == nil && f != nil && (tdiv == 0 || math.Abs(f.Snapshot.TdivS-tdiv) <= 1e-3*tdiv)
	})
	if f == nil {
		return false, "FAIL " + head + " :: no frame " + fmt.Sprint(err)
	}
	r := framecalc.Decode(f, p)
	okm, why := matchTruth(r, bus.Truth)
	if !okm && bus.Truth.Mirrored != nil {
		m := bus.Truth
		m.Bytes = bus.Truth.Mirrored
		if ok2, why2 := matchTruth(r, m); ok2 {
			okm, why = true, why2+" (mirrored)"
		}
	}
	if !okm {
		var ed []string
		for ch := range 4 {
			c := framecalc.Channel(f, ch, framecalc.Valid(f))
			n, lo, hi := 0, 255, 0
			for i := range c {
				lo, hi = min(lo, int(c[i])), max(hi, int(c[i]))
				if i > 0 && (c[i] > 128) != (c[i-1] > 128) {
					n++
				}
			}
			ed = append(ed, fmt.Sprintf("C%d:%d[%d..%d]", ch+1, n, lo, hi))
		}
		why += " edges " + strings.Join(ed, " ")
	}
	verdict := "FAIL"
	if okm {
		verdict = "PASS"
	}
	return okm, fmt.Sprintf("%s %s :: rec %d@%.3g trigAt %.0f :: %s", verdict, head, f.Len(), f.SampleS, f.TriggerAt, why)
}

func (u *rig) rawFrame() (*sample.Frame, error) {
	r, err := http.Get(u.url + "/api/frame.bin?v=2&raw=1&waitms=2000") //nolint:noctx // test
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Body.Close() }()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	wf, err := wire.DecodeFrame2(b)
	if err != nil {
		return nil, err
	}
	h := wf.Header
	f := &sample.Frame{Content: sample.RawFullRate, Flags: sample.Ordered, SampleS: h.SampleS, CaptureS: h.CaptureS, TriggerAt: h.TriggerAt, Seq: h.Seq, Snapshot: sample.Settings{TdivS: h.TdivS}}
	n := 0
	for _, t := range wf.Traces {
		n = max(n, t.Codes.Len())
	}
	if n == 0 {
		return nil, fmt.Errorf("no samples")
	}
	f.Traces = make([]sample.Trace, 4)
	for i := range f.Traces {
		f.Traces[i] = sample.Trace{Ch: i, Valid: n}
	}
	for _, t := range wf.Traces {
		if t.Ch < 0 || t.Ch >= 4 || t.Codes.Len() == 0 {
			continue
		}
		tr := &f.Traces[t.Ch]
		tr.Format = sample.CodeFormat{Bits: t.Fmt.Bits, Shift: t.Fmt.Shift, Frac: t.Fmt.Frac, Min: t.Fmt.Min, Max: t.Fmt.Max, Mid: t.Fmt.Mid}
		if t.Codes.U16 != nil {
			tr.Format.Storage, tr.Codes16 = sample.U16, t.Codes.U16
		} else {
			tr.Format.Storage, tr.Codes8 = sample.U8, t.Codes.U8
		}
	}
	return f, nil
}

func (u *rig) busSettings() (framecalc.DecodeParams, string, string) {
	var sheets struct {
		Tabs []struct {
			ID   string
			Rows []struct {
				ID        string
				Text      string
				Value     float64
				Available bool
			}
		}
	}
	_ = u.get("/api/sheets", &sheets)
	p := framecalc.DecodeParams{Format: "hex"}
	var set []string
	chOf := func(t string) int {
		var n int
		if _, err := fmt.Sscanf(t, "CH%d", &n); err == nil {
			return n - 1
		}
		return 0
	}
	for _, t := range sheets.Tabs {
		if t.ID != "bus" {
			continue
		}
		for _, r := range t.Rows {
			if !r.Available {
				continue
			}
			switch r.ID {
			case "bus.protocol":
				p.Proto = int(r.Value)
			case "bus.source", "bus.scl", "bus.clk":
				p.ChA = chOf(r.Text)
			case "bus.sda", "bus.data":
				p.ChB = chOf(r.Text)
			case "bus.spi_mode":
				m := int(r.Value)
				p.CPOL, p.CPHA = m >= 2, m%2 == 1
			case "bus.bit_order":
				p.LSB = strings.HasPrefix(r.Text, "LSB")
			case "bus.parity":
				if r.Text != "Auto" {
					p.Parity = strings.ToLower(r.Text)
				}
			case "bus.uart_line":
				p.Inverted = r.Text == "Inverted"
			case "bus.baud":
				p.Baud = baudOf(r.Text)
			default:
				continue
			}
			set = append(set, strings.TrimPrefix(r.ID, "bus.")+"="+r.Text)
		}
	}
	return p, framecalc.ResultName(p.Proto), strings.Join(set, " ")
}

func baudOf(t string) int {
	t = strings.TrimSuffix(strings.TrimSuffix(t, "b/s"), "Bd")
	mul := 1.0
	for _, u := range []struct {
		s string
		m float64
	}{{"k", 1e3}, {"M", 1e6}} {
		if i := strings.Index(t, u.s); i >= 0 {
			t = t[:i] + "." + t[i+1:]
			mul = u.m
		}
	}
	var v float64
	if _, err := fmt.Sscanf(strings.TrimSuffix(t, "."), "%g", &v); err != nil {
		return 0
	}
	return int(v*mul + 0.5)
}

func matchTruth(r decode.Result, t synth.Truth) (bool, string) {
	if !r.OK && len(r.Bytes) == 0 {
		return false, "no decode: " + r.Error
	}
	someMessages := func() (bool, string) {
		n := 0
		for _, m := range t.Messages {
			if strings.Contains(r.Text, m) {
				n++
			}
		}
		if n == 0 || (t.Bytes != nil && longestCyclic(r.Bytes, repeatUnit(t.Bytes)) < len(r.Bytes)) {
			return false, ""
		}
		return true, fmt.Sprintf("partial %d/%d msgs", n, len(t.Messages))
	}
	for _, w := range t.Contains {
		if !strings.Contains(r.Text, w) {
			if ok, why := someMessages(); ok {
				return true, why
			}
			return false, fmt.Sprintf("lacks %q", w)
		}
	}
	if t.Bytes == nil {
		return true, "tokens"
	}
	unit := repeatUnit(t.Bytes)
	for i := 0; i+len(unit) <= len(r.Bytes); i++ {
		if slices.Equal(r.Bytes[i:i+len(unit)], unit) {
			return true, "whole"
		}
	}
	if n := longestCyclic(r.Bytes, unit); n >= min(4, len(unit)) {
		return true, fmt.Sprintf("partial run %d/%d", n, len(unit))
	}
	if ok, why := someMessages(); ok {
		return true, why
	}
	return false, fmt.Sprintf("decoded % x want % x", r.Bytes[:min(16, len(r.Bytes))], unit[:min(16, len(unit))])
}

func longestCyclic(b, unit []int) int {
	if len(unit) == 0 {
		return 0
	}
	best := 0
	for i := range b {
		for s := range unit {
			n := 0
			for i+n < len(b) && b[i+n] == unit[(s+n)%len(unit)] {
				n++
			}
			best = max(best, n)
		}
	}
	return best
}

func repeatUnit(b []int) []int {
	for n := 1; n <= len(b); n++ {
		if len(b)%n != 0 {
			continue
		}
		ok := true
		for i := n; i < len(b) && ok; i++ {
			ok = b[i] == b[i-n]
		}
		if ok {
			return b[:n]
		}
	}
	return b
}

func (u *rig) setRowText(row, text string) uimodel.SetResult {
	b, _ := json.Marshal(map[string]any{"row": row, "text": text})
	var res uimodel.SetResult
	if err := u.post("/api/sheets/set", string(b), &res); err != nil {
		res.Error = err.Error()
	}
	return res
}
