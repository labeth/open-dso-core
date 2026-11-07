// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

import "slices"

// Generators of the first buses (CAN and CAN FD, USB, FlexRay, SENT,
// MIL-STD-1553, ARINC 429 and plain Manchester): each frame's bits as the
// wire carries them, and the Line renderers that lay them out in time.

// BitsMSB is n bits of v, most significant first.
// TRLC-LINKS: REQ-DSO-018
func BitsMSB(v, n int) []int {
	out := make([]int, n)
	for k := range n {
		out[k] = (v >> (n - 1 - k)) & 1
	}
	return out
}

// CANCRC15 is classic CAN's CRC-15 (x^15+x^14+x^10+x^8+x^7+x^4+x^3+1, from
// 0) over the unstuffed bits from SOF through the data field.
// TRLC-LINKS: REQ-DSO-018
func CANCRC15(bits []int) int {
	crc := 0
	for _, b := range bits {
		next := ((crc >> 14) & 1) ^ (b & 1)
		crc = (crc << 1) & 0x7fff
		if next != 0 {
			crc ^= 0x4599
		}
	}
	return crc & 0x7fff
}

// CANStuff applies CAN bit stuffing (an opposite bit after five equal ones)
// and reports the last run, so a caller can add the stuff bit a CRC ending
// on a run of five needs before its (unstuffed) delimiter.
// TRLC-LINKS: REQ-DSO-018
func CANStuff(bits []int) (out []int, runVal, runLen int) {
	runVal, runLen = -1, 0
	for _, b := range bits {
		if runLen == 5 {
			sb := 1 - runVal
			out = append(out, sb)
			runVal, runLen = sb, 1
		}
		out = append(out, b)
		if b == runVal {
			runLen++
		} else {
			runVal, runLen = b, 1
		}
	}
	return out, runVal, runLen
}

// CANFrame is one classic CAN frame.
type CANFrame struct {
	ID     int // 11-bit identifier (29-bit when Ext)
	DLC    int
	Data   []int // not on the wire for a remote frame
	RTR    bool
	Ext    bool
	NACK   bool // no node acknowledges: the ACK slot stays recessive
	CRCXor int  // xored into the CRC sent (0: a clean frame)
}

// CANWire is a classic frame's bits on the wire (0 dominant, 1 recessive):
// stuffed SOF to CRC, the CRC delimiter, the ACK slot and delimiter, and the
// least gap (end of frame and intermission), so frames written one after
// another are back to back; the stuff bits inserted and the CRC sent.
// TRLC-LINKS: REQ-DSO-018
func CANWire(f CANFrame) (wire []int, nStuff, txCRC int) {
	rtrBit := 0
	if f.RTR {
		rtrBit = 1
	}
	bits := []int{0} // SOF (dominant)
	if !f.Ext {
		bits = append(bits, BitsMSB(f.ID, 11)...)
		bits = append(bits, rtrBit, 0, 0) // RTR, IDE (dominant: standard), r0
	} else {
		bits = append(bits, BitsMSB((f.ID>>18)&0x7ff, 11)...)
		bits = append(bits, 1, 1) // SRR, IDE (recessive: extended)
		bits = append(bits, BitsMSB(f.ID&0x3ffff, 18)...)
		bits = append(bits, rtrBit, 0, 0) // RTR, r1, r0
	}
	bits = append(bits, BitsMSB(f.DLC, 4)...)
	if !f.RTR {
		for _, d := range f.Data {
			bits = append(bits, BitsMSB(d, 8)...)
		}
	}
	txCRC = CANCRC15(bits) ^ f.CRCXor
	stuffIn := append(append([]int{}, bits...), BitsMSB(txCRC, 15)...)
	stuffed, rv, rl := CANStuff(stuffIn)
	if rl == 5 {
		stuffed = append(stuffed, 1-rv) // trailing stuff bit before the CRC delimiter
	}
	nStuff = len(stuffed) - len(stuffIn)
	ackBit := 0 // another node drives the ACK slot dominant
	if f.NACK {
		ackBit = 1
	}
	wire = slices.Concat(stuffed, []int{1, ackBit, 1}) // CRC delimiter, ACK slot, ACK delimiter
	for range 7 + 3 {
		wire = append(wire, 1) // end of frame (7) and intermission (3)
	}
	return wire, nStuff, txCRC
}

// CANFDFrame is a CAN FD base-format frame's bits through its data field,
// stuffed, and a recessive tail where the stuff count, CRC and
// acknowledgement would follow (no bit-rate switch).
// TRLC-LINKS: REQ-DSO-018
func CANFDFrame(id, dlc int, data []int) []int {
	var bits []int
	bits = append(bits, 0)                  // SOF
	bits = append(bits, BitsMSB(id, 11)...) // ID
	bits = append(bits, 0)                  // RRS (dominant)
	bits = append(bits, 0)                  // IDE (dominant, base format)
	bits = append(bits, 1)                  // FDF (recessive: FD)
	bits = append(bits, 0)                  // res
	bits = append(bits, 0)                  // BRS (no rate switch)
	bits = append(bits, 0)                  // ESI
	bits = append(bits, BitsMSB(dlc, 4)...) // DLC
	for _, d := range data {
		bits = append(bits, BitsMSB(d, 8)...)
	}
	stuffed, _, _ := CANStuff(bits)
	wire := append([]int{}, stuffed...)
	for range 24 {
		wire = append(wire, 1)
	}
	return wire
}

// WireBits writes wire bits at baud on w (a CAN RX line: recessive 1
// high), lead bit times of idle before and trail after.
// TRLC-LINKS: REQ-DSO-018
func WireBits(w *Line, baud, lead, trail float64, wires ...[]int) {
	bt := 1 / baud
	w.Add(1, lead*bt)
	for _, wire := range wires {
		for _, b := range wire {
			w.Add(lvl(b), bt)
		}
	}
	w.Add(1, trail*bt)
}

// USB low and full speed.

// USBLowSpeed and USBFullSpeed are the bit rates.
const (
	USBLowSpeed  = 1.5e6
	USBFullSpeed = 12e6
)

// LSBFirst is n bits of v, least significant first (USB's field order).
// TRLC-LINKS: REQ-DSO-018
func LSBFirst(v, n int) []int {
	out := make([]int, n)
	for i := range n {
		out[i] = (v >> i) & 1
	}
	return out
}

// reverseBits reverses the low count bits of num.
func reverseBits(num, count int) int {
	out := 0
	for i := range count {
		if num>>i&1 == 1 {
			out |= 1 << (count - 1 - i)
		}
	}
	return out
}

// USBCRC5 is a token's CRC-5 over its bits in sending order, as its field
// reads least significant bit first.
// TRLC-LINKS: REQ-DSO-018
func USBCRC5(bits []int) int {
	crc := 0x1F
	for _, b := range bits {
		crc <<= 1
		if b != crc>>5 {
			crc ^= 0x25
		}
		crc &= 0x1F
	}
	return reverseBits(crc^0x1F, 5)
}

// USBCRC16 is a data packet's CRC-16 over its bits in sending order.
// TRLC-LINKS: REQ-DSO-018
func USBCRC16(bits []int) int {
	crc := 0xFFFF
	for _, b := range bits {
		crc <<= 1
		if b != crc>>16 {
			crc ^= 0x18005
		}
		crc &= 0xFFFF
	}
	return reverseBits(crc^0xFFFF, 16)
}

// USBByteBits is bytes as the wire sends them, each least significant bit
// first.
// TRLC-LINKS: REQ-DSO-018
func USBByteBits(data []int) []int {
	var bits []int
	for _, d := range data {
		bits = append(bits, LSBFirst(d, 8)...)
	}
	return bits
}

// USBTokenBits are a token's bits after its PID: address, endpoint, CRC-5.
// TRLC-LINKS: REQ-DSO-018
func USBTokenBits(addr, ep int) []int {
	bits := append(LSBFirst(addr, 7), LSBFirst(ep, 4)...)
	return append(bits, LSBFirst(USBCRC5(bits), 5)...)
}

// USBDataBits are a data packet's bits after its PID: the bytes and their
// CRC-16 (crcXor spoils the CRC sent).
// TRLC-LINKS: REQ-DSO-018
func USBDataBits(data []int, crcXor int) []int {
	bits := USBByteBits(data)
	return append(bits, LSBFirst(USBCRC16(bits)^crcXor, 16)...)
}

// USBPacketBits is a packet's bits, stuffed: SYNC, the PID (and its
// complement) and the payload bits.
// TRLC-LINKS: REQ-DSO-018
func USBPacketBits(pid int, payload []int) []int {
	return USBPacketBitsRawPID((pid&0xF)|((^pid&0xF)<<4), payload)
}

// USBPacketBitsRawPID is USBPacketBits with the whole PID byte given (a
// spoiled complement can be sent). A 0 follows six 1s, counted from the
// SYNC's last bit on.
// TRLC-LINKS: REQ-DSO-018
func USBPacketBitsRawPID(pidByte int, payload []int) []int {
	bits := []int{0, 0, 0, 0, 0, 0, 0, 1} // SYNC
	bits = append(bits, LSBFirst(pidByte&0xFF, 8)...)
	bits = append(bits, payload...)
	var out []int
	ones := 0
	for _, b := range bits {
		out = append(out, b)
		if b == 1 {
			if ones++; ones == 6 {
				out = append(out, 0)
				ones = 0
			}
		} else {
			ones = 0
		}
	}
	return out
}

// USBWave writes packets on the pair (A D+, B D-) at rate: NRZI from the
// idle J (a 0 toggles J and K, a 1 holds), each packet's EOP (two bit times
// of SE0, one of J) and gapBits of idle; kaGapBits > 0 puts nKA keep-alives
// (bare EOPs) that far apart first. Low speed's J is D- high, full speed's
// D+ high.
// TRLC-LINKS: REQ-DSO-018
func USBWave(p Pair, rate float64, nKA int, kaGapBits float64, packets [][]int, gapBits float64) {
	bt := 1.0 / rate
	jHigh := byte(0) // low speed: J is D- high
	if rate > USBLowSpeed {
		jHigh = 1
	}
	sym := func(dp, dm byte, cells float64) { p.Add(dp, dm, cells*bt) }
	stJ, stK := 1, 0
	emit := func(st int, cells float64) {
		if st == stJ {
			sym(jHigh, 1-jHigh, cells)
		} else {
			sym(1-jHigh, jHigh, cells)
		}
	}
	emit(stJ, 20) // lead idle
	for range nKA {
		sym(0, 0, 2)
		emit(stJ, 1+kaGapBits)
	}
	for _, bits := range packets {
		st := stJ
		for _, b := range bits {
			if b == 0 {
				st = stJ + stK - st
			}
			emit(st, 1)
		}
		sym(0, 0, 2)         // EOP: SE0
		emit(stJ, 1+gapBits) // its closing J, then idle
	}
	emit(stJ, 20) // trail idle
}

// FlexRay.

// FlexRayHeaderCRC is the 11-bit header CRC (x^11+x^9+x^8+x^7+x^2+1, from
// 0x1A) over the sync and startup bits, the frame ID and the payload length.
// TRLC-LINKS: REQ-DSO-018
func FlexRayHeaderCRC(sync, startup, frameID, payloadLen int) int {
	bits := make([]int, 0, 20)
	bits = append(bits, sync&1, startup&1)
	for b := 10; b >= 0; b-- {
		bits = append(bits, (frameID>>b)&1)
	}
	for b := 6; b >= 0; b-- {
		bits = append(bits, (payloadLen>>b)&1)
	}
	crc := 0x1A
	for _, bit := range bits {
		msb := (crc >> 10) & 1
		crc = (crc << 1) & 0x7FF
		if (msb ^ bit) == 1 {
			crc ^= 0x385
		}
	}
	return crc & 0x7FF
}

// FlexRayHeader packs the 40-bit header (indicators, frame ID, payload
// length in words, header CRC, cycle; most significant first) into 5 bytes.
// TRLC-LINKS: REQ-DSO-018
func FlexRayHeader(sync, startup, frameID, payloadLen, crc, cycle int) []int {
	var h uint64
	h |= uint64(sync&1) << 36
	h |= uint64(startup&1) << 35
	h |= uint64(frameID&0x7FF) << 24
	h |= uint64(payloadLen&0x7F) << 17
	h |= uint64(crc&0x7FF) << 6
	h |= uint64(cycle & 0x3F)
	return []int{
		int((h >> 32) & 0xFF), int((h >> 24) & 0xFF), int((h >> 16) & 0xFF),
		int((h >> 8) & 0xFF), int(h & 0xFF),
	}
}

// FlexRayCRC24 is the frame CRC (0x5D6DCB from 0xFEDCBA, channel A) over the
// header and payload.
// TRLC-LINKS: REQ-DSO-018
func FlexRayCRC24(bytes []int) int {
	crc := 0xFEDCBA
	for _, b := range bytes {
		for d := 7; d >= 0; d-- {
			bit := ((crc >> 23) & 1) ^ ((b >> d) & 1)
			crc = (crc << 1) & 0xFFFFFF
			if bit == 1 {
				crc ^= 0x5D6DCB
			}
		}
	}
	return crc
}

// FlexRayFrameFlags is a data frame's bytes on the wire: the header (null
// frame indicator 1, the sync and startup bits given), the payload (an even
// number of bytes) and the frame CRC.
// TRLC-LINKS: REQ-DSO-018
func FlexRayFrameFlags(sync, startup, frameID, cycle int, payload []int) []int {
	plen := len(payload) / 2
	hdr := FlexRayHeader(sync, startup, frameID, plen, FlexRayHeaderCRC(sync, startup, frameID, plen), cycle)
	hdr[0] |= 0x20 // a data frame, not a null frame (outside the header CRC)
	body := slices.Concat(hdr, payload)
	c := FlexRayCRC24(body)
	return append(body, (c>>16)&0xFF, (c>>8)&0xFF, c&0xFF)
}

// FlexRayFrame is a static-segment data frame (no sync, no startup).
// TRLC-LINKS: REQ-DSO-018
func FlexRayFrame(frameID, cycle int, payload []int) []int {
	return FlexRayFrameFlags(0, 0, frameID, cycle, payload)
}

// FlexRayLine writes frames on the bus line (idle high) at bitrate: TSS
// (tssBits low), FSS, per byte BSS and eight bits most significant first,
// FES, then idleBits; eight bit times of idle before and after.
// TRLC-LINKS: REQ-DSO-018
func FlexRayLine(w *Line, bitrate float64, frames [][]int, tssBits, idleBits float64) {
	bt := 1 / bitrate
	w.Add(1, 8*bt)
	for _, fb := range frames {
		w.Add(0, tssBits*bt) // TSS
		w.Add(1, bt)         // FSS
		for _, b := range fb {
			w.Add(1, bt) // BSS
			w.Add(0, bt)
			for d := 7; d >= 0; d-- {
				w.Add(lvl(b>>d)&1, bt)
			}
		}
		w.Add(0, bt) // FES
		w.Add(1, bt)
		w.Add(1, idleBits*bt)
	}
	w.Add(1, 8*bt)
}

// SENT.

// sentCRC4Table is SAE J2716's CRC-4 nibble table (x^4+x^3+x^2+1).
var sentCRC4Table = [16]int{0, 13, 7, 10, 14, 3, 9, 4, 1, 12, 6, 11, 15, 2, 8, 5}

// SENTCRC4 is the J2716 CRC-4 of the data nibbles (seed 5, augmented).
// TRLC-LINKS: REQ-DSO-018
func SENTCRC4(data []int) int {
	crc := 5
	for _, d := range data {
		crc = sentCRC4Table[crc] ^ (d & 0xf)
	}
	crc = sentCRC4Table[crc]
	return crc & 0xf
}

// SENTFrames writes SENT frames at a tick of tick seconds on an idle-high
// line: each frame's 56-tick synchronization pulse and its nibbles (a pulse
// of 12+v ticks, the first five low), then pauseTicks of pause pulse when
// that is set; a last low pulse closes the last nibble.
// TRLC-LINKS: REQ-DSO-018
func SENTFrames(w *Line, tick float64, frames [][]int, pauseTicks int) {
	pulse := func(ticks int) {
		w.Add(0, 5*tick)
		w.Add(1, float64(ticks-5)*tick)
	}
	w.Add(1, 8*tick)
	for _, nibs := range frames {
		pulse(56)
		for _, v := range nibs {
			pulse(12 + v)
		}
		if pauseTicks > 0 {
			pulse(pauseTicks)
		}
	}
	w.Add(0, 5*tick)
	w.Add(1, 8*tick)
}

// SENTNibbles is a fast-channel frame's nibbles: the status, the data and
// their CRC.
// TRLC-LINKS: REQ-DSO-018
func SENTNibbles(status int, data []int) []int {
	n := append([]int{status & 0xf}, data...)
	return append(n, SENTCRC4(data))
}

// MIL-STD-1553 and Manchester.

// MIL1553Words writes 1553 words at 1 Mbit/s (or bit seconds a bit) on w:
// each word's sync (command/status: high 1.5 bits then low 1.5; data: the
// inverse), 16 bits most significant first and odd parity, Manchester II
// (a 1 high then low); the words back to back, idle low around them.
// TRLC-LINKS: REQ-DSO-018
func MIL1553Words(w *Line, bit float64, words []int, cmd []bool) {
	half := bit / 2
	put := func(b int) {
		w.Add(lvl(b), half)
		w.Add(lvl(1-b), half)
	}
	w.Add(0, 4*bit)
	for i, wd := range words {
		first := byte(1)
		if !cmd[i] {
			first = 0
		}
		w.Add(first, 1.5*bit)
		w.Add(1-first, 1.5*bit)
		ones := 0
		for b := 15; b >= 0; b-- {
			v := (wd >> b) & 1
			ones += v
			put(v)
		}
		put(1 - ones&1)
	}
	w.Add(0, 6*bit)
}

// ManchesterBits writes bits at bit seconds each, IEEE 802.3's convention
// (a 1 rises mid-bit) or G.E. Thomas's (a 1 falls), idle seconds of idle
// level around them.
// TRLC-LINKS: REQ-DSO-018
func ManchesterBits(w *Line, bits []int, bit float64, ieee bool, idleLevel byte, idle float64) {
	w.Add(idleLevel, idle)
	for _, b := range bits {
		first, second := lvl(1-b), lvl(b)
		if !ieee {
			first, second = lvl(b), lvl(1-b)
		}
		w.Add(first, bit/2)
		w.Add(second, bit/2)
	}
	w.Add(idleLevel, idle)
}

// ARINC 429: a three-level line.

// ARINC 429's levels on a Line (Levels 3).
const (
	ARINCLo   = 0 // low (-10 V)
	ARINCNull = 1 // null (0 V)
	ARINCHi   = 2 // high (+10 V)
)

// ARINCWord is a 32-bit word's bits in sending order: the label (bit 0
// first: the label's most significant), SDI, 19 data bits, SSM and odd
// parity.
// TRLC-LINKS: REQ-DSO-018
func ARINCWord(label8, sdi2, data19, ssm2 int) []int {
	bits := make([]int, 32)
	for i := range 8 {
		bits[i] = (label8 >> i) & 1
	}
	bits[8] = sdi2 & 1
	bits[9] = (sdi2 >> 1) & 1
	for i := range 19 {
		bits[10+i] = (data19 >> i) & 1
	}
	bits[29] = ssm2 & 1
	bits[30] = (ssm2 >> 1) & 1
	ones := 0
	for i := range 31 {
		ones += bits[i]
	}
	if ones%2 == 0 {
		bits[31] = 1
	}
	return bits
}

// ARINCWords writes words at bit seconds a bit, bipolar return to zero (a
// half bit high or low, then null), gapBits of null after each and before
// the first.
// TRLC-LINKS: REQ-DSO-018
func ARINCWords(w *Line, bit float64, words [][]int, gapBits float64) {
	w.Levels = 3
	w.Add(ARINCNull, gapBits*bit)
	for _, bits := range words {
		for _, b := range bits {
			lv := byte(ARINCLo)
			if b == 1 {
				lv = ARINCHi
			}
			w.Add(lv, bit/2)
			w.Add(ARINCNull, bit/2)
		}
		w.Add(ARINCNull, gapBits*bit)
	}
}
