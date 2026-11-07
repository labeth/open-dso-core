// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// JSONSafe writes NaN and infinities as null, keeps JSON names and
// passes long finite series whole.
// TRLC-LINKS: REQ-DSO-274
func TestJSONSafe(t *testing.T) {
	type pt struct {
		T  float64 `json:"t"`
		Hz float64 `json:"hz"`
	}
	v := struct {
		A    float64   `json:"a"`
		Skip int       `json:"-"`
		S    []pt      `json:"s"`
		F    []float64 `json:"f"`
		N    []int     `json:"n"`
	}{A: math.NaN()}
	for i := range 40 {
		v.S = append(v.S, pt{T: float64(i), Hz: 440})
		v.F = append(v.F, float64(i))
	}
	v.F[3] = math.Inf(1)
	b, err := json.Marshal(JSONSafe(reflect.ValueOf(v)))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	sv, sok := got["s"].([]any)
	fv, fok := got["f"].([]any)
	nv, nok := got["n"].([]any)
	if !sok || !fok || !nok || got["a"] != nil || got["Skip"] != nil || len(sv) != 40 || len(fv) < 4 || fv[3] != nil || len(nv) != 0 {
		t.Fatalf("%s", b)
	}
}

// LEBytes is the words' little-endian bytes.
// TRLC-LINKS: REQ-DSO-163
func TestLEBytes(t *testing.T) {
	if b := LEBytes([]uint16{0x0102, 0xA0B0}); !bytes.Equal(b, []byte{2, 1, 0xB0, 0xA0}) || LEBytes(nil) != nil {
		t.Fatalf("% x", b)
	}
}

// TRLC-LINKS: REQ-DSO-265
func BenchmarkEncodeSoundBin(b *testing.B) {
	q1, q2 := make([]uint16, 4096), make([]uint16, 4096)
	h := map[string]int{"n": 4096}
	for b.Loop() {
		EncodeSoundBin(h, q1, q2, 4096)
	}
}

// JSONSafe leaves out an omitempty field that is an empty collection or a
// nil reference, and still writes an omitempty scalar.
// TRLC-LINKS: REQ-DSO-274
func TestJSONSafeOmitEmpty(t *testing.T) {
	v := struct {
		L     []int      `json:"l,omitempty"`
		P     *int       `json:"p,omitempty"`
		Kept  []int      `json:"kept,omitempty"`
		Err   string     `json:"err,omitempty"`
		Plain []int      `json:"plain"`
		NaN   float64    `json:"nan"`
		Lanes SoundScale `json:"lanes"`
	}{Kept: []int{1}, NaN: math.NaN()}
	b, err := json.Marshal(JSONSafe(reflect.ValueOf(v)))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"err":"","kept":[1],"lanes":{"from":0,"off":[0,0],"vpc":[0,0]},"nan":null,"plain":[]}`; string(b) != want {
		t.Fatalf("%s, want %s", b, want)
	}
}
