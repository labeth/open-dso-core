// Package webtest is the web server's conformance suite: any scope runs Run
// against its adapter (and its simulator) and so checks that the shared
// page and the phone can drive it through the one API. Fake is a scope for
// the server's own tests and a starting point for a new scope's adapter.
// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER
package webtest

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// helloHandler is the fake's own route.
type helloHandler struct{}

// TRLC-LINKS: REQ-DSO-161
func (helloHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	webserver.WriteJSON(w, map[string]any{"ok": true, "hello": "fake"})
}

// client is one server under test.
type client struct {
	t *testing.T
	h http.Handler
}

// do serves one request.
// TRLC-LINKS: REQ-DSO-161
func (c client) do(method, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, httptest.NewRequestWithContext(c.t.Context(), method, path, rd))
	return rec
}

// json serves one request and decodes its JSON object reply.
// TRLC-LINKS: REQ-DSO-162
func (c client) json(method, path, body string, wantCode int) map[string]any {
	c.t.Helper()
	rec := c.do(method, path, body)
	if rec.Code != wantCode {
		c.t.Fatalf("%s %s: status %d, want %d (%s)", method, path, rec.Code, wantCode, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		c.t.Fatalf("%s %s: content type %q", method, path, ct)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		c.t.Fatalf("%s %s: %v (%s)", method, path, err, rec.Body.String())
	}
	return out
}

// frame fetches /api/frame.bin and decodes its header, flags and payload.
// TRLC-LINKS: REQ-DSO-163
func (c client) frame(path string) (wire.FrameReply, byte, []byte) {
	c.t.Helper()
	rec := c.do(http.MethodGet, path, "")
	b := rec.Body.Bytes()
	if rec.Code != http.StatusOK || len(b) < 8 || b[0] != wire.BinMagic {
		c.t.Fatalf("%s: status %d, %d bytes", path, rec.Code, len(b))
	}
	h := int(binary.LittleEndian.Uint32(b[4:8]))
	if 8+h > len(b) {
		c.t.Fatalf("%s: header of %d bytes overruns %d", path, h, len(b))
	}
	var rep wire.FrameReply
	if err := json.Unmarshal(b[8:8+h], &rep); err != nil {
		c.t.Fatalf("%s: header: %v", path, err)
	}
	return rep, b[1], b[8+h:]
}

// Run checks the server over the scope newScope makes (a fresh one each
// check) against the API's contract: the pages, the claim, the status, the
// settings, the frames, and every optional part answering as present or as
// absent. opts are the server's options as the scope's app passes them.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func Run(t *testing.T, newScope func() webserver.Scope, opts ...webserver.Option) {
	t.Helper()
	open := func(t *testing.T) (client, webserver.Scope) {
		t.Helper()
		sc := newScope()
		return client{t: t, h: webserver.New(sc, opts...).Handler()}, sc
	}
	for _, check := range []struct {
		name string
		run  func(*testing.T, client, webserver.Scope)
	}{
		{"pages", checkPages},
		{"claim", checkClaim},
		{"status", checkStatus},
		{"set", checkSet},
		{"frames", checkFrames},
		{"ui", checkUI},
		{"refs", checkRefs},
		{"stream", checkStream},
		{"record", checkRecord},
		{"parts", checkParts},
		{"caps", checkCaps},
		{"extensions", checkExtensions},
	} {
		t.Run(check.name, func(t *testing.T) {
			c, sc := open(t)
			check.run(t, c, sc)
		})
	}
}

// checkPages checks the pages, the short remote link, an asset with its ETag,
// and that pages are not served by file name.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkPages(t *testing.T, c client, _ webserver.Scope) {
	t.Helper()
	for _, p := range []string{"/", "/remote"} {
		rec := c.do(http.MethodGet, p, "")
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("Content-Security-Policy") != webserver.PageCSP {
			t.Fatalf("%s: %d %q", p, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if rec := c.do(http.MethodGet, "/r", ""); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/remote" {
		t.Fatalf("/r: %d", rec.Code)
	}
	if rec := c.do(http.MethodGet, "/app_core.js", ""); rec.Code != http.StatusOK || rec.Header().Get("ETag") == "" {
		t.Fatalf("an asset: %d", rec.Code)
	}
	if rec := c.do(http.MethodGet, "/ui.html", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("a page by name: %d", rec.Code)
	}
}

// checkClaim checks that a superseded page's frame requests conflict and the
// current epoch's are served.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkClaim(t *testing.T, c client, sc webserver.Scope) {
	t.Helper()
	out := c.json(http.MethodGet, "/api/claim", "", http.StatusOK)
	epoch, _ := out["epoch"].(float64)
	if rec := c.do(http.MethodGet, fmt.Sprintf("/api/frame.bin?epoch=%d&cols=64", uint64(epoch)-1), ""); rec.Code != http.StatusConflict {
		t.Fatalf("a superseded page: %d", rec.Code)
	}
	if _, only := sc.Frames().(webserver.V2Only); only {
		c.frame2(fmt.Sprintf("/api/frame.bin?epoch=%d&cols=64&v=2", uint64(epoch)))
		return
	}
	c.frame(fmt.Sprintf("/api/frame.bin?epoch=%d&cols=64", uint64(epoch)))
}

// checkStatus checks that the status answers.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkStatus(t *testing.T, c client, _ webserver.Scope) {
	t.Helper()
	c.json(http.MethodGet, "/api/status", "", http.StatusOK)
}

// checkSet checks /api/set's refusals: GET, bad JSON, an infinite value and
// an unknown control.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkSet(t *testing.T, c client, _ webserver.Scope) {
	t.Helper()
	if rec := c.do(http.MethodGet, "/api/set", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
	if out := c.json(http.MethodPost, "/api/set", "{", http.StatusOK); out["ok"] != false || out["err"] != "bad json" {
		t.Fatalf("bad json: %v", out)
	}
	if out := c.json(http.MethodPost, "/api/set", `{"control":"tdiv","value":1e999}`, http.StatusOK); out["ok"] != false {
		t.Fatalf("an infinite value: %v", out)
	}
	if out := c.json(http.MethodPost, "/api/set", `{"control":"no-such-control","value":1}`, http.StatusOK); out["ok"] != false || out["err"] == "" {
		t.Fatalf("an unknown control: %v", out)
	}
}

// checkFrames checks v1 frames: a native frame's payload size, a held frame
// unchanged, raw frames, and clamped column counts; a scope v1 cannot carry
// serves those requests in v2.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkFrames(t *testing.T, c client, sc webserver.Scope) {
	t.Helper()
	if _, only := sc.Frames().(webserver.V2Only); only {
		for _, p := range []string{"/api/frame.bin?cols=100", "/api/frame.bin?raw=1"} {
			c.frame2(p) // served in v2, which its magic says
		}
		return
	}
	rep, flags, pay := c.frame("/api/frame.bin?cols=100")
	if rep.Seq == 0 || flags&wire.FlagUnchanged != 0 {
		t.Skip("the scope has published no frame")
	}
	if flags&(wire.FlagEnv|wire.FlagDeep|wire.FlagEmpty|wire.FlagPrecision) == 0 && len(pay) != 2*(rep.Cols-rep.Head-rep.Tail) {
		t.Fatalf("a native frame of %d columns carries %d bytes", rep.Cols, len(pay))
	}
	// a running scope may have published a newer frame since: that one, or
	// the frame held as unchanged
	if again, flags, pay := c.frame(fmt.Sprintf("/api/frame.bin?cols=100&since=%d", rep.Seq)); again.Seq <= rep.Seq && (!again.Unchanged || flags&wire.FlagUnchanged == 0 || len(pay) != 0) {
		t.Fatalf("the frame held: %+v flags %#x", again, flags)
	}
	if _, flags, _ := c.frame("/api/frame.bin?raw=1"); flags&wire.FlagRaw == 0 {
		t.Fatalf("raw: flags %#x", flags)
	}
	for _, cols := range []string{"1", "99999", "x"} {
		if r, _, _ := c.frame("/api/frame.bin?cols=" + cols); r.Cols != 0 && (r.Cols < 64 || r.Cols > 4096) && r.Depth == 0 {
			t.Fatalf("cols=%s: %d columns", cols, r.Cols)
		}
	}
}

// checkUI checks the panel and the remote sheets, present or absent.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkUI(t *testing.T, c client, sc webserver.Scope) {
	t.Helper()
	ui, ok := sc.UI()
	if !ok {
		if out := c.json(http.MethodPost, "/api/panel", `{"button":"F1"}`, http.StatusOK); out["ok"] != false || out["err"] != "no panel" {
			t.Fatalf("no panel: %v", out)
		}
		for _, p := range []string{"/api/sheets", "/api/panel/wait"} {
			if rec := c.do(http.MethodGet, p, ""); rec.Code != http.StatusNotFound {
				t.Fatalf("%s without a UI: %d", p, rec.Code)
			}
		}
		return
	}
	c.json(http.MethodPost, "/api/panel", `{"button":"F1"}`, http.StatusOK)
	if _, remote := ui.(webserver.UIRemote); !remote {
		if rec := c.do(http.MethodGet, "/api/sheets", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("sheets without the remote surface: %d", rec.Code)
		}
		return
	}
	sheets := c.json(http.MethodGet, "/api/sheets", "", http.StatusOK)
	if _, ok := sheets["Tabs"].([]any); !ok {
		t.Fatalf("sheets: %v", sheets)
	}
	c.json(http.MethodGet, "/api/panel/wait?timeout_ms=1", "", http.StatusOK)
	if out := c.json(http.MethodPost, "/api/sheets/set", `{"value":1}`, http.StatusOK); out["ok"] == true {
		t.Fatalf("a set without a row: %v", out)
	}
}

// checkRefs checks the references, present or absent.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkRefs(t *testing.T, c client, sc webserver.Scope) {
	t.Helper()
	out := c.json(http.MethodGet, "/api/refs", "", http.StatusOK)
	if _, ok := sc.Refs(); !ok {
		if out["ok"] != false {
			t.Fatalf("no references: %v", out)
		}
		return
	}
	if refs, _ := out["refs"].([]any); out["ok"] != true || len(refs) != 2 {
		t.Fatalf("references: %v", out)
	}
}

// checkStream checks that a scope without a stream serves no stream lines.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkStream(t *testing.T, c client, sc webserver.Scope) {
	t.Helper()
	if _, ok := sc.Stream(); ok {
		return // a stream's lines exist only while it runs
	}
	for _, p := range []string{"/api/stream/lines", "/api/stream/lines.bin", "/api/stream/window"} {
		if rec := c.do(http.MethodGet, p, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s without a stream: %d", p, rec.Code)
		}
	}
}

// checkRecord checks record requests without an interval or without records.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkRecord(t *testing.T, c client, sc webserver.Scope) {
	t.Helper()
	if rec := c.do(http.MethodGet, "/api/record.bin", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("no interval: %d", rec.Code)
	}
	if _, ok := sc.Records(); !ok {
		if out := c.json(http.MethodGet, "/api/record.bin?sample_s=1e-6&wait_ms=1", "", http.StatusConflict); out["err"] != webserver.RecordNever {
			t.Fatalf("no records: %v", out)
		}
	}
}

// checkParts checks the optional parts (zones, masks, Bode, SOUND, client
// decode, setups, the screen), each answering as present or as absent.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkParts(t *testing.T, c client, sc webserver.Scope) {
	t.Helper()
	if _, ok := sc.Triggers(); ok {
		if out := c.json(http.MethodPost, "/api/zones", `[]`, http.StatusOK); out["ok"] != true {
			t.Fatalf("zones: %v", out)
		}
	}
	if _, ok := sc.Masks(); ok {
		if out := c.json(http.MethodPost, "/api/mask", `{"lo":[]}`, http.StatusOK); out["ok"] != true || out["cleared"] != true {
			t.Fatalf("mask clear: %v", out)
		}
		c.json(http.MethodGet, "/api/maskfail?i=999", "", http.StatusOK)
	}
	if _, ok := sc.Bode(); ok {
		if out := c.json(http.MethodGet, "/api/bode", "", http.StatusOK); out["ok"] != true {
			t.Fatalf("bode: %v", out)
		}
	}
	if _, ok := sc.SoundControls(); !ok {
		if rec := c.do(http.MethodGet, "/api/sound", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("sound without SOUND: %d", rec.Code)
		}
	} else {
		c.json(http.MethodGet, "/api/sound?lease=0", "", http.StatusOK)
	}
	if _, ok := sc.Sound(); !ok {
		if rec := c.do(http.MethodGet, "/api/sound.bin", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("sound.bin without a stream: %d", rec.Code)
		}
	} else if rec := c.do(http.MethodGet, "/api/sound.bin?max=4", ""); rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), wire.SoundBinMagic[:]) {
		t.Fatalf("sound.bin: %d", rec.Code)
	}
	if _, ok := sc.ClientDecode(); !ok {
		if rec := c.do(http.MethodGet, "/api/stream/words.bin", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("words without client decode: %d", rec.Code)
		}
	}
	_, haveSetups := sc.Setups()
	if out := c.json(http.MethodGet, "/api/setup", "", http.StatusOK); out["ok"] != haveSetups {
		t.Fatalf("setups: %v", out)
	}
	if out := c.json(http.MethodPost, "/api/setup", `{"op":"save","slot":0}`, http.StatusOK); out["ok"] != false {
		t.Fatalf("slot 0: %v", out)
	}
	_, haveScreen := sc.Screen()
	if rec := c.do(http.MethodGet, "/api/screen.png", ""); (rec.Code == http.StatusOK) != haveScreen {
		t.Fatalf("screen: %d", rec.Code)
	}
}

// checkExtensions checks that every extension route has its pattern, feature
// and handler, under /api/.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func checkExtensions(t *testing.T, _ client, sc webserver.Scope) {
	t.Helper()
	for _, r := range sc.Extensions() {
		if r.Pattern == "" || r.Feature == "" || r.Handler == nil {
			t.Fatalf("an extension route without its pattern, feature or handler: %+v", r)
		}
		if !strings.Contains(r.Pattern, "/api/") {
			t.Fatalf("an extension outside /api: %s", r.Pattern)
		}
	}
}
