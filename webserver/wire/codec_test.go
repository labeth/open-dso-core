// ENGMODEL-OWNER-UNIT: FU-APP-WEB

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
// TRLC-LINKS: REQ-SDS-274
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
	if got["a"] != nil || got["Skip"] != nil || len(got["s"].([]any)) != 40 || got["f"].([]any)[3] != nil || len(got["n"].([]any)) != 0 {
		t.Fatalf("%s", b)
	}
}

// LEBytes is the words' little-endian bytes.
// TRLC-LINKS: REQ-SDS-163
func TestLEBytes(t *testing.T) {
	if b := LEBytes([]uint16{0x0102, 0xA0B0}); !bytes.Equal(b, []byte{2, 1, 0xB0, 0xA0}) || LEBytes(nil) != nil {
		t.Fatalf("% x", b)
	}
}

// TRLC-LINKS: REQ-SDS-265
func BenchmarkEncodeSoundBin(b *testing.B) {
	q1, q2 := make([]uint16, 4096), make([]uint16, 4096)
	h := map[string]int{"n": 4096}
	for b.Loop() {
		EncodeSoundBin(h, q1, q2, 4096)
	}
}
