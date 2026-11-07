package profile

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
	durationType    = reflect.TypeFor[time.Duration]()
)

// decode stores n into v, which must be settable. Struct fields are named by
// their `yaml` tag ("-" or no tag: not in the file); an unknown key is an
// error so a misspelt setting never passes silently. A type implementing
// encoding.TextUnmarshaler reads a scalar's text, or a list's items joined
// by commas (how a profile writes a set: content, options). A null leaves
// the zero value.
// TRLC-LINKS: REQ-DSO-1002
func decode(n *node, v reflect.Value, path string) error {
	if n.kind == nullNode {
		return nil
	}
	if reflect.PointerTo(v.Type()).Implements(textUnmarshaler) {
		text, err := textOf(n, path)
		if err != nil {
			return err
		}
		// Implements above makes the assertion hold.
		if u, ok := reflect.TypeAssert[encoding.TextUnmarshaler](v.Addr()); ok {
			if err := u.UnmarshalText([]byte(text)); err != nil {
				return fmt.Errorf("%s (line %d): %w", path, n.line, err)
			}
		}
		return nil
	}
	switch v.Kind() { //nolint:exhaustive // default: every other kind is a scalar, which decodeScalar takes or refuses
	case reflect.Struct:
		return decodeStruct(n, v, path)
	case reflect.Map:
		return decodeMap(n, v, path)
	case reflect.Slice:
		if n.kind != seqNode {
			return mismatch(n, path, "a list")
		}
		s := reflect.MakeSlice(v.Type(), len(n.items), len(n.items))
		for i, it := range n.items {
			if err := decode(it, s.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		v.Set(s)
		return nil
	}
	if n.kind != scalarNode {
		return mismatch(n, path, "a scalar")
	}
	return decodeScalar(n, v, path)
}

// TRLC-LINKS: REQ-DSO-1002
func decodeStruct(n *node, v reflect.Value, path string) error {
	if n.kind != mapNode {
		return mismatch(n, path, "a mapping")
	}
	fields := map[string]int{}
	t := v.Type()
	for i := range t.NumField() {
		if tag, ok := t.Field(i).Tag.Lookup("yaml"); ok && tag != "-" {
			fields[tag] = i
		}
	}
	for _, k := range n.keys {
		i, ok := fields[k]
		if !ok {
			return fmt.Errorf("%s (line %d): unknown key", join(path, k), n.m[k].line)
		}
		if err := decode(n.m[k], v.Field(i), join(path, k)); err != nil {
			return err
		}
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-1002
func decodeMap(n *node, v reflect.Value, path string) error {
	if n.kind != mapNode {
		return mismatch(n, path, "a mapping")
	}
	t := v.Type()
	if v.IsNil() {
		v.Set(reflect.MakeMapWithSize(t, len(n.keys)))
	}
	for _, k := range n.keys {
		key := reflect.New(t.Key()).Elem()
		if err := decode(&node{kind: scalarNode, value: k, quoted: true, line: n.m[k].line}, key, join(path, k)); err != nil {
			return err
		}
		val := reflect.New(t.Elem()).Elem()
		if err := decode(n.m[k], val, join(path, k)); err != nil {
			return err
		}
		v.SetMapIndex(key, val)
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-1002
func decodeScalar(n *node, v reflect.Value, path string) error {
	s := n.value
	bad := func(want string) error {
		return fmt.Errorf("%s (line %d): %q is not %s", path, n.line, s, want)
	}
	if v.Type() == durationType {
		d, err := time.ParseDuration(s)
		if err != nil {
			return bad("a duration")
		}
		v.SetInt(int64(d))
		return nil
	}
	switch v.Kind() { //nolint:exhaustive // the scalar kinds a profile holds; the default refuses the rest
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		if n.quoted || (s != "true" && s != "false") {
			return bad("true or false")
		}
		v.SetBool(s == "true")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(s, 0, v.Type().Bits())
		if n.quoted || err != nil {
			return bad("an integer")
		}
		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(s, 0, v.Type().Bits())
		if n.quoted || err != nil {
			return bad("an unsigned integer")
		}
		v.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, v.Type().Bits())
		if n.quoted || err != nil {
			return bad("a number")
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("%s: the profile cannot hold a %s", path, v.Type())
	}
	return nil
}

// TRLC-LINKS: REQ-DSO-1002
func textOf(n *node, path string) (string, error) {
	switch n.kind {
	case scalarNode:
		return n.value, nil
	case seqNode:
		parts := make([]string, len(n.items))
		for i, it := range n.items {
			if it.kind != scalarNode {
				return "", mismatch(it, fmt.Sprintf("%s[%d]", path, i), "a scalar")
			}
			parts[i] = it.value
		}
		return strings.Join(parts, ","), nil
	case nullNode, mapNode:
	}
	return "", mismatch(n, path, "a scalar or a list")
}

// TRLC-LINKS: REQ-DSO-1002
func mismatch(n *node, path, want string) error {
	return fmt.Errorf("%s (line %d): want %s, have %s", path, n.line, want, n.describe())
}

// TRLC-LINKS: REQ-DSO-1002
func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
