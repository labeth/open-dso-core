// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"net/http"
	"net/http/pprof"
	"strings"
	"sync/atomic"
	"time"
)

// Server serves the page, its assets and the API over a Scope. A reply is
// built under the scope's locks and written after they are released (never
// a lock held over I/O).
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
type Server struct {
	sc Scope

	// claims is the single-active-client epoch (/api/claim): a scope serves
	// one live page, bounding the CPU that competes with its acquisition.
	claims *Epoch

	// recordBusy is a /api/record.bin under way: one at a time, as each holds
	// a whole record (two channels of up to half a million samples).
	recordBusy atomic.Bool

	// frameAt is when a page last asked for a frame (/api/frame.bin, unix
	// ns): a page showing the scope keeps that long-poll open (FrameClient).
	frameAt atomic.Int64

	pprof bool
}

// Option configures a Server.
type Option func(*Server)

// WithPprof serves the runtime profiles at /debug/pprof/ (a lab device's
// live CPU and heap profiling).
// TRLC-LINKS: REQ-DSO-161
func WithPprof() Option { return func(s *Server) { s.pprof = true } }

// New is the web server over sc.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func New(sc Scope, opts ...Option) *Server {
	s := &Server{sc: sc, claims: NewEpoch()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Epoch is the server's single-client epoch.
// TRLC-LINKS: REQ-DSO-161
func (s *Server) Epoch() *Epoch { return s.claims }

// FrameClient reports whether a page asked for a frame within the last d:
// a page showing the scope keeps /api/frame.bin's long-poll open (it waits
// up to 1.5 s a poll), and one that closed, or hid, stops asking.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-168
func (s *Server) FrameClient(d time.Duration) bool {
	at := s.frameAt.Load()
	return at != 0 && time.Since(time.Unix(0, at)) < d
}

// Handler routes the page, its assets, the API and the scope's own routes.
// TRLC-LINKS: REQ-DSO-161
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.sc.Extensions() {
		mux.Handle(r.Pattern, r.Handler)
	}
	mux.HandleFunc("/", s.hRoot)
	mux.HandleFunc("GET /remote", s.hRemote)
	mux.HandleFunc("GET /r", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/remote", http.StatusFound) })
	mux.HandleFunc("GET /remote/", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/remote", http.StatusFound) })
	mux.HandleFunc("/api/status", s.hStatus)
	mux.HandleFunc("/api/caps", s.hCaps)
	mux.HandleFunc("/api/frame.bin", s.hFrameBin)
	mux.HandleFunc("/api/set", s.hSet)
	mux.HandleFunc("/api/panel", s.hPanel)
	mux.HandleFunc("/api/refs", s.hRefs)
	mux.HandleFunc("GET /api/panel/wait", s.hPanelWait)
	mux.HandleFunc("GET /api/sheets", s.hSheets)
	mux.HandleFunc("POST /api/sheets/set", s.hSheetsSet)
	mux.HandleFunc("POST /api/stream/page", s.hStreamPage)
	mux.HandleFunc("POST /api/sheets/action", s.hSheetsAction)
	mux.HandleFunc("GET /api/stream/lines", s.hStreamLines)
	mux.HandleFunc("GET /api/stream/lines.bin", s.hStreamLinesBin)
	mux.HandleFunc("GET /api/stream/window", s.hStreamWindow)
	mux.HandleFunc("GET /api/stream/words.bin", s.hStreamWords)
	mux.HandleFunc("POST /api/stream/frames.bin", s.hStreamFrames)
	mux.HandleFunc("GET /api/record.bin", s.hRecord)
	mux.HandleFunc("/api/zones", s.hZones)
	mux.HandleFunc("/api/serial", s.hSerial)
	mux.HandleFunc("GET /api/serial/protocols", s.hSerialProtocols)
	mux.HandleFunc("/api/mask", s.hMask)
	mux.HandleFunc("/api/maskfail", s.hMaskFail)
	mux.HandleFunc("/api/bode", s.hBode)
	mux.HandleFunc("GET /api/sound.bin", s.hSoundBin)
	mux.HandleFunc("GET /api/sound/take.bin", s.hSoundTakeBin)
	mux.HandleFunc("/api/sound", s.hSound)
	mux.HandleFunc("/api/screen.png", s.hScreen)
	mux.HandleFunc("/api/claim", s.claims.ServeClaim)
	mux.HandleFunc("/api/setup", s.hSetup)
	if s.pprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	}
	// "/" catches the page plus every embedded .js/.css (served in hRoot).
	return mux
}

// hStatus serves the scope's status document: everything the page polls
// once a second.
// TRLC-LINKS: REQ-DSO-164
func (s *Server) hStatus(w http.ResponseWriter, _ *http.Request) {
	s.writeStatus(w, s.sc.Status())
}

// hScreen returns a PNG of the device screen.
// TRLC-LINKS: REQ-DSO-161
func (s *Server) hScreen(w http.ResponseWriter, _ *http.Request) {
	sc, ok := s.sc.Screen()
	if !ok {
		http.Error(w, "no screen", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(sc.PNG())
}

// hRoot serves the page at "/" (with the strict CSP) and every embedded
// asset by bare filename (.js/.css, the remote's manifest, icons and
// videos). It is the ServeMux catch-all; the /api/* routes are registered
// explicitly and never reach here.
// TRLC-LINKS: REQ-DSO-161
func (s *Server) hRoot(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		w.Header().Set("Content-Security-Policy", PageCSP)
		ServeAsset(w, r, "ui.html")
		return
	}
	if StaticName(p) && ServeAsset(w, r, p) {
		return
	}
	http.NotFound(w, r)
}

// hRemote serves the phone remote (/remote; /r redirects here): its own
// small app on the sheet model API, with the same strict CSP as the page.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-220
func (s *Server) hRemote(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", PageCSP)
	ServeAsset(w, r, "remote.html")
}
