// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webserver

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// assetGet serves one GET of an embedded file by its bare name.
// TRLC-LINKS: REQ-SDS-161
func assetGet(t *testing.T, path string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	name := strings.TrimPrefix(req.URL.Path, "/")
	if !ServeAsset(rec, req, name) {
		http.NotFound(rec, req)
	}
	return rec
}

// Every embedded file carries an ETag hashed once at start-up and answers
// If-None-Match with 304 and no body.
// TRLC-LINKS: REQ-SDS-161, REQ-SDS-220
func TestAssetsRevalidate(t *testing.T) {
	files, _ := Assets.ReadDir(".")
	var paths []string
	for _, f := range files {
		if StaticName(f.Name()) {
			paths = append(paths, "/"+f.Name())
		}
	}
	if len(paths) < 40 {
		t.Fatalf("only %d files", len(paths))
	}
	seen := map[string]string{}
	for _, p := range append(paths, "/ui.html", "/remote.html") {
		r := assetGet(t, p)
		tag := r.Header().Get("ETag")
		if r.Code != 200 || !regexp.MustCompile(`^"[0-9a-f]{16}"$`).MatchString(tag) || r.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: %d etag %q cache %q", p, r.Code, tag, r.Header().Get("Cache-Control"))
		}
		if other, ok := seen[tag]; ok && other != p {
			t.Fatalf("%s and %s share the tag %s", p, other, tag)
		}
		seen[tag] = p
		if again := assetGet(t, p, "If-None-Match", tag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Fatalf("%s revalidated: %d, %d bytes", p, again.Code, again.Body.Len())
		}
	}
	if StaticName("ui.html") || StaticName("a/b.js") || StaticName("x.go") || !StaticName("app_core.js") {
		t.Fatal("static names: pages, paths and other kinds are not served by name")
	}
}

// A page names each script and stylesheet with its current tag, a request
// carrying that tag is cached for good, and text goes gzip-encoded to a
// browser that takes it.
// TRLC-LINKS: REQ-SDS-161
func TestAssetsVersionedAndCompressed(t *testing.T) {
	page := assetGet(t, "/ui.html").Body.String()
	want := `src="app_core.js?v=` + strings.Trim(AssetTag("app_core.js"), `"`) + `"`
	if !strings.Contains(page, want) {
		t.Fatalf("the page lacks %s", want)
	}
	if r := assetGet(t, "/app_core.js?v="+strings.Trim(AssetTag("app_core.js"), `"`)); r.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("a tagged request: cache %q", r.Header().Get("Cache-Control"))
	}
	raw, _ := Assets.ReadFile("app_core.js")
	r := assetGet(t, "/app_core.js", "Accept-Encoding", "gzip, deflate")
	if r.Header().Get("Content-Encoding") != "gzip" || r.Body.Len() >= len(raw)/2 || r.Header().Get("ETag") == AssetTag("app_core.js") {
		t.Fatalf("gzip: encoding %q, %d of %d bytes", r.Header().Get("Content-Encoding"), r.Body.Len(), len(raw))
	}
	zr, err := gzip.NewReader(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(zr); !bytes.Equal(got, raw) {
		t.Fatal("the gzip body is not the file")
	}
	if v := assetGet(t, "/remote-awake.mp4", "Accept-Encoding", "gzip"); v.Header().Get("Content-Encoding") != "" {
		t.Fatal("a video was gzip-encoded")
	}
	if b, _ := Assets.ReadFile("tokens.css"); len(b) == 0 {
		t.Fatal("the design system's stylesheets are not served")
	}
}

// Until a page claims, no epoch is refused (a page from before a restart
// keeps running); once one claims, an older epoch and one this run never
// issued are refused, the claimed one and unclaimed clients are not.
// TRLC-LINKS: REQ-SDS-161
func TestClaimSupersedesEarlierRun(t *testing.T) {
	e := NewEpoch()
	req := func(n uint64) *http.Request {
		return httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/frame.bin?epoch=%d", n), nil)
	}
	if e.Superseded(req(3)) {
		t.Fatal("before any claim in this run, a page from before the restart was refused")
	}
	rr := httptest.NewRecorder()
	e.ServeClaim(rr, req(0))
	var c struct{ Epoch uint64 }
	if err := json.Unmarshal(rr.Body.Bytes(), &c); err != nil || c.Epoch <= 3 || c.Epoch != e.Current() {
		t.Fatalf("claim: %v %s", err, rr.Body.String())
	}
	for _, n := range []uint64{3, c.Epoch - 1, c.Epoch + 5} {
		if !e.Superseded(req(n)) {
			t.Errorf("epoch %d not refused after claim %d", n, c.Epoch)
		}
	}
	if e.Superseded(req(c.Epoch)) || e.Superseded(req(0)) {
		t.Error("the claimed page or an unclaimed client was refused")
	}
}

// DecodeSmall reads a body's first value, also with more after it, and
// refuses what is not JSON.
// TRLC-LINKS: REQ-SDS-162
func TestDecodeSmall(t *testing.T) {
	var v struct{ A int }
	if err := DecodeSmall(strings.NewReader(`{"A":3} {"A":4}`), &v); err != nil || v.A != 3 {
		t.Fatalf("first value: %v %d", err, v.A)
	}
	if err := DecodeSmall(strings.NewReader(`{`), &v); err == nil {
		t.Fatal("a broken body decoded")
	}
}
