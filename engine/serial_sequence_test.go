// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"github.com/labeth/open-dso-core/decode"
	"testing"
)

// The software form follows the hardware contract on decoded spans.
// TRLC-LINKS: REQ-SDS-013
func TestMatchSequence(t *testing.T) {
	data := func(i, v int) decode.Span { return decode.Span{I0: i, I1: i, Kind: "data", Val: v} }
	sof := func(i int) decode.Span { return decode.Span{I0: i, I1: i, Kind: "sof"} }
	crcErr := func(i int) decode.Span { return decode.Span{I0: i, I1: i, Kind: "frame-error"} }
	// Two CAN packets carry AA 0F; the first ends in a CRC error.
	spans := []decode.Span{sof(0), data(1, 0x55), data(2, 0xaa), data(3, 0x0f), crcErr(4), sof(10), data(11, 0xaa), data(12, 0x0f), data(13, 0xf0)}
	if ok, at := MatchSequence(spans, SerialParams{Proto: SerCAN, Bytes: []int{0xaa, 0x0f}}); !ok || at != 11 {
		t.Fatalf("qualified data: %v %d", ok, at)
	}
	if ok, at := MatchSequence(spans, SerialParams{Proto: SerCAN, Pattern: []PatternElem{{4, 0, 0}}}); !ok || at != 4 {
		t.Fatalf("error: %v %d", ok, at)
	}
	if ok, at := MatchSequence(spans, SerialParams{Proto: SerCAN, Pattern: []PatternElem{{2, 0xa0, 0xf0}, {2, 0, 0}, {2, 0xf0, 0xff}}}); !ok || at != 11 {
		t.Fatalf("wildcards: %v %d", ok, at)
	}
	// A packet boundary breaks contiguity.
	if ok, _ := MatchSequence(spans, SerialParams{Proto: SerCAN, Pattern: []PatternElem{{4, 0, 0}, {2, 0xaa, 0xff}}}); ok {
		t.Fatal("matched across a packet boundary")
	}
	// UART is not framed: the data before an error still counts, and a long sequence matches.
	var uart []decode.Span
	for i, c := range "Hello, long sequence" {
		uart = append(uart, data(i, int(c)))
	}
	var want []int
	for _, c := range "Hello, long sequence" {
		want = append(want, int(c))
	}
	if ok, at := MatchSequence(uart, SerialParams{Proto: SerUART, Bytes: want}); !ok || at != 0 {
		t.Fatalf("UART long: %v %d", ok, at)
	}
	// I2C counts data only in transactions to the requested address.
	addr := func(i, a int) decode.Span { return decode.Span{I0: i, I1: i, Kind: "addr", Val: a} }
	rw := func(i int, t string) decode.Span { return decode.Span{I0: i, I1: i, Kind: "rw", Text: t} }
	start := decode.Span{Kind: "start"}
	i2c := []decode.Span{start, addr(1, 0x50), rw(2, "W"), data(3, 1), data(4, 2), {Kind: "stop"}, start, addr(10, 0x24), rw(11, "W"), data(12, 1), data(13, 2)}
	if ok, at := MatchSequence(i2c, SerialParams{Proto: SerI2C, Addr: 0x24, RW: 0, Pattern: []PatternElem{{2, 1, 0xff}, {2, 2, 0xff}}}); !ok || at != 12 {
		t.Fatalf("I2C address filter: %v %d", ok, at)
	}
	if ok, _ := MatchSequence(i2c, SerialParams{Proto: SerI2C, Addr: 0x24, RW: 1, Pattern: []PatternElem{{2, 1, 0xff}}}); ok {
		t.Fatal("I2C direction ignored")
	}
}

// The frame a frame-start trigger fires in opens at its SOF, SYNC or START,
// past its own fields; a unit with nothing marked before it opens itself.
// TRLC-LINKS: REQ-SDS-013, REQ-SDS-223
func TestFrameOpening(t *testing.T) {
	can := []decode.Span{
		{I0: 0, I1: 9, Kind: "data"},
		{I0: 50, I1: 51, Kind: "sof"},
		{I0: 52, I1: 80, Kind: "id"},
		{I0: 81, I1: 90, Kind: "ctl"},
		{I0: 91, I1: 110, Kind: "data"},
	}
	if at := frameOpening(can, 111); at != 50 {
		t.Fatalf("CAN frame opens at %d, want its SOF at 50", at)
	}
	if at := frameOpening(can, 10); at != 0 {
		t.Fatalf("a unit with no opening before it: %d", at)
	}
	if at := frameOpening(can, 300); at != -1 {
		t.Fatalf("no unit ends near 300: %d", at)
	}
	// The Manchester image fires as the frame's fourth word ends (the bench:
	// 24 us past the SYNC at 1 Mb/s): the frame still opens at its SYNC. A
	// frame before it, an idle gap away, is not this one's.
	man := []decode.Span{
		{I0: 0, I1: 8, Kind: "data"},
		{I0: 20, I1: 24, Kind: "start", Text: "SYNC"},
		{I0: 20, I1: 28, Kind: "data"},
		{I0: 28, I1: 36, Kind: "data"},
		{I0: 36, I1: 44, Kind: "data"},
		{I0: 44, I1: 52, Kind: "data"},
	}
	if at := frameOpening(man, 52); at != 20 {
		t.Fatalf("Manchester frame fired on its fourth word opens at %d, want its SYNC at 20", at)
	}
	usb := []decode.Span{
		{I0: 0, I1: 5, Kind: "start", Text: "SYNC"},
		{I0: 5, I1: 10, Kind: "addr", Text: "DATA0"},
		{I0: 10, I1: 15, Kind: "data"},
		{I0: 15, I1: 20, Kind: "data"},
		{I0: 20, I1: 25, Kind: "data"},
	}
	if at := frameOpening(usb, 25); at != 0 {
		t.Fatalf("USB packet fired on its third byte opens at %d, want its SYNC at 0", at)
	}
	gapped := []decode.Span{{I0: 0, I1: 4, Kind: "sync"}, {I0: 4, I1: 12, Kind: "data"}, {I0: 40, I1: 48, Kind: "data"}}
	if at := frameOpening(gapped, 48); at != 40 {
		t.Fatalf("a unit an idle gap after the last frame opens at %d, want itself at 40", at)
	}
}

// A word bus's matched word begins before its data field: an ARINC 429 word
// at its label, a MIL-1553 word at its sync. Other fields stay where they are.
// TRLC-LINKS: REQ-SDS-013, REQ-SDS-223
func TestWordOpening(t *testing.T) {
	arinc := []decode.Span{{I0: 0, I1: 80, Kind: "addr"}, {I0: 100, I1: 290, Kind: "data"}, {I0: 290, I1: 310, Kind: "gap"}}
	if at := wordOpening(arinc, 100); at != 0 {
		t.Fatalf("ARINC 429 word opens at %d, want its label at 0", at)
	}
	mil := []decode.Span{{I0: 40, I1: 42, Kind: "start", Text: "csync"}, {I0: 43, I1: 59, Kind: "data"}}
	if at := wordOpening(mil, 43); at != 40 {
		t.Fatalf("MIL-1553 word opens at %d, want its sync at 40", at)
	}
	far := []decode.Span{{I0: 0, I1: 10, Kind: "addr"}, {I0: 200, I1: 290, Kind: "data"}}
	if at := wordOpening(far, 200); at != 200 {
		t.Fatalf("a label far before the field taken: %d", at)
	}
}
