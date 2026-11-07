// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"github.com/labeth/open-dso-core/decode"
)

// PatternElem is one trigger pattern token (ADR-PROTOCOL-SEQUENCE-TRIGGER):
// Kind 2 is a decoded unit (byte, word or ARINC data field) equal to Value
// under Mask, in the decoder's own units; Mask 0 accepts any value. Kind 4 is
// a decoder-reported error (CRC, parity, framing, stuffing or coding). Kind 1
// is the start of a frame as the hardware decoder reports it (a START, SOF,
// SYNC or the idle gap before an SPI word), so [start, any unit] fires on a
// frame's first unit. A MIL-1553 unit may also require the command sync
// (MILCommand in Value and Mask).
// TRLC-LINKS: REQ-DSO-013
type PatternElem struct {
	Kind  int `json:"kind"`
	Value int `json:"value"`
	Mask  int `json:"mask"`
}

// Pattern token kinds.
const (
	PatternStart = 1
	PatternData  = 2
	PatternError = 4
)

// MILCommand is the flag of a MIL-1553 word received after a command (or
// status) sync, beside its sixteen bits in the matcher's event value.
const MILCommand = 1 << 16

// UnitFlags are the event metadata bits a pattern unit may require beside
// the unit (the MIL command sync).
// TRLC-LINKS: REQ-DSO-013
func UnitFlags(p SerialParams) int {
	if p.Proto == SerMIL1553 {
		return MILCommand
	}
	return 0
}

// nativeLimit is the longest plain pattern the protocol's own hardware
// matcher takes; longer ones and any Pattern use the sequence trigger.
// TRLC-LINKS: REQ-DSO-013
func nativeLimit(proto int) int {
	switch proto {
	case SerMIL1553, SerARINC:
		return 1
	case SerCAN, SerFlexRay:
		return 8
	}
	return 4
}

// SequenceNeeded reports a request beyond the protocol's native matcher.
// TRLC-LINKS: REQ-DSO-013
func SequenceNeeded(p SerialParams) bool {
	return len(p.Pattern) != 0 || len(p.Bytes) > nativeLimit(p.Proto)
}

// PatternOf is the request as tokens: Pattern, or Bytes as exact units.
// TRLC-LINKS: REQ-DSO-013
func PatternOf(p SerialParams) []PatternElem {
	if len(p.Pattern) != 0 {
		return p.Pattern
	}
	out := make([]PatternElem, len(p.Bytes))
	for i, b := range p.Bytes {
		out[i] = PatternElem{Kind: PatternData, Value: b, Mask: UnitMask(p)}
	}
	return out
}

// UnitMask covers one decoded unit of the protocol.
// TRLC-LINKS: REQ-DSO-013
func UnitMask(p SerialParams) int {
	switch p.Proto {
	case SerMIL1553:
		return 0xffff
	case SerARINC:
		return 0x7ffff
	case SerSENT:
		return 0xf
	case SerMDIO, SerWiegand, SerIRMP:
		return 0xffff
	case SerQuad, SerStepper:
		return 1 // a step: its direction
	case SerOOK:
		return 0xf // a nibble
	case SerCJTAG:
		return 0x7fffffff // a scan's low 31 bits
	case SerSPDIF:
		return 0xffffff
	case SerSWD:
		// a data word's low 31 bits (a pattern unit is an int, 32 bits on the
		// scope); the request and acknowledge fit too
		return 0x7fffffff
	case SerManchester:
		bits := p.Bits
		if bits <= 0 || bits > 16 {
			bits = 8
		}
		return 1<<bits - 1
	}
	return 0xff
}

// FramedProtocol decoders validate whole frames; a data sequence on them
// triggers only when its frame ends valid, as their native matchers do.
// TRLC-LINKS: REQ-DSO-013
func FramedProtocol(proto int) bool {
	switch proto {
	case SerCAN, SerFlexRay, SerUSB, SerSENT, SerManchester, SerARINC:
		return true
	}
	return proto >= SerLIN && proto != SerMIDI // the decoders that frame their units
}

// i2cSelected reports whether the I2C address span sp[i] opens a
// transaction p asks for: its address (any when p.Addr < 0) and direction
// (p.RW 0 write, 1 read, 2 either).
// TRLC-LINKS: REQ-DSO-013
func i2cSelected(sp []decode.Span, i int, p SerialParams) bool {
	rw := "" // the direction's text: W, R (none for another RW)
	switch p.RW {
	case 0:
		rw = "W"
	case 1:
		rw = "R"
	}
	return (p.Addr < 0 || sp[i].Val == p.Addr) && (p.RW == 2 || i+1 < len(sp) && sp[i+1].Kind == "rw" && sp[i+1].Text == rw)
}

// MatchSequence is the software form of the sequence trigger over decoded
// spans, in the decoder's units: data spans are units, error spans are
// errors, and packet boundaries break contiguity. On framed protocols a data
// sequence counts only in a packet without errors, like matchPackets. I2C
// counts only data in transactions whose address and direction match.
// TRLC-LINKS: REQ-DSO-013
func MatchSequence(sp []decode.Span, p SerialParams) (bool, int) {
	elems := PatternOf(p)
	if len(elems) == 0 {
		return false, -1
	}
	dataOnly := true
	for _, e := range elems {
		dataOnly = dataOnly && e.Kind == PatternData
	}
	qualify := dataOnly && FramedProtocol(p.Proto)
	var buf [16]int
	state := buf[:0] // start index of each partial match, -1 none (on the stack up to 16)
	if len(elems) > len(buf) {
		state = make([]int, 0, len(elems))
	}
	state = state[:len(elems)]
	reset := func() {
		for k := range state {
			state[k] = -1
		}
	}
	reset()
	selected := p.Proto != SerI2C
	command := 0 // MILCommand while the word after a command sync is read
	bad, hit := false, -1
	finish := func() (bool, int) {
		if hit >= 0 && !bad {
			return true, hit
		}
		bad, hit = false, -1
		return false, -1
	}
	for i, s := range sp {
		kind := 0
		switch s.Kind {
		case SpanData:
			kind = PatternData
		case SpanFrameError, SpanParityError:
			kind, bad = PatternError, true
		case SpanGap, spanStart, spanStop, SpanSOF, spanSync:
			reset()
			if qualify {
				if ok, at := finish(); ok {
					return true, at
				}
			}
			if p.Proto == SerI2C && s.Kind == spanStop {
				selected = false
			}
			if p.Proto == SerMIL1553 {
				command = 0
				if s.Text == "csync" {
					command = MILCommand
				}
			}
			if s.Kind == SpanGap || s.Kind == spanStop {
				continue
			}
			kind = PatternStart // a frame opens: a start element matches here
		case "addr":
			if p.Proto != SerI2C {
				continue
			}
			reset()
			selected = i2cSelected(sp, i, p)
			continue
		default:
			continue
		}
		if kind == PatternData && !selected {
			reset()
			continue
		}
		for k := len(elems) - 1; k >= 0; k-- {
			e := elems[k]
			ok := e.Kind == kind && (kind != PatternData || ((s.Val|command)^e.Value)&e.Mask == 0)
			start := i
			if k > 0 {
				start = state[k-1]
			}
			if ok && start >= 0 {
				state[k] = start
			} else {
				state[k] = -1
			}
		}
		if at := state[len(elems)-1]; at >= 0 {
			if !qualify {
				return true, sp[at].I0
			}
			if hit < 0 {
				hit = sp[at].I0
			}
		}
	}
	if qualify {
		return finish()
	}
	return false, -1
}
