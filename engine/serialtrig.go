// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/hal/prototrig"
)

// The decode.Span kinds the serial trigger and the streams read.
const (
	SpanData        = "data"
	spanStart       = "start"
	spanStop        = "stop"
	SpanGap         = "gap"
	SpanSOF         = "sof"
	spanSync        = "sync"
	SpanFrameError  = "frame-error"
	SpanParityError = "parity-error"
)

// ParityNone is a UART request without parity ("" reads the same).
const ParityNone = "none"

// The serial trigger modes (SerialOff, SerialTrigger) and the protocol ids.
const (
	SerialOff     = 0 // the trigger is the hardware's own
	SerialTrigger = 1 // the trigger is a decoded serial match

	SerialFallback = 60 // AUTO liveness: publish one unmatched frame every N holds

	// protocol ids (match the web/panel decode convention: uart/i2c/spi)
	SerUART       = 1
	SerI2C        = 2
	SerSPI        = 3
	SerManchester = 4
	SerSENT       = 5
	SerCAN        = 6
	SerMIL1553    = 7
	SerARINC      = 8
	SerUSB        = 9 // USB low and full speed (Baud 1.5e6 or 12e6)
	SerFlexRay    = 10
	SerLIN        = 11
	SerDMX        = 12
	SerMIDI       = 13
	SerModbus     = 14
	SerOneWire    = 15
	SerWS281x     = 16
	SerDHT        = 17
	SerCEC        = 18
	SerJ1850      = 19
	SerIRNEC      = 20
	SerIRSIRC     = 21
	SerIRRC5      = 22
	SerIRRC6      = 23
	SerDALI       = 24
	SerUSBPD      = 25
	SerSPDIF      = 26
	SerPS2        = 27
	SerSWD        = 28
	SerMDIO       = 29
	SerWiegand    = 30
	SerQuad       = 31 // a quadrature encoder: A (ChA) and B
	SerStepper    = 32 // STEP (ChA) and DIR
	SerPWM        = 33
	SerRCEncode   = 34 // PT2262 trits (Bits 12) or EV1527 bits (Bits 24)
	SerOOK        = 35 // Oregon Scientific bursts (Bits 3 or 21: the protocol)
	SerMiller     = 36
	SerMorse      = 37
	SerDCF77      = 38
	SerEM4100     = 39
	SerEM4305     = 40 // a reader's commands (Inverted: read off its carrier)
	SerT55xx      = 41 // likewise; Bits 70 or 38, the write's length
	SerSWIM       = 42
	SerSDQ        = 43
	SerPJDL       = 44 // PJON over PJDL (Bits: the mode, 1..4)
	SerDSI        = 45
	SerQi         = 46
	SerSDCard     = 47 // CLK (ChA) and CMD
	SerPDI        = 48 // PDI_CLK (ChA) and PDI_DATA
	SerCJTAG      = 49 // TCKC (ChA) and TMSC
	SerMaple      = 50 // SDCKA (ChA) and SDCKB
	SerCaliper    = 51 // CLK (ChA) and DATA
	SerIRMP       = 52 // the IR multi protocols (Bits: IRMP's number)

	// SerialProtocols bounds the protocol ids (1..SerialProtocols-1).
	SerialProtocols = 53
)

// SerialParams is the serial trigger's request (prototrig.SerialParams):
// the protocol, the channel roles, the decode parameters and the pattern to
// match. It lives beside the HAL so a board's Bus trigger takes it.
type SerialParams = prototrig.SerialParams

// SerialState is the serial trigger's parameters under their own lock.
// TRLC-LINKS: REQ-DSO-013
type SerialState struct {
	// LOCK ORDER: e.mu is acquired BEFORE ser.mu (Snapshot nests that way), same
	// rule as zoneMaskState — never take e.mu while holding ser.mu.
	Mu     sync.Mutex
	Params SerialParams
}

// AutoLiveMax is the longest AUTO holds the frames a gate rejects: a slow
// timebase's captures come seconds apart, so counting fallback holds alone
// kept the screen still for minutes.
const AutoLiveMax = time.Second

// SerialMatchIn decodes the two lines (a request's first and second line)
// and finds its match nearest at.target.
// TRLC-LINKS: REQ-DSO-013
func SerialMatchIn(chA, chB []uint8, sampleS float64, p SerialParams, at AnchorSpan) (bool, int) {
	d, ok := LookupSerialDecoder(p.Proto)
	if !ok {
		return false, -1
	} // Unsupported protocols must never qualify a trigger.
	res := d.Decode(chA, chB, sampleS, p)
	if !res.OK {
		return false, -1
	}
	return SerialMatchSpans(res.Spans, p, at)
}

// SerialMatchSpans finds a request's match nearest at.target in a decode's
// spans: SerialMatchIn past the decode, for a caller that marks what its
// receiver frames by (an SPI word after a gap in the clock) first.
// TRLC-LINKS: REQ-DSO-013
func SerialMatchSpans(spans []decode.Span, p SerialParams, at AnchorSpan) (bool, int) {
	d, ok := LookupSerialDecoder(p.Proto)
	if !ok {
		return false, -1
	}
	match := d.Match
	if SequenceNeeded(p) {
		match = MatchSequence
	}
	ok, start := pickSerialMatch(spans, p, match, at)
	if ok && (p.Proto == SerARINC || p.Proto == SerMIL1553) {
		start = wordOpening(spans, start)
	}
	return ok, start
}

// wordOpening is where the word whose data field starts at sample at begins:
// an ARINC 429 word at its label, a MIL-1553 word at its sync, when the
// decoder marks one right before the field (an ARINC 429 word's trigger point
// sat on its data field, 100 us into the word; a MIL-1553 command word's 3 us
// after its sync). at itself otherwise.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-223
func wordOpening(spans []decode.Span, at int) int {
	for k, sp := range spans {
		if sp.I0 != at || sp.Kind != SpanData {
			continue
		}
		for j := k - 1; j >= 0; j-- {
			o := spans[j]
			if o.I1 > at {
				continue // a span over the field (a decoder note)
			}
			if (o.Kind == "addr" || o.Kind == spanStart || o.Kind == spanSync) && at-o.I1 <= max(1, (sp.I1-sp.I0)/4) {
				return o.I0
			}
			break
		}
		break
	}
	return at
}

// pickSerialMatch walks the record's matches in order and keeps the one
// nearest at.target that the display can centre a full screen on. A record
// holds more than a screen, so the first match can sit near a record end and
// leave part of the screen off the record, drawn as a repeated edge sample (a
// flat 0 V tail on a repeating UART message). When no
// match fits - at slow timebases one SRAM read holds little more than the
// screen - the match nearest the target leaves the least of it off.
// TRLC-LINKS: REQ-DSO-013
func pickSerialMatch(sp []decode.Span, p SerialParams, match func([]decode.Span, SerialParams) (bool, int), at AnchorSpan) (bool, int) {
	ok, first := match(sp, p)
	if !ok || first < 0 {
		return ok, first
	}
	pick, pickD, near, nearD := -1, math.MaxInt, first, math.MaxInt
	for anchor, k, n := first, 0, 0; n < 256; n++ {
		d := anchor - at.Target
		if d < 0 {
			d = -d
		}
		if d < nearD {
			near, nearD = anchor, d
		}
		if at.holds(anchor) && d < pickD {
			pick, pickD = anchor, d
		}
		if anchor >= at.Target {
			break // later matches are farther from the target
		}
		for k < len(sp) && sp[k].I0 <= anchor {
			k++
		}
		if k >= len(sp) {
			break
		}
		var more bool
		if more, anchor = match(sp[k:], p); !more || anchor < 0 {
			break
		}
	}
	if pick < 0 {
		return true, near
	}
	return true, pick
}

// matchI2C finds a transaction addressing p.Addr (or any if <0) with the wanted
// R/W direction; when p.Bytes is set, the transaction's data bytes must also
// contain that sequence. Anchors on the address span (the transaction start).
// TRLC-LINKS: REQ-DSO-013
func matchI2C(sp []decode.Span, p SerialParams) (bool, int) {
	for i := range sp {
		if sp[i].Kind != "addr" {
			continue
		}
		if p.Addr >= 0 && sp[i].Val != p.Addr {
			continue
		}
		if p.RW != 2 { // R/W is a separate span right after the address
			if i+1 >= len(sp) || sp[i+1].Kind != "rw" {
				continue
			}
			want := "W"
			if p.RW == 1 {
				want = "R"
			}
			if sp[i+1].Text != want {
				continue
			}
		}
		if len(p.Bytes) == 0 {
			return true, sp[i].I0 // address (+ R/W) match, no data requirement
		}
		// gather this transaction's data bytes (until the next start/stop/addr)
		var vals []int
		for j := i + 1; j < len(sp); j++ {
			switch sp[j].Kind {
			case spanStart, spanStop, "addr":
				j = len(sp) // break out
			case SpanData:
				vals = append(vals, sp[j].Val)
			}
		}
		if indexSeq(vals, p.Bytes) >= 0 {
			return true, sp[i].I0
		}
	}
	return false, -1
}

// matchBytes finds the wanted byte sequence in the UART/SPI data stream (empty
// want = any decodable byte). Anchors on the first matching byte's sample index.
// Two rules keep it honest: (1) only Kind=="data" spans count — a byte the
// decoder flagged frame-error/parity-error is NOT a valid protocol byte and must
// not satisfy a data trigger (matchI2C is data-only too); (2) a MULTI-byte
// pattern must be CONTIGUOUS on the wire — consecutive matched bytes must abut
// (no large idle gap / burst boundary between them), so "AB" cannot be forged
// from an 'A' and a 'B' emitted far apart in separate transmissions.
// TRLC-LINKS: REQ-DSO-013
func matchBytes(sp []decode.Span, want []int) (bool, int) {
	type db struct{ val, i0, i1, segment int }
	var buf [32]db // a packet's bytes: on the stack up to 32
	seq := buf[:0]
	segment := 0
	for _, s := range sp {
		switch s.Kind {
		case SpanData:
			seq = append(seq, db{s.Val, s.I0, s.I1, segment})
		case SpanGap, SpanFrameError, SpanParityError:
			// Never manufacture a sequence by dropping a corrupt byte or an
			// explicit decoder gap, even when the sample indices are close.
			segment++
		}
	}
	if len(want) == 0 {
		if len(seq) == 0 {
			return false, -1
		}
		return true, seq[0].i0 // any valid data byte present
	}
	for start := 0; start+len(want) <= len(seq); start++ {
		ok := true
		for k := range want {
			if seq[start+k].val != want[k] || seq[start+k].segment != seq[start].segment {
				ok = false
				break
			}
			if k > 0 { // require adjacency: gap ≤ ~2 byte-widths, else it bridged a gap
				w := max(seq[start+k].i1-seq[start+k].i0, 1)
				if seq[start+k].i0-seq[start+k-1].i1 > 2*w {
					ok = false
					break
				}
			}
		}
		if ok {
			return true, seq[start].i0
		}
	}
	return false, -1
}

// indexSeq returns the start index of the first occurrence of needle in hay, or
// -1. Empty needle matches at 0.
// TRLC-LINKS: REQ-DSO-013
func indexSeq(hay, needle []int) int {
	if len(needle) == 0 {
		return 0
	}
	for start := 0; start+len(needle) <= len(hay); start++ {
		ok := true
		for k := range needle {
			if hay[start+k] != needle[k] {
				ok = false
				break
			}
		}
		if ok {
			return start
		}
	}
	return -1
}
