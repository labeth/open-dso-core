// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Generators of the IR multi decoder's protocols (proto_gen_ir_test.go's
// IRLine): pulse-distance frames of each protocol's timing, bits in the
// order sent.

// IRPulseDistance appends a pulse-distance frame: a leader (when lead > 0), the bits,
// each a pulse of p and a pause of zero or one seconds, a stop pulse, then
// rest seconds of space.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRPulseDistance(l IRLine, lead, leadSpace float64, bits []int, p, zero, one, rest float64) {
	if lead > 0 {
		l.Mark(lead)
		l.Space(leadSpace)
	}
	for _, b := range bits {
		l.Mark(p)
		if b == 1 {
			l.Space(one)
		} else {
			l.Space(zero)
		}
	}
	l.Mark(p)
	l.Space(rest)
}

// LSBBits is n bits of v, least significant first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func LSBBits(v, n int) []int {
	b := make([]int, n)
	for i := range b {
		b[i] = v >> i & 1
	}
	return b
}

// MSBBits is n bits of v, most significant first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func MSBBits(v, n int) []int {
	b := make([]int, n)
	for i := range b {
		b[i] = v >> (n - 1 - i) & 1
	}
	return b
}

// IRSamsung32 appends a Samsung32 frame: 4.5 ms leader and space, a 16-bit
// address and 16-bit command least significant first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRSamsung32(l IRLine, addr, cmd int) {
	IRPulseDistance(l, 4.5e-3, 4.5e-3, append(LSBBits(addr, 16), LSBBits(cmd, 16)...), 550e-6, 550e-6, 1650e-6, 50e-3)
}

// IRJVC appends a JVC frame (NEC timing, 16 bits: a 4-bit address and a
// 12-bit command, least significant first), its leader unless repeat.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRJVC(l IRLine, addr, cmd int, repeat bool) {
	lead := 9e-3
	if repeat {
		lead = 0
	}
	IRPulseDistance(l, lead, 4.5e-3, append(LSBBits(addr, 4), LSBBits(cmd, 12)...), 560e-6, 560e-6, 1690e-6, 25e-3)
}

// IRNEC16 appends an NEC16 frame: the NEC leader, an 8-bit address, a
// sync (a pulse and a 4.5 ms pause), an 8-bit command.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRNEC16(l IRLine, addr, cmd int) {
	l.Mark(9e-3)
	l.Space(4.5e-3)
	bit := func(b int) {
		l.Mark(560e-6)
		if b == 1 {
			l.Space(1690e-6)
		} else {
			l.Space(560e-6)
		}
	}
	for _, b := range LSBBits(addr, 8) {
		bit(b)
	}
	l.Mark(560e-6)
	l.Space(4.5e-3)
	for _, b := range LSBBits(cmd, 8) {
		bit(b)
	}
	l.Mark(560e-6)
	l.Space(50e-3)
}

// IRNEC42 appends an NEC42 frame: 13-bit address and its inverse, 8-bit
// command and its inverse, least significant first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRNEC42(l IRLine, addr, cmd int) {
	b := append(LSBBits(addr, 13), LSBBits(^addr&0x1fff, 13)...)
	b = append(append(b, LSBBits(cmd, 8)...), LSBBits(^cmd&0xff, 8)...)
	IRPulseDistance(l, 9e-3, 4.5e-3, b, 560e-6, 560e-6, 1690e-6, 50e-3)
}

// IRKaseikyo appends a Kaseikyo (Panasonic) frame: 16-bit manufacturer
// code, its 4-bit parity (the xor of its nibbles), genre 1 and 2, a 10-bit
// command and 2-bit id, and the xor of bytes 2 to 4; least significant
// first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRKaseikyo(l IRLine, maker, genre1, genre2, cmd, id int) {
	par := (maker ^ maker>>4 ^ maker>>8 ^ maker>>12) & 15
	var bytes [6]int
	bytes[0], bytes[1] = maker&0xff, maker>>8
	bytes[2] = par | genre1<<4
	bytes[3] = genre2 | (cmd&15)<<4
	bytes[4] = cmd>>4&0x3f | id<<6
	bytes[5] = bytes[2] ^ bytes[3] ^ bytes[4]
	var b []int
	for _, v := range bytes {
		b = append(b, LSBBits(v, 8)...)
	}
	IRPulseDistance(l, 3380e-6, 1690e-6, b, 423e-6, 423e-6, 1269e-6, 74e-3)
}

// IRDenon appends a Denon (Sharp) frame pair: 5-bit address and 8 command
// bits and 2 extension bits, most significant first, no leader; 45 ms on
// the same frame with the command and extension inverted.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRDenon(l IRLine, addr, cmd int) {
	b := append(MSBBits(addr, 5), MSBBits(cmd, 10)...)
	IRPulseDistance(l, 0, 0, b, 310e-6, 745e-6, 1780e-6, 45e-3)
	b = append(MSBBits(addr, 5), MSBBits(^cmd&0x3ff, 10)...)
	IRPulseDistance(l, 0, 0, b, 310e-6, 745e-6, 1780e-6, 60e-3)
}

// IRMatsushita appends a Matsushita frame: 3.5 ms leader and space, a
// 6-bit custom code, 6-bit command and 12-bit address... as IRMP reads it:
// the command's 12 bits then the address's, least significant first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRMatsushita(l IRLine, cmd, addr int) {
	IRPulseDistance(l, 3488e-6, 3488e-6, append(LSBBits(cmd, 12), LSBBits(addr, 12)...), 872e-6, 872e-6, 2616e-6, 50e-3)
}

// IRSamsung37 appends a Samsung frame: 4.5 ms leader and space, a 16-bit
// address, a sync (a pulse and a 4.5 ms space), a 4-bit id and the 8-bit
// command and its inverse, least significant first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-255
func IRSamsung37(l IRLine, addr, id, cmd int) {
	l.Mark(4.5e-3)
	l.Space(4.5e-3)
	bit := func(b int) {
		l.Mark(550e-6)
		if b == 1 {
			l.Space(1650e-6)
		} else {
			l.Space(550e-6)
		}
	}
	for _, b := range LSBBits(addr, 16) {
		bit(b)
	}
	l.Mark(550e-6)
	l.Space(4.5e-3)
	for _, b := range append(append(LSBBits(id, 4), LSBBits(cmd, 8)...), LSBBits(^cmd&0xff, 8)...) {
		bit(b)
	}
	l.Mark(550e-6)
	l.Space(50e-3)
}
