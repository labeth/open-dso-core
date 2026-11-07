// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/streamwire"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// hStreamLines serves the decoded stream's transcript as data from an
// absolute line index (?from=N&max=M, at most 8192 lines), so a client can
// follow a stream and check it for gaps (ADR-PANEL-DECODED-STREAM).
// TRLC-LINKS: REQ-DSO-018
func (s *Server) hStreamLines(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sc.Stream()
	if !ok {
		http.Error(w, "no decoded stream", http.StatusNotFound)
		return
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	n, err := strconv.Atoi(r.URL.Query().Get("max"))
	if err != nil || n <= 0 || n > 8192 {
		n = 8192
	}
	page, ok := src.StreamLines(from, n)
	if !ok {
		http.Error(w, "no decoded stream", http.StatusNotFound)
		return
	}
	WriteJSON(w, page)
}

// hStreamWindow serves the stream's newest ?rows=N (1..40) transcript lines
// as the display's list writes them: the web's BUS shows the scope's stream.
// TRLC-LINKS: REQ-DSO-018
func (s *Server) hStreamWindow(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sc.Stream()
	if !ok {
		http.Error(w, "no decoded stream", http.StatusNotFound)
		return
	}
	n, err := strconv.Atoi(r.URL.Query().Get("rows"))
	if err != nil || n <= 0 || n > streamwire.ListRows {
		n = streamwire.ListRows
	}
	page, ok := src.StreamWindow(n)
	if !ok {
		http.Error(w, "no decoded stream", http.StatusNotFound)
		return
	}
	WriteJSON(w, page)
}

// hStreamLinesBin serves the transcript from ?from=N in the compact binary
// line format, up to 1 MiB: JSON at 1.5 MBd cost a scope's one CPU more
// than the stream itself.
// TRLC-LINKS: REQ-DSO-018
func (s *Server) hStreamLinesBin(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sc.Stream()
	if !ok {
		http.Error(w, "no decoded stream", http.StatusNotFound)
		return
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	buf := make([]byte, 1<<20)
	n, ok := src.StreamLinesBinary(from, buf)
	if !ok {
		http.Error(w, "no decoded stream", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf[:n])
}

// The client decode's transport (ADR-STREAM-CLIENT-DECODE): a page's web
// worker (webdec_worker.js) long-polls /api/stream/words.bin for the digital
// stream's words as the scope's ring holds them, decodes them, and posts the
// frames to /api/stream/frames.bin, which the scope queues as its own.

// wordsBufs are a reply's chunk list and head, and a post's body, reused:
// a page at the densest stream asks a few times a second and posts as often.
var wordsBufs = sync.Pool{New: func() any { return &wordsBuf{} }}

// wordsBuf is one request's reused memory.
type wordsBuf struct {
	chunks []wire.WordChunk
	head   []byte
}

// framesBufs are posts' bodies, reused.
var framesBufs = sync.Pool{New: func() any { b := make([]byte, 0, 64<<10); return &b }}

// hStreamWords serves the digital stream's words to a page that decodes
// them: ?from=seq (the reply's next), ?max bytes, ?wait ms to park for words.
// Asking holds the client decode (the scope's lease); a reply with active
// false says the scope decodes, and why. The body after the header is the
// chunks' heads (wire.AppendWordChunkHeads), then their words as the scope
// codes them, written straight from its ring (pinned until written): no
// copy of the words on the scope but the socket's.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *Server) hStreamWords(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sc.ClientDecode()
	if !ok {
		http.Error(w, "no client decode", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	from, _ := strconv.ParseUint(q.Get("from"), 10, 64)
	maxB := 1 << 20
	if v, err := strconv.Atoi(q.Get("max")); err == nil && v > 0 {
		maxB = min(v, src.ReplyMax())
	}
	wait := 0
	if v, err := strconv.Atoi(q.Get("wait")); err == nil && v > 0 {
		wait = min(v, 1500)
	}
	wb, ok := wordsBufs.Get().(*wordsBuf)
	if !ok {
		wb = new(wordsBuf)
	}
	defer wordsBufs.Put(wb)
	cw, release := src.ReadWords(from, maxB, time.Duration(wait)*time.Millisecond, wb.chunks[:0])
	defer release()
	wb.chunks = cw.Chunks
	h := wire.WordsHeader{
		Info: cw.Info, Next: cw.Next, Dropped: cw.Dropped, Chunks: len(cw.Chunks), Lag: cw.Lag,
		ServerMS: time.Now().UnixMilli(),
	}
	hj, err := json.Marshal(h)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The header padded to whole words: the page reads the words in place.
	for len(hj)%4 != 0 {
		hj = append(hj, ' ')
	}
	head := wb.head[:0]
	head = append(head, wire.WordsBinMagic[:]...)
	head = binary.LittleEndian.AppendUint32(head, uint32(len(hj))) //nolint:gosec // G115: a JSON header of a few KB
	head = append(head, hj...)
	head = wire.AppendWordChunkHeads(head, cw.Chunks)
	wb.head = head
	size := len(head)
	for _, c := range cw.Chunks {
		size += len(c.Enc)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(size))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := w.Write(head); err != nil {
		return
	}
	for _, c := range cw.Chunks {
		if _, err := w.Write(c.Enc); err != nil {
			return
		}
	}
}

// framesBinMax bounds a /api/stream/frames.bin request.
const framesBinMax = 4 << 20

// hStreamFrames takes a page's decoded frames, read into a reused buffer:
// the scope queues them without keeping it.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *Server) hStreamFrames(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sc.ClientDecode()
	if !ok {
		http.Error(w, "no client decode", http.StatusNotFound)
		return
	}
	bp, ok := framesBufs.Get().(*[]byte)
	if !ok {
		bp = new([]byte)
	}
	defer framesBufs.Put(bp)
	body, err := ReadBody(http.MaxBytesReader(w, r.Body, framesBinMax), (*bp)[:0], r.ContentLength, framesBinMax)
	*bp = body[:0]
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	if err := src.PostFrames(body); err != nil {
		code := http.StatusConflict
		if isBadRequest(err) {
			code = http.StatusBadRequest
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
