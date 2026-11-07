// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/sample"
	"math"
	"slices"
	"sync/atomic"
	"time"
)

// SerialLevels is what the captures taught about the lines.
type SerialLevels struct {
	Volts [2]atomic.Uint64 // each channel's decoder threshold in volts at the BNC (float64 bits)
	Have  [2]atomic.Bool
	SPIHz atomic.Uint64 // the clock rate on the armed SPI request's clock line (float64 bits), 0 unknown
	// SPIQuiet is when the hardware last triggered on the learned SPI clock,
	// or its plan began (unix ns, 0 none); spiProbe asks one untriggered
	// capture to measure the clock again (spiQuietCheck).
	SPIQuiet atomic.Int64
	SPIProbe atomic.Bool
	SCLHz    atomic.Uint64 // the clock rate on the armed I2C request's SCL (float64 bits), 0 unknown
	Baud     atomic.Uint64 // the bit rate on the armed UART request's line (float64 bits), 0 unknown
	SentNs   atomic.Uint64 // the armed SENT request's tick in ns (float64 bits), 0 unknown
	SentNib  atomic.Int32  // the nibbles of its frames (status, data, CRC), 0 unknown
	ShiftS   atomic.Uint64 // how far the last match began before the FPGA's trigger, s (float64 bits)
}

// FrameLevels is what one histogram of each channel of a capture gives
// (decode.AutoThresholdExtent): its threshold and its extent.
type FrameLevels struct {
	done         [2]bool
	Thr          [2]float64
	ThrOK, ExtOK [2]bool
	ExtLo, ExtHi [2]int
}

// Of measures channel ch (its codes) into lv once.
// TRLC-LINKS: REQ-SDS-011, REQ-SDS-013
func (lv *FrameLevels) Of(ch int, codes []uint8) {
	if !lv.done[ch] {
		lv.Thr[ch], lv.ThrOK[ch], lv.ExtLo[ch], lv.ExtHi[ch], lv.ExtOK[ch] = decode.AutoThresholdExtent(codes)
		lv.done[ch] = true
	}
}

// SentShape measures a SENT line: its tick in ns and the nibbles a frame
// holds (status, data and CRC). Each pulse runs falling edge to falling edge;
// a SYNC is 56 ticks and a nibble 12 to 27, so a pulse followed by one in the
// nibble range at a 56th of its length is a SYNC. The tick is the SYNCs'
// median 56th, the nibbles the run of nibble pulses after a SYNC most often
// seen (a pause pulse or the next SYNC ends it). 0, 0 when the line shows no
// two frames.
// TRLC-LINKS: REQ-SDS-013
func SentShape(codes []uint8, sampleS float64) (tickNs float64, nibbles int) {
	thr, ok := decode.AutoThreshold(codes)
	if !ok || !(sampleS > 0) || len(codes) == 0 {
		return 0, 0
	}
	var falls []int
	high := float64(codes[0]) >= thr
	for i, c := range codes {
		now := float64(c) >= thr
		if high && !now {
			falls = append(falls, i)
		}
		high = now
	}
	if len(falls) < 4 {
		return 0, 0
	}
	period := make([]float64, len(falls)-1)
	for i := range period {
		period[i] = float64(falls[i+1] - falls[i])
	}
	nibble := func(p, tick float64) bool { r := p / tick; return r >= 12*0.8 && r <= 27*1.2 }
	var ticks []float64
	count := map[int]int{}
	for k := 0; k+1 < len(period); k++ {
		tick := period[k] / 56
		if !nibble(period[k+1], tick) {
			continue
		}
		n := 0
		for k+1+n < len(period) && nibble(period[k+1+n], tick) && n < 64 {
			n++
		}
		if k+1+n < len(period) { // the frame ended inside the capture
			ticks = append(ticks, tick)
			count[n]++
		}
		k += n
	}
	if len(ticks) < 2 {
		return 0, 0
	}
	for n, v := range count {
		if v > count[nibbles] || (v == count[nibbles] && n > nibbles) {
			nibbles = n
		}
	}
	slices.Sort(ticks)
	return ticks[len(ticks)/2] * sampleS * 1e9, nibbles
}

// standardBauds are the rates a measured UART line snaps to.
var standardBauds = []float64{1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600, 1e6, 2e6, 3e6}

// UARTBaud measures a UART line's bit rate: the shortest common run between
// edges is one bit (the median of the shortest fifth of the runs, from at
// least sixteen edges), snapped to a standard rate within 3 %. 0 when the
// line's bits are not resolved at this sample interval (under four samples).
// TRLC-LINKS: REQ-SDS-013
func UARTBaud(codes []uint8, sampleS float64) float64 {
	thr, ok := decode.AutoThreshold(codes)
	if !ok || !(sampleS > 0) || len(codes) == 0 {
		return 0
	}
	var runs []int
	high, last := float64(codes[0]) >= thr, 0
	for i, c := range codes {
		if now := float64(c) >= thr; now != high {
			if last > 0 {
				runs = append(runs, i-last)
			}
			high, last = now, i
		}
	}
	if len(runs) < 16 {
		return 0
	}
	slices.Sort(runs)
	short := runs[:max(1, len(runs)/5)]
	bit := float64(short[len(short)/2])
	if bit < 4 {
		return 0
	}
	b := 1 / (bit * sampleS)
	for _, s := range standardBauds {
		if math.Abs(b-s) <= 0.03*s {
			return s
		}
	}
	return math.Round(b)
}

// ClockRate measures a clock line's rate: the interval between rising edges
// near the one a quarter of them are no longer than (a word's clocks, not the
// gaps between words), from at least eight edges each four or more samples
// apart; extremes says the codes are bucket extremes (peak detect). 0 when
// the line shows no clock at this sample interval.
// TRLC-LINKS: REQ-SDS-013, REQ-SDS-229
func ClockRate(codes []uint8, sampleS float64, extremes bool) float64 {
	thr, ok := decode.AutoThreshold(codes)
	if !ok || !(sampleS > 0) {
		return 0
	}
	// a code is high at or above thr: at or above ⌈thr⌉, compared as integers
	on := int(math.Ceil(thr))
	rises := make([]int, 0, 64)
	high := len(codes) > 0 && int(codes[0]) >= on
	for i, c := range codes {
		now := int(c) >= on
		if now && !high {
			rises = append(rises, i)
		}
		high = now
	}
	if len(rises) < 8 {
		return 0
	}
	gaps := make([]int, len(rises)-1)
	for i := range gaps {
		gaps[i] = rises[i+1] - rises[i]
	}
	slices.Sort(gaps)
	// The mean of the gaps near the lower quartile's: a clock of a
	// non-integer number of samples (104 MHz: 4.8 at 2 ns) alternates.
	seed := gaps[len(gaps)/4]
	sum, n := 0, 0
	for _, g := range gaps {
		if 2*g >= seed && 2*g <= 3*seed {
			sum += g
			n++
		}
	}
	period := float64(sum) / float64(n)
	// The clock is not resolved at this timebase under four samples a period,
	// nor in bucket extremes (peak detect) under four buckets: a clock faster
	// than the buckets alternates their minima and maxima at a rate of the
	// buckets' own (a 25 MHz clock in 32 ns buckets read as 15.6 MHz).
	if period < 4 || (extremes && period < 8) {
		return 0
	}
	return 1 / (period * sampleS)
}

// SPIMeasuring is serialPlanParams' reason while the SPI clock is measured.
const SPIMeasuring = "measuring the SPI clock rate"

// SPIQuietNs is how long a learned SPI clock may go without a hardware
// trigger before one capture measures it again.
const SPIQuietNs = int64(2 * time.Second)

// ResolvesClock reports a frame whose clock measurement may replace a clock
// of hz (learned before; 0: none): the converters' own samples, whose edges
// are the bus's (a bus that slowed is measured anew), or a decimated record
// that would resolve hz (minClockSamples samples a period). On a decimated,
// filtered record a 10 MHz SPI clock measured as its alias, far slower: the
// gap planned for that split nothing, and no capture triggered again (the
// quiet check measuring again on the same records).
// TRLC-LINKS: REQ-SDS-013, REQ-SDS-229
func ResolvesClock(hz float64, f *sample.Frame) bool {
	return !(hz > 0) || uint32(f.AcqDecim) <= 1 || (f.SampleS > 0 && 1/(hz*f.SampleS) >= minClockSamples)
}

// minClockSamples is the samples a clock period spans on a frame that
// measures it.
const minClockSamples = 4

// JoinUnits is p's match bytes as the matcher's units where the scope's
// match editor holds a unit as several bytes.
// TRLC-LINKS: REQ-SDS-013
func JoinUnits(p SerialParams) SerialParams {
	// A MIL-1553 word typed as two bytes, an ARINC 429 data field as up to
	// three (the scope's match editor holds bytes, as its BUS table shows them);
	// a sequence of words as their bytes in turn (two each, three each).
	switch {
	case p.Proto == SerMIL1553 && len(p.Bytes) == 2:
		p.Bytes = joinBytes(p.Bytes, 0xffff)
	case p.Proto == SerMIL1553 && len(p.Bytes) > 2 && len(p.Bytes)%2 == 0:
		p.Bytes = joinGroups(p.Bytes, 2, 0xffff)
	case p.Proto == SerARINC && len(p.Bytes) >= 2 && len(p.Bytes) <= 3:
		p.Bytes = joinBytes(p.Bytes, 0x7ffff)
	case p.Proto == SerARINC && len(p.Bytes) > 3 && len(p.Bytes)%3 == 0:
		p.Bytes = joinGroups(p.Bytes, 3, 0x7ffff)
	case (p.Proto == SerMDIO || p.Proto == SerWiegand || p.Proto == SerIRMP) && len(p.Bytes) >= 2 && len(p.Bytes)%2 == 0:
		p.Bytes = joinGroups(p.Bytes, 2, 0xffff) // MDIO's header and data, Wiegand's fields, IR addresses and commands: 16 bits each
	case p.Proto == SerSPDIF && len(p.Bytes) >= 3 && len(p.Bytes)%3 == 0:
		p.Bytes = joinGroups(p.Bytes, 3, 0xffffff) // an S/PDIF sample: 24 bits
	case p.Proto == SerMIL1553 && len(p.Pattern) >= 2:
		p.Pattern = groupPattern(p.Pattern, 2, 0xffff)
	case p.Proto == SerARINC && len(p.Pattern) >= 3:
		p.Pattern = groupPattern(p.Pattern, 3, 0x7ffff)
	}
	return p
}

// ArincOneWord refuses an ARINC 429 match of more than one word: the
// matcher is not armed for it.
const ArincOneWord = "an ARINC 429 match is one word (00 AB CD: its data field)"

// groupPattern is a byte pattern as units of n bytes each (the words of
// MIL-1553 and ARINC 429 the matcher takes): a unit of n data bytes one
// data element, values and masks big-endian within limit; n errors one error
// (the BUS table writes a bad word as ERR for each of its bytes). The
// pattern unchanged when a unit mixes them or does not fit.
// TRLC-LINKS: REQ-SDS-013
func groupPattern(pat []PatternElem, n, limit int) []PatternElem {
	if len(pat)%n != 0 {
		return pat
	}
	var out []PatternElem
	for i := 0; i < len(pat); i += n {
		g := pat[i : i+n]
		switch {
		case allKind(g, PatternError):
			out = append(out, PatternElem{Kind: PatternError})
		case allKind(g, PatternData):
			v, m := 0, 0
			for _, e := range g {
				if e.Value < 0 || e.Value > 0xff || e.Mask < 0 || e.Mask > 0xff {
					return pat
				}
				v, m = v<<8|e.Value, m<<8|e.Mask
			}
			if v&^limit != 0 {
				return pat
			}
			out = append(out, PatternElem{Kind: PatternData, Value: v, Mask: m & limit})
		default:
			return pat
		}
	}
	return out
}

// allKind reports whether every element of g is of kind k.
// TRLC-LINKS: REQ-SDS-013
func allKind(g []PatternElem, k int) bool {
	for _, e := range g {
		if e.Kind != k {
			return false
		}
	}
	return true
}

// joinGroups is bytes as big-endian values of n bytes each (joinBytes of
// each group); bytes unchanged when a group does not join.
// TRLC-LINKS: REQ-SDS-013
func joinGroups(bytes []int, n, limit int) []int {
	var out []int
	for i := 0; i+n <= len(bytes); i += n {
		v := joinBytes(bytes[i:i+n], limit)
		if len(v) != 1 {
			return bytes
		}
		out = append(out, v[0])
	}
	return out
}

// joinBytes is bytes as one big-endian value when every one is a byte and the
// value fits limit; bytes unchanged otherwise.
// TRLC-LINKS: REQ-SDS-013
func joinBytes(bytes []int, limit int) []int {
	v := 0
	for _, b := range bytes {
		if b < 0 || b > 0xff {
			return bytes
		}
		v = v<<8 | b
	}
	if v > limit {
		return bytes
	}
	return []int{v}
}

// FrameStartLeadUnits is the record a frame-start match keeps before the
// FPGA's trigger until one has been placed (and how far it moved is known):
// the frame's opening and the units the image fires after it - a SENT
// frame's SYNC and first nibble (the raw record at 200us/div held 0.2 ms
// before the trigger, the SYNC began right at its start, and no frame start
// was found); the line image fires on a Manchester frame's fourth word.
const FrameStartLeadUnits = 4

// FrameUnitsBefore and frameUnitsAfter bound the record a frame-start match
// is placed on, in units around the FPGA's trigger: the frame's opening (a
// CAN identifier and control field, two units and a half) before it, the
// rest of the longest frame (FlexRay's 254 bytes) after it.
const (
	FrameUnitsBefore = 8
	FrameUnitsAfter  = 264
)

// FrameUnitStart is where a frame-start match (a frame's start, then any
// unit) began: the matcher fires as the frame's first unit ends, so the
// decoded unit ending nearest the FPGA's trigger at, and the frame's opening
// right before it (its start of a decoder that marks none: SPI's idle gap).
// -1 when no unit ends near it.
// TRLC-LINKS: REQ-SDS-013, REQ-SDS-223
func FrameUnitStart(a, b []uint8, sampleS float64, p SerialParams, at, lo int) float64 {
	d, ok := LookupSerialDecoder(p.Proto)
	if !ok {
		return -1
	}
	if at := frameOpening(d.Decode(a, b, sampleS, p).Spans, at); at >= 0 {
		return float64(lo + at)
	}
	return -1
}

// frameOpening is frameUnitStart on a decode's spans: the start of the frame
// whose first unit ends nearest at, -1 for none.
// TRLC-LINKS: REQ-SDS-013, REQ-SDS-223
func frameOpening(spans []decode.Span, at int) int {
	best, bestD := -1, math.MaxInt
	for k, sp := range spans {
		if sp.Kind != SpanData || sp.I0 > at {
			continue
		}
		dd := at - sp.I1
		if dd < 0 {
			dd = -dd
		}
		if dd < bestD && dd <= max(1, sp.I1-sp.I0) {
			best, bestD = k, dd
		}
	}
	if best < 0 {
		return -1
	}
	// the frame opens before its first unit: its SYNC, SOF or START when the
	// decoder marks one there, past the frame's own fields (a CAN identifier)
	// and its units before the one the FPGA fired on - the line and packet
	// images fire a few units into the frame (a Manchester or USB frame's
	// fourth), as they match buffered units. Units back to back belong to
	// one frame; a unit with no opening marked before it opens itself.
	first := spans[best]
	prev := first
	for k := best - 1; k >= 0; k-- {
		switch sp := spans[k]; sp.Kind {
		case spanStart, SpanSOF, spanSync:
			return sp.I0
		case SpanData:
			if prev.I0-sp.I1 > max(1, prev.I1-prev.I0) {
				return first.I0
			}
			prev = sp
		case SpanGap, spanStop, SpanFrameError, SpanParityError, "crc":
			return first.I0
		}
	}
	return first.I0
}

// SerialUnitSamples is one decoded unit's length in samples (a UART
// character, eight SPI clocks, a Manchester word, a MIL-1553 word, an ARINC
// 429 word with its gap, a CAN, FlexRay or USB byte with its stuffing or
// framing, SENT's longest nibble), 0 when it is not known (I2C's clock): a
// match is then sought over the whole capture.
// TRLC-LINKS: REQ-SDS-013
func SerialUnitSamples(p SerialParams, sampleS, spiHz float64) int {
	if !(sampleS > 0) {
		return 0
	}
	bits := 0.0 // a unit's bits at the request's rate
	switch p.Proto {
	case SerUART:
		bits = 10 // start, 8 data, stop
		if p.Bits > 0 {
			bits = float64(p.Bits) + 2
		}
		if p.Parity != "" && p.Parity != ParityNone {
			bits++
		}
	case SerSPI:
		hz := float64(p.SPIClockHz)
		if !(hz > 0) {
			hz = spiHz
		}
		if hz > 0 {
			return int(math.Ceil(8 / hz / sampleS))
		}
		return 0
	case SerManchester:
		bits = 8
		if p.Bits > 0 {
			bits = float64(p.Bits)
		}
	case SerMIL1553:
		bits = 20 // sync, 16 data, parity
	case SerARINC:
		bits = 36 // 32 and the 4-bit gap
	case SerCAN, SerFlexRay, SerUSB:
		bits = 10
	case SerLIN, SerMIDI:
		bits = 10
	case SerDMX, SerModbus:
		bits = 11
	case SerOneWire, SerWS281x, SerDHT, SerJ1850, SerIRNEC, SerIRRC6, SerDALI, SerMDIO:
		bits = 8 // a byte (RC-6 and DALI, MDIO: their bit times at Baud)
	case SerIRSIRC, SerIRRC5:
		bits = 8 // the longest field: SIRC's address, RC-5's command
	case SerCEC:
		bits = 10 // a block
	case SerUSBPD:
		bits = 10 // a byte: two 4b5b symbols
	case SerSPDIF:
		bits = 32 // a subframe
	case SerPS2:
		bits = 11
	case SerSWD:
		bits = 46 // request, turnaround, ACK and a data word
	case SerWiegand:
		bits = 16
	case SerQuad, SerStepper, SerPWM:
		bits = 1 // a step or a period (Baud their rate)
	case SerRCEncode, SerMiller, SerEM4100, SerSDQ, SerDSI, SerSDCard, SerMaple:
		bits = 8 // a trit's four pulses of two shorts, or a byte
	case SerOOK:
		bits = 4
	case SerMorse:
		bits = 20 // a letter's units
	case SerDCF77:
		bits = 8 // a field's seconds
	case SerEM4305:
		bits = 9 * 32 // a byte and its parity, in field clocks (Baud the field's)
	case SerT55xx:
		bits = 8 * 56
	case SerSWIM, SerQi:
		bits = 11
	case SerPJDL, SerPDI:
		bits = 12
	case SerCJTAG:
		bits = 3 * 32 // a 32-bit scan, three TCKC cycles a TCK
	case SerCaliper:
		bits = 24
	case SerIRMP:
		bits = 16 * 3 // a 16-bit field of pulses and pauses (Baud: 1/pulse)
	case SerSENT:
		if p.TickNs > 0 {
			return int(math.Ceil(27 * p.TickNs * 1e-9 / sampleS))
		}
		return 0
	}
	if !(bits > 0) || p.Baud <= 0 {
		return 0
	}
	return int(math.Ceil(bits / float64(p.Baud) / sampleS))
}
