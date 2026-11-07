// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"time"
	"unsafe"
)

// LEBytes is the memory of w: its words little-endian on these CPUs.
// TRLC-LINKS: REQ-DSO-163
func LEBytes(w []uint16) []byte {
	if len(w) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&w[0])), 2*len(w)) //nolint:gosec // G103: the words' own memory, little-endian on this CPU
}

// finiteSeries reports a slice or array of finite floats, or of structs of
// finite floats, ints, bools and strings without JSON tag options (their
// encoding/json form is JSONSafe's).
// TRLC-LINKS: REQ-DSO-274
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
// null (encoding/json refuses them): structs by their JSON names. A field
// tagged omitempty is left out when it is an empty slice, map or array or
// a nil pointer or interface, as encoding/json leaves it out; an omitempty
// scalar (a false, a zero, an empty string) is still written, as this
// fallback always wrote it and pages read err and paused from it.
// TRLC-LINKS: REQ-DSO-274
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
				n, opts, _ := strings.Cut(tag, ",")
				if n != "" {
					name = n
				}
				if hasOpt(opts, "omitempty") && emptyRef(v.Field(i)) {
					continue
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

// hasOpt reports whether a JSON tag's options name opt.
// TRLC-LINKS: REQ-DSO-274
func hasOpt(opts, opt string) bool {
	for opts != "" {
		var o string
		o, opts, _ = strings.Cut(opts, ",")
		if o == opt {
			return true
		}
	}
	return false
}

// emptyRef reports an empty collection or a nil reference.
// TRLC-LINKS: REQ-DSO-274
func emptyRef(v reflect.Value) bool {
	switch v.Kind() { //nolint:exhaustive // scalars are never left out by JSONSafe
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	}
	return false
}
