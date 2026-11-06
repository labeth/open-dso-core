// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
)

// PageCSP is the strict same-origin CSP of the pages. Script and connect are
// 'self' only (no inline script — every module is an external same-origin
// classic script). style keeps 'unsafe-inline' for the display:none hooks.
// img allows data: for the canvas PNG export. Media and the manifest fall
// back to 'self'.
const PageCSP = "default-src 'self'; script-src 'self'; connect-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"object-src 'none'; base-uri 'none'"

// WriteBadJSON answers a request whose body is not the JSON it takes.
// TRLC-LINKS: REQ-SDS-162
func WriteBadJSON(w http.ResponseWriter) {
	WriteJSON(w, map[string]any{"ok": false, "err": "bad json"})
}

// WriteJSON writes v as the JSON reply.
// TRLC-LINKS: REQ-SDS-161, REQ-SDS-162, REQ-SDS-163, REQ-SDS-164
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v) // a client that went away has no one to report to
}

// WriteWithin writes b with a write deadline d away, cleared afterwards.
// The server sets no global timeouts (the long-polls depend on that), so a
// half-open peer must not hold the goroutine for minutes; with
// WriteTimeout=0 net/http never resets a deadline, and the keep-alive
// connection is reused by handlers that set none (an absolute deadline left
// armed would fail them later). A writer without deadlines (a test
// recorder) writes unbounded, and a failed write has no one to report to.
// TRLC-LINKS: REQ-SDS-163
func WriteWithin(w http.ResponseWriter, b []byte, d time.Duration) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(d))
	_, _ = w.Write(b)
	_ = rc.SetWriteDeadline(time.Time{})
}

// DecodeSmall decodes the first JSON value of a body of at most 4 KB, as
// the stream decoder did, read whole into a pooled buffer: a body that is one
// value (all of them, in practice) is unmarshalled without a decoder and its
// buffers; one with more after it still decodes its first value.
// TRLC-LINKS: REQ-SDS-162
func DecodeSmall(body io.Reader, v any) error {
	buf, ok := smallBodies.Get().(*[4 << 10]byte)
	if !ok {
		buf = new([4 << 10]byte)
	}
	defer smallBodies.Put(buf)
	n, err := io.ReadFull(body, buf[:])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return err
	}
	if json.Unmarshal(buf[:n], v) == nil {
		return nil
	}
	return json.NewDecoder(bytes.NewReader(buf[:n])).Decode(v)
}

// smallBodies lends the 4 KB request buffers.
var smallBodies = sync.Pool{New: func() any { return new([4 << 10]byte) }}

// ReadBody reads r to its end into dst (grown once to n when the length is
// known and at most limit).
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-240
func ReadBody(r io.Reader, dst []byte, n, limit int64) ([]byte, error) {
	if n > 0 && n <= limit {
		dst = slices.Grow(dst, int(n)+1)
	}
	for {
		if len(dst) == cap(dst) {
			dst = slices.Grow(dst, 64<<10)
		}
		k, err := r.Read(dst[len(dst):cap(dst)])
		dst = dst[:len(dst)+k]
		if err == io.EOF {
			return dst, nil
		}
		if err != nil {
			return dst, err
		}
	}
}
