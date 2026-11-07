package uimodel

import (
	"encoding/json"
	"strings"
	"testing"
)

// A reference's per-channel lists are the two-channel wire's for two
// channels or fewer (zeros for an empty slot) and every channel's past two.
// TRLC-LINKS: REQ-DSO-138
func TestChanValuesOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		e    RefExport
		want string
	}{
		{RefExport{}, `"vdiv":[0,0],"off":[0,0],"probe":[0,0]`},
		{RefExport{Vdiv: ChanValues{1, 0.5}}, `"vdiv":[1,0.5],"off":[0,0]`},
		{RefExport{Vdiv: ChanValues{1, 2, 3, 4}, C3: []int{7}}, `"c3":[7],"vdiv":[1,2,3,4]`},
	} {
		b, err := json.Marshal(tc.e)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), tc.want) {
			t.Fatalf("%s lacks %s", b, tc.want)
		}
	}
	if v := (ChanValues{1}).At(3); v != 0 {
		t.Fatalf("At past the list: %g", v)
	}
}
