// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webtest

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// checkCaps checks the capabilities document and frame wire v2 against
// the scope: a scope without caps serves none, announces none in its status
// and answers a v2 frame request in v1; one with caps serves a document the
// page can build its lists from, repeats its generation in the status, and
// serves v2 frames exactly when the document lists them.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func checkCaps(t *testing.T, c client, sc webserver.Scope) {
	t.Helper()
	status := c.json(http.MethodGet, "/api/status", "", http.StatusOK)
	cs, ok := sc.(webserver.CapsSource)
	if !ok {
		checkNoCaps(t, c, sc, status)
		return
	}
	rec := c.do(http.MethodGet, "/api/caps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/caps: %d", rec.Code)
	}
	var caps wire.Caps
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("/api/caps: %v", err)
	}
	if caps.API != webserver.APIVersion || caps.Gen != cs.CapsGen() {
		t.Fatalf("caps api %d gen %d, want %d and %d", caps.API, caps.Gen, webserver.APIVersion, cs.CapsGen())
	}
	if g, _ := status["caps_gen"].(float64); uint64(g) != caps.Gen || status["api"] != float64(webserver.APIVersion) {
		t.Fatalf("status caps_gen %v api %v, caps gen %d", status["caps_gen"], status["api"], caps.Gen)
	}
	checkCapsLists(t, caps)
	_, only := sc.Frames().(webserver.V2Only)
	if slices.Contains(caps.Wire.Frame, 1) == only {
		t.Fatalf("frame wire %v on a scope that serves v1: %v", caps.Wire.Frame, !only)
	}
	checkControlsV2(t, c, caps)
	_, v2 := sc.Frames().(webserver.FramesV2)
	if slices.Contains(caps.Wire.Frame, 2) != v2 {
		t.Fatalf("frame wire %v, FramesV2 %v", caps.Wire.Frame, v2)
	}
	if rec := c.do(http.MethodPost, "/api/caps", "{}"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/caps: %d", rec.Code)
	}
	if v2 {
		checkFramesV2(t, c, caps)
	}
}

// checkNoCaps checks a scope without caps: no document, no caps_gen in its
// status, and a v2 frame request answered in v1 unless it serves v2.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func checkNoCaps(t *testing.T, c client, sc webserver.Scope, status map[string]any) {
	t.Helper()
	if rec := c.do(http.MethodGet, "/api/caps", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("/api/caps without caps: %d", rec.Code)
	}
	if _, has := status["caps_gen"]; has {
		t.Fatalf("a status without caps carries caps_gen")
	}
	if _, ok := sc.Frames().(webserver.FramesV2); ok {
		return
	}
	if rec := c.do(http.MethodGet, "/api/frame.bin?v=2&cols=64", ""); len(rec.Body.Bytes()) == 0 || rec.Body.Bytes()[0] != wire.BinMagic {
		t.Fatalf("v2 asked of a v1 scope: not v1")
	}
}

// checkCapsLists checks the lists the page builds from: channels in order
// with names, trigger sources that are channels (or EXT/LINE), and a reason
// for every unavailable feature.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func checkCapsLists(t *testing.T, caps wire.Caps) {
	t.Helper()
	if len(caps.Channels) == 0 || caps.Codes.CodesPerDiv <= 0 || caps.Codes.Storage == "" {
		t.Fatalf("caps without channels or a code format: %+v", caps)
	}
	for i, ch := range caps.Channels {
		if ch.Ch != i || ch.Name == "" {
			t.Fatalf("channel %d: %+v", i, ch)
		}
	}
	for _, s := range caps.Trigger.Sources {
		if !slices.ContainsFunc(caps.Channels, func(ch wire.CapsChannel) bool { return ch.Name == s }) && s != "EXT" && s != "LINE" {
			t.Fatalf("trigger source %q is no channel", s)
		}
	}
	for name, f := range caps.Features {
		if !f.OK && f.Why == "" {
			t.Fatalf("feature %s unavailable without a reason", name)
		}
	}
}

// checkFramesV2 checks v2 frames: traces in the caps' channel order with
// their scales, a held frame unchanged (or a newer one), and raw frames.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func checkFramesV2(t *testing.T, c client, caps wire.Caps) {
	t.Helper()
	h := c.frame2("/api/frame.bin?v=2&cols=100")
	if h.Unchanged {
		t.Skip("the scope has published no frame")
	}
	if len(h.Traces) == 0 {
		t.Fatalf("a v2 frame without traces")
	}
	for i, tr := range h.Traces {
		if tr.Ch < 0 || tr.Ch >= len(caps.Channels) || (i > 0 && tr.Ch <= h.Traces[i-1].Ch) {
			t.Fatalf("trace %d is channel %d: not in channel order of caps", i, tr.Ch)
		}
		if tr.Scale.CodesPerDiv <= 0 || tr.Scale.PerCodeV <= 0 {
			t.Fatalf("trace %d scale %+v", i, tr.Scale)
		}
	}
	if again := c.frame2("/api/frame.bin?v=2&cols=100&since=" + jsonUint(h.Seq)); again.Seq <= h.Seq && (!again.Unchanged || len(again.Segs) != 0) {
		t.Fatalf("the frame held: %+v", again)
	}
	if raw := c.frame2("/api/frame.bin?v=2&raw=1"); !raw.Unchanged && (!raw.Raw || len(raw.Segs) == 0 || raw.Segs[0].Role != wire.RoleRaw) {
		t.Fatalf("raw v2: %+v", raw.Segs)
	}
}

// jsonUint is v in decimal.
// TRLC-LINKS: REQ-DSO-163
func jsonUint(v uint64) string { b, _ := json.Marshal(v); return string(b) }

// frame2 fetches a v2 frame and checks its layout: the magic, the header,
// the padding and every segment inside the payload at an aligned offset.
// TRLC-LINKS: REQ-DSO-163
func (c client) frame2(path string) wire.FrameHeader {
	c.t.Helper()
	rec := c.do(http.MethodGet, path, "")
	b := rec.Body.Bytes()
	if rec.Code != http.StatusOK || len(b) < 8 || string(b[:4]) != string(wire.FrameMagic2[:]) {
		c.t.Fatalf("%s: status %d, %d bytes", path, rec.Code, len(b))
	}
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	if 8+n > len(b) {
		c.t.Fatalf("%s: header of %d bytes overruns %d", path, n, len(b))
	}
	var h wire.FrameHeader
	if err := json.Unmarshal(b[8:8+n], &h); err != nil {
		c.t.Fatalf("%s: header: %v", path, err)
	}
	start := (8 + n + 7) &^ 7
	if len(b) < start {
		c.t.Fatalf("%s: %d bytes, payload at %d", path, len(b), start)
	}
	for _, s := range h.Segs {
		size := s.N
		if s.Storage == "u16" {
			size *= 2
		}
		if s.At%8 != 0 || s.At < 0 || start+s.At+size > len(b) {
			c.t.Fatalf("%s: segment %+v outside the %d-byte payload", path, s, len(b)-start)
		}
	}
	return h
}

// checkControlsV2 checks settings v2 where caps list the per-channel
// controls: a channel past the last, or none, is refused with why.
// TRLC-LINKS: REQ-DSO-162
func checkControlsV2(t *testing.T, c client, caps wire.Caps) {
	t.Helper()
	if !slices.ContainsFunc(caps.Controls, func(k wire.CapsControl) bool { return k.Name == "vdiv" && k.PerChannel }) {
		return
	}
	for _, body := range []string{
		`{"control":"vdiv","ch":` + jsonUint(uint64(len(caps.Channels))) + `,"value":1}`,
		`{"control":"vdiv","value":1}`,
	} {
		if out := c.json(http.MethodPost, "/api/set", body, http.StatusOK); out["ok"] != false || out["err"] == "" {
			t.Fatalf("%s: %v", body, out)
		}
	}
}
