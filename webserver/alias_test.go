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
