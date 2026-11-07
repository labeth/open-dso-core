// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"testing"

	"github.com/labeth/open-dso-core/decode"
)

// adversarial matchI2C span sequences (repeated-START, missing R/W, cross-txn data).
// TRLC-LINKS: REQ-SDS-013
func TestMatchI2CEdges(t *testing.T) {
	sp := func(kind string, val, i0 int, text string) decode.Span {
		return decode.Span{Kind: kind, Val: val, I0: i0, Text: text}
	}
	// A repeated-START bus: [S a50 W d.AA][S a51 R d.BB]P
	rep := []decode.Span{
		sp("start", 0, 0, "S"), sp("addr", 0x50, 5, "50"), sp("rw", 0, 10, "W"), sp("data", 0xAA, 15, "AA"),
		sp("start", 0, 20, "S"), sp("addr", 0x51, 25, "51"), sp("rw", 0, 30, "R"), sp("data", 0xBB, 35, "BB"),
		sp("stop", 0, 40, "P"),
	}
	check := func(name string, p SerialParams, wantOK bool, wantAnchor int) {
		ok, a := matchI2C(rep, p)
		if ok != wantOK || (wantOK && a != wantAnchor) {
			t.Fatalf("%s: got (ok=%v anchor=%d) want (ok=%v anchor=%d)", name, ok, a, wantOK, wantAnchor)
		}
	}
	// addr 0x50 write, data 0xAA → match, anchored on the 0x50 addr span
	check("50W+AA", SerialParams{Proto: SerI2C, Addr: 0x50, RW: 0, Bytes: []int{0xAA}}, true, 5)
	// addr 0x51 read, data 0xBB → match on the second transaction
	check("51R+BB", SerialParams{Proto: SerI2C, Addr: 0x51, RW: 1, Bytes: []int{0xBB}}, true, 25)
	// addr 0x50 with data 0xBB → NO match (0xBB belongs to the 0x51 transaction)
	check("50+BB-cross", SerialParams{Proto: SerI2C, Addr: 0x50, RW: 2, Bytes: []int{0xBB}}, false, 0)
	// addr 0x50 read → NO match (this txn is a write)
	check("50R-dir", SerialParams{Proto: SerI2C, Addr: 0x50, RW: 1}, false, 0)
	// any address, any dir, no data → match the first transaction
	check("any", SerialParams{Proto: SerI2C, Addr: -1, RW: 2}, true, 5)

	// addr as the LAST span, no following rw:
	trailing := []decode.Span{sp("start", 0, 0, "S"), sp("addr", 0x22, 5, "22")}
	if ok, a := matchI2C(trailing, SerialParams{Proto: SerI2C, Addr: 0x22, RW: 2}); !ok || a != 5 {
		t.Fatalf("trailing addr, RW=any should match (ok=%v a=%d)", ok, a)
	}
	if ok, _ := matchI2C(trailing, SerialParams{Proto: SerI2C, Addr: 0x22, RW: 0}); ok {
		t.Fatal("trailing addr with NO rw span must NOT match a W qualifier")
	}
	// empty spans
	if ok, _ := matchI2C(nil, SerialParams{Proto: SerI2C, Addr: -1, RW: 2}); ok {
		t.Fatal("empty spans must not match")
	}
}
