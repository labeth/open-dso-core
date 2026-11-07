// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webserver_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/webtest"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// A four-channel 12-bit scope with caps and v2 frames passes the
// conformance suite.
// TRLC-LINKS: REQ-SDS-163, REQ-SDS-164
func TestConformanceWide(t *testing.T) {
	webtest.Run(t, func() webserver.Scope { return webtest.NewWide() })
}

// A scope with caps puts api and caps_gen first in its status and keeps
// the rest of its document; one without caps has its status as before.
// TRLC-LINKS: REQ-SDS-164
func TestStatusCapsGen(t *testing.T) {
	wide := serve(t, webserver.New(webtest.NewWide()).Handler(), http.MethodGet, "/api/status", "").Body.String()
	if !strings.HasPrefix(wide, `{"api":2,"caps_gen":1,"mask_gen":0,`) {
		t.Fatalf("status with caps: %s", wide)
	}
	plain := serve(t, webserver.New(webtest.NewFake(false)).Handler(), http.MethodGet, "/api/status", "").Body.String()
	if want := `{"mask_gen":0,"running":true,"seq":1,"tdiv_s":0.000001}` + "\n"; plain != want {
		t.Fatalf("status without caps: %q, want %q", plain, want)
	}
}

// v2 is served only on ?v=2 and only by a scope with v2 frames; every
// other request, and every request to a v1 scope, is v1.
// TRLC-LINKS: REQ-SDS-163
func TestFrameV2Negotiation(t *testing.T) {
	wide := webserver.New(webtest.NewWide()).Handler()
	if b := serve(t, wide, http.MethodGet, "/api/frame.bin?v=2", "").Body.Bytes(); string(b[:4]) != "DSF2" {
		t.Fatalf("v2 of a v2 scope: % x", b[:4])
	}
	for _, p := range []string{"/api/frame.bin", "/api/frame.bin?v=1", "/api/frame.bin?raw=1"} {
		if b := serve(t, wide, http.MethodGet, p, "").Body.Bytes(); b[0] != wire.BinMagic {
			t.Fatalf("%s of a v2 scope: % x", p, b[:4])
		}
	}
	plain := webserver.New(webtest.NewFake(false)).Handler()
	if b := serve(t, plain, http.MethodGet, "/api/frame.bin?v=2", "").Body.Bytes(); b[0] != wire.BinMagic {
		t.Fatalf("v2 of a v1 scope: % x", b[:4])
	}
}
