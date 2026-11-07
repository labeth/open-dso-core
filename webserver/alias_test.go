// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/webtest"
)

// recordingSettings keeps the controls it is handed.
type recordingSettings struct{ got []webserver.Control }

// settingsScope is the fake with its controls recorded.
type settingsScope struct {
	*webtest.Fake
	s *recordingSettings
}

// TRLC-LINKS: REQ-DSO-162
func (s settingsScope) Settings() webserver.Settings { return s.s }

// TRLC-LINKS: REQ-DSO-162
func (r *recordingSettings) Set(_ context.Context, c webserver.Control) webserver.Applied {
	r.got = append(r.got, c)
	return webserver.Applied{OK: true, Value: c.Value}
}

// A v1 per-channel name reaches the scope as its v2 control and channel,
// a v2 body as it is (value_v as the value), and a v1 name the v2 API keeps
// unchanged with no channel.
// TRLC-LINKS: REQ-DSO-162
func TestSetAliases(t *testing.T) {
	rs := &recordingSettings{}
	h := webserver.New(settingsScope{Fake: webtest.NewFake(false), s: rs}).Handler()
	for _, body := range []string{
		`{"control":"vdiv2","value":0.5}`,
		`{"control":"coupling1","value":1}`,
		`{"control":"offset","ch":3,"value":-0.25}`,
		`{"control":"triglevel","ch":2,"value_v":0.125}`,
		`{"control":"tdiv","value":1e-6}`,
		`{"control":"bwl2","value":1}`,
	} {
		rec := serve(t, h, http.MethodPost, "/api/set", body)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	want := []webserver.Control{
		{Name: "vdiv", Ch: 1, Value: 0.5, V1: "vdiv2"},
		{Name: "coupling", Ch: 0, Value: 1, V1: "coupling1"},
		{Name: "offset", Ch: 3, Value: -0.25},
		{Name: "triglevel", Ch: 2, Value: 0.125},
		{Name: "tdiv", Ch: -1, Value: 1e-6},
		{Name: "bwlimit", Ch: 1, Value: 1, V1: "bwl2"},
	}
	if len(rs.got) != len(want) {
		t.Fatalf("%d controls", len(rs.got))
	}
	for i, w := range want {
		if g := rs.got[i]; g.Name != w.Name || g.Ch != w.Ch || g.Value != w.Value || g.V1 != w.V1 || g.ValueV != nil {
			t.Fatalf("control %d: %+v, want %+v", i, g, w)
		}
	}
}

// A failing frame of the mask test reads in the v2 shape with ?v=2: its
// traces with their channel, format and scale; without, as v1.
// TRLC-LINKS: REQ-DSO-162
func TestMaskFailV2(t *testing.T) {
	h := webserver.New(webtest.NewFake(false)).Handler()
	v2 := serve(t, h, http.MethodGet, "/api/maskfail?i=0&v=2", "").Body.String()
	if !strings.Contains(v2, `"traces":[{"ch":0,"fmt":{"bits":8,"storage":"u8"`) || !strings.Contains(v2, `"per_code_v":0.04`) || strings.Contains(v2, `"vpc1"`) {
		t.Fatalf("v2: %s", v2)
	}
	if v1 := serve(t, h, http.MethodGet, "/api/maskfail?i=0", "").Body.String(); !strings.Contains(v1, `"vpc1":0.04`) || strings.Contains(v1, "traces") {
		t.Fatalf("v1: %s", v1)
	}
	if out := serve(t, h, http.MethodPost, "/api/zones", `[{"dt_lo_s":0,"dt_hi_s":1,"v_lo":0,"v_hi":1}]`).Body.String(); !strings.Contains(out, "display codes") {
		t.Fatalf("v2 zones on a scope that takes codes: %s", out)
	}
}
