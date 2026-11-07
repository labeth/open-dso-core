// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/webtest"
)

// The server passes its own conformance suite over the fake scope, with
// every optional part and with none.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func TestConformanceFake(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		webtest.Run(t, func() webserver.Scope { return webtest.NewFake(false) }, webserver.WithPprof())
	})
	t.Run("bare", func(t *testing.T) {
		webtest.Run(t, func() webserver.Scope { return webtest.NewFake(true) })
	})
}

// serve runs one request against h.
// TRLC-LINKS: REQ-DSO-161
func serve(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body)))
	return rec
}

// A control's reply carries ok, the value applied and why it was refused;
// one that cannot be refused carries no err.
// TRLC-LINKS: REQ-DSO-162
func TestSetReply(t *testing.T) {
	h := webserver.New(webtest.NewFake(false)).Handler()
	for body, want := range map[string]string{
		`{"control":"tdiv","value":1e-6}`:     `{"applied":0.000001,"err":"","ok":true}`,
		`{"control":"tdiv","value":100}`:      `{"applied":100,"err":"tdiv not in ladder","ok":false}`,
		`{"control":"memdepth","value":4096}`: `{"applied":4096,"ok":true}`,
	} {
		if got := strings.TrimSpace(serve(t, h, http.MethodPost, "/api/set", body).Body.String()); got != want {
			t.Errorf("%s: %s, want %s", body, got, want)
		}
	}
}

// A page's lease is renewed by its polls and given back with end=1 (204),
// the view window by view=a,b,cols and ended by view=off; a page asking
// for frames is a frame client for a while.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-168
func TestFrameLeases(t *testing.T) {
	f := webtest.NewFake(false)
	s := webserver.New(f)
	h := s.Handler()
	if s.FrameClient(time.Minute) {
		t.Fatal("a frame client before any request")
	}
	serve(t, h, http.MethodGet, "/api/frame.bin?lease=spectrum&cols=64", "")
	if f.Lease(webserver.NeedSpectrum) != 3*time.Second || !s.FrameClient(time.Minute) {
		t.Fatalf("spectrum lease %v", f.Lease(webserver.NeedSpectrum))
	}
	if rec := serve(t, h, http.MethodGet, "/api/frame.bin?lease=spectrum&end=1", ""); rec.Code != http.StatusNoContent || f.Lease(webserver.NeedSpectrum) != 0 {
		t.Fatalf("end: %d, lease %v", rec.Code, f.Lease(webserver.NeedSpectrum))
	}
	serve(t, h, http.MethodGet, "/api/frame.bin?view=0.1,0.2,1200&cols=64", "")
	if f.Lease(webserver.NeedView) != 2*time.Second {
		t.Fatalf("view lease %v", f.Lease(webserver.NeedView))
	}
	serve(t, h, http.MethodGet, "/api/frame.bin?view=off&cols=64", "")
	if f.Lease(webserver.NeedView) != 0 {
		t.Fatal("view=off kept the view")
	}
}

// A scope's own routes are served beside the API.
// TRLC-LINKS: REQ-DSO-161
func TestExtensionsServed(t *testing.T) {
	h := webserver.New(webtest.NewFake(false)).Handler()
	if rec := serve(t, h, http.MethodGet, "/api/fake/hello", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hello":"fake"`) {
		t.Fatalf("extension: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, webserver.New(webtest.NewFake(true)).Handler(), http.MethodGet, "/api/fake/hello", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("no extension: %d", rec.Code)
	}
}

// A record comes back as its header and both channels' codes; one refused
// is a 409 naming why.
// TRLC-LINKS: REQ-DSO-280
func TestRecord(t *testing.T) {
	f := webtest.NewFake(false)
	h := webserver.New(f).Handler()
	rec := serve(t, h, http.MethodGet, "/api/record.bin?sample_s=1e-6", "")
	if rec.Code != http.StatusOK || rec.Body.Len() < 4+2*2000 {
		t.Fatalf("record: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	serve(t, h, http.MethodPost, "/api/set", `{"control":"run","value":0}`)
	if rec := serve(t, h, http.MethodGet, "/api/record.bin?sample_s=1e-6", ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "stopped") {
		t.Fatalf("stopped: %d %s", rec.Code, rec.Body.String())
	}
}
