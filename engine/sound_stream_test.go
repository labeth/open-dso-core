// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"testing"
)

// Mark words are 32-bit counts: they extend to the stream's ordinal near
// the words drained, before and after a wrap.
// TRLC-LINKS: REQ-DSO-272
func TestUnwrapWord(t *testing.T) {
	cases := []struct {
		w    uint32
		ref  uint64
		want uint64
	}{{100, 90, 100}, {100, 5000, 100}, {0xfffffff0, 1<<32 + 10, 0xfffffff0}, {5, 1<<32 - 3, 1<<32 + 5}, {7, 3<<32 + 2, 3<<32 + 7}}
	for _, c := range cases {
		if got := UnwrapWord(c.w, c.ref); got != c.want {
			t.Errorf("unwrap(%#x, %#x) = %#x, want %#x", c.w, c.ref, got, c.want)
		}
	}
}
