// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb_test

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/testenv"
	"github.com/labeth/open-dso-core/webclient"
	"github.com/labeth/open-dso-core/webserver"
)

// pageLoad is what pageload_browser.mjs reports of the page.
type pageLoad struct {
	CapsServed bool  `json:"capsServed"`
	Chans      []int `json:"chans"`
	Wire       int   `json:"wire"`
	Drawn      map[string]struct {
		N   int     `json:"n"`
		Lo  float64 `json:"lo"`
		Hi  float64 `json:"hi"`
		Vpc float64 `json:"vpc"`
		Off float64 `json:"off"`
	} `json:"drawn"`
	Vdiv1 []float64 `json:"vdiv1"`
	Trig  *struct {
		Types, Disabled int
		Why, Pick       string
		Type, Setting   string
		Value           float64
	} `json:"trig"`
	Errors []string `json:"errors"`
	Failed []string `json:"failed"`
}

// The shared page runs against the web scope of a four-channel 12-bit
// board driven by the core's HAL loop: it builds four channels from caps,
// reads frame wire v2 from the first request on and draws every channel's
// ±2-division square at its volts; its V/div step on C1 reaches the
// instrument state through the v1 name's alias; no page error, no failed
// reply.
// TRLC-LINKS: REQ-DSO-162, REQ-DSO-163, REQ-DSO-164, REQ-DSO-1004
func TestPageBrowser(t *testing.T) {
	testenv.NeedNode(t)
	r := newRig(t, halfake.Config{Channels: 4, Format: format12})
	srv := httptest.NewServer(webserver.New(r.web).Handler())
	defer srv.Close()
	cmd := exec.CommandContext(t.Context(), "node", "pageload_browser.mjs", srv.URL)
	cmd.Dir = webclient.Dir()
	out, err := cmd.CombinedOutput()
	if strings.HasPrefix(string(out), "SKIP") {
		testenv.SkipBrowser(t, "%s", out)
	}
	if err != nil {
		t.Fatalf("pageload_browser.mjs: %v\n%s", err, out)
	}
	var p pageLoad
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !p.CapsServed || len(p.Chans) != 4 || p.Wire != 2 {
		t.Fatalf("caps %v channels %v wire %d", p.CapsServed, p.Chans, p.Wire)
	}
	for k, d := range p.Drawn {
		top, bottom := (d.Hi-128)*d.Vpc-d.Off, (d.Lo-128)*d.Vpc-d.Off
		if d.N == 0 || math.Abs(top-2*p.Vdiv1[0]) > 0.05 && math.Abs(top-2*p.Vdiv1[1]) > 0.05 || math.Abs(top+bottom) > 0.05 {
			t.Fatalf("%s drawn %.3f … %.3f V: %+v", k, bottom, top, d)
		}
	}
	if len(p.Drawn) != 4 {
		t.Fatalf("drawn %v", p.Drawn)
	}
	if len(p.Vdiv1) != 2 || p.Vdiv1[0] != 1 || p.Vdiv1[1] != 2 || r.m.State().Channels[0].VoltsDiv != 2 {
		t.Fatalf("C1's V/div step: status %v, instrument %g", p.Vdiv1, r.m.State().Channels[0].VoltsDiv)
	}
	if len(p.Errors) > 0 || len(p.Failed) > 0 {
		t.Fatalf("page errors %v, failed replies %v", p.Errors, p.Failed)
	}
	// The trigger's types from caps: the fake board runs edge and pulse.
	if tr := p.Trig; tr == nil || tr.Types != 18 || tr.Disabled != 16 || !strings.Contains(tr.Why, "node") ||
		tr.Pick != "pulse" || tr.Type != "pulse" || tr.Value != 0.125 {
		t.Fatalf("trigger %+v", p.Trig)
	}
	if st := r.m.State(); st.Trigger.Type.String() != "pulse" {
		t.Fatalf("instrument trigger %v", st.Trigger.Type)
	}
}
