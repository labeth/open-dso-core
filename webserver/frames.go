// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labeth/open-dso-core/webserver/wire"
)

// screenCols is the frame's column count when a page names none.
const screenCols = 800

// FrameV2OnlyText is why a v1 frame request is refused on a scope whose
// frames v1 cannot carry.
const FrameV2OnlyText = "this scope serves frame wire v2 only"

// V2Only is optional on Frames (with FramesV2): frames that wire v1 cannot
// carry (more than two channels, words wider than 8 bits). A v1 request is
// answered 409 with FrameV2OnlyText, and caps list frame wire 2 only.
type V2Only interface {
	V2Only()
}

// hFrameBin is the long-poll binary frame endpoint: a small JSON header and
// the raw code payload (wire.EncodeBinFrame) — about 1 ms on a scope's ARM
// against 50-150 ms of reflective JSON over int16 arrays, which capped the
// browser at a few fps. With since= and waitms= the request parks (no locks
// held) until the scope publishes a newer frame, so delivery latency is one
// response write and the client needs no poll timer: request-when-ready IS
// the backpressure, and a slow client simply skips to the newest frame.
// TRLC-LINKS: REQ-DSO-163
func (s *Server) hFrameBin(w http.ResponseWriter, r *http.Request) {
	if s.claims.Superseded(r) {
		w.WriteHeader(http.StatusConflict) // 409 — a newer browser claimed the device
		return
	}
	s.frameAt.Store(time.Now().UnixNano())
	fr := s.sc.Frames()
	// A consumer that needs true samples renews a lease before waiting (a
	// closed analyzer lets it expire): lease=raw for full-rate samples (eye,
	// super-res), lease=spectrum for the frames a spectrum is taken of (FFT,
	// spectrogram: the display's SPECTRUM asks for the same), lease=samples for
	// evenly sampled ones, lease=search for the whole records BUS's search
	// reads at slow timebases (no roll). Live frames are otherwise time-ordered envelopes,
	// fast whatever is enabled.
	switch kind := r.URL.Query().Get("lease"); kind {
	case NeedRaw, NeedSpectrum, NeedSamples, NeedSearch:
		if r.URL.Query().Get("end") == "1" { // a consumer done with it: the live view back at once
			if fr.Lease(Need{Kind: kind}) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		} else {
			fr.Lease(Need{Kind: kind, Hold: 3 * time.Second})
		}
	}
	// The page zoomed into a live envelope asks for its stretch at full detail
	// (view=a,b,cols: screen widths from the trigger and the canvas columns,
	// renewed with every poll); view=off ends it.
	if v := r.URL.Query().Get("view"); v != "" {
		var a, b float64
		var cols int
		if n, _ := fmt.Sscanf(v, "%g,%g,%d", &a, &b, &cols); n == 3 {
			fr.Lease(Need{Kind: NeedView, Hold: 2 * time.Second, View: ViewWindow{A: a, B: b, Cols: cols}})
		} else {
			fr.Lease(Need{Kind: NeedView})
		}
	}
	if _, only := fr.(V2Only); only {
		if _, v2 := wantV2(fr, r); !v2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"err":"` + FrameV2OnlyText + `"}` + "\n"))
			return
		}
	}
	var since uint64
	// An absent or bad number leaves its default.
	_, _ = fmt.Sscanf(r.URL.Query().Get("since"), "%d", &since)
	cols := screenCols
	if c := r.URL.Query().Get("cols"); c != "" {
		_, _ = fmt.Sscanf(c, "%d", &cols)
	}
	if cols < 64 {
		cols = 64
	}
	if cols > 4096 {
		cols = 4096
	}
	full := r.URL.Query().Get("full") == "1"
	waitms := 0
	_, _ = fmt.Sscanf(r.URL.Query().Get("waitms"), "%d", &waitms)
	if waitms < 0 {
		waitms = 0
	}
	if waitms > 2000 {
		waitms = 2000
	}
	// Park even for since=0: the wait returns immediately once ANY frame has
	// published, and blocks when none has — otherwise a fresh page against an
	// idle scope hot-loops instant unchanged replies at the client's tick.
	if waitms > 0 {
		fr.Wait(since, time.Duration(waitms)*time.Millisecond)
	}
	var buf []byte
	// Serialize under the quiet READ-lock so this CPU burst pauses during the
	// scope's load-sensitive windows (an arm-settle and drain it would
	// otherwise corrupt on a single core). Held only for the ~ms of
	// serialization, not the network write.
	func() {
		if q, ok := fr.(Quiet); ok {
			q.QuietRLock()
			defer q.QuietRUnlock()
		}
		raw := r.URL.Query().Get("raw") == "1"
		if v2, ok := wantV2(fr, r); ok {
			buf = encodeV2(v2, FrameQueryV2{FrameQuery: FrameQuery{Since: since, Cols: cols, Full: full}, Raw: raw})
			return
		}
		if raw {
			buf = wire.EncodeRawFrame(fr.RawFrame(since))
			return
		}
		buf = wire.EncodeBinFrame(fr.Frame(FrameQuery{Since: since, Cols: cols, Full: full})) // encode inside the quiet lock; network write is outside
	}()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	WriteWithin(w, buf, 5*time.Second)
}
