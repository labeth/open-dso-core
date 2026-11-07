// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/labeth/open-dso-core/webserver/wire"
)

// APIVersion is the API's version as /api/caps and /api/status name it.
const APIVersion = 2

// CapsSource is optional on a Scope: its capabilities document. A scope
// without it serves no /api/caps (404) and its status carries no caps_gen,
// so the page keeps the two-channel 8-bit constants it was written for.
type CapsSource interface {
	// Caps is the document now. The server fills API and the frame wire
	// versions it speaks.
	Caps() wire.Caps
	// CapsGen is Caps().Gen, cheap enough for every status poll.
	CapsGen() uint64
}

// FramesV2 is optional on Frames: frames in wire v2 (/api/frame.bin?v=2).
// Without it a v2 request is served v1, which the page tells apart by the
// magic.
type FramesV2 interface {
	// FrameV2 fills dst (reset, its storage reused) for q under the scope's
	// frame lock: an unchanged header when no frame after q.Since published.
	FrameV2(q FrameQueryV2, dst *wire.Frame)
}

// FrameQueryV2 is a v2 frame request.
type FrameQueryV2 struct {
	FrameQuery
	Raw bool // the raw shape: the un-windowed, pre-coupling record
}

// frames2 lends the server's v2 frames, reused across requests.
var frames2 = sync.Pool{New: func() any { return new(wire.Frame) }}

// encodeV2 builds and encodes one v2 frame.
// TRLC-LINKS: REQ-SDS-163
func encodeV2(fr FramesV2, q FrameQueryV2) []byte {
	f, _ := frames2.Get().(*wire.Frame)
	defer frames2.Put(f)
	f.Reset()
	fr.FrameV2(q, f)
	buf, err := wire.EncodeFrame2(f)
	if err != nil { // a scope's inconsistent frame: served as no news, never as misread words
		f.Reset()
		f.Header = wire.FrameHeader{Seq: q.Since, Unchanged: true, EdgeX: -1, TriggerAt: -1}
		buf, _ = wire.EncodeFrame2(f)
	}
	return buf
}

// wantV2 reports a request for frame wire v2 that the scope can serve.
// TRLC-LINKS: REQ-SDS-163
func wantV2(fr Frames, r *http.Request) (FramesV2, bool) {
	if r.URL.Query().Get("v") != "2" {
		return nil, false
	}
	v2, ok := fr.(FramesV2)
	return v2, ok
}

// hCaps serves the capabilities document.
// TRLC-LINKS: REQ-SDS-164
func (s *Server) hCaps(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.sc.(CapsSource)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c := cs.Caps()
	c.API = APIVersion
	c.Wire.Frame = []int{1}
	if _, ok := s.sc.Frames().(FramesV2); ok {
		c.Wire.Frame = append(c.Wire.Frame, 2)
	}
	if len(c.Wire.Record) == 0 {
		c.Wire.Record = []int{1}
	}
	if len(c.Wire.Sound) == 0 {
		c.Wire.Sound = []int{1}
	}
	slices.Sort(c.Wire.Frame)
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, c)
}

// writeStatus writes the status document, with "api" and "caps_gen" first
// when the scope serves caps (its own document must not carry those keys):
// the page re-reads /api/caps when caps_gen moves. A scope without caps gets
// its document encoded exactly as it is.
// TRLC-LINKS: REQ-SDS-164
func (s *Server) writeStatus(w http.ResponseWriter, st any) {
	cs, ok := s.sc.(CapsSource)
	if !ok {
		WriteJSON(w, st)
		return
	}
	b, err := json.Marshal(st)
	if err != nil || len(b) < 2 || b[0] != '{' {
		WriteJSON(w, st)
		return
	}
	var out bytes.Buffer
	out.Grow(len(b) + 40)
	out.WriteString(`{"api":`)
	out.WriteString(strconv.Itoa(APIVersion))
	out.WriteString(`,"caps_gen":`)
	out.WriteString(strconv.FormatUint(cs.CapsGen(), 10))
	if len(b) > 2 {
		out.WriteByte(',')
	}
	out.Write(b[1:])
	out.WriteByte('\n')
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out.Bytes())
}
