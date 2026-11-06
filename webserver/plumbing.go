// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package webserver

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"
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

// LEBytes is the memory of w: its words little-endian on these CPUs.
// TRLC-LINKS: REQ-SDS-163
func LEBytes(w []uint16) []byte {
	if len(w) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&w[0])), 2*len(w)) //nolint:gosec // G103: the words' own memory, little-endian on this CPU
}

// finiteSeries reports a slice or array of finite floats, or of structs of
// finite floats, ints, bools and strings without JSON tag options (their
// encoding/json form is JSONSafe's).
// TRLC-LINKS: REQ-SDS-274
func finiteSeries(v reflect.Value) bool {
	et := v.Type().Elem()
	switch et.Kind() { //nolint:exhaustive // every other kind is not a series: false
	case reflect.Float64, reflect.Float32:
		for i := range v.Len() {
			if f := v.Index(i).Float(); math.IsNaN(f) || math.IsInf(f, 0) {
				return false
			}
		}
		return true
	case reflect.Struct:
		var floats []int
		for i := range et.NumField() {
			f := et.Field(i)
			if !f.IsExported() || strings.Contains(f.Tag.Get("json"), ",") || f.Anonymous {
				return false
			}
			switch f.Type.Kind() { //nolint:exhaustive // the kinds listed are the safe ones; default refuses the rest
			case reflect.Float64, reflect.Float32:
				floats = append(floats, i)
			case reflect.Int, reflect.Int64, reflect.Int32, reflect.Uint64, reflect.Uint32, reflect.Bool, reflect.String:
			default:
				return false
			}
		}
		for i := range v.Len() {
			e := v.Index(i)
			for _, k := range floats {
				if f := e.Field(k).Float(); math.IsNaN(f) || math.IsInf(f, 0) {
					return false
				}
			}
		}
		return true
	}
	return false
}

// JSONSafe turns v into plain JSON values with every NaN and infinity as
// null (encoding/json refuses them): structs by their JSON names.
// TRLC-LINKS: REQ-SDS-274
func JSONSafe(v reflect.Value) any {
	switch v.Kind() { //nolint:exhaustive // the kinds that can hold a NaN; every other value encodes as it is
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f
	case reflect.Struct:
		if t, ok := reflect.TypeAssert[time.Time](v); ok {
			return t
		}
		m := map[string]any{}
		ty := v.Type()
		for i := range ty.NumField() {
			f := ty.Field(i)
			if !f.IsExported() {
				continue
			}
			name := f.Name
			if tag := f.Tag.Get("json"); tag != "" {
				if tag == "-" {
					continue
				}
				if n, _, _ := strings.Cut(tag, ","); n != "" {
					name = n
				}
			}
			m[name] = JSONSafe(v.Field(i))
		}
		return m
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return []any{}
		}
		// A long series of plain finite numbers (the drift log, the envelope)
		// goes to encoding/json whole: a value built per element costs the
		// scope's CPU hundreds of milliseconds a report for a long log.
		if v.Len() > 16 && finiteSeries(v) {
			if b, err := json.Marshal(v.Interface()); err == nil {
				return json.RawMessage(b)
			}
		}
		out := make([]any, v.Len())
		for i := range out {
			out[i] = JSONSafe(v.Index(i))
		}
		return out
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return JSONSafe(v.Elem())
	}
	return v.Interface()
}
